package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/model"
)

// VehicleRepo persists driver-owned vehicles.
type VehicleRepo struct {
	master *sql.DB
}

func NewVehicleRepo(master *sql.DB) *VehicleRepo {
	return &VehicleRepo{master: master}
}

type UpdatePrimaryVehicleInput struct {
	VehicleType        *model.VehicleType
	LicenseInfo        *model.LicenseInfo
	IsEmergencyVehicle *bool
}

type UpdateSecondaryVehicleInput struct {
	VehicleType        *model.VehicleType
	LicenseInfo        *model.LicenseInfo
	IsEmergencyVehicle *bool
}

func (r *VehicleRepo) ListByUserID(ctx context.Context, userID string) ([]model.UserVehicle, error) {
	log := logWithTrace(ctx)
	log.Debug().
		Str("repository", "VehicleRepo.ListByUserID").
		Str("user_id", userID).
		Msg("loading user vehicles")

	primary, err := r.getPrimaryByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	secondary, err := r.listSecondaryByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	vehicles := make([]model.UserVehicle, 0, 1+len(secondary))
	vehicles = append(vehicles, *primary)
	vehicles = append(vehicles, secondary...)

	log.Debug().
		Str("repository", "VehicleRepo.ListByUserID").
		Str("user_id", userID).
		Int("vehicle_count", len(vehicles)).
		Msg("loaded user vehicles")
	return vehicles, nil
}

func (r *VehicleRepo) AddSecondary(ctx context.Context, userID, vehicleID string, vehicleType model.VehicleType, licenseInfo model.LicenseInfo, isEmergency bool) (*model.UserVehicle, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "VehicleRepo.AddSecondary").
		Str("user_id", userID).
		Str("vehicle_id", vehicleID).
		Str("vehicle_type", string(vehicleType)).
		Bool("is_emergency_vehicle", isEmergency).
		Msg("adding secondary vehicle")

	liJSON, err := json.Marshal(licenseInfo)
	if err != nil {
		return nil, fmt.Errorf("marshal license_info: %w", err)
	}

	const query = `
		INSERT INTO auth.user_secondary_vehicles (
			id, user_id, vehicle_type, license_info, is_emergency_vehicle, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
		RETURNING id, vehicle_type, license_info, is_emergency_vehicle, created_at, updated_at`

	vehicle, err := scanSecondaryVehicle(r.master.QueryRowContext(ctx, query, vehicleID, userID, string(vehicleType), liJSON, isEmergency))
	if err != nil {
		return nil, err
	}
	return vehicle, nil
}

func (r *VehicleRepo) UpdatePrimary(ctx context.Context, userID string, in UpdatePrimaryVehicleInput) (*model.UserVehicle, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "VehicleRepo.UpdatePrimary").
		Str("user_id", userID).
		Bool("vehicle_type_updated", in.VehicleType != nil).
		Bool("license_info_updated", in.LicenseInfo != nil).
		Bool("is_emergency_updated", in.IsEmergencyVehicle != nil).
		Msg("updating primary vehicle")

	setClauses := make([]string, 0, 4)
	args := make([]interface{}, 0, 4)
	idx := 1

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
	if in.IsEmergencyVehicle != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_emergency_vehicle = $%d", idx))
		args = append(args, *in.IsEmergencyVehicle)
		idx++
	}

	if len(setClauses) == 0 {
		return r.getPrimaryByUserID(ctx, userID)
	}

	args = append(args, userID)
	query := fmt.Sprintf(`
		UPDATE auth.users
		SET %s, updated_at = NOW()
		WHERE id = $%d
		RETURNING id, vehicle_type, license_info, is_emergency_vehicle, created_at, updated_at`,
		strings.Join(setClauses, ", "), idx,
	)

	return scanPrimaryVehicle(r.master.QueryRowContext(ctx, query, args...))
}

