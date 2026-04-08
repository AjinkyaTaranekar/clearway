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
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "UserRepo.Create").
		Str("user_id", id).
		Str("email", email).
		Str("role", string(role)).
		Str("vehicle_type", string(vehicleType)).
		Msg("inserting user record")

	liJSON, err := json.Marshal(licenseInfo)
	if err != nil {
		log.Error().
			Str("repository", "UserRepo.Create").
			Err(err).
			Str("user_id", id).
			Msg("failed to marshal license info")
		return nil, fmt.Errorf("marshal license_info: %w", err)
	}
	const query = `
		INSERT INTO auth.users (id, name, email, email_lower, password_hash, role, vehicle_type, license_info, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		RETURNING id, name, email, role, vehicle_type, license_info, created_at, updated_at`
	user, err := scanUser(r.master.QueryRowContext(ctx, query, id, name, email, emailLower, passwordHash, string(role), string(vehicleType), liJSON))
	if err != nil {
		log.Error().
			Str("repository", "UserRepo.Create").
			Err(err).
			Str("user_id", id).
			Msg("failed to insert user record")
		return nil, err
	}

	log.Info().
		Str("repository", "UserRepo.Create").
		Str("user_id", user.ID).
		Msg("user record inserted")
	return user, nil
}

// CreateTx inserts a new user row inside an active transaction.
func (r *UserRepo) CreateTx(ctx context.Context, tx *sql.Tx, id, name, email, emailLower, passwordHash string, role model.Role, vehicleType model.VehicleType, licenseInfo model.LicenseInfo) (*model.User, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "UserRepo.CreateTx").
		Str("user_id", id).
		Str("email", email).
		Msg("inserting user record inside transaction")

	liJSON, err := json.Marshal(licenseInfo)
	if err != nil {
		log.Error().
			Str("repository", "UserRepo.CreateTx").
			Err(err).
			Str("user_id", id).
			Msg("failed to marshal license info")
		return nil, fmt.Errorf("marshal license_info: %w", err)
	}
	const query = `
		INSERT INTO auth.users (id, name, email, email_lower, password_hash, role, vehicle_type, license_info, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		RETURNING id, name, email, role, vehicle_type, license_info, created_at, updated_at`
	user, err := scanUser(tx.QueryRowContext(ctx, query, id, name, email, emailLower, passwordHash, string(role), string(vehicleType), liJSON))
	if err != nil {
		log.Error().
			Str("repository", "UserRepo.CreateTx").
			Err(err).
			Str("user_id", id).
			Msg("failed to insert user record in transaction")
		return nil, err
	}

	log.Info().
		Str("repository", "UserRepo.CreateTx").
		Str("user_id", user.ID).
		Msg("user record inserted in transaction")
	return user, nil
}

// GetByEmail looks up a user by lowercased email and returns the user plus the
// stored bcrypt hash. Uses master to avoid replication-lag failures immediately
// after registration.
func (r *UserRepo) GetByEmail(ctx context.Context, emailLower string) (*model.User, string, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "UserRepo.GetByEmail").
		Str("email_lower", emailLower).
		Msg("querying user by normalized email")

	const query = `
		SELECT id, name, email, role, vehicle_type, license_info, created_at, updated_at, password_hash
		FROM auth.users WHERE email_lower = $1`
	row := r.master.QueryRowContext(ctx, query, emailLower)
	var u model.User
	var liRaw []byte
	var hash string
	if err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.VehicleType, &liRaw, &u.CreatedAt, &u.UpdatedAt, &hash); err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("repository", "UserRepo.GetByEmail").
				Str("email_lower", emailLower).
				Msg("no user found by normalized email")
		} else {
			log.Error().
				Str("repository", "UserRepo.GetByEmail").
				Err(err).
				Str("email_lower", emailLower).
				Msg("failed to scan user row by normalized email")
		}
		return nil, "", err
	}
	var err error
	u.LicenseInfo, err = model.LicenseInfoFromJSON(liRaw)
	if err != nil {
		log.Error().
			Str("repository", "UserRepo.GetByEmail").
			Err(err).
			Str("user_id", u.ID).
			Msg("failed to decode license info json")
		return nil, "", err
	}
	log.Debug().
		Str("repository", "UserRepo.GetByEmail").
		Str("user_id", u.ID).
		Msg("user loaded by normalized email")
	return &u, hash, nil
}

// GetByID fetches a user by ID using the master pool (safe for auth hot-path).
func (r *UserRepo) GetByID(ctx context.Context, id string) (*model.User, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "UserRepo.GetByID").
		Str("user_id", id).
		Msg("querying user by id")

	const query = `
		SELECT id, name, email, role, vehicle_type, license_info, created_at, updated_at
		FROM auth.users WHERE id = $1`
	user, err := scanUser(r.master.QueryRowContext(ctx, query, id))
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("repository", "UserRepo.GetByID").
				Str("user_id", id).
				Msg("user not found by id")
		} else {
			log.Error().
				Str("repository", "UserRepo.GetByID").
				Err(err).
				Str("user_id", id).
				Msg("failed to query user by id")
		}
		return nil, err
	}

	log.Debug().
		Str("repository", "UserRepo.GetByID").
		Str("user_id", user.ID).
		Msg("user loaded by id")
	return user, nil
}

