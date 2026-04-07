package service

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/repository"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/errors"
)

type AdminService struct {
	users  *repository.UserRepo
	tokens *repository.TokenRepo
}

func NewAdminService(users *repository.UserRepo, tokens *repository.TokenRepo) *AdminService {
	return &AdminService{users: users, tokens: tokens}
}

func (s *AdminService) ListUsers(ctx context.Context, roleFilter string, page, limit int) ([]*model.User, int, error) {
	return s.users.List(ctx, roleFilter, page, limit)
}

// GetUser fetches a single user by ID via a direct WHERE id = $1 lookup.
// This replaces the previous O(n) workaround that called ListUsers with limit=10000.
func (s *AdminService) GetUser(ctx context.Context, userID string) (*model.User, error) {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperrors.NotFound("User not found.")
		}
		return nil, apperrors.InternalError("database error", err)
	}
	return user, nil
}

func (s *AdminService) PromoteUser(ctx context.Context, userID string, newRole model.Role) (*model.User, error) {
	target, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, apperrors.NotFound("User not found.")
		}
		return nil, apperrors.InternalError("database error", err)
	}
	if target.Role == model.RoleAdmin && newRole == model.RoleDriver {
		count, err := s.users.CountByRole(ctx, model.RoleAdmin)
		if err != nil {
			return nil, apperrors.InternalError("database error", err)
		}
		if count <= 1 {
			return nil, &apperrors.AppError{Code: "CANNOT_DEMOTE_SOLE_ADMIN", Message: "Cannot demote the only remaining admin.", Status: http.StatusBadRequest}
		}
	}
	return s.users.UpdateRole(ctx, userID, newRole)
}

func (s *AdminService) ForceLogoutUser(ctx context.Context, userID string) error {
	if _, err := s.users.GetByID(ctx, userID); err != nil {
		if err == sql.ErrNoRows {
			return apperrors.NotFound("User not found.")
		}
		return apperrors.InternalError("database error", err)
	}
	return s.tokens.RevokeAllForUser(ctx, userID)
}
