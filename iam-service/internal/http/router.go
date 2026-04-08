package http

import (
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger"

	_ "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/docs"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/http/handlers"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/middleware"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/service"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/tracing"
)

type Router struct {
	mux            *mux.Router
	healthHandler  *handlers.HealthHandler
	authHandler    *handlers.AuthHandler
	profileHandler *handlers.ProfileHandler
	vehicleHandler *handlers.VehicleHandler
	jwksHandler    *handlers.JWKSHandler
	adminHandler   *handlers.AdminHandler
	jwks           *service.JWKSService
	logger         *logger.Logger
}

func NewRouter(health *handlers.HealthHandler, auth *handlers.AuthHandler, profile *handlers.ProfileHandler, vehicle *handlers.VehicleHandler, jwks *handlers.JWKSHandler, admin *handlers.AdminHandler, jwksSvc *service.JWKSService, log *logger.Logger) *Router {
	return &Router{mux: mux.NewRouter(), healthHandler: health, authHandler: auth, profileHandler: profile, vehicleHandler: vehicle, jwksHandler: jwks, adminHandler: admin, jwks: jwksSvc, logger: log}
}

func (r *Router) Setup() *mux.Router {
	r.mux.Use(CORSMiddleware)
	r.mux.Use(tracing.Middleware)
	r.mux.Use(MetricsMiddleware)
	r.mux.Use(LoggingMiddleware(r.logger))

	r.mux.Handle("/metrics", promhttp.Handler())
	r.mux.PathPrefix("/swagger/").Handler(httpSwagger.WrapHandler)
	r.mux.HandleFunc("/health", r.healthHandler.Health).Methods("GET")
	r.mux.HandleFunc("/ready", r.healthHandler.Readiness).Methods("GET")
	r.mux.HandleFunc("/.well-known/jwks.json", r.jwksHandler.ServeJWKS).Methods("GET")

	public := r.mux.PathPrefix("/api/v1/auth").Subrouter()
	public.HandleFunc("/register", r.authHandler.Register).Methods("POST")
	public.HandleFunc("/login", r.authHandler.Login).Methods("POST")
	public.HandleFunc("/refresh", r.authHandler.Refresh).Methods("POST")

	jwtMW := middleware.JWTMiddleware(r.jwks)

	authed := r.mux.PathPrefix("/api/v1/auth").Subrouter()
	authed.Use(jwtMW)
	authed.HandleFunc("/logout", r.authHandler.Logout).Methods("POST")
	authed.HandleFunc("/profile", r.profileHandler.GetProfile).Methods("GET")
	authed.HandleFunc("/profile", r.profileHandler.UpdateProfile).Methods("PUT")
	authed.HandleFunc("/vehicles", r.vehicleHandler.ListVehicles).Methods("GET")
	authed.HandleFunc("/vehicles/primary", r.vehicleHandler.UpdatePrimary).Methods("PUT")
	authed.HandleFunc("/vehicles/secondary", r.vehicleHandler.AddSecondary).Methods("POST")
	authed.HandleFunc("/vehicles/secondary/{id}", r.vehicleHandler.UpdateSecondary).Methods("PUT")
	authed.HandleFunc("/vehicles/secondary/{id}", r.vehicleHandler.DeleteSecondary).Methods("DELETE")

	adminRouter := r.mux.PathPrefix("/api/v1/admin/auth").Subrouter()
	adminRouter.Use(jwtMW)
	adminRouter.Use(middleware.RequireRole(model.RoleAdmin))
	adminRouter.HandleFunc("/users", r.adminHandler.ListUsers).Methods("GET")
	adminRouter.HandleFunc("/users/{id}", r.adminHandler.GetUser).Methods("GET")
	adminRouter.HandleFunc("/promote", r.adminHandler.PromoteUser).Methods("POST")
	adminRouter.HandleFunc("/force-logout", r.adminHandler.ForceLogout).Methods("POST")

	return r.mux
}
