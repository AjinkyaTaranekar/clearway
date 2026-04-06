package service

import (
	"context"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/internal/repository"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/iam-service/pkg/logger"
)

type CleanupService struct {
	tokens        *repository.TokenRepo
	retentionDays int
	interval      time.Duration
	log           *logger.Logger
}

func NewCleanupService(tokens *repository.TokenRepo, retentionDays int, interval time.Duration, log *logger.Logger) *CleanupService {
	return &CleanupService{tokens: tokens, retentionDays: retentionDays, interval: interval, log: log}
}

func (s *CleanupService) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.log.Info().Int("retention_days", s.retentionDays).Msg("cleanup job started")
	for {
		select {
		case <-ticker.C:
			deleted, err := s.tokens.DeleteExpired(ctx, s.retentionDays)
			if err != nil {
				s.log.Error().Err(err).Msg("cleanup: failed to delete expired tokens")
			} else if deleted > 0 {
				s.log.Info().Int64("deleted", deleted).Msg("cleanup: removed expired tokens")
			}
		case <-ctx.Done():
			return
		}
	}
}
