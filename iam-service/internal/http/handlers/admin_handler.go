package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/service"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/tracing"
)

type AdminHandler struct{ admin *service.AdminService }

func NewAdminHandler(admin *service.AdminService) *AdminHandler { return &AdminHandler{admin: admin} }

type userListItem struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	Role        string    `json:"role"`
	VehicleType string    `json:"vehicle_type"`
	CreatedAt   time.Time `json:"created_at"`
}

// ListUsers godoc
// @Summary List users
// @Description Returns a paginated user list with optional role filter. Admin only.
// @Tags Admin
// @Produce json
// @Security BearerAuth
// @Param role query string false "Role filter: driver or admin"
// @Param page query int false "Page number"
// @Param limit query int false "Page size (max 100)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} response.Response
// @Failure 401 {object} response.Response
// @Failure 403 {object} response.Response
// @Router /api/v1/admin/auth/users [get]
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "AdminHandler.ListUsers").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("list users request received")

	q := r.URL.Query()
	roleFilter := strings.ToLower(q.Get("role"))
	if roleFilter != "" && roleFilter != "driver" && roleFilter != "admin" {
		log.Warn().
			Str("handler", "AdminHandler.ListUsers").
			Str("role_filter", roleFilter).
			Msg("invalid role filter for list users")
		response.Error(w, apperrors.BadRequest("role must be 'driver' or 'admin'."), traceID)
		return
	}
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 20
	}

	log.Info().
		Str("handler", "AdminHandler.ListUsers").
		Str("role_filter", roleFilter).
		Int("page", page).
		Int("limit", limit).
		Msg("invoking admin service list users")

	users, total, err := h.admin.ListUsers(r.Context(), roleFilter, page, limit)
	if err != nil {
		log.Error().
			Str("handler", "AdminHandler.ListUsers").
			Err(err).
			Str("role_filter", roleFilter).
			Int("page", page).
			Int("limit", limit).
			Msg("admin service list users failed")
		response.Error(w, err, traceID)
		return
	}
	items := make([]userListItem, 0, len(users))
	for _, u := range users {
		items = append(items, userListItem{ID: u.ID, Name: u.Name, Email: u.Email, Role: string(u.Role), VehicleType: string(u.VehicleType), CreatedAt: u.CreatedAt})
	}

	log.Info().
		Str("handler", "AdminHandler.ListUsers").
		Int("result_count", len(items)).
		Int("total", total).
		Msg("list users request completed")
	response.Success(w, map[string]interface{}{"users": items, "pagination": map[string]int{"page": page, "limit": limit, "total": total}}, traceID)
}

// GetUser godoc
// @Summary Get user by ID
// @Description Fetches a single user by ID. Admin only.
// @Tags Admin
// @Produce json
// @Security BearerAuth
// @Param id path string true "User ID"
// @Success 200 {object} userListItem
// @Failure 401 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /api/v1/admin/auth/users/{id} [get]
// GetUser fetches a single user by ID using a direct lookup — no table scan.
func (h *AdminHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	userID := mux.Vars(r)["id"]
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "AdminHandler.GetUser").
		Str("user_id", userID).
		Msg("get user request received")

	u, err := h.admin.GetUser(r.Context(), userID)
	if err != nil {
		log.Error().
			Str("handler", "AdminHandler.GetUser").
			Str("user_id", userID).
			Err(err).
			Msg("admin service get user failed")
		response.Error(w, err, traceID)
		return
	}

	log.Info().
		Str("handler", "AdminHandler.GetUser").
		Str("user_id", u.ID).
		Str("role", string(u.Role)).
		Msg("get user request completed")
	response.Success(w, userListItem{ID: u.ID, Name: u.Name, Email: u.Email, Role: string(u.Role), VehicleType: string(u.VehicleType), CreatedAt: u.CreatedAt}, traceID)
}

// PromoteUser godoc
// @Summary Update a user's role
// @Description Promotes or demotes a user to driver/admin role. Admin only.
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body object true "Payload with user_id and role"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} response.Response
// @Failure 401 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /api/v1/admin/auth/promote [post]
func (h *AdminHandler) PromoteUser(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "AdminHandler.PromoteUser").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("promote user request received")

	var req struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		log.Warn().
			Str("handler", "AdminHandler.PromoteUser").
			Err(err).
			Msg("promote user request validation failed: user_id missing or invalid body")
		response.Error(w, apperrors.BadRequest("user_id is required."), traceID)
		return
	}
	newRole := model.Role(strings.ToLower(req.Role))
	if newRole != model.RoleDriver && newRole != model.RoleAdmin {
		log.Warn().
			Str("handler", "AdminHandler.PromoteUser").
			Str("user_id", req.UserID).
			Str("requested_role", req.Role).
			Msg("promote user request validation failed: invalid role")
		response.Error(w, apperrors.BadRequest("role must be 'driver' or 'admin'."), traceID)
		return
	}

	log.Info().
		Str("handler", "AdminHandler.PromoteUser").
		Str("user_id", req.UserID).
		Str("new_role", string(newRole)).
		Msg("invoking admin service promote user")

	updated, err := h.admin.PromoteUser(r.Context(), req.UserID, newRole)
	if err != nil {
		log.Error().
			Str("handler", "AdminHandler.PromoteUser").
			Str("user_id", req.UserID).
			Str("new_role", string(newRole)).
			Err(err).
			Msg("admin service promote user failed")
		response.Error(w, err, traceID)
		return
	}

	log.Info().
		Str("handler", "AdminHandler.PromoteUser").
		Str("user_id", updated.ID).
		Str("new_role", string(updated.Role)).
		Msg("promote user request completed")
	response.Success(w, map[string]interface{}{"user_id": updated.ID, "new_role": string(updated.Role), "updated_at": updated.UpdatedAt}, traceID)
}

// ForceLogout godoc
// @Summary Force logout user
// @Description Revokes all active refresh sessions for a user. Admin only.
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body object true "Payload with user_id"
// @Success 204
// @Failure 400 {object} response.Response
// @Failure 401 {object} response.Response
// @Failure 403 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /api/v1/admin/auth/force-logout [post]
func (h *AdminHandler) ForceLogout(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "AdminHandler.ForceLogout").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("force logout request received")

	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		log.Warn().
			Str("handler", "AdminHandler.ForceLogout").
			Err(err).
			Msg("force logout validation failed")
		response.Error(w, apperrors.BadRequest("user_id is required."), traceID)
		return
	}

	log.Info().
		Str("handler", "AdminHandler.ForceLogout").
		Str("user_id", req.UserID).
		Msg("invoking admin service force logout")

	if err := h.admin.ForceLogoutUser(r.Context(), req.UserID); err != nil {
		log.Error().
			Str("handler", "AdminHandler.ForceLogout").
			Str("user_id", req.UserID).
			Err(err).
			Msg("admin service force logout failed")
		response.Error(w, err, traceID)
		return
	}

	log.Info().
		Str("handler", "AdminHandler.ForceLogout").
		Str("user_id", req.UserID).
		Msg("force logout request completed")
	response.NoContent(w, traceID)
}
