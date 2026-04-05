package handlers

import (
	"net/http"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/service"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/tracing"
)

// AdminHandler handles admin-only notification endpoints.
type AdminHandler struct {
	notifRepo service.NotificationRepository
	logger    *logger.Logger
}

// NewAdminHandler creates a new AdminHandler.
func NewAdminHandler(
	notifRepo service.NotificationRepository,
	log *logger.Logger,
) *AdminHandler {
	return &AdminHandler{
		notifRepo: notifRepo,
		logger:    log,
	}
}

// ListAll godoc
// @Summary List recent notifications across all drivers (admin only)
// @Tags Admin
// @Produce json
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Results per page" default(50)
// @Param type query string false "Filter by notification type"
// @Param delivery_status query string false "Filter by delivery status"
// @Param driver_id query string false "Filter by driver ID"
// @Success 200 {object} model.AdminNotificationListResponse
// @Failure 401 {object} response.Response
// @Failure 403 {object} response.Response
// @Router /api/v1/admin/notifications [get]
func (h *AdminHandler) ListAll(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	role := GetUserRole(r.Context())
	if role != "admin" {
		response.Error(w, errors.Forbidden("Admin access required"), traceID)
		return
	}

	filter := model.NotificationFilter{
		DriverID:       r.URL.Query().Get("driver_id"),
		Page:           intQuery(r, "page", 1),
		Limit:          intQuery(r, "limit", 50),
		TypeFilter:     r.URL.Query().Get("type"),
		DeliveryStatus: r.URL.Query().Get("delivery_status"),
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.Page < 1 {
		filter.Page = 1
	}

	notifications, total, err := h.notifRepo.ListAll(r.Context(), filter)
	if err != nil {
		h.logger.Error().Err(err).Msg("failed to list admin notifications")
		response.Error(w, errors.InternalError("Failed to list notifications", err), traceID)
		return
	}

	if notifications == nil {
		notifications = []model.Notification{}
	}

	resp := model.AdminNotificationListResponse{
		Notifications: notifications,
		Total:         total,
		Page:          filter.Page,
		Limit:         filter.Limit,
	}
	response.Success(w, resp, traceID)
}
