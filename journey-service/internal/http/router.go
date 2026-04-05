package http

import (
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/handler"
	internalhttp "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/http/handlers"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/middleware"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/tracing"
	"github.com/gorilla/mux"
	httpSwagger "github.com/swaggo/http-swagger"

	// Import generated swagger docs so `swag.Register` runs during init()
	_ "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/docs"
)

// Router configures and returns the HTTP router
type Router struct {
	mux            *mux.Router
	healthHandler  *internalhttp.HealthHandler
	journeyHandler *handler.JourneyHandler
	adminHandler   *handler.AdminHandler
	logger         *logger.Logger
	jwtSecret      string
}

// NewRouter creates a new router instance
func NewRouter(
	healthHandler *internalhttp.HealthHandler,
	journeyHandler *handler.JourneyHandler,
	adminHandler *handler.AdminHandler,
	log *logger.Logger,
	jwtSecret string,
) *Router {
	return &Router{
		mux:            mux.NewRouter(),
		healthHandler:  healthHandler,
		journeyHandler: journeyHandler,
		adminHandler:   adminHandler,
		logger:         log,
		jwtSecret:      jwtSecret,
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

	// Health & readiness (no auth)
	r.mux.HandleFunc("/health", r.healthHandler.Health).Methods("GET")
	r.mux.HandleFunc("/ready", r.healthHandler.Readiness).Methods("GET")

	// API v1 subrouter — all routes require JWT auth
	api := r.mux.PathPrefix("/api/v1").Subrouter()
	api.Use(middleware.JWTAuth(r.jwtSecret))
	api.Use(middleware.IdempotencyKeyMiddleware)

	// Driver journey endpoints
	api.HandleFunc("/journeys", r.journeyHandler.CreateJourney).Methods("POST", "OPTIONS")
	api.HandleFunc("/journeys", r.journeyHandler.ListJourneys).Methods("GET")
	api.HandleFunc("/journeys/{id}", r.journeyHandler.GetJourney).Methods("GET")
	api.HandleFunc("/journeys/{id}/cancel", r.journeyHandler.CancelJourney).Methods("PUT", "OPTIONS")
	api.HandleFunc("/journeys/{id}/activate", r.journeyHandler.ActivateJourney).Methods("PUT", "OPTIONS")
	api.HandleFunc("/journeys/{id}/complete", r.journeyHandler.CompleteJourney).Methods("PUT", "OPTIONS")

	// Admin subrouter — requires admin role
	admin := api.PathPrefix("/admin").Subrouter()
	admin.Use(middleware.AdminOnly)
	admin.HandleFunc("/journeys", r.adminHandler.ListJourneys).Methods("GET")
	admin.HandleFunc("/journeys/{id}/cancel", r.adminHandler.CancelJourney).Methods("PUT", "OPTIONS")

	// Enforcement subrouter — requires enforcement or admin role
	enforcement := api.PathPrefix("/enforcement").Subrouter()
	enforcement.Use(middleware.EnforcementOnly)
	enforcement.HandleFunc("/verify", r.adminHandler.EnforcementVerify).Methods("GET")

	return r.mux
}
