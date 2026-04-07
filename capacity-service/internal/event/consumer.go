package event

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/repository"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/service"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/logger"
	"github.com/redis/go-redis/v9"
)

const (
	streamName    = "journey.events"
	consumerGroup = "capacity-service"
	maxRetries    = 3
	batchSize     = 10
	blockDuration = 5 * time.Second
)

// Consumer reads journey events from a Redis Stream and releases capacity slots
// for cancelled, completed, or expired journeys.
type Consumer struct {
	redis        *redis.Client
	reservRepo   *repository.ReservationRepo
	reservSvc    *service.ReservationService
	consumerName string
	log          *logger.Logger
}

// NewConsumer creates a new event Consumer.
func NewConsumer(
	redisClient *redis.Client,
	reservRepo *repository.ReservationRepo,
	reservSvc *service.ReservationService,
	consumerName string,
	log *logger.Logger,
) *Consumer {
	return &Consumer{
		redis:        redisClient,
		reservRepo:   reservRepo,
		reservSvc:    reservSvc,
		consumerName: consumerName,
		log:          log,
	}
}

// Start begins consuming the journey.events stream. It returns when ctx is cancelled.
func (c *Consumer) Start(ctx context.Context) {
	if err := c.ensureConsumerGroup(ctx); err != nil {
		c.log.Error().Err(err).Msg("event consumer: failed to create consumer group — stream processing disabled")
		return
	}

	c.log.Info().
		Str("stream", streamName).
		Str("group", consumerGroup).
		Str("consumer", c.consumerName).
		Msg("event consumer: started")

	for {
		select {
		case <-ctx.Done():
			c.log.Info().Msg("event consumer: shutting down")
			return
		default:
			c.poll(ctx)
		}
	}
}

func (c *Consumer) ensureConsumerGroup(ctx context.Context) error {
	// MKSTREAM creates the stream if it doesn't exist yet.
	err := c.redis.XGroupCreateMkStream(ctx, streamName, consumerGroup, "$").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return fmt.Errorf("XGROUP CREATE: %w", err)
	}
	return nil
}

func (c *Consumer) poll(ctx context.Context) {
	streams, err := c.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    consumerGroup,
		Consumer: c.consumerName,
		Streams:  []string{streamName, ">"},
		Count:    batchSize,
		Block:    blockDuration,
	}).Result()
	if err != nil {
		if err == redis.Nil {
			return // no new messages, try again
		}
		// Context cancelled — normal shutdown.
		if ctx.Err() != nil {
			return
		}
		c.log.Error().Err(err).Msg("event consumer: XREADGROUP error")
		time.Sleep(time.Second)
		return
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			c.processWithRetry(ctx, msg)
		}
	}
}

func (c *Consumer) processWithRetry(ctx context.Context, msg redis.XMessage) {
	var lastErr error
	backoff := time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := c.processMessage(ctx, msg); err != nil {
			lastErr = err
			c.log.Warn().
				Err(err).
				Str("message_id", msg.ID).
				Int("attempt", attempt).
				Msg("event consumer: processing failed, will retry")
			time.Sleep(backoff)
			backoff *= 2
			continue
		}
		// Success — acknowledge the message.
		if err := c.redis.XAck(ctx, streamName, consumerGroup, msg.ID).Err(); err != nil {
			c.log.Warn().Err(err).Str("message_id", msg.ID).Msg("event consumer: XACK failed")
		}
		return
	}

	// All retries exhausted — acknowledge to prevent infinite redelivery and log the failure.
	c.log.Error().
		Err(lastErr).
		Str("message_id", msg.ID).
		Msg("event consumer: giving up after max retries, acknowledging message")
	c.redis.XAck(ctx, streamName, consumerGroup, msg.ID)
}

// journeyEvent is the envelope used by Journey Service when publishing events.
type journeyEvent struct {
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

type journeyPayload struct {
	JourneyID string `json:"journey_id"`
}

func (c *Consumer) processMessage(ctx context.Context, msg redis.XMessage) error {
	rawData, ok := msg.Values["data"]
	if !ok {
		// Try the whole map encoded as a single JSON string (some publishers use this).
		rawData = encodeMapToJSON(msg.Values)
	}

	dataStr, ok := rawData.(string)
	if !ok {
		return fmt.Errorf("unexpected message value type %T", rawData)
	}

	var event journeyEvent
	if err := json.Unmarshal([]byte(dataStr), &event); err != nil {
		return fmt.Errorf("unmarshal event: %w", err)
	}

	switch event.EventType {
	case "journey.cancelled", "journey.completed", "journey.expired":
		// fall through to release logic
	default:
		// Not a release event — acknowledge silently.
		return nil
	}

	var payload journeyPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}
	if payload.JourneyID == "" {
		return fmt.Errorf("event payload missing journey_id")
	}

	affected, err := c.reservRepo.ReleaseByJourneyID(ctx, payload.JourneyID)
	if err != nil {
		return fmt.Errorf("release journey %s: %w", payload.JourneyID, err)
	}

	if len(affected) > 0 {
		c.reservSvc.InvalidateCacheForJourney(ctx, affected)
		c.log.Info().
			Str("journey_id", payload.JourneyID).
			Str("event_type", event.EventType).
			Int("segments_released", len(affected)).
			Msg("event consumer: released capacity slots")
	}

	return nil
}

func encodeMapToJSON(m map[string]interface{}) string {
	b, _ := json.Marshal(m)
	return string(b)
}
