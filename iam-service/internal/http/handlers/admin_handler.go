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

func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	q := r.URL.Query()
	roleFilter := strings.ToLower(q.Get("role"))
	if roleFilter != "" && roleFilter != "driver" && roleFilter != "admin" {
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
	users, total, err := h.admin.ListUsers(r.Context(), roleFilter, page, limit)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}
	items := make([]userListItem, 0, len(users))
	for _, u := range users {
		items = append(items, userListItem{ID: u.ID, Name: u.Name, Email: u.Email, Role: string(u.Role), VehicleType: string(u.VehicleType), CreatedAt: u.CreatedAt})
	}
	response.Success(w, map[string]interface{}{"users": items, "pagination": map[string]int{"page": page, "limit": limit, "total": total}}, traceID)
}

func (h *AdminHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	userID := mux.Vars(r)["id"]
	users, _, err := h.admin.ListUsers(r.Context(), "", 1, 10000)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}
	for _, u := range users {
		if u.ID == userID {
			response.Success(w, userListItem{ID: u.ID, Name: u.Name, Email: u.Email, Role: string(u.Role), VehicleType: string(u.VehicleType), CreatedAt: u.CreatedAt}, traceID)
			return
		}
	}
	response.Error(w, apperrors.NotFound("User not found."), traceID)
}

func (h *AdminHandler) PromoteUser(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	var req struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		response.Error(w, apperrors.BadRequest("user_id is required."), traceID)
		return
	}
	newRole := model.Role(strings.ToLower(req.Role))
	if newRole != model.RoleDriver && newRole != model.RoleAdmin {
		response.Error(w, apperrors.BadRequest("role must be 'driver' or 'admin'."), traceID)
		return
	}
	updated, err := h.admin.PromoteUser(r.Context(), req.UserID, newRole)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}
	response.Success(w, map[string]interface{}{"user_id": updated.ID, "new_role": string(updated.Role), "updated_at": updated.UpdatedAt}, traceID)
}

func (h *AdminHandler) ForceLogout(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UserID == "" {
		response.Error(w, apperrors.BadRequest("user_id is required."), traceID)
		return
	}
	if err := h.admin.ForceLogoutUser(r.Context(), req.UserID); err != nil {
		response.Error(w, err, traceID)
		return
	}
	response.NoContent(w, traceID)
}
