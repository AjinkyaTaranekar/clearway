package service

import (
	"context"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/model"
)

// NotificationRepository defines the data-access interface that will be
// implemented when you add PostgreSQL support.
// For now, an in-memory stub is used so the HTTP layer can be tested end-to-end.
type NotificationRepository interface {
	// Insert persists a new notification. Duplicate event_id should return
	// a recognisable error so callers can skip safely.
	Insert(ctx context.Context, n *model.Notification) error

	// ListByDriver returns a paginated list and total/unread counts.
	ListByDriver(ctx context.Context, f model.NotificationFilter) ([]model.Notification, int, int, error)

	// ListAll returns a paginated admin feed.
	ListAll(ctx context.Context, f model.NotificationFilter) ([]model.Notification, int, error)

	// MarkRead marks a single notification as read. Returns false if not found
	// or not owned by the given driver.
	MarkRead(ctx context.Context, notificationID, driverID string) (*model.Notification, error)

	// MarkAllRead marks every unread notification for a driver. Returns the
	// number of rows updated.
	MarkAllRead(ctx context.Context, driverID string) (int, error)

	// UpdateDeliveryStatus tracks push-delivery progress for a notification.
	UpdateDeliveryStatus(ctx context.Context, notificationID, status string, retryCount int, lastError string, sentAt, failedAt *time.Time) error
}

// DeviceTokenRepository defines the data-access interface for device tokens.
type DeviceTokenRepository interface {
	Upsert(ctx context.Context, t *model.DeviceToken) (*model.DeviceToken, error)
	FindActiveByDriver(ctx context.Context, driverID string) ([]model.DeviceToken, error)
	Deactivate(ctx context.Context, tokenID, reason string) error
	DeactivateByDriverAndFCMToken(ctx context.Context, driverID, fcmToken, reason string) error
}
