package fcm

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/model"
)

const firebaseMessagingScope = "https://www.googleapis.com/auth/firebase.messaging"

var ErrClientDisabled = errors.New("fcm client disabled")

// DeliveryError captures whether an FCM send failed permanently.
type DeliveryError struct {
	Code      string
	Message   string
	Permanent bool
}

func (e *DeliveryError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

type serviceAccount struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

type tokenCache struct {
	AccessToken string
	ExpiresAt   time.Time
}

// Client sends push notifications through FCM HTTP v1.
type Client struct {
	httpClient     *http.Client
	serviceAccount serviceAccount
	privateKey     *rsa.PrivateKey
	endpoint       string

	mu    sync.Mutex
	token tokenCache
}

// NewClientFromEnv loads FCM credentials from environment variables.
func NewClientFromEnv() (*Client, error) {
	rawJSON := os.Getenv("FCM_SERVICE_ACCOUNT_JSON")
	if rawJSON == "" {
		path := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
		if path != "" {
			bytes, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read GOOGLE_APPLICATION_CREDENTIALS: %w", err)
			}
			rawJSON = string(bytes)
		}
	}
	if rawJSON == "" {
		return nil, ErrClientDisabled
	}

	var account serviceAccount
	if err := json.Unmarshal([]byte(rawJSON), &account); err != nil {
		return nil, fmt.Errorf("parse FCM service account: %w", err)
	}
	if account.TokenURI == "" {
		account.TokenURI = "https://oauth2.googleapis.com/token"
	}
	if account.ProjectID == "" || account.ClientEmail == "" || account.PrivateKey == "" {
		return nil, fmt.Errorf("fcm service account is missing required fields")
	}

	key, err := parsePrivateKey(account.PrivateKey)
	if err != nil {
		return nil, err
	}

	endpoint := os.Getenv("FCM_ENDPOINT")
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", account.ProjectID)
	}

	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		serviceAccount: account,
		privateKey:     key,
		endpoint:       endpoint,
	}, nil
}

type sendRequest struct {
	Message sendMessage `json:"message"`
}

type sendMessage struct {
	Token        string                 `json:"token"`
	Notification sendNotification       `json:"notification"`
	Data         map[string]string      `json:"data,omitempty"`
	Webpush      map[string]interface{} `json:"webpush,omitempty"`
}

type sendNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Send dispatches a notification to a single FCM token.
func (c *Client) Send(ctx context.Context, token string, n *model.Notification) error {
	accessToken, err := c.accessToken(ctx)
	if err != nil {
		return err
	}

	body := sendRequest{
		Message: sendMessage{
			Token: token,
			Notification: sendNotification{
				Title: n.Title,
				Body:  n.Message,
			},
			Data: map[string]string{
				"notification_id": n.ID,
				"journey_id":      n.JourneyID,
				"event_type":      n.EventType,
				"type":            n.Type,
			},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal FCM request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("build FCM request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send FCM request: %w", err)
	}
	defer resp.Body.Close()

	var parsed map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&parsed)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	message := extractFCMMessage(parsed)
	code := extractFCMErrorCode(parsed)
	if code == "" {
		code = resp.Status
	}

	if isPermanentFCMError(code, resp.StatusCode) {
		return &DeliveryError{Code: code, Message: message, Permanent: true}
	}
	if isTransientStatus(resp.StatusCode) {
		return &DeliveryError{Code: code, Message: message, Permanent: false}
	}
	return fmt.Errorf("fcm send failed: %s", strings.TrimSpace(fmt.Sprintf("%s %s", code, message)))
}

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	cached := c.token
	c.mu.Unlock()

	if cached.AccessToken != "" && time.Until(cached.ExpiresAt) > time.Minute {
		return cached.AccessToken, nil
	}

	signedJWT, err := c.signedJWT()
	if err != nil {
		return "", err
	}

	values := url.Values{}
	values.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	values.Set("assertion", signedJWT)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.serviceAccount.TokenURI, strings.NewReader(values.Encode()))
	if err != nil {
		return "", fmt.Errorf("build oauth token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request oauth token: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode oauth token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("oauth token request failed: %s", tokenResp.Error)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("oauth token response missing access_token")
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	c.mu.Lock()
	c.token = tokenCache{AccessToken: tokenResp.AccessToken, ExpiresAt: expiresAt}
	c.mu.Unlock()

	return tokenResp.AccessToken, nil
}

func (c *Client) signedJWT() (string, error) {
	now := time.Now()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))

	claims, err := json.Marshal(map[string]interface{}{
		"iss":   c.serviceAccount.ClientEmail,
		"sub":   c.serviceAccount.ClientEmail,
		"aud":   c.serviceAccount.TokenURI,
		"scope": firebaseMessagingScope,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("marshal jwt claims: %w", err)
	}

	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	sum := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("sign jwt assertion: %w", err)
	}

	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func parsePrivateKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, fmt.Errorf("decode private key PEM")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA")
		}
		return rsaKey, nil
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return key, nil
}

func extractFCMMessage(body map[string]interface{}) string {
	errorBody, ok := body["error"].(map[string]interface{})
	if !ok {
		return ""
	}
	if message, ok := errorBody["message"].(string); ok {
		return message
	}
	return ""
}

func extractFCMErrorCode(body map[string]interface{}) string {
	errorBody, ok := body["error"].(map[string]interface{})
	if !ok {
		return ""
	}
	if status, ok := errorBody["status"].(string); ok && status != "" {
		return status
	}

	details, ok := errorBody["details"].([]interface{})
	if !ok {
		return ""
	}
	for _, detail := range details {
		item, ok := detail.(map[string]interface{})
		if !ok {
			continue
		}
		if code, ok := item["errorCode"].(string); ok && code != "" {
			return code
		}
	}
	return ""
}

func isPermanentFCMError(code string, statusCode int) bool {
	switch code {
	case "UNREGISTERED", "INVALID_ARGUMENT":
		return true
	}
	return statusCode == http.StatusBadRequest || statusCode == http.StatusNotFound
}

func isTransientStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode == http.StatusInternalServerError || statusCode == http.StatusServiceUnavailable
}
