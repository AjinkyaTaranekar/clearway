package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type genericEnvelope struct {
	Success *bool           `json:"success,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   *envelopeError  `json:"error,omitempty"`
	TraceID string          `json:"trace_id,omitempty"`
}

// HTTPResult captures one HTTP response and rich debug context.
type HTTPResult struct {
	RequestName   string
	Method        string
	URL           string
	StatusCode    int
	Duration      time.Duration
	TraceID       string
	UsedAuthToken bool
	RequestBody   []byte
	ResponseBody  []byte
	Headers       http.Header
}

func (r *HTTPResult) ToTrace() *HTTPTrace {
	if r == nil {
		return nil
	}
	return &HTTPTrace{
		RequestName:   r.RequestName,
		Method:        r.Method,
		URL:           r.URL,
		StatusCode:    r.StatusCode,
		DurationMS:    r.Duration.Milliseconds(),
		TraceID:       r.TraceID,
		UsedAuthToken: r.UsedAuthToken,
		RequestBody:   truncateForLog(r.RequestBody, 1200),
		ResponseBody:  truncateForLog(r.ResponseBody, 1600),
	}
}

func (r *HTTPResult) decodeJSON(out interface{}) error {
	if len(r.ResponseBody) == 0 {
		return fmt.Errorf("response body is empty")
	}
	if err := json.Unmarshal(r.ResponseBody, out); err != nil {
		return fmt.Errorf("failed to decode JSON body: %w", err)
	}
	return nil
}

func (r *HTTPResult) decodeEnvelopeData(out interface{}) error {
	var env genericEnvelope
	if err := r.decodeJSON(&env); err != nil {
		return err
	}
	if len(env.Data) == 0 {
		return fmt.Errorf("envelope has no data field")
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("failed to decode envelope data: %w", err)
	}
	return nil
}

func (r *HTTPResult) envelopeSuccess() (bool, bool) {
	var env genericEnvelope
	if err := json.Unmarshal(r.ResponseBody, &env); err != nil {
		return false, false
	}
	if env.Success == nil {
		return false, false
	}
	return *env.Success, true
}

func (r *HTTPResult) errorMessage() string {
	if len(r.ResponseBody) == 0 {
		return ""
	}

	var env genericEnvelope
	if err := json.Unmarshal(r.ResponseBody, &env); err == nil {
		if env.Error != nil && strings.TrimSpace(env.Error.Message) != "" {
			return env.Error.Message
		}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(r.ResponseBody, &raw); err == nil {
		if errValue, ok := raw["error"]; ok {
			switch typed := errValue.(type) {
			case string:
				if strings.TrimSpace(typed) != "" {
					return typed
				}
			case map[string]interface{}:
				if msg, ok := typed["message"].(string); ok && strings.TrimSpace(msg) != "" {
					return msg
				}
			}
		}
		if msg, ok := raw["message"].(string); ok && strings.TrimSpace(msg) != "" {
			return msg
		}
	}

	return string(r.ResponseBody)
}

// RequestSpec describes a single outbound API request.
type RequestSpec struct {
	Name    string
	Method  string
	Path    string
	Query   url.Values
	Body    interface{}
	Headers map[string]string
	UseAuth bool
}

// APIClient is a thin structured-logging HTTP client for this suite.
type APIClient struct {
	BaseURL     string
	HTTP        *http.Client
	Logger      *slog.Logger
	Scenario    string
	UserAlias   string
	BearerToken string
}

func newAPIClient(baseURL string, timeout time.Duration, logger *slog.Logger, scenario, userAlias string) *APIClient {
	return &APIClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP: &http.Client{
			Timeout: timeout,
		},
		Logger:    logger,
		Scenario:  scenario,
		UserAlias: userAlias,
	}
}

func (c *APIClient) setToken(token string) {
	c.BearerToken = strings.TrimSpace(token)
}

func (c *APIClient) do(ctx context.Context, spec RequestSpec) (*HTTPResult, error) {
	if c == nil {
		return nil, fmt.Errorf("api client is nil")
	}

	method := strings.ToUpper(strings.TrimSpace(spec.Method))
	if method == "" {
		method = http.MethodGet
	}
	path := spec.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	fullURL := c.BaseURL + path
	if spec.Query != nil && len(spec.Query) > 0 {
		fullURL += "?" + spec.Query.Encode()
	}

	var requestBody []byte
	if spec.Body != nil {
		encoded, err := json.Marshal(spec.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to encode request body: %w", err)
		}
		requestBody = encoded
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewReader(requestBody))
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	if len(requestBody) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range spec.Headers {
		req.Header.Set(key, value)
	}

	usedAuthToken := false
	if spec.UseAuth && c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
		usedAuthToken = true
	}

	requestID := newID("req")
	start := time.Now().UTC()
	c.Logger.Info("request_start",
		slog.String("scenario", c.Scenario),
		slog.String("user", c.UserAlias),
		slog.String("request_id", requestID),
		slog.String("name", spec.Name),
		slog.String("method", method),
		slog.String("url", fullURL),
		slog.Bool("auth_header", usedAuthToken),
		slog.String("request_body", truncateForLog(requestBody, 700)),
	)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		c.Logger.Error("request_transport_error",
			slog.String("scenario", c.Scenario),
			slog.String("user", c.UserAlias),
			slog.String("request_id", requestID),
			slog.String("name", spec.Name),
			slog.String("method", method),
			slog.String("url", fullURL),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("failed to read response body: %w", readErr)
	}

	traceID := strings.TrimSpace(resp.Header.Get("X-Trace-ID"))
	if traceID == "" {
		var env genericEnvelope
		if err := json.Unmarshal(body, &env); err == nil {
			traceID = strings.TrimSpace(env.TraceID)
		}
	}

	duration := time.Since(start)
	c.Logger.Info("request_end",
		slog.String("scenario", c.Scenario),
		slog.String("user", c.UserAlias),
		slog.String("request_id", requestID),
		slog.String("name", spec.Name),
		slog.Int("status_code", resp.StatusCode),
		slog.Int64("duration_ms", duration.Milliseconds()),
		slog.String("trace_id", traceID),
		slog.String("response_body", truncateForLog(body, 900)),
	)

	return &HTTPResult{
		RequestName:   spec.Name,
		Method:        method,
		URL:           fullURL,
		StatusCode:    resp.StatusCode,
		Duration:      duration,
		TraceID:       traceID,
		UsedAuthToken: usedAuthToken,
		RequestBody:   requestBody,
		ResponseBody:  body,
		Headers:       resp.Header,
	}, nil
}

func truncateForLog(raw []byte, max int) string {
	if len(raw) == 0 {
		return ""
	}
	asString := strings.TrimSpace(string(raw))
	if max <= 0 || len(asString) <= max {
		return asString
	}
	return asString[:max] + "...<truncated>"
}
