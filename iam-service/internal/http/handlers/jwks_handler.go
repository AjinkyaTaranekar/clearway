package handlers

import (
	"net/http"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/service"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/tracing"
)

type JWKSHandler struct {
	jwks *service.JWKSService
}

func NewJWKSHandler(jwks *service.JWKSService) *JWKSHandler {
	return &JWKSHandler{jwks: jwks}
}

func (h *JWKSHandler) ServeJWKS(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	jwksJSON, err := h.jwks.BuildJWKS()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace-ID", traceID)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"INTERNAL_ERROR","message":"Failed to build JWKS"}}`)) //nolint:errcheck
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(jwksJSON) //nolint:errcheck
}
