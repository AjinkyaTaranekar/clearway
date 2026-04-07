package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/model"
)

// NotificationRepo implements service.NotificationRepository using PostgreSQL.
type NotificationRepo struct {
	master *sql.DB
	slave  *sql.DB
}

// NewNotificationRepo creates a new NotificationRepo.
func NewNotificationRepo(master, slave *sql.DB) *NotificationRepo {
	return &NotificationRepo{master: master, slave: slave}
}

// Insert persists a new notification. Returns an error that callers can check
// for duplicate event_id (unique constraint violation).
func (r *NotificationRepo) Insert(ctx context.Context, n *model.Notification) error {
	const q = `
		INSERT INTO notification.notifications
			(notification_id, event_id, driver_id, journey_id, event_type,
			 title, message, type, delivery_status, is_read, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,false,NOW(),NOW())`

	_, err := r.master.ExecContext(ctx, q,
		n.ID, n.EventID, n.DriverID, n.JourneyID, n.EventType,
		n.Title, n.Message, n.Type, n.DeliveryStatus,
	)
	if err != nil {
		return fmt.Errorf("notification_repo.Insert: %w", err)
	}
	return nil
}

// ListByDriver returns a paginated list plus total and unread counts.
func (r *NotificationRepo) ListByDriver(ctx context.Context, f model.NotificationFilter) ([]model.Notification, int, int, error) {
	// Build dynamic WHERE clause
	args := []interface{}{f.DriverID}
	where := "WHERE driver_id = $1"
	idx := 2

	if f.ReadFilter != nil {
		where += fmt.Sprintf(" AND is_read = $%d", idx)
		args = append(args, *f.ReadFilter)
		idx++
	}
	if f.TypeFilter != "" {
		where += fmt.Sprintf(" AND type = $%d", idx)
		args = append(args, f.TypeFilter)
		idx++
	}

	// Total count
	var total int
	countQ := fmt.Sprintf("SELECT COUNT(*) FROM notification.notifications %s", where)
	if err := r.slave.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, 0, fmt.Errorf("notification_repo.ListByDriver count: %w", err)
	}

	// Unread count (always for this driver, ignoring read/type filters)
	var unread int
	if err := r.slave.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM notification.notifications WHERE driver_id = $1 AND is_read = false",
		f.DriverID,
	).Scan(&unread); err != nil {
		return nil, 0, 0, fmt.Errorf("notification_repo.ListByDriver unread: %w", err)
	}

	// Paginated rows
	offset := (f.Page - 1) * f.Limit
	listQ := fmt.Sprintf(`
		SELECT notification_id, event_id, driver_id, journey_id, event_type,
		       title, message, type, delivery_status, retry_count,
		       COALESCE(last_error,''), is_read, read_at,
		       created_at, updated_at, sent_at, failed_at
		FROM notification.notifications
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1)

	args = append(args, f.Limit, offset)
	rows, err := r.slave.QueryContext(ctx, listQ, args...)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("notification_repo.ListByDriver rows: %w", err)
	}
	defer rows.Close()

	notifications, err := scanNotifications(rows)
	if err != nil {
		return nil, 0, 0, err
	}
	return notifications, total, unread, nil
}

// ListAll returns a paginated admin feed.
func (r *NotificationRepo) ListAll(ctx context.Context, f model.NotificationFilter) ([]model.Notification, int, error) {
	args := []interface{}{}
	where := "WHERE 1=1"
	idx := 1

	if f.DriverID != "" {
		where += fmt.Sprintf(" AND driver_id = $%d", idx)
		args = append(args, f.DriverID)
		idx++
	}
	if f.TypeFilter != "" {
		where += fmt.Sprintf(" AND type = $%d", idx)
		args = append(args, f.TypeFilter)
		idx++
	}
	if f.DeliveryStatus != "" {
		where += fmt.Sprintf(" AND delivery_status = $%d", idx)
		args = append(args, f.DeliveryStatus)
		idx++
	}

	var total int
	countQ := fmt.Sprintf("SELECT COUNT(*) FROM notification.notifications %s", where)
	if err := r.slave.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("notification_repo.ListAll count: %w", err)
	}

	offset := (f.Page - 1) * f.Limit
	listQ := fmt.Sprintf(`
		SELECT notification_id, event_id, driver_id, journey_id, event_type,
		       title, message, type, delivery_status, retry_count,
		       COALESCE(last_error,''), is_read, read_at,
		       created_at, updated_at, sent_at, failed_at
		FROM notification.notifications
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, where, idx, idx+1)

	args = append(args, f.Limit, offset)
	rows, err := r.slave.QueryContext(ctx, listQ, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("notification_repo.ListAll rows: %w", err)
	}
	defer rows.Close()

	notifications, err := scanNotifications(rows)
	if err != nil {
		return nil, 0, err
	}
	return notifications, total, nil
}

