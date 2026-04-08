package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	docs "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/docs"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/event"
	httpHandler "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/http"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/http/handlers"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/repository"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/service"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/config"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/postgres"
	"github.com/redis/go-redis/v9"
)

// @title Capacity Microservice API
// @version 1.0
// @termsOfService http://swagger.io/terms/
// @license.name Apache 2.0
// @license.url http://www.apache.org/licenses/LICENSE-2.0.html
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Enter "Bearer" followed by a space and the JWT token.
func main() {
	// Load configuration
	configPath := "config.yaml"
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Printf("Failed to load config (path=%s): %v\n", configPath, err)
		os.Exit(1)
	}

	// Initialize logger
	log := logger.New(logger.Config{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
	})
	swaggerBaseURL := os.Getenv("VCS_SWAGGER_PUBLIC_BASE_URL")
	if err := configureSwaggerFromEnv(swaggerBaseURL); err != nil {
		log.Warn().Err(err).Str("value", swaggerBaseURL).Msg("invalid swagger public base url; using request host")
	}
	log.Info().Msg("starting vcs-capacity service")
	baseCtx := logger.WithContext(context.Background(), log)

	// Initialize database connections
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

	migrationsDir := resolveMigrationsDir()
	if err := postgres.RunMigrations(dbPools.Master, migrationsDir); err != nil {
		log.Fatal().Err(err).Msg("failed to run database migrations")
	}
	log.Info().Str("dir", migrationsDir).Msg("database migrations applied")

	// Initialize Redis (optional - service degrades gracefully without it)
	var redisClient *redis.Client
	if cfg.Redis.Host != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Host,
			Password: cfg.Redis.Password,
		})
		pingCtx, pingCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer pingCancel()
		if err := redisClient.Ping(pingCtx).Err(); err != nil {
			log.Warn().Err(err).Msg("Redis ping failed - caching and stream consumer disabled")
			redisClient = nil
		} else {
			log.Info().Str("host", cfg.Redis.Host).Msg("Redis connected")
		}
	}

	// Apply config defaults
	cacheTTL := cfg.Capacity.AvailabilityCacheTTL
	if cacheTTL == 0 {
		cacheTTL = 30 * time.Second
	}
	cleanupInterval := cfg.Capacity.OrphanCleanupInterval
	if cleanupInterval == 0 {
		cleanupInterval = 30 * time.Minute
	}
	orphanThreshold := cfg.Capacity.OrphanThreshold
	if orphanThreshold == 0 {
		orphanThreshold = time.Hour
	}
	vmID := cfg.Capacity.VMID
	if vmID == "" {
		vmID = "vm-a"
	}

	// Wire up repositories
	segmentRepo := repository.NewSegmentRepo(dbPools.Slave)
	reservRepo := repository.NewReservationRepo(dbPools.Master, dbPools.Slave)
	closureRepo := repository.NewClosureRepo(dbPools.Master)
	idempRepo := repository.NewIdempotencyRepo(dbPools.Master)

	// Wire up services
	reservSvc := service.NewReservationService(
		dbPools.Master,
		segmentRepo,
		reservRepo,
		closureRepo,
		idempRepo,
		redisClient,
		cacheTTL,
		log,
	)
	cleanupSvc := service.NewCleanupService(
		reservRepo,
		idempRepo,
		redisClient,
		cleanupInterval,
		orphanThreshold,
		log,
	)

	// Background context for long-running goroutines
	bgCtx, bgCancel := context.WithCancel(baseCtx)
	defer bgCancel()

	// Start background cleanup jobs
	cleanupSvc.Start(bgCtx)

	// Start Redis Streams event consumer (only if Redis is available)
	if redisClient != nil {
		consumerName := vmID + "-capacity"
		consumer := event.NewConsumer(redisClient, reservRepo, reservSvc, consumerName, log)
		go consumer.Start(bgCtx)
		log.Info().Str("consumer", consumerName).Msg("event consumer started")
	}

	var jwksValidator *httpHandler.JWKSValidator
	jwksURL := strings.TrimSpace(cfg.Services.JWKSURL)
	if jwksURL == "" {
		jwksURL = strings.TrimSpace(os.Getenv("JWKS_URL"))
	}
	if jwksURL != "" {
		jwksValidator = httpHandler.NewJWKSValidator(jwksURL)
		log.Info().Str("jwks_url", jwksURL).Msg("JWKS validator configured")
	} else {
		log.Warn().Msg("JWKS URL not configured; IAM-protected capacity endpoints will fail authorization")
	}

	// Initialize HTTP handlers
	healthHandler := handlers.NewHealthHandler()
	capacityHandler := handlers.NewCapacityHandler(reservSvc, log)
	occupancyHandler := handlers.NewOccupancyHandler(reservSvc, log)
	closureHandler := handlers.NewClosureHandler(reservSvc, log)

	// Setup router
	router := httpHandler.NewRouter(healthHandler, capacityHandler, occupancyHandler, closureHandler, log, jwksValidator)
	mux := router.Setup()

	// Create HTTP server
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      mux,
		ReadTimeout:  cfg.Server.Timeout,
		WriteTimeout: cfg.Server.Timeout,
		IdleTimeout:  cfg.Server.Timeout * 2,
	}

	// Start server in goroutine
	go func() {
		log.Info().Int("port", cfg.Server.Port).Msg("server starting")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server failed")
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down server...")
	bgCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(baseCtx, 30*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatal().Err(err).Msg("server forced to shutdown")
	}

	if redisClient != nil {
		redisClient.Close()
	}

	log.Info().Msg("server exited")
}

func resolveMigrationsDir() string {
	candidates := []string{
		strings.TrimSpace(os.Getenv("VCS_MIGRATIONS_DIR")),
		"migrations",
		filepath.Join("capacity-service", "migrations"),
	}

	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir
		}
	}

	// Keep default value so startup error clearly reports the attempted path.
	return "migrations"
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