// GetByIDTx fetches a user by ID inside an active transaction.
func (r *UserRepo) GetByIDTx(ctx context.Context, tx *sql.Tx, id string) (*model.User, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "UserRepo.GetByIDTx").
		Str("user_id", id).
		Msg("querying user by id inside transaction")

	const query = `
		SELECT id, name, email, role, vehicle_type, license_info, created_at, updated_at
		FROM auth.users WHERE id = $1`
	user, err := scanUser(tx.QueryRowContext(ctx, query, id))
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("repository", "UserRepo.GetByIDTx").
				Str("user_id", id).
				Msg("user not found by id in transaction")
		} else {
			log.Error().
				Str("repository", "UserRepo.GetByIDTx").
				Err(err).
				Str("user_id", id).
				Msg("failed to query user by id in transaction")
		}
		return nil, err
	}

	log.Debug().
		Str("repository", "UserRepo.GetByIDTx").
		Str("user_id", user.ID).
		Msg("user loaded by id in transaction")
	return user, nil
}

// UpdateProfileInput carries optional fields for a partial profile update.
type UpdateProfileInput struct {
	Name        *string
	VehicleType *model.VehicleType
	LicenseInfo *model.LicenseInfo
}

// UpdateProfile applies a partial update and returns the updated record.
func (r *UserRepo) UpdateProfile(ctx context.Context, userID string, in UpdateProfileInput) (*model.User, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "UserRepo.UpdateProfile").
		Str("user_id", userID).
		Bool("name_updated", in.Name != nil).
		Bool("vehicle_type_updated", in.VehicleType != nil).
		Bool("license_info_updated", in.LicenseInfo != nil).
		Msg("updating user profile fields")

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
		log.Debug().
			Str("repository", "UserRepo.UpdateProfile").
			Str("user_id", userID).
			Msg("no profile fields provided; returning current user")
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
	user, err := scanUser(r.master.QueryRowContext(ctx, query, args...))
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("repository", "UserRepo.UpdateProfile").
				Str("user_id", userID).
				Msg("profile update affected no rows")
		} else {
			log.Error().
				Str("repository", "UserRepo.UpdateProfile").
				Err(err).
				Str("user_id", userID).
				Msg("failed to update user profile")
		}
		return nil, err
	}

	log.Info().
		Str("repository", "UserRepo.UpdateProfile").
		Str("user_id", user.ID).
		Msg("user profile updated")
	return user, nil
}

// UpdateRole sets a user's role and returns the updated record.
func (r *UserRepo) UpdateRole(ctx context.Context, userID string, role model.Role) (*model.User, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "UserRepo.UpdateRole").
		Str("user_id", userID).
		Str("new_role", string(role)).
		Msg("updating user role")

	const query = `
		UPDATE auth.users SET role = $1, updated_at = NOW()
		WHERE id = $2
		RETURNING id, name, email, role, vehicle_type, license_info, created_at, updated_at`
	user, err := scanUser(r.master.QueryRowContext(ctx, query, string(role), userID))
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("repository", "UserRepo.UpdateRole").
				Str("user_id", userID).
				Msg("role update affected no rows")
		} else {
			log.Error().
				Str("repository", "UserRepo.UpdateRole").
				Err(err).
				Str("user_id", userID).
				Msg("failed to update user role")
		}
		return nil, err
	}

	log.Info().
		Str("repository", "UserRepo.UpdateRole").
		Str("user_id", user.ID).
		Str("new_role", string(user.Role)).
		Msg("user role updated")
	return user, nil
}

// CountByRole returns the number of users with the given role.
// Runs on the slave pool - safe for admin analytics where ~100 ms lag is acceptable.
func (r *UserRepo) CountByRole(ctx context.Context, role model.Role) (int, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "UserRepo.CountByRole").
		Str("role", string(role)).
		Msg("counting users by role")

	var count int
	err := r.slave.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth.users WHERE role = $1`, string(role)).Scan(&count)
	if err != nil {
		log.Error().
			Str("repository", "UserRepo.CountByRole").
			Err(err).
			Str("role", string(role)).
			Msg("failed to count users by role")
		return 0, err
	}

	log.Debug().
		Str("repository", "UserRepo.CountByRole").
		Str("role", string(role)).
		Int("count", count).
		Msg("counted users by role")
	return count, err
}

// List returns a paginated, optionally role-filtered list of users.
// Runs on the slave pool - admin listings are not latency-critical.
func (r *UserRepo) List(ctx context.Context, roleFilter string, page, limit int) ([]*model.User, int, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "UserRepo.List").
		Str("role_filter", roleFilter).
		Int("page", page).
		Int("limit", limit).
		Msg("listing paginated users")

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
		log.Error().
			Str("repository", "UserRepo.List").
			Err(err).
			Str("role_filter", roleFilter).
			Msg("failed to count users for list")
		return nil, 0, err
	}

	args = append(args, limit, offset)
	rows, err := r.slave.QueryContext(ctx, fmt.Sprintf(`
		SELECT id, name, email, role, vehicle_type, license_info, created_at, updated_at
		FROM auth.users %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		whereClause, idx, idx+1), args...)
	if err != nil {
		log.Error().
			Str("repository", "UserRepo.List").
			Err(err).
			Str("role_filter", roleFilter).
			Msg("failed to query users for list")
		return nil, 0, err
	}
	defer rows.Close()

	var users []*model.User
	for rows.Next() {
		var u model.User
		var liRaw []byte
		if err := rows.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.VehicleType, &liRaw, &u.CreatedAt, &u.UpdatedAt); err != nil {
			log.Error().
				Str("repository", "UserRepo.List").
				Err(err).
				Msg("failed to scan user row")
			return nil, 0, err
		}
		u.LicenseInfo, _ = model.LicenseInfoFromJSON(liRaw)
		users = append(users, &u)
	}
	if err := rows.Err(); err != nil {
		log.Error().
			Str("repository", "UserRepo.List").
			Err(err).
			Msg("row iteration failed during user listing")
		return nil, 0, err
	}

	log.Info().
		Str("repository", "UserRepo.List").
		Int("result_count", len(users)).
		Int("total", total).
		Msg("user listing completed")
	return users, total, nil
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
