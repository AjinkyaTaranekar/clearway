package http

import (
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/http/handlers"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/tracing"
	"github.com/gorilla/mux"
	httpSwagger "github.com/swaggo/http-swagger"

	// Import generated swagger docs so `swag.Register` runs during init()
	_ "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/docs"
)

// Router configures and returns the HTTP router
type Router struct {
	mux           *mux.Router
	healthHandler *handlers.HealthHandler
	logger        *logger.Logger
}

// NewRouter creates a new router instance
func NewRouter(
	healthHandler *handlers.HealthHandler,
	log *logger.Logger,
) *Router {
	return &Router{
		mux:           mux.NewRouter(),
		healthHandler: healthHandler,
		logger:        log,
	}
}

// Setup configures all routes and middleware
func (r *Router) Setup() *mux.Router {
	// Apply global middleware
	r.mux.Use(CORSMiddleware)
	r.mux.Use(tracing.Middleware)
	r.mux.Use(LoggingMiddleware(r.logger))

	// Swagger documentation
	r.mux.PathPrefix("/swagger/").Handler(httpSwagger.WrapHandler)

	// Health check
	r.mux.HandleFunc("/health", r.healthHandler.Health).Methods("GET")
	r.mux.HandleFunc("/ready", r.healthHandler.Readiness).Methods("GET")

	return r.mux
}
