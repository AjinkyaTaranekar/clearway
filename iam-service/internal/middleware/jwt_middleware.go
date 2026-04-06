package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/service"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/tracing"
)

type contextKey string

const ClaimsKey contextKey = "claims"

func JWTMiddleware(jwks *service.JWKSService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			traceID := tracing.GetTraceID(r.Context())
			header := r.Header.Get("Authorization")
			if header == "" || !strings.HasPrefix(header, "Bearer ") {
				response.Error(w, apperrors.Unauthorized("Missing or malformed Authorization header."), traceID)
				return
			}
			rawToken := strings.TrimPrefix(header, "Bearer ")
			claims := &model.Claims{}
			_, err := jwt.ParseWithClaims(rawToken, claims, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
					return nil, apperrors.Unauthorized("Unexpected signing method.")
				}
				return &jwks.PrivateKey().PublicKey, nil
			})
			if err != nil || claims.Issuer != "traffic-iam" {
				response.Error(w, apperrors.Unauthorized("Invalid or expired token."), traceID)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ClaimsKey, claims)))
		})
	}
}

func RequireRole(roles ...model.Role) func(http.Handler) http.Handler {
	allowed := make(map[string]bool)
	for _, r := range roles {
		allowed[string(r)] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			traceID := tracing.GetTraceID(r.Context())
			claims, ok := r.Context().Value(ClaimsKey).(*model.Claims)
			if !ok || claims == nil {
				response.Error(w, apperrors.Unauthorized("Not authenticated."), traceID)
				return
			}
			if !allowed[claims.Role] {
				response.Error(w, apperrors.Forbidden("Insufficient permissions."), traceID)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func ClaimsFromContext(ctx context.Context) *model.Claims {
	c, _ := ctx.Value(ClaimsKey).(*model.Claims)
	return c
}
