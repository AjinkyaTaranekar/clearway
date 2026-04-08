package service

import (
	"context"
	"database/sql"
	"strings"

	"github.com/google/uuid"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/repository"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/errors"
)

type VehicleService struct {
	vehicles *repository.VehicleRepo
}

func NewVehicleService(vehicles *repository.VehicleRepo) *VehicleService {
	return &VehicleService{vehicles: vehicles}
}

type AddSecondaryVehicleInput struct {
	VehicleType        model.VehicleType
	LicenseInfo        model.LicenseInfo
	IsEmergencyVehicle bool
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

func (s *VehicleService) ListVehicles(ctx context.Context, userID string) ([]model.UserVehicle, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("service", "VehicleService.ListVehicles").
		Str("user_id", userID).
		Msg("listing user vehicles")

	vehicles, err := s.vehicles.ListByUserID(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperrors.NotFound("User not found.")
		}
		return nil, apperrors.InternalError("failed to load vehicles", err)
	}
	return vehicles, nil
}

func (s *VehicleService) AddSecondaryVehicle(ctx context.Context, userID string, in AddSecondaryVehicleInput) (*model.UserVehicle, error) {
	if err := validateVehicleType(in.VehicleType); err != nil {
		return nil, err
	}

	vehicleID := "veh_" + strings.ReplaceAll(uuid.New().String(), "-", "")[:16]
	vehicle, err := s.vehicles.AddSecondary(ctx, userID, vehicleID, in.VehicleType, in.LicenseInfo, in.IsEmergencyVehicle)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperrors.NotFound("User not found.")
		}
		return nil, apperrors.InternalError("failed to add secondary vehicle", err)
	}
	return vehicle, nil
}

func (s *VehicleService) UpdatePrimaryVehicle(ctx context.Context, userID string, in UpdatePrimaryVehicleInput) (*model.UserVehicle, error) {
	if in.VehicleType != nil {
		if err := validateVehicleType(*in.VehicleType); err != nil {
			return nil, err
		}
	}

	vehicle, err := s.vehicles.UpdatePrimary(ctx, userID, repository.UpdatePrimaryVehicleInput{
		VehicleType:        in.VehicleType,
		LicenseInfo:        in.LicenseInfo,
		IsEmergencyVehicle: in.IsEmergencyVehicle,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperrors.NotFound("User not found.")
		}
		return nil, apperrors.InternalError("failed to update primary vehicle", err)
	}
	return vehicle, nil
}

func (s *VehicleService) UpdateSecondaryVehicle(ctx context.Context, userID, vehicleID string, in UpdateSecondaryVehicleInput) (*model.UserVehicle, error) {
	if in.VehicleType != nil {
		if err := validateVehicleType(*in.VehicleType); err != nil {
			return nil, err
		}
	}

	vehicle, err := s.vehicles.UpdateSecondary(ctx, userID, vehicleID, repository.UpdateSecondaryVehicleInput{
		VehicleType:        in.VehicleType,
		LicenseInfo:        in.LicenseInfo,
		IsEmergencyVehicle: in.IsEmergencyVehicle,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperrors.NotFound("Secondary vehicle not found.")
		}
		return nil, apperrors.InternalError("failed to update secondary vehicle", err)
	}
	return vehicle, nil
}

func (s *VehicleService) DeleteSecondaryVehicle(ctx context.Context, userID, vehicleID string) error {
	err := s.vehicles.DeleteSecondary(ctx, userID, vehicleID)
	if err != nil {
		if err == sql.ErrNoRows {
			return apperrors.NotFound("Secondary vehicle not found.")
		}
		return apperrors.InternalError("failed to delete secondary vehicle", err)
	}
	return nil
}

func validateVehicleType(vt model.VehicleType) error {
	if !model.ValidVehicleTypes[vt] {
		return apperrors.BadRequest("vehicle_type must be: car, van, motorcycle, truck.")
	}
	return nil
}
