package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/service"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/tracing"
	"github.com/gorilla/mux"
)

// NotificationHandler handles driver-facing notification endpoints.
type NotificationHandler struct {
	notifRepo service.NotificationRepository
	tokenRepo service.DeviceTokenRepository
	logger    *logger.Logger
}

// NewNotificationHandler creates a new NotificationHandler.
func NewNotificationHandler(
	notifRepo service.NotificationRepository,
	tokenRepo service.DeviceTokenRepository,
	log *logger.Logger,
) *NotificationHandler {
	return &NotificationHandler{
		notifRepo: notifRepo,
		tokenRepo: tokenRepo,
		logger:    log,
	}
}

// List godoc
// @Summary List notifications for the authenticated driver
// @Description Returns a paginated list of notifications for the current driver
// @Tags Notifications
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Results per page" default(20)
// @Param read query bool false "Filter by read status"
// @Param type query string false "Filter by type (info, success, warning, error)"
// @Success 200 {object} model.NotificationListResponse
// @Failure 401 {object} response.Response
// @Router /api/v1/notifications [get]
func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "NotificationHandler.List").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("list notifications request received")

	driverID := GetUserID(r.Context())
	if driverID == "" {
		log.Warn().Str("handler", "NotificationHandler.List").Msg("list notifications denied: missing user id")
		response.Error(w, errors.Unauthorized("Authentication required"), traceID)
		return
	}

	filter := model.NotificationFilter{
		DriverID: driverID,
		Page:     intQuery(r, "page", 1),
		Limit:    intQuery(r, "limit", 20),
	}
	if filter.Limit > 100 {
		filter.Limit = 100
	}
	if filter.Page < 1 {
		filter.Page = 1
	}

	if readStr := r.URL.Query().Get("read"); readStr != "" {
		b, err := strconv.ParseBool(readStr)
		if err == nil {
			filter.ReadFilter = &b
		}
	}
	filter.TypeFilter = r.URL.Query().Get("type")
	log.Info().
		Str("handler", "NotificationHandler.List").
		Str("driver_id", driverID).
		Int("page", filter.Page).
		Int("limit", filter.Limit).
		Str("type_filter", filter.TypeFilter).
		Msg("invoking notification repository list by driver")

	notifications, total, unread, err := h.notifRepo.ListByDriver(r.Context(), filter)
	if err != nil {
		log.Error().
			Str("handler", "NotificationHandler.List").
			Err(err).
			Str("driver_id", driverID).
			Msg("failed to list notifications")
		response.Error(w, errors.InternalError("Failed to list notifications", err), traceID)
		return
	}

	if notifications == nil {
		notifications = []model.Notification{}
	}

	resp := model.NotificationListResponse{
		Notifications: notifications,
		UnreadCount:   unread,
		Total:         total,
		Page:          filter.Page,
		Limit:         filter.Limit,
	}
	log.Info().
		Str("handler", "NotificationHandler.List").
		Str("driver_id", driverID).
		Int("result_count", len(notifications)).
		Int("total", total).
		Int("unread", unread).
		Msg("list notifications request completed")
	response.Success(w, resp, traceID)
}

// MarkRead godoc
// @Summary Mark a single notification as read
// @Tags Notifications
// @Produce json
// @Security BearerAuth
// @Param id path string true "Notification ID"
// @Success 200 {object} model.MarkReadResponse
// @Failure 401 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /api/v1/notifications/{id}/read [put]
func (h *NotificationHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "NotificationHandler.MarkRead").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("mark read request received")

	driverID := GetUserID(r.Context())
	if driverID == "" {
		log.Warn().Str("handler", "NotificationHandler.MarkRead").Msg("mark read denied: missing user id")
		response.Error(w, errors.Unauthorized("Authentication required"), traceID)
		return
	}

	notifID := mux.Vars(r)["id"]
	if notifID == "" {
		log.Warn().
			Str("handler", "NotificationHandler.MarkRead").
			Str("driver_id", driverID).
			Msg("mark read validation failed: notification id missing")
		response.Error(w, errors.BadRequest("Notification ID is required"), traceID)
		return
	}

	log.Info().
		Str("handler", "NotificationHandler.MarkRead").
		Str("driver_id", driverID).
		Str("notification_id", notifID).
		Msg("invoking mark read")

	n, err := h.notifRepo.MarkRead(r.Context(), notifID, driverID)
	if err != nil {
		log.Error().
			Str("handler", "NotificationHandler.MarkRead").
			Err(err).
			Str("driver_id", driverID).
			Str("notification_id", notifID).
			Msg("failed to mark read")
		response.Error(w, errors.InternalError("Failed to mark notification as read", err), traceID)
		return
	}
	if n == nil {
		log.Warn().
			Str("handler", "NotificationHandler.MarkRead").
			Str("driver_id", driverID).
			Str("notification_id", notifID).
			Msg("notification not found for mark read")
		response.Error(w, errors.NotFound("Notification not found"), traceID)
		return
	}

	readAt := time.Now().UTC()
	if n.ReadAt != nil {
		readAt = *n.ReadAt
	}
	resp := model.MarkReadResponse{
		NotificationID: n.ID,
		Read:           true,
		ReadAt:         readAt,
	}
	log.Info().
		Str("handler", "NotificationHandler.MarkRead").
		Str("driver_id", driverID).
		Str("notification_id", n.ID).
		Msg("mark read request completed")
	response.Success(w, resp, traceID)
}

