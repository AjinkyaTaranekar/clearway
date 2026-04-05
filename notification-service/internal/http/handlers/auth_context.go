package handlers

import "context"

type contextKey string

const (
	ctxUserID   contextKey = "user_id"
	ctxUserRole contextKey = "user_role"
)

// WithUserID stores the authenticated user ID in the context.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ctxUserID, userID)
}

// WithUserRole stores the authenticated user role in the context.
func WithUserRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, ctxUserRole, role)
}

// GetUserID extracts the authenticated user ID from the context.
func GetUserID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxUserID).(string); ok {
		return v
	}
	return ""
}

// GetUserRole extracts the authenticated user role from the context.
func GetUserRole(ctx context.Context) string {
	if v, ok := ctx.Value(ctxUserRole).(string); ok {
		return v
	}
	return ""
}
