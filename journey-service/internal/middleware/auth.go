package middleware

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

	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/tracing"
)

// ---------------------------------------------------------------------------
// Context keys
// ---------------------------------------------------------------------------

type contextKey string

const (
	contextKeyDriverID contextKey = "driver_id"
	contextKeyRole     contextKey = "role"
)

// ---------------------------------------------------------------------------
// JWKS validator
// ---------------------------------------------------------------------------

// jwksKey is a single entry in a JWKS response.
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

// JWKSValidator fetches RSA public keys from the IAM service JWKS endpoint and
// caches them in memory. It is safe for concurrent use.
type JWKSValidator struct {
	jwksURL    string
	httpClient *http.Client

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey // kid → public key
	fetchedAt time.Time
	cacheTTL  time.Duration
}

// NewJWKSValidator creates a validator that will fetch keys from jwksURL.
// Example: NewJWKSValidator("http://iam-service:8082/.well-known/jwks.json")
func NewJWKSValidator(jwksURL string) *JWKSValidator {
	return &JWKSValidator{
		jwksURL:    jwksURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		keys:       make(map[string]*rsa.PublicKey),
		cacheTTL:   1 * time.Hour,
	}
}

// fetchKeys retrieves the current JWKS from the IAM service and replaces the
// in-memory cache. It must be called with no locks held.
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
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Use != "sig" || k.Kid == "" {
			continue
		}
		pub, err := rsaPublicKeyFromJWK(k)
		if err != nil {
			continue // skip malformed keys
		}
		fresh[k.Kid] = pub
	}

	v.mu.Lock()
	v.keys = fresh
	v.fetchedAt = time.Now()
	v.mu.Unlock()
	return nil
}

// getKey returns the RSA public key for the given kid.
// It refreshes the cache when it has expired or the kid is not found.
// If a refresh fails but a stale key exists, the stale key is returned so that
// a temporary IAM outage does not lock out all existing valid tokens.
func (v *JWKSValidator) getKey(kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	key, ok := v.keys[kid]
	expired := time.Since(v.fetchedAt) > v.cacheTTL
	v.mu.RUnlock()

	if ok && !expired {
		return key, nil
	}

	// Cache miss or TTL expired - refresh from IAM service.
	refreshErr := v.fetchKeys()

	v.mu.RLock()
	key, ok = v.keys[kid]
	v.mu.RUnlock()

	if ok {
		return key, nil // may be stale if refreshErr != nil; that is intentional
	}
	if refreshErr != nil {
		return nil, fmt.Errorf("jwks refresh failed and kid %q not in cache: %w", kid, refreshErr)
	}
	return nil, fmt.Errorf("kid %q not found in JWKS", kid)
}

// rsaPublicKeyFromJWK constructs an *rsa.PublicKey from a JWK entry.
// N and E are base64url-encoded (no padding), as per RFC 7518.
func rsaPublicKeyFromJWK(k jwksKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode N: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode E: %w", err)
	}
	n := new(big.Int).SetBytes(nBytes)
	e := int(new(big.Int).SetBytes(eBytes).Int64())
	if e == 0 {
		return nil, fmt.Errorf("invalid exponent (0)")
	}
	return &rsa.PublicKey{N: n, E: e}, nil
}

// ---------------------------------------------------------------------------
// JWT middleware
// ---------------------------------------------------------------------------

// JWTAuth returns an HTTP middleware that validates RS256 Bearer tokens.
// Keys are fetched lazily from the IAM JWKS endpoint and cached with a 1-hour TTL.
func JWTAuth(validator *JWKSValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			traceID := tracing.GetTraceID(r.Context())

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				response.Error(w, apperrors.Unauthorized("missing or invalid authorization header"), traceID)
				return
			}
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				// Reject any non-RSA signing method.
				if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				// Look up the public key by kid.
				kid, _ := t.Header["kid"].(string)
				return validator.getKey(kid)
			})

			if err != nil || !token.Valid {
				response.Error(w, apperrors.Unauthorized("invalid or expired token"), traceID)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				response.Error(w, apperrors.Unauthorized("invalid token claims"), traceID)
				return
			}

			driverID, _ := claims["sub"].(string)
			role, _ := claims["role"].(string)

			if driverID == "" {
				response.Error(w, apperrors.Unauthorized("missing subject claim"), traceID)
				return
			}

			ctx := context.WithValue(r.Context(), contextKeyDriverID, driverID)
			ctx = context.WithValue(ctx, contextKeyRole, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ---------------------------------------------------------------------------
// Role-based middleware
// ---------------------------------------------------------------------------

// AdminOnly permits only requests whose JWT role claim equals "admin".
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := tracing.GetTraceID(r.Context())
		if GetRole(r.Context()) != "admin" {
			response.Error(w, apperrors.Forbidden("admin role required"), traceID)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// EnforcementOnly permits only requests whose JWT role claim equals "enforcement"
// or "admin".
func EnforcementOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := tracing.GetTraceID(r.Context())
		role := GetRole(r.Context())
		if role != "enforcement" && role != "admin" {
			response.Error(w, apperrors.Forbidden("enforcement role required"), traceID)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// Context accessors
// ---------------------------------------------------------------------------

// GetDriverID extracts the authenticated user's ID from the request context.
func GetDriverID(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyDriverID).(string)
	return v
}

// GetRole extracts the authenticated user's role from the request context.
func GetRole(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyRole).(string)
	return v
}
