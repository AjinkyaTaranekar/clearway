package service

import (
	"context"
	"database/sql"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/repository"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/errors"
)

type ProfileService struct {
	users *repository.UserRepo
}

func NewProfileService(users *repository.UserRepo) *ProfileService {
	return &ProfileService{users: users}
}

func (s *ProfileService) GetProfile(ctx context.Context, userID string) (*model.User, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("service", "ProfileService.GetProfile").
		Str("user_id", userID).
		Msg("fetching user profile")

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("service", "ProfileService.GetProfile").
				Str("user_id", userID).
				Msg("profile not found")
			return nil, apperrors.NotFound("User not found.")
		}
		log.Error().
			Str("service", "ProfileService.GetProfile").
			Err(err).
			Str("user_id", userID).
			Msg("database error while fetching profile")
		return nil, apperrors.InternalError("database error", err)
	}
	log.Info().
		Str("service", "ProfileService.GetProfile").
		Str("user_id", user.ID).
		Msg("profile fetch completed")
	return user, nil
}

type UpdateProfileInput struct {
	Name        *string
	VehicleType *model.VehicleType
	LicenseInfo *model.LicenseInfo
}

func (s *ProfileService) UpdateProfile(ctx context.Context, userID string, in UpdateProfileInput) (*model.User, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("service", "ProfileService.UpdateProfile").
		Str("user_id", userID).
		Bool("name_updated", in.Name != nil).
		Bool("vehicle_type_updated", in.VehicleType != nil).
		Bool("license_info_updated", in.LicenseInfo != nil).
		Msg("updating user profile")

	user, err := s.users.UpdateProfile(ctx, userID, repository.UpdateProfileInput{
		Name: in.Name, VehicleType: in.VehicleType, LicenseInfo: in.LicenseInfo,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("service", "ProfileService.UpdateProfile").
				Str("user_id", userID).
				Msg("profile update failed: user not found")
			return nil, apperrors.NotFound("User not found.")
		}
		log.Error().
			Str("service", "ProfileService.UpdateProfile").
			Err(err).
			Str("user_id", userID).
			Msg("profile update failed")
		return nil, apperrors.InternalError("failed to update profile", err)
	}
	log.Info().
		Str("service", "ProfileService.UpdateProfile").
		Str("user_id", user.ID).
		Msg("profile update completed")
	return user, nil
}
