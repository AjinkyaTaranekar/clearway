package model

import "time"

// Platform constants for device tokens.
const (
	PlatformWeb     = "web"
	PlatformAndroid = "android"
	PlatformIOS     = "ios"
)

// DeviceToken represents a registered FCM device token.
type DeviceToken struct {
	ID                  string     `json:"device_token_id"`
	DriverID            string     `json:"driver_id"`
	FCMToken            string     `json:"fcm_token"`
	Platform            string     `json:"platform"`
	IsActive            bool       `json:"is_active"`
	LastSeenAt          time.Time  `json:"last_seen_at"`
	InvalidatedAt       *time.Time `json:"invalidated_at,omitempty"`
	InvalidationReason  string     `json:"invalidation_reason,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// RegisterTokenRequest is the body of POST /api/v1/notifications/device-token.
type RegisterTokenRequest struct {
	DriverID string `json:"driver_id"`
	FCMToken string `json:"fcm_token"`
	Platform string `json:"platform"`
}

// RegisterTokenResponse is returned after token registration.
type RegisterTokenResponse struct {
	Status        string    `json:"status"`
	DriverID      string    `json:"driver_id"`
	DeviceTokenID string    `json:"device_token_id"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ValidPlatform checks whether the given platform string is supported.
func ValidPlatform(p string) bool {
	switch p {
	case PlatformWeb, PlatformAndroid, PlatformIOS:
		return true
	}
	return false
}
