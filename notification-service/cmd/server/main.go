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

	"github.com/redis/go-redis/v9"

	docs "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/docs"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/event"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/fcm"
	httpHandler "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/http"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/http/handlers"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/repository"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/config"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/postgres"
)

// @title Notification Microservice API
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
	log.Info().Msg("starting vcs-notification service")
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
		log.Fatal().Err(err).Msg("Failed to initialize database connections")
	}
	defer dbPools.Close()

	log.Info().Msg("Database connections established")
	migrationsDir := resolveMigrationsDir()
	if err := postgres.RunMigrations(dbPools.Master, migrationsDir); err != nil {
		log.Fatal().Err(err).Msg("failed to run database migrations")
	}
	log.Info().Str("dir", migrationsDir).Msg("database migrations applied")

	// Initialize PostgreSQL repositories
	notifRepo := repository.NewNotificationRepo(dbPools.Master, dbPools.Slave)
	tokenRepo := repository.NewDeviceTokenRepo(dbPools.Master, dbPools.Slave)

	var fcmClient *fcm.Client
	fcmClient, err = fcm.NewClientFromEnv()
	if err != nil {
		if err == fcm.ErrClientDisabled {
			log.Warn().Msg("FCM credentials not configured — push delivery will be marked as skipped")
		} else {
			log.Warn().Err(err).Msg("Failed to initialize FCM client — push delivery will be marked as skipped")
		}
		fcmClient = nil
	} else {
		log.Info().Msg("FCM client initialized")
	}

	// Initialize Redis (optional — service degrades gracefully without it)
	var redisClient *redis.Client
	if cfg.Redis.Host != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Host,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		pingCtx, pingCancel := context.WithTimeout(baseCtx, 3*time.Second)
		if err := redisClient.Ping(pingCtx).Err(); err != nil {
			log.Warn().Err(err).Msg("Redis ping failed — stream consumer disabled")
			redisClient = nil
		} else {
			log.Info().Str("host", cfg.Redis.Host).Msg("Redis connected")
		}
		pingCancel()
	}

	// JWKS URL for JWT validation (empty = parse claims only, no signature check)
	jwksURL := os.Getenv("JWKS_URL") // e.g. http://iam-service:8082/.well-known/jwks.json

	// Initialize HTTP handlers
	healthHandler := handlers.NewHealthHandler()
	notificationHandler := handlers.NewNotificationHandler(notifRepo, tokenRepo, log)
	adminHandler := handlers.NewAdminHandler(notifRepo, log)

	// Setup router
	router := httpHandler.NewRouter(
		healthHandler,
		notificationHandler,
		adminHandler,
		log,
		jwksURL,
	)
	mux := router.Setup()

	// Start Redis Streams consumer in background (only when Redis is available)
	ctx, cancel := context.WithCancel(baseCtx)
	defer cancel()

	if redisClient != nil {
		vmID := os.Getenv("VM_ID")
		if vmID == "" {
			vmID = "vm-default"
		}
		consumerName := "notification-svc-" + vmID
		consumer := event.NewConsumer(redisClient, notifRepo, tokenRepo, fcmClient, consumerName, log)
		go consumer.Start(ctx)
		log.Info().Str("consumer", consumerName).Msg("Redis Streams consumer started")
	}

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
		log.Info().
			Int("port", cfg.Server.Port).
			Msg("server starting")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server failed")
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down server...")

	// Stop the Redis consumer
	cancel()

	// Graceful HTTP shutdown
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
		filepath.Join("notification-service", "migrations"),
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
