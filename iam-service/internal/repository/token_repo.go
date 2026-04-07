package repository

import (
	"context"
	"database/sql"
	"net"
	"strings"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/model"
)

type TokenRepo struct {
	db *sql.DB
}

func NewTokenRepo(db *sql.DB) *TokenRepo {
	return &TokenRepo{db: db}
}

// Create inserts a new refresh token (auto-committed, used by the Login flow).
func (r *TokenRepo) Create(ctx context.Context, userID, tokenHash, userAgent, ipAddress string, expiresAt time.Time) error {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "TokenRepo.Create").
		Str("user_id", userID).
		Time("expires_at", expiresAt).
		Str("ip", ipAddress).
		Msg("creating refresh token")

	ipValue := nullableINET(ipAddress)

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO auth.refresh_tokens (user_id, token_hash, expires_at, created_at, user_agent, ip_address)
		VALUES ($1, $2, $3, NOW(), $4, $5)`,
		userID, tokenHash, expiresAt, userAgent, ipValue)
	if err != nil {
		log.Error().
			Str("repository", "TokenRepo.Create").
			Err(err).
			Str("user_id", userID).
			Msg("failed to create refresh token")
		return err
	}

	log.Debug().
		Str("repository", "TokenRepo.Create").
		Str("user_id", userID).
		Msg("refresh token created")
	return err
}

// CreateTx inserts a new refresh token inside an active transaction.
func (r *TokenRepo) CreateTx(ctx context.Context, tx *sql.Tx, userID, tokenHash, userAgent, ipAddress string, expiresAt time.Time) error {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "TokenRepo.CreateTx").
		Str("user_id", userID).
		Time("expires_at", expiresAt).
		Msg("creating refresh token in transaction")

	ipValue := nullableINET(ipAddress)

	_, err := tx.ExecContext(ctx, `
		INSERT INTO auth.refresh_tokens (user_id, token_hash, expires_at, created_at, user_agent, ip_address)
		VALUES ($1, $2, $3, NOW(), $4, $5)`,
		userID, tokenHash, expiresAt, userAgent, ipValue)
	if err != nil {
		log.Error().
			Str("repository", "TokenRepo.CreateTx").
			Err(err).
			Str("user_id", userID).
			Msg("failed to create refresh token in transaction")
		return err
	}

	log.Debug().
		Str("repository", "TokenRepo.CreateTx").
		Str("user_id", userID).
		Msg("refresh token created in transaction")
	return err
}

// GetActiveByHash fetches an active (non-revoked, non-expired) token by its hash.
func (r *TokenRepo) GetActiveByHash(ctx context.Context, tokenHash string) (*model.RefreshToken, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "TokenRepo.GetActiveByHash").
		Msg("querying active refresh token by hash")

	var t model.RefreshToken
	err := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
		FROM auth.refresh_tokens
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > NOW()`, tokenHash).
		Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("repository", "TokenRepo.GetActiveByHash").
				Msg("active refresh token not found")
		} else {
			log.Error().
				Str("repository", "TokenRepo.GetActiveByHash").
				Err(err).
				Msg("failed to query active refresh token")
		}
		return nil, err
	}
	log.Debug().
		Str("repository", "TokenRepo.GetActiveByHash").
		Str("user_id", t.UserID).
		Msg("active refresh token loaded")
	return &t, nil
}

// GetActiveByHashTx fetches and row-locks a token inside a transaction.
// SELECT … FOR UPDATE prevents two concurrent refresh calls with the same token
// from both seeing it as active; only one will obtain the lock.
func (r *TokenRepo) GetActiveByHashTx(ctx context.Context, tx *sql.Tx, tokenHash string) (*model.RefreshToken, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "TokenRepo.GetActiveByHashTx").
		Msg("querying and locking active refresh token by hash")

	var t model.RefreshToken
	err := tx.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
		FROM auth.refresh_tokens
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > NOW()
		FOR UPDATE`, tokenHash).
		Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("repository", "TokenRepo.GetActiveByHashTx").
				Msg("active refresh token not found in transaction")
		} else {
			log.Error().
				Str("repository", "TokenRepo.GetActiveByHashTx").
				Err(err).
				Msg("failed to query active refresh token in transaction")
		}
		return nil, err
	}
	log.Debug().
		Str("repository", "TokenRepo.GetActiveByHashTx").
		Str("user_id", t.UserID).
		Msg("active refresh token loaded and locked")
	return &t, nil
}

// Revoke marks a single token as revoked (auto-committed).
func (r *TokenRepo) Revoke(ctx context.Context, tokenHash string) (bool, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "TokenRepo.Revoke").
		Msg("revoking refresh token")

	res, err := r.db.ExecContext(ctx, `
		UPDATE auth.refresh_tokens SET revoked_at = NOW()
		WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash)
	if err != nil {
		log.Error().
			Str("repository", "TokenRepo.Revoke").
			Err(err).
			Msg("failed to revoke refresh token")
		return false, err
	}
	n, _ := res.RowsAffected()
	log.Debug().
		Str("repository", "TokenRepo.Revoke").
		Int64("rows_affected", n).
		Msg("refresh token revoke attempted")
	return n > 0, nil
}

