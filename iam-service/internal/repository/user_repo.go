package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/model"
)

// UserRepo wraps two connection pools: master for all writes and auth-path reads
// (where replication lag would cause correctness issues), slave for read-only
// admin queries that are tolerant of ~100 ms lag.
type UserRepo struct {
	master *sql.DB
	slave  *sql.DB
}

// NewUserRepo creates a UserRepo. Pass the same pool for both arguments if you
// only have one database; in production use separate master and slave pools.
func NewUserRepo(master, slave *sql.DB) *UserRepo {
	return &UserRepo{master: master, slave: slave}
}

// Create inserts a new user row and returns the created record (auto-committed).
func (r *UserRepo) Create(ctx context.Context, id, name, email, emailLower, passwordHash string, role model.Role, vehicleType model.VehicleType, licenseInfo model.LicenseInfo) (*model.User, error) {
	liJSON, err := json.Marshal(licenseInfo)
	if err != nil {
		return nil, fmt.Errorf("marshal license_info: %w", err)
	}
	const query = `
		INSERT INTO auth.users (id, name, email, email_lower, password_hash, role, vehicle_type, license_info, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		RETURNING id, name, email, role, vehicle_type, license_info, created_at, updated_at`
	return scanUser(r.master.QueryRowContext(ctx, query, id, name, email, emailLower, passwordHash, string(role), string(vehicleType), liJSON))
}

// CreateTx inserts a new user row inside an active transaction.
func (r *UserRepo) CreateTx(ctx context.Context, tx *sql.Tx, id, name, email, emailLower, passwordHash string, role model.Role, vehicleType model.VehicleType, licenseInfo model.LicenseInfo) (*model.User, error) {
	liJSON, err := json.Marshal(licenseInfo)
	if err != nil {
		return nil, fmt.Errorf("marshal license_info: %w", err)
	}
	const query = `
		INSERT INTO auth.users (id, name, email, email_lower, password_hash, role, vehicle_type, license_info, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		RETURNING id, name, email, role, vehicle_type, license_info, created_at, updated_at`
	return scanUser(tx.QueryRowContext(ctx, query, id, name, email, emailLower, passwordHash, string(role), string(vehicleType), liJSON))
}

// GetByEmail looks up a user by lowercased email and returns the user plus the
// stored bcrypt hash. Uses master to avoid replication-lag failures immediately
// after registration.
func (r *UserRepo) GetByEmail(ctx context.Context, emailLower string) (*model.User, string, error) {
	const query = `
		SELECT id, name, email, role, vehicle_type, license_info, created_at, updated_at, password_hash
		FROM auth.users WHERE email_lower = $1`
	row := r.master.QueryRowContext(ctx, query, emailLower)
	var u model.User
	var liRaw []byte
	var hash string
	if err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.VehicleType, &liRaw, &u.CreatedAt, &u.UpdatedAt, &hash); err != nil {
		return nil, "", err
	}
	var err error
	u.LicenseInfo, err = model.LicenseInfoFromJSON(liRaw)
	if err != nil {
		return nil, "", err
	}
	return &u, hash, nil
}

// GetByID fetches a user by ID using the master pool (safe for auth hot-path).
func (r *UserRepo) GetByID(ctx context.Context, id string) (*model.User, error) {
	const query = `
		SELECT id, name, email, role, vehicle_type, license_info, created_at, updated_at
		FROM auth.users WHERE id = $1`
	return scanUser(r.master.QueryRowContext(ctx, query, id))
}

// GetByIDTx fetches a user by ID inside an active transaction.
func (r *UserRepo) GetByIDTx(ctx context.Context, tx *sql.Tx, id string) (*model.User, error) {
	const query = `
		SELECT id, name, email, role, vehicle_type, license_info, created_at, updated_at
		FROM auth.users WHERE id = $1`
	return scanUser(tx.QueryRowContext(ctx, query, id))
}

// UpdateProfileInput carries optional fields for a partial profile update.
type UpdateProfileInput struct {
	Name        *string
	VehicleType *model.VehicleType
	LicenseInfo *model.LicenseInfo
}

// UpdateProfile applies a partial update and returns the updated record.
func (r *UserRepo) UpdateProfile(ctx context.Context, userID string, in UpdateProfileInput) (*model.User, error) {
	setClauses := []string{}
	args := []interface{}{}
	idx := 1
	if in.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", idx))
		args = append(args, *in.Name)
		idx++
	}
	if in.VehicleType != nil {
		setClauses = append(setClauses, fmt.Sprintf("vehicle_type = $%d", idx))
		args = append(args, string(*in.VehicleType))
		idx++
	}
	if in.LicenseInfo != nil {
		liJSON, err := json.Marshal(*in.LicenseInfo)
		if err != nil {
			return nil, fmt.Errorf("marshal license_info: %w", err)
		}
		setClauses = append(setClauses, fmt.Sprintf("license_info = $%d", idx))
		args = append(args, liJSON)
		idx++
	}
	if len(setClauses) == 0 {
		return r.GetByID(ctx, userID)
	}
	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", idx))
	args = append(args, time.Now())
	idx++
	args = append(args, userID)
	query := fmt.Sprintf(`
		UPDATE auth.users SET %s WHERE id = $%d
		RETURNING id, name, email, role, vehicle_type, license_info, created_at, updated_at`,
		strings.Join(setClauses, ", "), idx)
	return scanUser(r.master.QueryRowContext(ctx, query, args...))
}

// UpdateRole sets a user's role and returns the updated record.
func (r *UserRepo) UpdateRole(ctx context.Context, userID string, role model.Role) (*model.User, error) {
	const query = `
		UPDATE auth.users SET role = $1, updated_at = NOW()
		WHERE id = $2
		RETURNING id, name, email, role, vehicle_type, license_info, created_at, updated_at`
	return scanUser(r.master.QueryRowContext(ctx, query, string(role), userID))
}

// CountByRole returns the number of users with the given role.
// Runs on the slave pool — safe for admin analytics where ~100 ms lag is acceptable.
func (r *UserRepo) CountByRole(ctx context.Context, role model.Role) (int, error) {
	var count int
	err := r.slave.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth.users WHERE role = $1`, string(role)).Scan(&count)
	return count, err
}

// List returns a paginated, optionally role-filtered list of users.
// Runs on the slave pool — admin listings are not latency-critical.
func (r *UserRepo) List(ctx context.Context, roleFilter string, page, limit int) ([]*model.User, int, error) {
	offset := (page - 1) * limit
	whereClause := ""
	args := []interface{}{}
	idx := 1
	if roleFilter != "" {
		whereClause = fmt.Sprintf("WHERE role = $%d", idx)
		args = append(args, roleFilter)
		idx++
	}

	var total int
	if err := r.slave.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM auth.users %s", whereClause),
		args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	rows, err := r.slave.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, name, email, role, vehicle_type, license_info, created_at, updated_at
		FROM auth.users %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		whereClause, idx, idx+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var users []*model.User
	for rows.Next() {
		var u model.User
		var liRaw []byte
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.VehicleType, &liRaw, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, 0, err
		}
		u.LicenseInfo, _ = model.LicenseInfoFromJSON(liRaw)
		users = append(users, &u)
	}
	return users, total, rows.Err()
}

func scanUser(row *sql.Row) (*model.User, error) {
	var u model.User
	var liRaw []byte
	if err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.VehicleType, &liRaw, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	var err error
	u.LicenseInfo, err = model.LicenseInfoFromJSON(liRaw)
	return &u, err
}
