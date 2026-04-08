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
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "NotificationRepo.Insert").
		Str("notification_id", n.ID).
		Str("event_id", n.EventID).
		Str("driver_id", n.DriverID).
		Str("event_type", n.EventType).
		Msg("inserting notification row")

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
		log.Error().
			Str("repository", "NotificationRepo.Insert").
			Err(err).
			Str("notification_id", n.ID).
			Str("event_id", n.EventID).
			Msg("failed to insert notification row")
		return fmt.Errorf("notification_repo.Insert: %w", err)
	}
	log.Debug().
		Str("repository", "NotificationRepo.Insert").
		Str("notification_id", n.ID).
		Msg("notification row inserted")
	return nil
}

// ListByDriver returns a paginated list plus total and unread counts.
func (r *NotificationRepo) ListByDriver(ctx context.Context, f model.NotificationFilter) ([]model.Notification, int, int, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "NotificationRepo.ListByDriver").
		Str("driver_id", f.DriverID).
		Int("page", f.Page).
		Int("limit", f.Limit).
		Str("type_filter", f.TypeFilter).
		Msg("listing notifications by driver")

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
		log.Error().
			Str("repository", "NotificationRepo.ListByDriver").
			Err(err).
			Str("driver_id", f.DriverID).
			Msg("failed to count notifications by driver")
		return nil, 0, 0, fmt.Errorf("notification_repo.ListByDriver count: %w", err)
	}

	// Unread count (always for this driver, ignoring read/type filters)
	var unread int
	if err := r.slave.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM notification.notifications WHERE driver_id = $1 AND is_read = false",
		f.DriverID,
	).Scan(&unread); err != nil {
		log.Error().
			Str("repository", "NotificationRepo.ListByDriver").
			Err(err).
			Str("driver_id", f.DriverID).
			Msg("failed to count unread notifications")
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
		log.Error().
			Str("repository", "NotificationRepo.ListByDriver").
			Err(err).
			Str("driver_id", f.DriverID).
			Msg("failed to query notifications by driver")
		return nil, 0, 0, fmt.Errorf("notification_repo.ListByDriver rows: %w", err)
	}
	defer rows.Close()

	notifications, err := scanNotifications(rows)
	if err != nil {
		log.Error().
			Str("repository", "NotificationRepo.ListByDriver").
			Err(err).
			Str("driver_id", f.DriverID).
			Msg("failed to scan notifications by driver")
		return nil, 0, 0, err
	}
	log.Info().
		Str("repository", "NotificationRepo.ListByDriver").
		Str("driver_id", f.DriverID).
		Int("result_count", len(notifications)).
		Int("total", total).
		Int("unread", unread).
		Msg("listed notifications by driver")
	return notifications, total, unread, nil
}

// ListAll returns a paginated admin feed.
func (r *NotificationRepo) ListAll(ctx context.Context, f model.NotificationFilter) ([]model.Notification, int, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "NotificationRepo.ListAll").
		Str("driver_id_filter", f.DriverID).
		Str("type_filter", f.TypeFilter).
		Str("delivery_status_filter", f.DeliveryStatus).
		Int("page", f.Page).
		Int("limit", f.Limit).
		Msg("listing notifications for admin")

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
		log.Error().
			Str("repository", "NotificationRepo.ListAll").
			Err(err).
			Msg("failed to count admin notification listing")
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
		log.Error().
			Str("repository", "NotificationRepo.ListAll").
			Err(err).
			Msg("failed to query admin notification listing")
		return nil, 0, fmt.Errorf("notification_repo.ListAll rows: %w", err)
	}
	defer rows.Close()

	notifications, err := scanNotifications(rows)
	if err != nil {
		log.Error().
			Str("repository", "NotificationRepo.ListAll").
			Err(err).
			Msg("failed to scan admin notification listing")
		return nil, 0, err
	}
	log.Info().
		Str("repository", "NotificationRepo.ListAll").
		Int("result_count", len(notifications)).
		Int("total", total).
		Msg("listed notifications for admin")
	return notifications, total, nil
}

// MarkRead marks a single notification as read. Returns nil if not found or
// not owned by the given driver.
func (r *NotificationRepo) MarkRead(ctx context.Context, notificationID, driverID string) (*model.Notification, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "NotificationRepo.MarkRead").
		Str("notification_id", notificationID).
		Str("driver_id", driverID).
		Msg("marking notification as read")

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
		log.Error().
			Str("repository", "NotificationRepo.MarkRead").
			Err(err).
			Str("notification_id", notificationID).
			Str("driver_id", driverID).
			Msg("failed to mark notification as read")
		return nil, fmt.Errorf("notification_repo.MarkRead: %w", err)
	}
	if len(notifications) == 0 {
		log.Warn().
			Str("repository", "NotificationRepo.MarkRead").
			Str("notification_id", notificationID).
			Str("driver_id", driverID).
			Msg("notification not found for mark read")
		return nil, nil
	}
	log.Info().
		Str("repository", "NotificationRepo.MarkRead").
		Str("notification_id", notifications[0].ID).
		Str("driver_id", notifications[0].DriverID).
		Msg("notification marked as read")
	return &notifications[0], nil
}

// MarkAllRead marks all unread notifications for a driver as read.
func (r *NotificationRepo) MarkAllRead(ctx context.Context, driverID string) (int, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "NotificationRepo.MarkAllRead").
		Str("driver_id", driverID).
		Msg("marking all notifications as read")

	const q = `
		UPDATE notification.notifications
		SET is_read = true, read_at = NOW(), updated_at = NOW()
		WHERE driver_id = $1 AND is_read = false`

	res, err := r.master.ExecContext(ctx, q, driverID)
	if err != nil {
		log.Error().
			Str("repository", "NotificationRepo.MarkAllRead").
			Err(err).
			Str("driver_id", driverID).
			Msg("failed to mark all notifications as read")
		return 0, fmt.Errorf("notification_repo.MarkAllRead: %w", err)
	}
	n, _ := res.RowsAffected()
	log.Info().
		Str("repository", "NotificationRepo.MarkAllRead").
		Str("driver_id", driverID).
		Int64("rows_affected", n).
		Msg("marked all notifications as read")
	return int(n), nil
}

// UpdateDeliveryStatus updates the delivery_status, retry_count and related
// timestamps. Called by the retry worker and event consumer.
func (r *NotificationRepo) UpdateDeliveryStatus(ctx context.Context, notificationID, status string, retryCount int, lastError string, sentAt, failedAt *time.Time) error {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "NotificationRepo.UpdateDeliveryStatus").
		Str("notification_id", notificationID).
		Str("status", status).
		Int("retry_count", retryCount).
		Msg("updating notification delivery status")

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
		log.Error().
			Str("repository", "NotificationRepo.UpdateDeliveryStatus").
			Err(err).
			Str("notification_id", notificationID).
			Msg("failed to update notification delivery status")
		return fmt.Errorf("notification_repo.UpdateDeliveryStatus: %w", err)
	}
	log.Info().
		Str("repository", "NotificationRepo.UpdateDeliveryStatus").
		Str("notification_id", notificationID).
		Str("status", status).
		Msg("notification delivery status updated")
	return nil
}

// ---------- scan helpers ----------

type scanner interface {
	Scan(dest ...interface{}) error
}

// singleRow adapts *sql.Row to the same Scan interface used by *sql.Rows.
type singleRow struct{ row *sql.Row }

func (s *singleRow) Scan(dest ...interface{}) error { return s.row.Scan(dest...) }
func (s *singleRow) Next() bool                     { return true }
func (s *singleRow) Close() error                   { return nil }

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
