package service

import (
	"context"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/repository"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/logger"
	"github.com/redis/go-redis/v9"
) // TODO: check import

// CleanupService runs periodic background jobs:
//  1. Orphaned reservation cleanup — releases reservations whose time window ended
//     more than orphanThreshold ago (catches Journey Service crashes).
//  2. Idempotency cache cleanup — purges expired entries.
type CleanupService struct {
	reservRepo      *repository.ReservationRepo
	idempRepo       *repository.IdempotencyRepo
	redis           *redis.Client
	cleanupInterval time.Duration
	orphanThreshold time.Duration
	log             *logger.Logger
}

// NewCleanupService creates a new CleanupService.
func NewCleanupService(
	reservRepo *repository.ReservationRepo,
	idempRepo *repository.IdempotencyRepo,
	redisClient *redis.Client,
	cleanupInterval, orphanThreshold time.Duration,
	log *logger.Logger,
) *CleanupService {
	return &CleanupService{
		reservRepo:      reservRepo,
		idempRepo:       idempRepo,
		redis:           redisClient,
		cleanupInterval: cleanupInterval,
		orphanThreshold: orphanThreshold,
		log:             log,
	}
}

// Start launches both cleanup goroutines. It blocks until ctx is cancelled.
func (s *CleanupService) Start(ctx context.Context) {
	go s.runOrphanCleanup(ctx)
	go s.runIdempotencyCleanup(ctx)
}

func (s *CleanupService) runOrphanCleanup(ctx context.Context) {
	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.releaseOrphans(ctx)
		}
	}
}

func (s *CleanupService) runIdempotencyCleanup(ctx context.Context) {
	// Run every hour regardless of the cleanup interval.
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.purgeExpiredIdempotencyEntries(ctx)
		}
	}
}

func (s *CleanupService) releaseOrphans(ctx context.Context) {
	olderThan := time.Now().UTC().Add(-s.orphanThreshold)

	affected, err := s.reservRepo.ReleaseOrphans(ctx, olderThan)
	if err != nil {
		s.log.Error().Err(err).Msg("orphan cleanup: failed to release orphaned reservations")
		return
	}

	if len(affected) == 0 {
		return
	}

	s.log.Info().Int("count", len(affected)).Msg("orphan cleanup: released orphaned reservations")
	s.invalidateCache(ctx, affected)
}

func (s *CleanupService) purgeExpiredIdempotencyEntries(ctx context.Context) {
	n, err := s.idempRepo.DeleteExpired(ctx)
	if err != nil {
		s.log.Error().Err(err).Msg("idempotency cleanup: failed to purge expired entries")
		return
	}
	if n > 0 {
		s.log.Info().Int64("deleted", n).Msg("idempotency cleanup: purged expired entries")
	}
}

func (s *CleanupService) invalidateCache(ctx context.Context, affected []model.SegmentReservation) {
	if s.redis == nil || len(affected) == 0 {
		return
	}
	keys := make([]string, 0, len(affected))
	for _, r := range affected {
		keys = append(keys, availabilityCacheKey(r.SegmentID, r.TimeWindowStart, r.TimeWindowEnd))
	}
	if err := s.redis.Del(ctx, keys...).Err(); err != nil {
		s.log.Warn().Err(err).Msg("orphan cleanup: failed to invalidate availability cache")
	}
}
