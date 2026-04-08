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

	docs "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/docs"
	httpHandler "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/internal/http"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/internal/http/handlers"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/config"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/postgres"
)

// @title Map Microservice API
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
	log.Info().Msg("starting vcs-map service")
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

	graphStore := handlers.NewGraphStore(log)
	if err := graphStore.LoadFromDB(baseCtx, dbPools.Slave); err != nil {
		graphStore.UseFallback(err)
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
		log.Warn().Msg("JWKS URL not configured; IAM-protected map endpoints will fail authorization")
	}

	// Initialize HTTP handlers
	healthHandler := handlers.NewHealthHandler(graphStore)
	mapHandler := handlers.NewMapHandler(
		graphStore,
		handlers.NewCapacityClient(cfg.Services.CapacityBaseURL, log),
		log,
	)
	geoClient := handlers.NewGeoClient(
		cfg.Services.NominatimBaseURL,
		cfg.Services.OSRMBaseURL,
		cfg.Services.UserAgent,
		log,
	)
	searchHandler := handlers.NewSearchHandler(geoClient, log)

	// Setup router
	router := httpHandler.NewRouter(
		healthHandler,
		mapHandler,
		searchHandler,
		log,
		jwksValidator,
	)
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

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(baseCtx, 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("server forced to shutdown")
	}

	log.Info().Msg("server exited")
}

func resolveMigrationsDir() string {
	candidates := []string{
		strings.TrimSpace(os.Getenv("VCS_MIGRATIONS_DIR")),
		"migrations",
		filepath.Join("map-service", "migrations"),
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
