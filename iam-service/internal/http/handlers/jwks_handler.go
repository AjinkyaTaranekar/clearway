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
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "JWKSHandler.ServeJWKS").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("jwks request received")

	jwksJSON, err := h.jwks.BuildJWKS()
	if err != nil {
		log.Error().
			Str("handler", "JWKSHandler.ServeJWKS").
			Err(err).
			Msg("failed to build jwks response")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Trace-ID", traceID)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"INTERNAL_ERROR","message":"Failed to build JWKS"}}`)) //nolint:errcheck
		return
	}

	log.Info().
		Str("handler", "JWKSHandler.ServeJWKS").
		Int("payload_bytes", len(jwksJSON)).
		Msg("jwks response generated")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(jwksJSON) //nolint:errcheck
}
