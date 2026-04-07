package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/middleware"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/service"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/tracing"
)

type ProfileHandler struct{ profile *service.ProfileService }

func NewProfileHandler(profile *service.ProfileService) *ProfileHandler {
	return &ProfileHandler{profile: profile}
}

func (h *ProfileHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "ProfileHandler.GetProfile").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("get profile request received")

	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		log.Warn().
			Str("handler", "ProfileHandler.GetProfile").
			Msg("get profile denied: missing auth claims")
		response.Error(w, apperrors.Unauthorized("Not authenticated."), traceID)
		return
	}

	log.Info().
		Str("handler", "ProfileHandler.GetProfile").
		Str("user_id", claims.Sub).
		Msg("invoking profile service get profile")

	user, err := h.profile.GetProfile(r.Context(), claims.Sub)
	if err != nil {
		log.Error().
			Str("handler", "ProfileHandler.GetProfile").
			Str("user_id", claims.Sub).
			Err(err).
			Msg("profile service get profile failed")
		response.Error(w, err, traceID)
		return
	}

	log.Info().
		Str("handler", "ProfileHandler.GetProfile").
		Str("user_id", user.ID).
		Msg("get profile request completed")
	response.Success(w, user, traceID)
}

func (h *ProfileHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "ProfileHandler.UpdateProfile").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("update profile request received")

	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		log.Warn().
			Str("handler", "ProfileHandler.UpdateProfile").
			Msg("update profile denied: missing auth claims")
		response.Error(w, apperrors.Unauthorized("Not authenticated."), traceID)
		return
	}
	var req struct {
		Name        *string            `json:"name"`
		VehicleType *string            `json:"vehicle_type"`
		LicenseInfo *model.LicenseInfo `json:"license_info"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Warn().
			Str("handler", "ProfileHandler.UpdateProfile").
			Str("user_id", claims.Sub).
			Err(err).
			Msg("failed to decode update profile request body")
		response.Error(w, apperrors.BadRequest("Invalid JSON body."), traceID)
		return
	}
	in := service.UpdateProfileInput{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > 100 {
			log.Warn().
				Str("handler", "ProfileHandler.UpdateProfile").
				Str("user_id", claims.Sub).
				Msg("update profile validation failed: invalid name")
			response.Error(w, apperrors.BadRequest("name must be 1-100 characters."), traceID)
			return
		}
		in.Name = &name
	}
	if req.VehicleType != nil {
		vt := model.VehicleType(strings.ToLower(*req.VehicleType))
		if !model.ValidVehicleTypes[vt] {
			log.Warn().
				Str("handler", "ProfileHandler.UpdateProfile").
				Str("user_id", claims.Sub).
				Str("vehicle_type", *req.VehicleType).
				Msg("update profile validation failed: invalid vehicle type")
			response.Error(w, apperrors.BadRequest("vehicle_type must be: car, van, motorcycle, truck."), traceID)
			return
		}
		in.VehicleType = &vt
	}
	if req.LicenseInfo != nil {
		in.LicenseInfo = req.LicenseInfo
	}

	log.Info().
		Str("handler", "ProfileHandler.UpdateProfile").
		Str("user_id", claims.Sub).
		Bool("name_updated", in.Name != nil).
		Bool("vehicle_type_updated", in.VehicleType != nil).
		Bool("license_info_updated", in.LicenseInfo != nil).
		Msg("invoking profile service update profile")

	user, err := h.profile.UpdateProfile(r.Context(), claims.Sub, in)
	if err != nil {
		log.Error().
			Str("handler", "ProfileHandler.UpdateProfile").
			Str("user_id", claims.Sub).
			Err(err).
			Msg("profile service update profile failed")
		response.Error(w, err, traceID)
		return
	}

	log.Info().
		Str("handler", "ProfileHandler.UpdateProfile").
		Str("user_id", user.ID).
		Msg("update profile request completed")
	response.Success(w, user, traceID)
}
