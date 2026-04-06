package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/client"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/event"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/handler"
	httpHandler "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/http"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/http/handlers"
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

	// Run migrations
	journeyRepo := repository.NewJourneyRepository(dbPools.Master)
	migrationSQL, err := os.ReadFile("migrations/001_create_schema.sql")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to read migration file")
	}
	if err := journeyRepo.RunMigrations(context.Background(), string(migrationSQL)); err != nil {
		log.Fatal().Err(err).Msg("failed to run migrations")
	}
	log.Info().Msg("database migrations applied")

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

	// JWT secret
	jwtSecret := cfg.Services.JWTSecret
	if jwtSecret == "" {
		jwtSecret = "dev-secret"
	}

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

	// Event publisher
	publisher := event.NewPublisher(redisClient, log.Logger)

	// Repositories
	idempRepo := repository.NewIdempotencyRepository(dbPools.Master)

	// Service
	journeySvc := service.NewJourneyService(
		journeyRepo, idempRepo,
		mapClient, routeCache, capacityClient,
		publisher,
		minAdvance, minCancel, activationGrace,
		log,
	)

	// Handlers
	healthHandler := handlers.NewHealthHandler()
	journeyHandler := handler.NewJourneyHandler(journeySvc)
	adminHandler := handler.NewAdminHandler(journeySvc)

	// Router
	router := httpHandler.NewRouter(healthHandler, journeyHandler, adminHandler, log, jwtSecret)
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
	ctx, cancelJobs := context.WithCancel(context.Background())
	defer cancelJobs()
	go journeySvc.RunExpiryJob(ctx)
	log.Info().Msg("background expiry job started")

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
