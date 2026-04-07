package repository

import (
	"context"
	"database/sql"
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
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO auth.refresh_tokens (user_id, token_hash, expires_at, created_at, user_agent, ip_address)
		VALUES ($1, $2, $3, NOW(), $4, $5)`,
		userID, tokenHash, expiresAt, userAgent, ipAddress)
	return err
}

// CreateTx inserts a new refresh token inside an active transaction.
func (r *TokenRepo) CreateTx(ctx context.Context, tx *sql.Tx, userID, tokenHash, userAgent, ipAddress string, expiresAt time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO auth.refresh_tokens (user_id, token_hash, expires_at, created_at, user_agent, ip_address)
		VALUES ($1, $2, $3, NOW(), $4, $5)`,
		userID, tokenHash, expiresAt, userAgent, ipAddress)
	return err
}

// GetActiveByHash fetches an active (non-revoked, non-expired) token by its hash.
func (r *TokenRepo) GetActiveByHash(ctx context.Context, tokenHash string) (*model.RefreshToken, error) {
	var t model.RefreshToken
	err := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
		FROM auth.refresh_tokens
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > NOW()`, tokenHash).
		Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetActiveByHashTx fetches and row-locks a token inside a transaction.
// SELECT … FOR UPDATE prevents two concurrent refresh calls with the same token
// from both seeing it as active; only one will obtain the lock.
func (r *TokenRepo) GetActiveByHashTx(ctx context.Context, tx *sql.Tx, tokenHash string) (*model.RefreshToken, error) {
	var t model.RefreshToken
	err := tx.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
		FROM auth.refresh_tokens
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > NOW()
		FOR UPDATE`, tokenHash).
		Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Revoke marks a single token as revoked (auto-committed).
func (r *TokenRepo) Revoke(ctx context.Context, tokenHash string) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE auth.refresh_tokens SET revoked_at = NOW()
		WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RevokeTx marks a single token as revoked inside an active transaction.
func (r *TokenRepo) RevokeTx(ctx context.Context, tx *sql.Tx, tokenHash string) (bool, error) {
	res, err := tx.ExecContext(ctx, `
		UPDATE auth.refresh_tokens SET revoked_at = NOW()
		WHERE token_hash = $1 AND revoked_at IS NULL`, tokenHash)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RevokeAllForUser revokes every active token for a given user (force-logout).
func (r *TokenRepo) RevokeAllForUser(ctx context.Context, userID string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE auth.refresh_tokens SET revoked_at = NOW()
		WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	return err
}

// DeleteExpired removes tokens that have expired or been revoked for longer
// than retentionDays, keeping the table from growing unbounded.
func (r *TokenRepo) DeleteExpired(ctx context.Context, retentionDays int) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM auth.refresh_tokens
		WHERE expires_at < NOW() - ($1 || ' days')::interval
		   OR (revoked_at IS NOT NULL AND revoked_at < NOW() - ($1 || ' days')::interval)`,
		retentionDays)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
