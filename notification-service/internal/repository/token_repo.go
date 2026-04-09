package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/model"
	"github.com/google/uuid"
)

// DeviceTokenRepo implements service.DeviceTokenRepository using PostgreSQL.
type DeviceTokenRepo struct {
	master *sql.DB
	slave  *sql.DB
}

// NewDeviceTokenRepo creates a new DeviceTokenRepo.
func NewDeviceTokenRepo(master, slave *sql.DB) *DeviceTokenRepo {
	return &DeviceTokenRepo{master: master, slave: slave}
}

// Upsert inserts a new token or updates the existing active one for
// the same driver+fcm_token combination.
func (r *DeviceTokenRepo) Upsert(ctx context.Context, t *model.DeviceToken) (*model.DeviceToken, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "DeviceTokenRepo.Upsert").
		Str("driver_id", t.DriverID).
		Str("platform", string(t.Platform)).
		Msg("upserting device token")

	// Deactivate any existing active token with the same fcm_token value
	// (different driver may have acquired it) then insert fresh.
	const deactivateQ = `
		UPDATE notification.device_tokens
		SET is_active = false,
		    invalidated_at = NOW(),
		    invalidation_reason = 'replaced',
		    updated_at = NOW()
		WHERE fcm_token = $1 AND is_active = true AND driver_id != $2`

	if _, err := r.master.ExecContext(ctx, deactivateQ, t.FCMToken, t.DriverID); err != nil {
		log.Error().
			Str("repository", "DeviceTokenRepo.Upsert").
			Err(err).
			Str("driver_id", t.DriverID).
			Msg("failed to deactivate existing active token")
		return nil, fmt.Errorf("device_token_repo.Upsert deactivate old: %w", err)
	}

	// Upsert for the current driver
	const upsertQ = `
		INSERT INTO notification.device_tokens
			(device_token_id, driver_id, fcm_token, platform, is_active, last_seen_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, true, NOW(), NOW(), NOW())
		ON CONFLICT (fcm_token) WHERE is_active = true
		DO UPDATE SET
			platform     = EXCLUDED.platform,
			last_seen_at = NOW(),
			updated_at   = NOW()
		RETURNING device_token_id, driver_id, fcm_token, platform, is_active,
		          last_seen_at, invalidated_at, invalidation_reason, created_at, updated_at`

	id := "dvt_" + uuid.New().String()[:8]
	row := r.master.QueryRowContext(ctx, upsertQ, id, t.DriverID, t.FCMToken, t.Platform)

	var saved model.DeviceToken
	err := row.Scan(
		&saved.ID, &saved.DriverID, &saved.FCMToken, &saved.Platform, &saved.IsActive,
		&saved.LastSeenAt, &saved.InvalidatedAt, &saved.InvalidationReason,
		&saved.CreatedAt, &saved.UpdatedAt,
	)
	if err != nil {
		log.Error().
			Str("repository", "DeviceTokenRepo.Upsert").
			Err(err).
			Str("driver_id", t.DriverID).
			Msg("failed to scan upserted token")
		return nil, fmt.Errorf("device_token_repo.Upsert scan: %w", err)
	}
	log.Info().
		Str("repository", "DeviceTokenRepo.Upsert").
		Str("driver_id", saved.DriverID).
		Str("device_token_id", saved.ID).
		Msg("device token upsert completed")
	return &saved, nil
}

// FindActiveByDriver returns all active FCM tokens for a driver.
func (r *DeviceTokenRepo) FindActiveByDriver(ctx context.Context, driverID string) ([]model.DeviceToken, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "DeviceTokenRepo.FindActiveByDriver").
		Str("driver_id", driverID).
		Msg("querying active device tokens")

	// Use primary here so just-registered tokens are visible to the event
	// consumer without waiting for replica convergence.
	readDB := r.master
	if readDB == nil {
		readDB = r.slave
	}

	const q = `
		SELECT device_token_id, driver_id, fcm_token, platform, is_active,
		       last_seen_at, invalidated_at, invalidation_reason, created_at, updated_at
		FROM notification.device_tokens
		WHERE driver_id = $1 AND is_active = true
		ORDER BY last_seen_at DESC`

	rows, err := readDB.QueryContext(ctx, q, driverID)
	if err != nil {
		log.Error().
			Str("repository", "DeviceTokenRepo.FindActiveByDriver").
			Err(err).
			Str("driver_id", driverID).
			Msg("failed to query active device tokens")
		return nil, fmt.Errorf("device_token_repo.FindActiveByDriver: %w", err)
	}
	defer rows.Close()

	var tokens []model.DeviceToken
	for rows.Next() {
		var t model.DeviceToken
		if err := rows.Scan(
			&t.ID, &t.DriverID, &t.FCMToken, &t.Platform, &t.IsActive,
			&t.LastSeenAt, &t.InvalidatedAt, &t.InvalidationReason,
			&t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			log.Error().
				Str("repository", "DeviceTokenRepo.FindActiveByDriver").
				Err(err).
				Str("driver_id", driverID).
				Msg("failed to scan active device token row")
			return nil, fmt.Errorf("device_token_repo.FindActiveByDriver scan: %w", err)
		}
		tokens = append(tokens, t)
	}
	log.Debug().
		Str("repository", "DeviceTokenRepo.FindActiveByDriver").
		Str("driver_id", driverID).
		Int("token_count", len(tokens)).
		Msg("loaded active device tokens")
	return tokens, nil
}

// Deactivate marks a token as inactive and records the reason.
func (r *DeviceTokenRepo) Deactivate(ctx context.Context, tokenID, reason string) error {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "DeviceTokenRepo.Deactivate").
		Str("device_token_id", tokenID).
		Str("reason", reason).
		Msg("deactivating device token")

	now := time.Now().UTC()
	const q = `
		UPDATE notification.device_tokens
		SET is_active = false,
		    invalidated_at = $2,
		    invalidation_reason = $3,
		    updated_at = NOW()
		WHERE device_token_id = $1`

	_, err := r.master.ExecContext(ctx, q, tokenID, now, reason)
	if err != nil {
		log.Error().
			Str("repository", "DeviceTokenRepo.Deactivate").
			Err(err).
			Str("device_token_id", tokenID).
			Msg("failed to deactivate device token")
		return fmt.Errorf("device_token_repo.Deactivate: %w", err)
	}
	log.Info().
		Str("repository", "DeviceTokenRepo.Deactivate").
		Str("device_token_id", tokenID).
		Msg("device token deactivated")
	return nil
}

// DeactivateByDriverAndFCMToken marks matching active token rows as inactive.
func (r *DeviceTokenRepo) DeactivateByDriverAndFCMToken(ctx context.Context, driverID, fcmToken, reason string) error {
	now := time.Now().UTC()
	const q = `
		UPDATE notification.device_tokens
		SET is_active = false,
		    invalidated_at = $3,
		    invalidation_reason = $4,
		    updated_at = NOW()
		WHERE driver_id = $1
		  AND fcm_token = $2
		  AND is_active = true`

	_, err := r.master.ExecContext(ctx, q, driverID, fcmToken, now, reason)
	if err != nil {
		return fmt.Errorf("device_token_repo.DeactivateByDriverAndFCMToken: %w", err)
	}
	return nil
}