// RevokeTx marks a single token as revoked inside an active transaction.
func (r *TokenRepo) RevokeTx(ctx context.Context, tx *sql.Tx, tokenHash string) (bool, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "TokenRepo.RevokeTx").
		Msg("revoking refresh token in transaction")

	res, err := tx.ExecContext(ctx, `
		UPDATE auth.refresh_tokens SET revoked_at = NOW()
		WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash)
	if err != nil {
		log.Error().
			Str("repository", "TokenRepo.RevokeTx").
			Err(err).
			Msg("failed to revoke refresh token in transaction")
		return false, err
	}
	n, _ := res.RowsAffected()
	log.Debug().
		Str("repository", "TokenRepo.RevokeTx").
		Int64("rows_affected", n).
		Msg("transactional refresh token revoke attempted")
	return n > 0, nil
}

// RevokeAllForUser revokes every active token for a given user (force-logout).
func (r *TokenRepo) RevokeAllForUser(ctx context.Context, userID string) error {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "TokenRepo.RevokeAllForUser").
		Str("user_id", userID).
		Msg("revoking all active refresh tokens for user")

	_, err := r.db.ExecContext(ctx, `
		UPDATE auth.refresh_tokens SET revoked_at = NOW()
		WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		log.Error().
			Str("repository", "TokenRepo.RevokeAllForUser").
			Err(err).
			Str("user_id", userID).
			Msg("failed to revoke all user tokens")
		return err
	}

	log.Info().
		Str("repository", "TokenRepo.RevokeAllForUser").
		Str("user_id", userID).
		Msg("all active refresh tokens revoked for user")
	return err
}

// DeleteExpired removes tokens that have expired or been revoked for longer
// than retentionDays, keeping the table from growing unbounded.
func (r *TokenRepo) DeleteExpired(ctx context.Context, retentionDays int) (int64, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "TokenRepo.DeleteExpired").
		Int("retention_days", retentionDays).
		Msg("deleting expired or old revoked refresh tokens")

	res, err := r.db.ExecContext(ctx, `
		DELETE FROM auth.refresh_tokens
		WHERE expires_at < NOW() - ($1 || ' days')::interval
		   OR (revoked_at IS NOT NULL AND revoked_at < NOW() - ($1 || ' days')::interval)`,
		retentionDays)
	if err != nil {
		log.Error().
			Str("repository", "TokenRepo.DeleteExpired").
			Err(err).
			Int("retention_days", retentionDays).
			Msg("failed to delete expired refresh tokens")
		return 0, err
	}
	n, rowsErr := res.RowsAffected()
	if rowsErr != nil {
		log.Error().
			Str("repository", "TokenRepo.DeleteExpired").
			Err(rowsErr).
			Msg("failed to fetch rows affected for delete expired")
		return 0, rowsErr
	}

	log.Debug().
		Str("repository", "TokenRepo.DeleteExpired").
		Int64("deleted_count", n).
		Msg("expired refresh token cleanup completed")
	return n, nil
}

func nullableINET(raw string) interface{} {
	ip := normalizeIPAddress(raw)
	if ip == "" {
		return nil
	}
	return ip
}

func normalizeIPAddress(raw string) string {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return ""
	}

	// Forwarded chains look like: "client, proxy1, proxy2".
	if comma := strings.Index(candidate, ","); comma >= 0 {
		candidate = strings.TrimSpace(candidate[:comma])
	}

	candidate = strings.Trim(candidate, `"'`)

	if host, _, err := net.SplitHostPort(candidate); err == nil {
		candidate = host
	}

	candidate = strings.TrimPrefix(candidate, "[")
	candidate = strings.TrimSuffix(candidate, "]")

	if zone := strings.Index(candidate, "%"); zone >= 0 {
		candidate = candidate[:zone]
	}

	ip := net.ParseIP(candidate)
	if ip == nil {
		return ""
	}

	return ip.String()
}
