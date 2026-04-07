package event

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/service"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/logger"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	streamName    = "journey.events"
	consumerGroup = "notification-service"
	batchSize     = 10
	blockDuration = 5 * time.Second
	// Pending messages older than this are reclaimed from crashed consumers.
	pendingIdleThreshold = 60 * time.Second
)

// Consumer reads journey events from a Redis Stream and persists notifications.
type Consumer struct {
	redis        *redis.Client
	notifRepo    service.NotificationRepository
	consumerName string
	log          *logger.Logger
}

// NewConsumer creates a new event Consumer.
func NewConsumer(
	redisClient *redis.Client,
	notifRepo service.NotificationRepository,
	consumerName string,
	log *logger.Logger,
) *Consumer {
	return &Consumer{
		redis:        redisClient,
		notifRepo:    notifRepo,
		consumerName: consumerName,
		log:          log,
	}
}

// Start begins consuming the journey.events stream. It blocks until ctx is cancelled.
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

	// Also reclaim pending entries on startup
	go c.reclaimPending(ctx)

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
			return
		}
		if ctx.Err() != nil {
			return
		}
		c.log.Error().Err(err).Msg("event consumer: XREADGROUP error")
		time.Sleep(time.Second)
		return
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			c.process(ctx, msg)
		}
	}
}

func (c *Consumer) process(ctx context.Context, msg redis.XMessage) {
	raw, ok := msg.Values["payload"].(string)
	if !ok {
		// Try the whole message as JSON envelope
		raw = mapToJSON(msg.Values)
	}

	var env Envelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		c.log.Warn().Str("msg_id", msg.ID).Msg("event consumer: failed to parse envelope, skipping")
		// ACK to avoid infinite retry of unparseable messages
		c.ack(ctx, msg.ID)
		return
	}

	if env.EventID == "" {
		env.EventID = msg.ID
	}

	mapped, err := MapEvent(&env)
	if err != nil {
		c.log.Warn().Str("event_type", env.EventType).Msg("event consumer: unknown event type, skipping")
		c.ack(ctx, msg.ID)
		return
	}

	n := &model.Notification{
		ID:             "ntf_" + uuid.New().String()[:8],
		EventID:        env.EventID,
		DriverID:       env.Payload.DriverID,
		JourneyID:      env.Payload.JourneyID,
		EventType:      env.EventType,
		Title:          mapped.Title,
		Message:        mapped.Message,
		Type:           mapped.Type,
		DeliveryStatus: model.DeliveryPending,
	}

	if err := c.notifRepo.Insert(ctx, n); err != nil {
		// Duplicate event_id — already processed, safe to ACK
		c.log.Info().Str("event_id", env.EventID).Msg("event consumer: duplicate event, skipping")
		c.ack(ctx, msg.ID)
		return
	}

	c.log.Info().
		Str("notification_id", n.ID).
		Str("driver_id", n.DriverID).
		Str("event_type", env.EventType).
		Msg("event consumer: notification persisted")

	// Acknowledge only after DB write succeeds
	c.ack(ctx, msg.ID)
}

func (c *Consumer) ack(ctx context.Context, msgID string) {
	if err := c.redis.XAck(ctx, streamName, consumerGroup, msgID).Err(); err != nil {
		c.log.Warn().Str("msg_id", msgID).Err(err).Msg("event consumer: XACK failed")
	}
}

// reclaimPending reclaims messages that have been idle longer than the threshold,
// handling the case where this consumer (or another in the group) crashed.
func (c *Consumer) reclaimPending(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.doReclaim(ctx)
		}
	}
}

func (c *Consumer) doReclaim(ctx context.Context) {
	pending, err := c.redis.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: streamName,
		Group:  consumerGroup,
		Start:  "-",
		End:    "+",
		Count:  100,
		Idle:   pendingIdleThreshold,
	}).Result()
	if err != nil {
		c.log.Warn().Err(err).Msg("event consumer: XPENDING failed")
		return
	}

	for _, p := range pending {
		msgs, err := c.redis.XClaim(ctx, &redis.XClaimArgs{
			Stream:   streamName,
			Group:    consumerGroup,
			Consumer: c.consumerName,
			MinIdle:  pendingIdleThreshold,
			Messages: []string{p.ID},
		}).Result()
		if err != nil {
			c.log.Warn().Str("msg_id", p.ID).Err(err).Msg("event consumer: XCLAIM failed")
			continue
		}
		for _, msg := range msgs {
			c.process(ctx, msg)
		}
	}
}

// mapToJSON converts a map[string]interface{} to a JSON string.
func mapToJSON(m map[string]interface{}) string {
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}