// MarkRead marks a single notification as read. Returns nil if not found or
// not owned by the given driver.
func (r *NotificationRepo) MarkRead(ctx context.Context, notificationID, driverID string) (*model.Notification, error) {
	const q = `
		UPDATE notification.notifications
		SET is_read = true, read_at = NOW(), updated_at = NOW()
		WHERE notification_id = $1 AND driver_id = $2
		RETURNING notification_id, event_id, driver_id, journey_id, event_type,
		          title, message, type, delivery_status, retry_count,
		          COALESCE(last_error,''), is_read, read_at,
		          created_at, updated_at, sent_at, failed_at`

	row := r.master.QueryRowContext(ctx, q, notificationID, driverID)
	notifications, err := scanNotifications(&singleRow{row})
	if err != nil {
		return nil, fmt.Errorf("notification_repo.MarkRead: %w", err)
	}
	if len(notifications) == 0 {
		return nil, nil
	}
	return &notifications[0], nil
}

// MarkAllRead marks all unread notifications for a driver as read.
func (r *NotificationRepo) MarkAllRead(ctx context.Context, driverID string) (int, error) {
	const q = `
		UPDATE notification.notifications
		SET is_read = true, read_at = NOW(), updated_at = NOW()
		WHERE driver_id = $1 AND is_read = false`

	res, err := r.master.ExecContext(ctx, q, driverID)
	if err != nil {
		return 0, fmt.Errorf("notification_repo.MarkAllRead: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// UpdateDeliveryStatus updates the delivery_status, retry_count and related
// timestamps. Called by the retry worker and event consumer.
func (r *NotificationRepo) UpdateDeliveryStatus(ctx context.Context, notificationID, status string, retryCount int, lastError string, sentAt, failedAt *time.Time) error {
	const q = `
		UPDATE notification.notifications
		SET delivery_status = $2,
		    retry_count     = $3,
		    last_error      = NULLIF($4,''),
		    sent_at         = $5,
		    failed_at       = $6,
		    updated_at      = NOW()
		WHERE notification_id = $1`

	_, err := r.master.ExecContext(ctx, q, notificationID, status, retryCount, lastError, sentAt, failedAt)
	if err != nil {
		return fmt.Errorf("notification_repo.UpdateDeliveryStatus: %w", err)
	}
	return nil
}

// ---------- scan helpers ----------

type scanner interface {
	Scan(dest ...interface{}) error
}

// singleRow adapts *sql.Row to the same Scan interface used by *sql.Rows.
type singleRow struct{ row *sql.Row }

func (s *singleRow) Scan(dest ...interface{}) error  { return s.row.Scan(dest...) }
func (s *singleRow) Next() bool                      { return true }
func (s *singleRow) Close() error                    { return nil }

// rowsIface abstracts *sql.Rows so we can reuse scanNotifications for both
// multi-row and single-row results.
type rowsIface interface {
	Next() bool
	Scan(dest ...interface{}) error
	Close() error
}

func scanNotifications(rows rowsIface) ([]model.Notification, error) {
	defer rows.Close()
	var result []model.Notification
	for rows.Next() {
		var n model.Notification
		if err := rows.Scan(
			&n.ID, &n.EventID, &n.DriverID, &n.JourneyID, &n.EventType,
			&n.Title, &n.Message, &n.Type, &n.DeliveryStatus, &n.RetryCount,
			&n.LastError, &n.IsRead, &n.ReadAt,
			&n.CreatedAt, &n.UpdatedAt, &n.SentAt, &n.FailedAt,
		); err != nil {
			return nil, fmt.Errorf("notification_repo scan: %w", err)
		}
		result = append(result, n)
	}
	return result, nil
}
