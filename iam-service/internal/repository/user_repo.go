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

type UserRepo struct {
	db *sql.DB
}

func NewUserRepo(db *sql.DB) *UserRepo {
	return &UserRepo{db: db}
}

func (r *UserRepo) Create(ctx context.Context, id, name, email, emailLower, passwordHash string, role model.Role, vehicleType model.VehicleType, licenseInfo model.LicenseInfo) (*model.User, error) {
	liJSON, err := json.Marshal(licenseInfo)
	if err != nil {
		return nil, fmt.Errorf("marshal license_info: %w", err)
	}
	query := `
		INSERT INTO auth.users (id, name, email, email_lower, password_hash, role, vehicle_type, license_info, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		RETURNING id, name, email, role, vehicle_type, license_info, created_at, updated_at`
	row := r.db.QueryRowContext(ctx, query, id, name, email, emailLower, passwordHash, string(role), string(vehicleType), liJSON)
	return scanUser(row)
}

func (r *UserRepo) GetByEmail(ctx context.Context, emailLower string) (*model.User, string, error) {
	query := `
		SELECT id, name, email, role, vehicle_type, license_info, created_at, updated_at, password_hash
		FROM auth.users WHERE email_lower = $1`
	row := r.db.QueryRowContext(ctx, query, emailLower)
	var u model.User
	var liRaw []byte
	var hash string
	err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.VehicleType, &liRaw, &u.CreatedAt, &u.UpdatedAt, &hash)
	if err != nil {
		return nil, "", err
	}
	u.LicenseInfo, err = model.LicenseInfoFromJSON(liRaw)
	if err != nil {
		return nil, "", err
	}
	return &u, hash, nil
}

func (r *UserRepo) GetByID(ctx context.Context, id string) (*model.User, error) {
	query := `
		SELECT id, name, email, role, vehicle_type, license_info, created_at, updated_at
		FROM auth.users WHERE id = $1`
	return scanUser(r.db.QueryRowContext(ctx, query, id))
}

type UpdateProfileInput struct {
	Name        *string
	VehicleType *model.VehicleType
	LicenseInfo *model.LicenseInfo
}

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
	return scanUser(r.db.QueryRowContext(ctx, query, args...))
}

func (r *UserRepo) UpdateRole(ctx context.Context, userID string, role model.Role) (*model.User, error) {
	query := `
		UPDATE auth.users SET role = $1, updated_at = NOW()
		WHERE id = $2
		RETURNING id, name, email, role, vehicle_type, license_info, created_at, updated_at`
	return scanUser(r.db.QueryRowContext(ctx, query, string(role), userID))
}

func (r *UserRepo) CountByRole(ctx context.Context, role model.Role) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth.users WHERE role = $1`, string(role)).Scan(&count)
	return count, err
}

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
	if err := r.db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM auth.users %s", whereClause), args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, name, email, role, vehicle_type, license_info, created_at, updated_at
		FROM auth.users %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, whereClause, idx, idx+1), args...)
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
