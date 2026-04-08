package http

import (
	"net/http"

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
	closureHandler   *handlers.ClosureHandler
	logger           *logger.Logger
	jwksValidator    *JWKSValidator
}

// NewRouter creates a new router instance
func NewRouter(
	healthHandler *handlers.HealthHandler,
	capacityHandler *handlers.CapacityHandler,
	occupancyHandler *handlers.OccupancyHandler,
	closureHandler *handlers.ClosureHandler,
	log *logger.Logger,
	jwksValidator *JWKSValidator,
) *Router {
	return &Router{
		mux:              mux.NewRouter(),
		healthHandler:    healthHandler,
		capacityHandler:  capacityHandler,
		occupancyHandler: occupancyHandler,
		closureHandler:   closureHandler,
		logger:           log,
		jwksValidator:    jwksValidator,
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
	// /segments must be registered before /segments/occupancy so gorilla/mux
	// does not shadow the more-specific path.
	api.HandleFunc("/segments", r.occupancyHandler.Segments).Methods("GET")
	api.HandleFunc("/segments/occupancy", r.occupancyHandler.Occupancy).Methods("GET")
	if r.closureHandler != nil {
		listClosures := JWTAuth(r.jwksValidator)(AdminOnly(http.Handler(http.HandlerFunc(r.closureHandler.List))))
		createClosure := JWTAuth(r.jwksValidator)(AdminOnly(http.Handler(http.HandlerFunc(r.closureHandler.Create))))
		api.Handle("/closures", listClosures).Methods("GET")
		api.Handle("/closures", createClosure).Methods("POST")
	}

	return r.mux
}
