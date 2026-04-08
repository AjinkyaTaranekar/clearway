package event

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// OutboxFetcher is the subset of JourneyRepository used by the relay.
// Defined here to avoid a circular import between the event and repository packages.
type OutboxFetcher interface {
	FetchUnpublishedOutbox(ctx context.Context, limit int) ([]OutboxEvent, error)
	MarkOutboxPublished(ctx context.Context, ids []int64) error
}

// StreamWriter abstracts the Redis XADD operation so relayBatch can be tested
// without a real Redis server. *redis.Client satisfies this interface.
type StreamWriter interface {
	XAdd(ctx context.Context, args *redis.XAddArgs) *redis.StringCmd
}

// OutboxEvent is an event row from journey.outbox that has not yet been relayed.
type OutboxEvent struct {
	ID        int64
	EventID   string
	EventType string
	Payload   []byte
}

// RunOutboxRelay polls journey.outbox every second and publishes unpublished
// events to the Redis stream. On success it marks the rows as published.
// If Redis is unavailable the relay logs the error and retries on the next tick
// without marking the rows, ensuring no events are lost.
//
// This goroutine is the delivery guarantee for F-03: even if the process
// crashed immediately after the DB commit, the next start will pick up and
// relay any rows where published = FALSE.
func RunOutboxRelay(ctx context.Context, fetcher OutboxFetcher, rdb *redis.Client, log *zerolog.Logger) {
	if rdb == nil {
		log.Warn().Msg("outbox relay: Redis nil — relay disabled (events will not be published)")
		return
	}

	log.Info().Msg("outbox relay started")
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("outbox relay stopped")
			return
		case <-ticker.C:
			relayBatch(ctx, fetcher, rdb, log)
		}
	}
}

func relayBatch(ctx context.Context, fetcher OutboxFetcher, rdb StreamWriter, log *zerolog.Logger) {
	events, err := fetcher.FetchUnpublishedOutbox(ctx, 100)
	if err != nil {
		log.Error().Err(err).Msg("outbox relay: failed to fetch unpublished events")
		return
	}
	if len(events) == 0 {
		return
	}

	var published []int64
	for _, ev := range events {
		if err := rdb.XAdd(ctx, &redis.XAddArgs{
			Stream: StreamName,
			ID:     "*",
			Values: map[string]interface{}{"data": string(ev.Payload)},
		}).Err(); err != nil {
			log.Error().
				Err(err).
				Str("event_id", ev.EventID).
				Str("event_type", ev.EventType).
				Msg("outbox relay: XAdd failed — will retry next tick")
			// Stop processing this batch; the remaining events will be retried.
			break
		}
		published = append(published, ev.ID)
		log.Debug().
			Str("event_id", ev.EventID).
			Str("event_type", ev.EventType).
			Msg("outbox relay: event published to Redis stream")
	}

	if len(published) > 0 {
		if err := fetcher.MarkOutboxPublished(ctx, published); err != nil {
			log.Error().Err(err).Msg("outbox relay: failed to mark events as published")
		}
	}
}
