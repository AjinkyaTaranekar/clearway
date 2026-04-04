package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/tracing"
)

type contextKey string

const (
	contextKeyDriverID contextKey = "driver_id"
	contextKeyRole     contextKey = "role"
)

// JWTAuth returns middleware that validates HS256 JWT tokens
func JWTAuth(secret string) func(http.Handler) http.Handler {
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
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, apperrors.Unauthorized("unexpected signing method")
				}
				return []byte(secret), nil
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

// AdminOnly returns middleware that requires the admin role
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

// GetDriverID extracts the driver ID from the request context
func GetDriverID(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyDriverID).(string)
	return v
}

// GetRole extracts the role from the request context
func GetRole(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyRole).(string)
	return v
}
