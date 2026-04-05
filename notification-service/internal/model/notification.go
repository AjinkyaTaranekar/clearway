package model

import "time"

// Notification type constants
const (
	TypeInfo    = "info"
	TypeSuccess = "success"
	TypeWarning = "warning"
	TypeError   = "error"
)

// Delivery status constants
const (
	DeliveryPending  = "PENDING"
	DeliverySent     = "SENT"
	DeliveryFailed   = "FAILED"
	DeliverySkipped  = "SKIPPED"
	DeliveryRetrying = "RETRYING"
)

// Notification represents a stored notification record.
type Notification struct {
	ID             string     `json:"id"`
	EventID        string     `json:"event_id,omitempty"`
	DriverID       string     `json:"driver_id,omitempty"`
	JourneyID      string     `json:"journey_id,omitempty"`
	EventType      string     `json:"event_type,omitempty"`
	Title          string     `json:"title"`
	Message        string     `json:"message"`
	Type           string     `json:"type"`
	DeliveryStatus string     `json:"delivery_status,omitempty"`
	RetryCount     int        `json:"retry_count,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	IsRead         bool       `json:"read"`
	ReadAt         *time.Time `json:"read_at,omitempty"`
	CreatedAt      time.Time  `json:"timestamp"`
	UpdatedAt      time.Time  `json:"updated_at,omitempty"`
	SentAt         *time.Time `json:"sent_at,omitempty"`
	FailedAt       *time.Time `json:"failed_at,omitempty"`
}

// NotificationListResponse is the paginated list returned to drivers.
type NotificationListResponse struct {
	Notifications []Notification `json:"notifications"`
	UnreadCount   int            `json:"unread_count"`
	Total         int            `json:"total"`
	Page          int            `json:"page"`
	Limit         int            `json:"limit"`
}

// AdminNotificationListResponse is the paginated list returned to admins.
type AdminNotificationListResponse struct {
	Notifications []Notification `json:"notifications"`
	Total         int            `json:"total"`
	Page          int            `json:"page"`
	Limit         int            `json:"limit"`
}

// MarkReadResponse is returned after marking a notification as read.
type MarkReadResponse struct {
	NotificationID string    `json:"notification_id"`
	Read           bool      `json:"read"`
	ReadAt         time.Time `json:"read_at"`
}

// MarkAllReadResponse is returned after marking all notifications as read.
type MarkAllReadResponse struct {
	Status       string    `json:"status"`
	UpdatedCount int       `json:"updated_count"`
	ReadAt       time.Time `json:"read_at"`
}

// NotificationFilter holds query parameters for listing notifications.
type NotificationFilter struct {
	DriverID       string
	Page           int
	Limit          int
	ReadFilter     *bool  // nil = no filter
	TypeFilter     string // "" = no filter
	DeliveryStatus string // "" = no filter (admin only)
}
