package http

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	appErrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/tracing"
)

type authContextKey string

const contextKeyRole authContextKey = "role"

type jwksKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksResponse struct {
	Keys []jwksKey `json:"keys"`
}

type JWKSValidator struct {
	jwksURL    string
	httpClient *http.Client

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
	cacheTTL  time.Duration
}

func NewJWKSValidator(jwksURL string) *JWKSValidator {
	if jwksURL == "" {
		return nil
	}

	return &JWKSValidator{
		jwksURL:    jwksURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		keys:       make(map[string]*rsa.PublicKey),
		cacheTTL:   time.Hour,
	}
}

func (v *JWKSValidator) fetchKeys() error {
	resp, err := v.httpClient.Get(v.jwksURL)
	if err != nil {
		return fmt.Errorf("jwks fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks fetch: unexpected status %d", resp.StatusCode)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return fmt.Errorf("jwks decode: %w", err)
	}

	fresh := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, key := range jwks.Keys {
		if key.Kty != "RSA" || key.Use != "sig" || key.Kid == "" {
			continue
		}
		pub, err := rsaPublicKeyFromJWK(key)
		if err != nil {
			continue
		}
		fresh[key.Kid] = pub
	}

	v.mu.Lock()
	v.keys = fresh
	v.fetchedAt = time.Now()
	v.mu.Unlock()

	return nil
}

func (v *JWKSValidator) getKey(kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	key, ok := v.keys[kid]
	expired := time.Since(v.fetchedAt) > v.cacheTTL
	v.mu.RUnlock()

	if ok && !expired {
		return key, nil
	}

	refreshErr := v.fetchKeys()

	v.mu.RLock()
	key, ok = v.keys[kid]
	v.mu.RUnlock()

	if ok {
		return key, nil
	}
	if refreshErr != nil {
		return nil, fmt.Errorf("jwks refresh failed and kid %q not in cache: %w", kid, refreshErr)
	}

	return nil, fmt.Errorf("kid %q not found in JWKS", kid)
}

func rsaPublicKeyFromJWK(key jwksKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, fmt.Errorf("decode N: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return nil, fmt.Errorf("decode E: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := int(new(big.Int).SetBytes(eBytes).Int64())
	if e == 0 {
		return nil, fmt.Errorf("invalid exponent")
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}

func JWTAuth(validator *JWKSValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			traceID := tracing.GetTraceID(r.Context())

			if validator == nil {
				response.Error(w, appErrors.InternalError("jwt validation not configured", nil), traceID)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				response.Error(w, appErrors.Unauthorized("missing or invalid authorization header"), traceID)
				return
			}

			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				kid, _ := t.Header["kid"].(string)
				return validator.getKey(kid)
			})
			if err != nil || !token.Valid {
				response.Error(w, appErrors.Unauthorized("invalid or expired token"), traceID)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				response.Error(w, appErrors.Unauthorized("invalid token claims"), traceID)
				return
			}

			role, _ := claims["role"].(string)
			ctx := context.WithValue(r.Context(), contextKeyRole, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := tracing.GetTraceID(r.Context())
		role, _ := r.Context().Value(contextKeyRole).(string)
		if role != "admin" {
			response.Error(w, appErrors.Forbidden("admin role required"), traceID)
			return
		}
		next.ServeHTTP(w, r)
	})
}
