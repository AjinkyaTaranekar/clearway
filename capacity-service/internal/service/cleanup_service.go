package service

import (
	"context"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/repository"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/logger"
	"github.com/redis/go-redis/v9"
)

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
	log := s.logWithTrace(ctx)
	log.Info().
		Str("service", "CleanupService.Start").
		Dur("cleanup_interval", s.cleanupInterval).
		Dur("orphan_threshold", s.orphanThreshold).
		Msg("starting cleanup background workers")

	go s.runOrphanCleanup(ctx)
	go s.runIdempotencyCleanup(ctx)
}

func (s *CleanupService) runOrphanCleanup(ctx context.Context) {
	log := s.logWithTrace(ctx)
	log.Info().
		Str("service", "CleanupService.runOrphanCleanup").
		Dur("interval", s.cleanupInterval).
		Msg("orphan cleanup worker started")

	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Str("service", "CleanupService.runOrphanCleanup").Msg("orphan cleanup worker stopped")
			return
		case <-ticker.C:
			log.Debug().Str("service", "CleanupService.runOrphanCleanup").Msg("orphan cleanup tick")
			s.releaseOrphans(ctx)
		}
	}
}

func (s *CleanupService) runIdempotencyCleanup(ctx context.Context) {
	log := s.logWithTrace(ctx)
	log.Info().
		Str("service", "CleanupService.runIdempotencyCleanup").
		Dur("interval", time.Hour).
		Msg("idempotency cleanup worker started")

	// Run every hour regardless of the cleanup interval.
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Str("service", "CleanupService.runIdempotencyCleanup").Msg("idempotency cleanup worker stopped")
			return
		case <-ticker.C:
			log.Debug().Str("service", "CleanupService.runIdempotencyCleanup").Msg("idempotency cleanup tick")
			s.purgeExpiredIdempotencyEntries(ctx)
		}
	}
}

func (s *CleanupService) releaseOrphans(ctx context.Context) {
	log := s.logWithTrace(ctx)
	olderThan := time.Now().UTC().Add(-s.orphanThreshold)

	affected, err := s.reservRepo.ReleaseOrphans(ctx, olderThan)
	if err != nil {
		log.Error().Err(err).Msg("orphan cleanup: failed to release orphaned reservations")
		return
	}

	if len(affected) == 0 {
		log.Debug().Str("service", "CleanupService.releaseOrphans").Msg("orphan cleanup: nothing to release")
		return
	}

	log.Info().Int("count", len(affected)).Msg("orphan cleanup: released orphaned reservations")
	s.invalidateCache(ctx, affected)
}

func (s *CleanupService) purgeExpiredIdempotencyEntries(ctx context.Context) {
	log := s.logWithTrace(ctx)
	n, err := s.idempRepo.DeleteExpired(ctx)
	if err != nil {
		log.Error().Err(err).Msg("idempotency cleanup: failed to purge expired entries")
		return
	}
	if n > 0 {
		log.Info().Int64("deleted", n).Msg("idempotency cleanup: purged expired entries")
		return
	}
	log.Debug().Str("service", "CleanupService.purgeExpiredIdempotencyEntries").Msg("idempotency cleanup: no expired entries")
}

func (s *CleanupService) invalidateCache(ctx context.Context, affected []model.SegmentReservation) {
	log := s.logWithTrace(ctx)
	if s.redis == nil || len(affected) == 0 {
		log.Debug().
			Str("service", "CleanupService.invalidateCache").
			Bool("redis_enabled", s.redis != nil).
			Int("segment_count", len(affected)).
			Msg("cleanup cache invalidation skipped")
		return
	}
	keys := make([]string, 0, len(affected))
	for _, r := range affected {
		keys = append(keys, availabilityCacheKey(r.SegmentID, r.TimeWindowStart, r.TimeWindowEnd))
	}
	if err := s.redis.Del(ctx, keys...).Err(); err != nil {
		log.Warn().Err(err).Msg("orphan cleanup: failed to invalidate availability cache")
		return
	}
	log.Debug().
		Str("service", "CleanupService.invalidateCache").
		Int("key_count", len(keys)).
		Msg("orphan cleanup availability cache invalidated")
}
