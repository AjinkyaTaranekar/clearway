package http

import (
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/http/handlers"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/tracing"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger"

	// Import generated swagger docs so `swag.Register` runs during init()
	_ "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/docs"
)

// Router configures and returns the HTTP router
type Router struct {
	mux              *mux.Router
	healthHandler    *handlers.HealthHandler
	capacityHandler  *handlers.CapacityHandler
	occupancyHandler *handlers.OccupancyHandler
	logger           *logger.Logger
}

// NewRouter creates a new router instance
func NewRouter(
	healthHandler *handlers.HealthHandler,
	capacityHandler *handlers.CapacityHandler,
	occupancyHandler *handlers.OccupancyHandler,
	log *logger.Logger,
) *Router {
	return &Router{
		mux:              mux.NewRouter(),
		healthHandler:    healthHandler,
		capacityHandler:  capacityHandler,
		occupancyHandler: occupancyHandler,
		logger:           log,
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

	// Health checks
	r.mux.HandleFunc("/health", r.healthHandler.Health).Methods("GET")
	r.mux.HandleFunc("/ready", r.healthHandler.Readiness).Methods("GET")

	// Capacity API
	api := r.mux.PathPrefix("/api/v1/capacity").Subrouter()
	api.HandleFunc("/reserve", r.capacityHandler.Reserve).Methods("POST")
	api.HandleFunc("/check", r.capacityHandler.Check).Methods("GET")
	api.HandleFunc("/segments/occupancy", r.occupancyHandler.Occupancy).Methods("GET")

	return r.mux
}
