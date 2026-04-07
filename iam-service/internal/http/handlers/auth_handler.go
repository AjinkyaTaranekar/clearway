package handlers

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/middleware"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/service"
	apperrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/tracing"
)

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

type AuthHandler struct{ auth *service.AuthService }

func NewAuthHandler(auth *service.AuthService) *AuthHandler { return &AuthHandler{auth: auth} }

type registerRequest struct {
	Name        string            `json:"name"`
	Email       string            `json:"email"`
	Password    string            `json:"password"`
	VehicleType string            `json:"vehicle_type"`
	LicenseInfo model.LicenseInfo `json:"license_info"`
}

type authResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	User         struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Email       string `json:"email"`
		Role        string `json:"role"`
		VehicleType string `json:"vehicle_type"`
	} `json:"user"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "AuthHandler.Register").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("register request received")

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Warn().
			Str("handler", "AuthHandler.Register").
			Err(err).
			Msg("failed to decode register request body")
		response.Error(w, apperrors.BadRequest("Invalid JSON body."), traceID)
		return
	}
	type fe struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	}
	var errs []fe
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 100 {
		errs = append(errs, fe{"name", "Name is required (max 100 chars)."})
	}
	req.Email = strings.TrimSpace(req.Email)
	if !emailRegex.MatchString(req.Email) {
		errs = append(errs, fe{"email", "Invalid email format."})
	}
	if !validPassword(req.Password) {
		errs = append(errs, fe{"password", "Min 8 chars, at least 1 letter and 1 digit."})
	}
	vt := model.VehicleType(strings.ToLower(req.VehicleType))
	if !model.ValidVehicleTypes[vt] {
		errs = append(errs, fe{"vehicle_type", "Must be: car, van, motorcycle, or truck."})
	}
	if req.LicenseInfo.LicenseNumber == "" {
		errs = append(errs, fe{"license_info.license_number", "Required."})
	}
	if len(errs) > 0 {
		log.Warn().
			Str("handler", "AuthHandler.Register").
			Int("validation_error_count", len(errs)).
			Str("email", req.Email).
			Msg("register request validation failed")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": map[string]interface{}{"code": "VALIDATION_ERROR", "message": "One or more fields are invalid.", "details": errs}}) //nolint:errcheck
		return
	}

	log.Info().
		Str("handler", "AuthHandler.Register").
		Str("email", req.Email).
		Str("vehicle_type", string(vt)).
		Msg("register request validated, invoking auth service")

	result, err := h.auth.Register(r.Context(), service.RegisterInput{Name: req.Name, Email: req.Email, Password: req.Password, VehicleType: vt, LicenseInfo: req.LicenseInfo}, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		log.Error().
			Str("handler", "AuthHandler.Register").
			Err(err).
			Str("email", req.Email).
			Msg("auth service register failed")
		response.Error(w, err, traceID)
		return
	}

	log.Info().
		Str("handler", "AuthHandler.Register").
		Str("user_id", result.User.ID).
		Str("role", string(result.User.Role)).
		Msg("register request completed")
	response.JSON(w, http.StatusCreated, buildAuthResp(result), traceID)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "AuthHandler.Login").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("login request received")

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		log.Warn().
			Str("handler", "AuthHandler.Login").
			Err(err).
			Str("email", req.Email).
			Msg("login request validation failed")
		response.Error(w, apperrors.BadRequest("email and password are required."), traceID)
		return
	}

	log.Info().
		Str("handler", "AuthHandler.Login").
		Str("email", req.Email).
		Msg("login request validated, invoking auth service")

	result, err := h.auth.Login(r.Context(), req.Email, req.Password, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		log.Error().
			Str("handler", "AuthHandler.Login").
			Err(err).
			Str("email", req.Email).
			Msg("auth service login failed")
		response.Error(w, err, traceID)
		return
	}

	log.Info().
		Str("handler", "AuthHandler.Login").
		Str("user_id", result.User.ID).
		Str("role", string(result.User.Role)).
		Msg("login request completed")
	response.JSON(w, http.StatusOK, buildAuthResp(result), traceID)
}

func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "AuthHandler.Refresh").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("refresh request received")

	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		log.Warn().
			Str("handler", "AuthHandler.Refresh").
			Err(err).
			Msg("refresh request validation failed")
		response.Error(w, apperrors.BadRequest("refresh_token is required."), traceID)
		return
	}

	log.Info().
		Str("handler", "AuthHandler.Refresh").
		Msg("refresh request validated, invoking auth service")

	result, err := h.auth.Refresh(r.Context(), req.RefreshToken, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		log.Error().
			Str("handler", "AuthHandler.Refresh").
			Err(err).
			Msg("auth service refresh failed")
		response.Error(w, err, traceID)
		return
	}

	log.Info().
		Str("handler", "AuthHandler.Refresh").
		Msg("refresh request completed")
	response.JSON(w, http.StatusOK, map[string]string{"access_token": result.AccessToken, "refresh_token": result.RefreshToken}, traceID)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "AuthHandler.Logout").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("logout request received")

	claims := middleware.ClaimsFromContext(r.Context())
	if claims == nil {
		log.Warn().
			Str("handler", "AuthHandler.Logout").
			Msg("logout denied: missing auth claims")
		response.Error(w, apperrors.Unauthorized("Not authenticated."), traceID)
		return
	}
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		log.Warn().
			Str("handler", "AuthHandler.Logout").
			Err(err).
			Str("user_id", claims.Sub).
			Msg("logout request validation failed")
		response.Error(w, apperrors.BadRequest("refresh_token is required."), traceID)
		return
	}

	log.Info().
		Str("handler", "AuthHandler.Logout").
		Str("user_id", claims.Sub).
		Msg("invoking auth service logout")

	if err := h.auth.Logout(r.Context(), claims.Sub, req.RefreshToken); err != nil {
		log.Error().
			Str("handler", "AuthHandler.Logout").
			Err(err).
			Str("user_id", claims.Sub).
			Msg("auth service logout failed")
		response.Error(w, err, traceID)
		return
	}

	log.Info().
		Str("handler", "AuthHandler.Logout").
		Str("user_id", claims.Sub).
		Msg("logout request completed")
	response.NoContent(w, traceID)
}

func buildAuthResp(result *service.AuthResult) authResponse {
	var resp authResponse
	resp.AccessToken = result.AccessToken
	resp.RefreshToken = result.RefreshToken
	resp.User.ID = result.User.ID
	resp.User.Name = result.User.Name
	resp.User.Email = result.User.Email
	resp.User.Role = string(result.User.Role)
	resp.User.VehicleType = string(result.User.VehicleType)
	return resp
}

func validPassword(pw string) bool {
	if len(pw) < 8 {
		return false
	}
	var hasLetter, hasDigit bool
	for _, ch := range pw {
		if unicode.IsLetter(ch) {
			hasLetter = true
		}
		if unicode.IsDigit(ch) {
			hasDigit = true
		}
	}
	return hasLetter && hasDigit
}