func (r *VehicleRepo) UpdateSecondary(ctx context.Context, userID, vehicleID string, in UpdateSecondaryVehicleInput) (*model.UserVehicle, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "VehicleRepo.UpdateSecondary").
		Str("user_id", userID).
		Str("vehicle_id", vehicleID).
		Bool("vehicle_type_updated", in.VehicleType != nil).
		Bool("license_info_updated", in.LicenseInfo != nil).
		Bool("is_emergency_updated", in.IsEmergencyVehicle != nil).
		Msg("updating secondary vehicle")

	setClauses := make([]string, 0, 4)
	args := make([]interface{}, 0, 6)
	idx := 1

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
	if in.IsEmergencyVehicle != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_emergency_vehicle = $%d", idx))
		args = append(args, *in.IsEmergencyVehicle)
		idx++
	}

	if len(setClauses) == 0 {
		return r.getSecondaryByID(ctx, userID, vehicleID)
	}

	args = append(args, userID, vehicleID)
	query := fmt.Sprintf(`
		UPDATE auth.user_secondary_vehicles
		SET %s, updated_at = NOW()
		WHERE user_id = $%d AND id = $%d
		RETURNING id, vehicle_type, license_info, is_emergency_vehicle, created_at, updated_at`,
		strings.Join(setClauses, ", "), idx, idx+1,
	)

	return scanSecondaryVehicle(r.master.QueryRowContext(ctx, query, args...))
}

func (r *VehicleRepo) DeleteSecondary(ctx context.Context, userID, vehicleID string) error {
	log := logWithTrace(ctx)
	log.Info().
		Str("repository", "VehicleRepo.DeleteSecondary").
		Str("user_id", userID).
		Str("vehicle_id", vehicleID).
		Msg("deleting secondary vehicle")

	const query = `DELETE FROM auth.user_secondary_vehicles WHERE user_id = $1 AND id = $2`
	result, err := r.master.ExecContext(ctx, query, userID, vehicleID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *VehicleRepo) getPrimaryByUserID(ctx context.Context, userID string) (*model.UserVehicle, error) {
	const query = `
		SELECT id, vehicle_type, license_info, is_emergency_vehicle, created_at, updated_at
		FROM auth.users
		WHERE id = $1`
	return scanPrimaryVehicle(r.master.QueryRowContext(ctx, query, userID))
}

func (r *VehicleRepo) listSecondaryByUserID(ctx context.Context, userID string) ([]model.UserVehicle, error) {
	const query = `
		SELECT id, vehicle_type, license_info, is_emergency_vehicle, created_at, updated_at
		FROM auth.user_secondary_vehicles
		WHERE user_id = $1
		ORDER BY created_at ASC`

	rows, err := r.master.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	vehicles := make([]model.UserVehicle, 0)
	for rows.Next() {
		var vehicle model.UserVehicle
		var liRaw []byte
		if err := rows.Scan(
			&vehicle.ID,
			&vehicle.VehicleType,
			&liRaw,
			&vehicle.IsEmergencyVehicle,
			&vehicle.CreatedAt,
			&vehicle.UpdatedAt,
		); err != nil {
			return nil, err
		}
		li, err := model.LicenseInfoFromJSON(liRaw)
		if err != nil {
			return nil, err
		}
		vehicle.LicenseInfo = li
		vehicle.IsPrimary = false
		vehicles = append(vehicles, vehicle)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return vehicles, nil
}

func (r *VehicleRepo) getSecondaryByID(ctx context.Context, userID, vehicleID string) (*model.UserVehicle, error) {
	const query = `
		SELECT id, vehicle_type, license_info, is_emergency_vehicle, created_at, updated_at
		FROM auth.user_secondary_vehicles
		WHERE user_id = $1 AND id = $2`
	return scanSecondaryVehicle(r.master.QueryRowContext(ctx, query, userID, vehicleID))
}

func scanPrimaryVehicle(row *sql.Row) (*model.UserVehicle, error) {
	var ignoredID string
	var vehicle model.UserVehicle
	var liRaw []byte
	if err := row.Scan(
		&ignoredID,
		&vehicle.VehicleType,
		&liRaw,
		&vehicle.IsEmergencyVehicle,
		&vehicle.CreatedAt,
		&vehicle.UpdatedAt,
	); err != nil {
		return nil, err
	}
	li, err := model.LicenseInfoFromJSON(liRaw)
	if err != nil {
		return nil, err
	}
	vehicle.ID = "primary"
	vehicle.IsPrimary = true
	vehicle.LicenseInfo = li
	return &vehicle, nil
}

func scanSecondaryVehicle(row *sql.Row) (*model.UserVehicle, error) {
	var vehicle model.UserVehicle
	var liRaw []byte
	if err := row.Scan(
		&vehicle.ID,
		&vehicle.VehicleType,
		&liRaw,
		&vehicle.IsEmergencyVehicle,
		&vehicle.CreatedAt,
		&vehicle.UpdatedAt,
	); err != nil {
		return nil, err
	}
	li, err := model.LicenseInfoFromJSON(liRaw)
	if err != nil {
		return nil, err
	}
	vehicle.IsPrimary = false
	vehicle.LicenseInfo = li
	return &vehicle, nil
}
