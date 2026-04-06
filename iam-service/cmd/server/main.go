package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpHandler "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/http"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/http/handlers"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/repository"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/service"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/config"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/postgres"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(logger.Config{Level: cfg.Logging.Level, Format: cfg.Logging.Format})
	log.Info().Msg("starting vcs-iam service")

	dbPools, err := postgres.NewConnectionPools(
		postgres.Config{Host: cfg.Database.Master.Host, Port: cfg.Database.Master.Port, User: cfg.Database.Master.User, Password: cfg.Database.Master.Password, DBName: cfg.Database.Master.DBName, MaxOpenConns: cfg.Database.Master.MaxOpenConns, MaxIdleConns: cfg.Database.Master.MaxIdleConns},
		postgres.Config{Host: cfg.Database.Slave.Host, Port: cfg.Database.Slave.Port, User: cfg.Database.Slave.User, Password: cfg.Database.Slave.Password, DBName: cfg.Database.Slave.DBName, MaxOpenConns: cfg.Database.Slave.MaxOpenConns, MaxIdleConns: cfg.Database.Slave.MaxIdleConns},
	)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer dbPools.Close()

	jwksSvc, err := service.NewJWKSService(cfg.IAM.PrivateKeyPath, cfg.IAM.SigningKID, cfg.IAM.PreviousKID, cfg.IAM.PreviousPubKeyPEM)
	if err != nil {
		log.Fatal().Err(err).Str("path", cfg.IAM.PrivateKeyPath).Msg("failed to load RSA key — run: openssl genrsa -out keys/private.pem 2048")
	}
	log.Info().Str("kid", cfg.IAM.SigningKID).Msg("RSA key loaded")

	userRepo := repository.NewUserRepo(dbPools.Master)
	tokenRepo := repository.NewTokenRepo(dbPools.Master)
	authSvc := service.NewAuthService(userRepo, tokenRepo, jwksSvc, cfg.IAM.AccessTokenTTL, cfg.IAM.RefreshTokenTTL, cfg.IAM.BcryptCost)
	profileSvc := service.NewProfileService(userRepo)
	adminSvc := service.NewAdminService(userRepo, tokenRepo)
	cleanupSvc := service.NewCleanupService(tokenRepo, cfg.IAM.TokenRetentionDays, cfg.IAM.CleanupInterval, log)

	router := httpHandler.NewRouter(
		handlers.NewHealthHandler(dbPools.Master),
		handlers.NewAuthHandler(authSvc),
		handlers.NewProfileHandler(profileSvc),
		handlers.NewJWKSHandler(jwksSvc),
		handlers.NewAdminHandler(adminSvc),
		jwksSvc, log,
	)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router.Setup(),
		ReadTimeout:  cfg.Server.Timeout,
		WriteTimeout: cfg.Server.Timeout,
		IdleTimeout:  cfg.Server.Timeout * 2,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go cleanupSvc.Start(ctx)

	go func() {
		log.Info().Int("port", cfg.Server.Port).Msg("server listening")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server failed")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	cancel()

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutCancel()
	if err := server.Shutdown(shutCtx); err != nil {
		log.Fatal().Err(err).Msg("forced shutdown")
	}
	log.Info().Msg("server stopped")
}
