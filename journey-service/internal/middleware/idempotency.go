package middleware

import (
	"context"
	"net/http"
)

const contextKeyIdempotencyKey contextKey = "idempotency_key"

// IdempotencyKeyMiddleware extracts the Idempotency-Key header into the context
func IdempotencyKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		ctx := context.WithValue(r.Context(), contextKeyIdempotencyKey, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetIdempotencyKey extracts the idempotency key from context
func GetIdempotencyKey(ctx context.Context) string {
	v, _ := ctx.Value(contextKeyIdempotencyKey).(string)
	return v
}
