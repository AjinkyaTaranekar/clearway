package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	docs "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/docs"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/client"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/event"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/handler"
	httpHandler "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/http"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/http/handlers"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/middleware"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/repository"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/service"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/config"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/postgres"
)

// @title Journey Microservice API
// @version 1.0
// @description Distributed Vehicle Capacity System — Journey Service. Manages journey bookings, state transitions, and enforcement checks.
// @termsOfService http://swagger.io/terms/
// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Enter "Bearer" followed by a space and the JWT token.
func main() {
	configPath := "config.yaml"
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Printf("Failed to load config (path=%s): %v\n", configPath, err)
		os.Exit(1)
	}

	log := logger.New(logger.Config{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
	})
	swaggerBaseURL := os.Getenv("VCS_SWAGGER_PUBLIC_BASE_URL")
	if err := configureSwaggerFromEnv(swaggerBaseURL); err != nil {
		log.Warn().Err(err).Str("value", swaggerBaseURL).Msg("invalid swagger public base url; using request host")
	}
	log.Info().Msg("starting vcs-journey service")

	// PostgreSQL
	dbPools, err := postgres.NewConnectionPools(
		postgres.Config{
			Host:         cfg.Database.Master.Host,
			Port:         cfg.Database.Master.Port,
			User:         cfg.Database.Master.User,
			Password:     cfg.Database.Master.Password,
			DBName:       cfg.Database.Master.DBName,
			MaxOpenConns: cfg.Database.Master.MaxOpenConns,
			MaxIdleConns: cfg.Database.Master.MaxIdleConns,
		},
		postgres.Config{
			Host:         cfg.Database.Slave.Host,
			Port:         cfg.Database.Slave.Port,
			User:         cfg.Database.Slave.User,
			Password:     cfg.Database.Slave.Password,
			DBName:       cfg.Database.Slave.DBName,
			MaxOpenConns: cfg.Database.Slave.MaxOpenConns,
			MaxIdleConns: cfg.Database.Slave.MaxIdleConns,
		},
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to initialize database connections")
	}
	defer dbPools.Close()
	log.Info().Msg("database connections established")

	// Run migrations (all files in order)
	journeyRepo := repository.NewJourneyRepository(dbPools.Master, dbPools.Slave)
	for _, mf := range []string{
		"migrations/001_create_schema.sql",
		"migrations/002_create_outbox.sql",
		"migrations/003_admin_actions.sql",
	} {
		migrationSQL, err := os.ReadFile(mf)
		if err != nil {
			log.Fatal().Err(err).Str("file", mf).Msg("failed to read migration file")
		}
		if err := journeyRepo.RunMigrations(context.Background(), string(migrationSQL)); err != nil {
			log.Fatal().Err(err).Str("file", mf).Msg("failed to run migration")
		}
		log.Info().Str("file", mf).Msg("migration applied")
	}

	// Redis (optional — graceful if unavailable)
	var redisClient *redis.Client
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Host,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		log.Warn().Err(err).Msg("Redis unavailable — caching and event publishing disabled")
		rdb.Close()
	} else {
		redisClient = rdb
		log.Info().Msg("Redis connected")
	}

	// JWKS validator — fetches RSA public keys from the IAM service to validate
	// RS256 tokens. Keys are cached for 1 hour and refreshed on cache miss.
	iamURL := cfg.Services.IAMURL
	if iamURL == "" {
		iamURL = "http://iam-service:8082"
	}
	jwksURL := iamURL + "/.well-known/jwks.json"
	jwksValidator := middleware.NewJWKSValidator(jwksURL)
	log.Info().Str("jwks_url", jwksURL).Msg("JWKS validator configured")

	// Business config defaults
	minAdvance := cfg.Business.MinAdvanceBookingMinutes
	if minAdvance == 0 {
		minAdvance = 60
	}
	minCancel := cfg.Business.MinCancellationWindowMinutes
	if minCancel == 0 {
		minCancel = 30
	}
	activationGrace := cfg.Business.ActivationGraceWindowMinutes
	if activationGrace == 0 {
		activationGrace = 30
	}
	routeCacheTTL := cfg.Business.RouteCacheTTLHours
	if routeCacheTTL == 0 {
		routeCacheTTL = 24
	}

	// Clients
	mapClient := client.NewMapClient(cfg.Services.MapURL)
	routeCache := client.NewRedisRouteCache(redisClient, routeCacheTTL)
	capacityClient := client.NewCapacityClient(cfg.Services.CapacityURL)

	// Repositories
	idempRepo := repository.NewIdempotencyRepository(dbPools.Master)

	// Service — event publishing now handled by transactional outbox (F-03)
	journeySvc := service.NewJourneyService(
		journeyRepo, idempRepo,
		mapClient, routeCache, capacityClient,
		minAdvance, minCancel, activationGrace,
		log,
	)

	// Handlers
	healthHandler := handlers.NewHealthHandler()
	journeyHandler := handler.NewJourneyHandler(journeySvc)
	adminHandler := handler.NewAdminHandler(journeySvc)

	// Router — JWT validation uses RS256 keys from the IAM JWKS endpoint
	router := httpHandler.NewRouter(healthHandler, journeyHandler, adminHandler, log, jwksValidator)
	mux := router.Setup()

	// HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  cfg.Server.Timeout,
		WriteTimeout: cfg.Server.Timeout,
		IdleTimeout:  cfg.Server.Timeout * 2,
	}

	// Start background jobs
	baseCtx := logger.WithContext(context.Background(), log)
	ctx, cancelJobs := context.WithCancel(baseCtx)
	defer cancelJobs()
	go journeySvc.RunExpiryJob(ctx)
	log.Info().Msg("background expiry job started")

	// Outbox relay — reads journey.outbox and publishes to Redis Streams (F-03).
	// Guarantees at-least-once delivery even across process restarts.
	go event.RunOutboxRelay(ctx, journeyRepo, redisClient, log.Logger)
	log.Info().Msg("outbox relay started")

	// Start HTTP server
	go func() {
		log.Info().Int("port", cfg.Server.Port).Msg("server starting")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server failed")
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down server...")
	cancelJobs()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatal().Err(err).Msg("server forced to shutdown")
	}
	log.Info().Msg("server exited")
}

func configureSwaggerFromEnv(rawBaseURL string) error {
	rawBaseURL = strings.TrimSpace(rawBaseURL)
	if rawBaseURL == "" {
		return nil
	}

	parsed, err := url.Parse(rawBaseURL)
	if err != nil {
		return fmt.Errorf("parse swagger base url: %w", err)
	}
	if parsed.Host == "" {
		return fmt.Errorf("swagger base url missing host")
	}

	docs.SwaggerInfo.Host = parsed.Host
	if parsed.Scheme != "" {
		docs.SwaggerInfo.Schemes = []string{parsed.Scheme}
	}

	basePath := strings.TrimRight(parsed.Path, "/")
	if basePath == "/" {
		basePath = ""
	}
	docs.SwaggerInfo.BasePath = basePath

	return nil
}
