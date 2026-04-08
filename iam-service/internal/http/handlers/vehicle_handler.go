package handlers

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/gorilla/mux"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/middleware"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/service"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/tracing"
)

var vehicleLicenseRegex = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9\- ]{0,28}[A-Za-z0-9]$|^[A-Za-z0-9]{1}$`)

type VehicleHandler struct {
	vehicles *service.VehicleService
}

func NewVehicleHandler(vehicles *service.VehicleService) *VehicleHandler {
	return &VehicleHandler{vehicles: vehicles}
}

func (h *VehicleHandler) ListVehicles(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		response.Error(w, apperrors.Unauthorized("Not authenticated."), traceID)
		return
	}

	vehicles, err := h.vehicles.ListVehicles(r.Context(), claims.Sub)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}

	response.Success(w, vehicles, traceID)
}

func (h *VehicleHandler) AddSecondary(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		response.Error(w, apperrors.Unauthorized("Not authenticated."), traceID)
		return
	}

	var req struct {
		VehicleType        string            `json:"vehicle_type"`
		LicenseInfo        model.LicenseInfo `json:"license_info"`
		IsEmergencyVehicle bool              `json:"is_emergency_vehicle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperrors.BadRequest("Invalid JSON body."), traceID)
		return
	}

	vehicleType, err := normalizeVehicleType(req.VehicleType)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}
	licenseInfo, err := normalizeLicenseInfo(req.LicenseInfo)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}

	vehicle, err := h.vehicles.AddSecondaryVehicle(r.Context(), claims.Sub, service.AddSecondaryVehicleInput{
		VehicleType:        vehicleType,
		LicenseInfo:        licenseInfo,
		IsEmergencyVehicle: req.IsEmergencyVehicle,
	})
	if err != nil {
		response.Error(w, err, traceID)
		return
	}

	response.Success(w, vehicle, traceID)
}

func (h *VehicleHandler) UpdatePrimary(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		response.Error(w, apperrors.Unauthorized("Not authenticated."), traceID)
		return
	}

	var req struct {
		VehicleType        *string            `json:"vehicle_type"`
		LicenseInfo        *model.LicenseInfo `json:"license_info"`
		IsEmergencyVehicle *bool              `json:"is_emergency_vehicle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperrors.BadRequest("Invalid JSON body."), traceID)
		return
	}
	if req.VehicleType == nil && req.LicenseInfo == nil && req.IsEmergencyVehicle == nil {
		response.Error(w, apperrors.BadRequest("At least one field must be provided."), traceID)
		return
	}

	in := service.UpdatePrimaryVehicleInput{IsEmergencyVehicle: req.IsEmergencyVehicle}
	if req.VehicleType != nil {
		vehicleType, err := normalizeVehicleType(*req.VehicleType)
		if err != nil {
			response.Error(w, err, traceID)
			return
		}
		in.VehicleType = &vehicleType
	}
	if req.LicenseInfo != nil {
		licenseInfo, err := normalizeLicenseInfo(*req.LicenseInfo)
		if err != nil {
			response.Error(w, err, traceID)
			return
		}
		in.LicenseInfo = &licenseInfo
	}

	vehicle, err := h.vehicles.UpdatePrimaryVehicle(r.Context(), claims.Sub, in)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}

	response.Success(w, vehicle, traceID)
}

func (h *VehicleHandler) UpdateSecondary(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		response.Error(w, apperrors.Unauthorized("Not authenticated."), traceID)
		return
	}

	vehicleID := strings.TrimSpace(mux.Vars(r)["id"])
	if vehicleID == "" {
		response.Error(w, apperrors.BadRequest("Secondary vehicle id is required."), traceID)
		return
	}

	var req struct {
		VehicleType        *string            `json:"vehicle_type"`
		LicenseInfo        *model.LicenseInfo `json:"license_info"`
		IsEmergencyVehicle *bool              `json:"is_emergency_vehicle"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, apperrors.BadRequest("Invalid JSON body."), traceID)
		return
	}
	if req.VehicleType == nil && req.LicenseInfo == nil && req.IsEmergencyVehicle == nil {
		response.Error(w, apperrors.BadRequest("At least one field must be provided."), traceID)
		return
	}

	in := service.UpdateSecondaryVehicleInput{IsEmergencyVehicle: req.IsEmergencyVehicle}
	if req.VehicleType != nil {
		vehicleType, err := normalizeVehicleType(*req.VehicleType)
		if err != nil {
			response.Error(w, err, traceID)
			return
		}
		in.VehicleType = &vehicleType
	}
	if req.LicenseInfo != nil {
		licenseInfo, err := normalizeLicenseInfo(*req.LicenseInfo)
		if err != nil {
			response.Error(w, err, traceID)
			return
		}
		in.LicenseInfo = &licenseInfo
	}

	vehicle, err := h.vehicles.UpdateSecondaryVehicle(r.Context(), claims.Sub, vehicleID, in)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}

	response.Success(w, vehicle, traceID)
}

func (h *VehicleHandler) DeleteSecondary(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		response.Error(w, apperrors.Unauthorized("Not authenticated."), traceID)
		return
	}

	vehicleID := strings.TrimSpace(mux.Vars(r)["id"])
	if vehicleID == "" {
		response.Error(w, apperrors.BadRequest("Secondary vehicle id is required."), traceID)
		return
	}

	if err := h.vehicles.DeleteSecondaryVehicle(r.Context(), claims.Sub, vehicleID); err != nil {
		response.Error(w, err, traceID)
		return
	}

	response.NoContent(w, traceID)
}

func normalizeVehicleType(raw string) (model.VehicleType, error) {
	vehicleType := model.VehicleType(strings.ToLower(strings.TrimSpace(raw)))
	if !model.ValidVehicleTypes[vehicleType] {
		return "", apperrors.BadRequest("vehicle_type must be: car, van, motorcycle, truck.")
	}
	return vehicleType, nil
}

func normalizeLicenseInfo(in model.LicenseInfo) (model.LicenseInfo, error) {
	in.LicenseNumber = strings.TrimSpace(in.LicenseNumber)
	if in.LicenseNumber == "" {
		return model.LicenseInfo{}, apperrors.BadRequest("license_info.license_number is required.")
	}
	if len(in.LicenseNumber) > 30 || !vehicleLicenseRegex.MatchString(in.LicenseNumber) {
		return model.LicenseInfo{}, apperrors.BadRequest("license_info.license_number must be 1-30 alphanumeric characters (hyphens and spaces allowed).")
	}
	return in, nil
}