// MarkAllRead godoc
// @Summary Mark all notifications as read
// @Tags Notifications
// @Produce json
// @Security BearerAuth
// @Success 200 {object} model.MarkAllReadResponse
// @Failure 401 {object} response.Response
// @Router /api/v1/notifications/read-all [put]
func (h *NotificationHandler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "NotificationHandler.MarkAllRead").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("mark all read request received")

	driverID := GetUserID(r.Context())
	if driverID == "" {
		log.Warn().Str("handler", "NotificationHandler.MarkAllRead").Msg("mark all read denied: missing user id")
		response.Error(w, errors.Unauthorized("Authentication required"), traceID)
		return
	}

	count, err := h.notifRepo.MarkAllRead(r.Context(), driverID)
	if err != nil {
		log.Error().
			Str("handler", "NotificationHandler.MarkAllRead").
			Err(err).
			Str("driver_id", driverID).
			Msg("failed to mark all read")
		response.Error(w, errors.InternalError("Failed to mark all notifications as read", err), traceID)
		return
	}

	resp := model.MarkAllReadResponse{
		Status:       "ok",
		UpdatedCount: count,
		ReadAt:       time.Now().UTC(),
	}
	log.Info().
		Str("handler", "NotificationHandler.MarkAllRead").
		Str("driver_id", driverID).
		Int("updated_count", count).
		Msg("mark all read request completed")
	response.Success(w, resp, traceID)
}

// RegisterDeviceToken godoc
// @Summary Register or update an FCM device token
// @Tags Notifications
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param body body model.RegisterTokenRequest true "Device token"
// @Success 200 {object} model.RegisterTokenResponse
// @Failure 400 {object} response.Response
// @Failure 401 {object} response.Response
// @Failure 403 {object} response.Response
// @Router /api/v1/notifications/device-token [post]
func (h *NotificationHandler) RegisterDeviceToken(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "NotificationHandler.RegisterDeviceToken").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("register device token request received")

	callerID := GetUserID(r.Context())
	callerRole := GetUserRole(r.Context())
	if callerID == "" {
		log.Warn().Str("handler", "NotificationHandler.RegisterDeviceToken").Msg("register device token denied: missing user id")
		response.Error(w, errors.Unauthorized("Authentication required"), traceID)
		return
	}

	var req model.RegisterTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Warn().
			Str("handler", "NotificationHandler.RegisterDeviceToken").
			Err(err).
			Str("caller_id", callerID).
			Msg("register device token validation failed: invalid body")
		response.Error(w, errors.BadRequest("Invalid request body"), traceID)
		return
	}

	if req.FCMToken == "" {
		log.Warn().
			Str("handler", "NotificationHandler.RegisterDeviceToken").
			Str("caller_id", callerID).
			Msg("register device token validation failed: fcm_token missing")
		response.Error(w, errors.BadRequest("fcm_token is required"), traceID)
		return
	}
	if req.DriverID == "" {
		req.DriverID = callerID
	}

	// Only the owner or admin may register a token for a given driver_id
	if req.DriverID != callerID && callerRole != "admin" {
		log.Warn().
			Str("handler", "NotificationHandler.RegisterDeviceToken").
			Str("caller_id", callerID).
			Str("target_driver_id", req.DriverID).
			Str("caller_role", callerRole).
			Msg("register device token denied: unauthorized target driver")
		response.Error(w, errors.Forbidden("Cannot register token for another driver"), traceID)
		return
	}

	if req.Platform == "" {
		req.Platform = model.PlatformWeb
	}
	if !model.ValidPlatform(req.Platform) {
		log.Warn().
			Str("handler", "NotificationHandler.RegisterDeviceToken").
			Str("driver_id", req.DriverID).
			Str("platform", string(req.Platform)).
			Msg("register device token validation failed: invalid platform")
		response.Error(w, errors.BadRequest("Unsupported platform; must be web, android, or ios"), traceID)
		return
	}

	token := &model.DeviceToken{
		DriverID: req.DriverID,
		FCMToken: req.FCMToken,
		Platform: req.Platform,
	}

	saved, err := h.tokenRepo.Upsert(r.Context(), token)
	if err != nil {
		log.Error().
			Str("handler", "NotificationHandler.RegisterDeviceToken").
			Err(err).
			Str("driver_id", req.DriverID).
			Msg("failed to register device token")
		response.Error(w, errors.InternalError("Failed to register device token", err), traceID)
		return
	}

	resp := model.RegisterTokenResponse{
		Status:        "registered",
		DriverID:      saved.DriverID,
		DeviceTokenID: saved.ID,
		UpdatedAt:     saved.UpdatedAt,
	}
	log.Info().
		Str("handler", "NotificationHandler.RegisterDeviceToken").
		Str("driver_id", saved.DriverID).
		Str("device_token_id", saved.ID).
		Msg("register device token request completed")
	response.Success(w, resp, traceID)
}

// intQuery parses an integer query parameter with a default.
func intQuery(r *http.Request, key string, defaultVal int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 {
		return defaultVal
	}
	return v
}
