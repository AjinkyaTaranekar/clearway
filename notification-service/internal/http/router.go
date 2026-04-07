package http

import (
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/http/handlers"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/tracing"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger"

	// Import generated swagger docs so `swag.Register` runs during init()
	_ "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/docs"
)

// Router configures and returns the HTTP router
type Router struct {
	mux                 *mux.Router
	healthHandler       *handlers.HealthHandler
	notificationHandler *handlers.NotificationHandler
	adminHandler        *handlers.AdminHandler
	logger              *logger.Logger
	jwksURL             string
}

// NewRouter creates a new router instance
func NewRouter(
	healthHandler *handlers.HealthHandler,
	notificationHandler *handlers.NotificationHandler,
	adminHandler *handlers.AdminHandler,
	log *logger.Logger,
	jwksURL string,
) *Router {
	return &Router{
		mux:                 mux.NewRouter(),
		healthHandler:       healthHandler,
		notificationHandler: notificationHandler,
		adminHandler:        adminHandler,
		logger:              log,
		jwksURL:             jwksURL,
	}
}

// Setup configures all routes and middleware
func (r *Router) Setup() *mux.Router {
	// Apply global middleware
	r.mux.Use(CORSMiddleware)
	r.mux.Use(tracing.Middleware)
	r.mux.Use(MetricsMiddleware)
	r.mux.Use(LoggingMiddleware(r.logger))

	// Observability
	r.mux.Handle("/metrics", promhttp.Handler())

	// Swagger documentation
	r.mux.PathPrefix("/swagger/").Handler(httpSwagger.WrapHandler)

	// Health check (no auth required)
	r.mux.HandleFunc("/health", r.healthHandler.Health).Methods("GET")
	r.mux.HandleFunc("/ready", r.healthHandler.Readiness).Methods("GET")

	// Authenticated API routes
	api := r.mux.PathPrefix("/api/v1").Subrouter()
	api.Use(AuthenticationMiddleware(r.jwksURL, r.logger))

	// Driver notification endpoints
	api.HandleFunc("/notifications", r.notificationHandler.List).Methods("GET")
	api.HandleFunc("/notifications/read-all", r.notificationHandler.MarkAllRead).Methods("PUT")
	api.HandleFunc("/notifications/{id}/read", r.notificationHandler.MarkRead).Methods("PUT")
	api.HandleFunc("/notifications/device-token", r.notificationHandler.RegisterDeviceToken).Methods("POST")

	// Admin endpoints
	api.HandleFunc("/admin/notifications", r.adminHandler.ListAll).Methods("GET")

	return r.mux
}
