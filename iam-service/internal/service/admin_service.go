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
	log := logWithTrace(ctx)
	log.Info().
		Str("service", "AdminService.ListUsers").
		Str("role_filter", roleFilter).
		Int("page", page).
		Int("limit", limit).
		Msg("listing users")

	users, total, err := s.users.List(ctx, roleFilter, page, limit)
	if err != nil {
		log.Error().
			Str("service", "AdminService.ListUsers").
			Err(err).
			Str("role_filter", roleFilter).
			Int("page", page).
			Int("limit", limit).
			Msg("failed to list users")
		return nil, 0, err
	}

	log.Info().
		Str("service", "AdminService.ListUsers").
		Int("result_count", len(users)).
		Int("total", total).
		Msg("list users completed")
	return users, total, nil
}

// GetUser fetches a single user by ID via a direct WHERE id = $1 lookup.
// This replaces the previous O(n) workaround that called ListUsers with limit=10000.
func (s *AdminService) GetUser(ctx context.Context, userID string) (*model.User, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("service", "AdminService.GetUser").
		Str("user_id", userID).
		Msg("getting user by id")

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("service", "AdminService.GetUser").
				Str("user_id", userID).
				Msg("user not found")
			return nil, apperrors.NotFound("User not found.")
		}
		log.Error().
			Str("service", "AdminService.GetUser").
			Err(err).
			Str("user_id", userID).
			Msg("database error while getting user")
		return nil, apperrors.InternalError("database error", err)
	}
	log.Info().
		Str("service", "AdminService.GetUser").
		Str("user_id", user.ID).
		Str("role", string(user.Role)).
		Msg("get user completed")
	return user, nil
}

func (s *AdminService) PromoteUser(ctx context.Context, userID string, newRole model.Role) (*model.User, error) {
	log := logWithTrace(ctx)
	log.Info().
		Str("service", "AdminService.PromoteUser").
		Str("user_id", userID).
		Str("new_role", string(newRole)).
		Msg("starting role update flow")

	target, err := s.users.GetByID(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("service", "AdminService.PromoteUser").
				Str("user_id", userID).
				Msg("role update failed: user not found")
			return nil, apperrors.NotFound("User not found.")
		}
		log.Error().
			Str("service", "AdminService.PromoteUser").
			Err(err).
			Str("user_id", userID).
			Msg("database error loading target user")
		return nil, apperrors.InternalError("database error", err)
	}
	if target.Role == model.RoleAdmin && newRole == model.RoleDriver {
		log.Debug().
			Str("service", "AdminService.PromoteUser").
			Str("user_id", userID).
			Msg("validating sole-admin demotion guard")

		count, err := s.users.CountByRole(ctx, model.RoleAdmin)
		if err != nil {
			log.Error().
				Str("service", "AdminService.PromoteUser").
				Err(err).
				Msg("failed to count admin users")
			return nil, apperrors.InternalError("database error", err)
		}
		if count <= 1 {
			log.Warn().
				Str("service", "AdminService.PromoteUser").
				Str("user_id", userID).
				Msg("blocked demotion of sole remaining admin")
			return nil, &apperrors.AppError{Code: "CANNOT_DEMOTE_SOLE_ADMIN", Message: "Cannot demote the only remaining admin.", Status: http.StatusBadRequest}
		}
	}
	updated, err := s.users.UpdateRole(ctx, userID, newRole)
	if err != nil {
		log.Error().
			Str("service", "AdminService.PromoteUser").
			Err(err).
			Str("user_id", userID).
			Str("new_role", string(newRole)).
			Msg("failed to persist user role update")
		return nil, err
	}

	log.Info().
		Str("service", "AdminService.PromoteUser").
		Str("user_id", updated.ID).
		Str("new_role", string(updated.Role)).
		Msg("role update completed")
	return updated, nil
}

func (s *AdminService) ForceLogoutUser(ctx context.Context, userID string) error {
	log := logWithTrace(ctx)
	log.Info().
		Str("service", "AdminService.ForceLogoutUser").
		Str("user_id", userID).
		Msg("starting force logout for user")

	if _, err := s.users.GetByID(ctx, userID); err != nil {
		if err == sql.ErrNoRows {
			log.Warn().
				Str("service", "AdminService.ForceLogoutUser").
				Str("user_id", userID).
				Msg("force logout failed: user not found")
			return apperrors.NotFound("User not found.")
		}
		log.Error().
			Str("service", "AdminService.ForceLogoutUser").
			Err(err).
			Str("user_id", userID).
			Msg("database error loading user before force logout")
		return apperrors.InternalError("database error", err)
	}
	if err := s.tokens.RevokeAllForUser(ctx, userID); err != nil {
		log.Error().
			Str("service", "AdminService.ForceLogoutUser").
			Err(err).
			Str("user_id", userID).
			Msg("failed to revoke user tokens")
		return err
	}

	log.Info().
		Str("service", "AdminService.ForceLogoutUser").
		Str("user_id", userID).
		Msg("force logout completed")
	return nil
}
