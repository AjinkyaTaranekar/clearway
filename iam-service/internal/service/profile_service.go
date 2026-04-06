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
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperrors.NotFound("User not found.")
		}
		return nil, apperrors.InternalError("database error", err)
	}
	return user, nil
}

type UpdateProfileInput struct {
	Name        *string
	VehicleType *model.VehicleType
	LicenseInfo *model.LicenseInfo
}

func (s *ProfileService) UpdateProfile(ctx context.Context, userID string, in UpdateProfileInput) (*model.User, error) {
	user, err := s.users.UpdateProfile(ctx, userID, repository.UpdateProfileInput{
		Name: in.Name, VehicleType: in.VehicleType, LicenseInfo: in.LicenseInfo,
	})
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperrors.NotFound("User not found.")
		}
		return nil, apperrors.InternalError("failed to update profile", err)
	}
	return user, nil
}
