package http

import (
	"net/http"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/internal/http/handlers"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/tracing"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger"

	// Import generated swagger docs so `swag.Register` runs during init()
	_ "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/docs"
)

// Router configures and returns the HTTP router
type Router struct {
	mux           *mux.Router
	healthHandler *handlers.HealthHandler
	mapHandler    *handlers.MapHandler
	searchHandler *handlers.SearchHandler
	logger        *logger.Logger
	jwksValidator *JWKSValidator
}

// NewRouter creates a new router instance
func NewRouter(
	healthHandler *handlers.HealthHandler,
	mapHandler *handlers.MapHandler,
	searchHandler *handlers.SearchHandler,
	log *logger.Logger,
	jwksValidator *JWKSValidator,
) *Router {
	return &Router{
		mux:           mux.NewRouter(),
		healthHandler: healthHandler,
		mapHandler:    mapHandler,
		searchHandler: searchHandler,
		logger:        log,
		jwksValidator: jwksValidator,
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

	// Health check
	r.mux.HandleFunc("/health", r.healthHandler.Health).Methods("GET")
	r.mux.HandleFunc("/ready", r.healthHandler.Readiness).Methods("GET")

	// Map APIs
	r.mux.HandleFunc("/api/v1/map/nodes", r.mapHandler.GetNodes).Methods("GET")
	r.mux.HandleFunc("/api/v1/map/segments", r.mapHandler.GetSegments).Methods("GET")
	r.mux.HandleFunc("/api/v1/map/route", r.mapHandler.GetRoute).Methods("GET")
	r.mux.HandleFunc("/api/v1/routes/compute", r.mapHandler.ComputeRoute).Methods("POST")

	// Geocoding proxy — search is unauthenticated so the booking form can call it without a JWT
	r.mux.HandleFunc("/api/v1/map/search", r.searchHandler.SearchPlaces).Methods("GET")

	trafficHandler := JWTAuth(r.jwksValidator)(AdminOnly(http.Handler(http.HandlerFunc(r.mapHandler.GetTraffic))))
	r.mux.Handle("/api/v1/map/traffic", trafficHandler).Methods("GET")

	return r.mux
}
