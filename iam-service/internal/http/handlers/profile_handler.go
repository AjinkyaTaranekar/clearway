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
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		response.Error(w, apperrors.Unauthorized("Not authenticated."), traceID)
		return
	}
	user, err := h.profile.GetProfile(r.Context(), claims.Sub)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}
	response.Success(w, user, traceID)
}

func (h *ProfileHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		response.Error(w, apperrors.Unauthorized("Not authenticated."), traceID)
		return
	}
	var req struct {
		Name        *string            `json:"name"`
		VehicleType *string            `json:"vehicle_type"`
		LicenseInfo *model.LicenseInfo `json:"license_info"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperrors.BadRequest("Invalid JSON body."), traceID)
		return
	}
	in := service.UpdateProfileInput{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" || len(name) > 100 {
			response.Error(w, apperrors.BadRequest("name must be 1-100 characters."), traceID)
			return
		}
		in.Name = &name
	}
	if req.VehicleType != nil {
		vt := model.VehicleType(strings.ToLower(*req.VehicleType))
		if !model.ValidVehicleTypes[vt] {
			response.Error(w, apperrors.BadRequest("vehicle_type must be: car, van, motorcycle, truck."), traceID)
			return
		}
		in.VehicleType = &vt
	}
	if req.LicenseInfo != nil {
		in.LicenseInfo = req.LicenseInfo
	}
	user, err := h.profile.UpdateProfile(r.Context(), claims.Sub, in)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}
	response.Success(w, user, traceID)
}
