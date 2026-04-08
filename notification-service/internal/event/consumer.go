package event

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/fcm"
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
	maxDeliveryAttempts  = 3
)

// Consumer reads journey events from a Redis Stream and persists notifications.
type Consumer struct {
	redis        *redis.Client
	notifRepo    service.NotificationRepository
	tokenRepo    service.DeviceTokenRepository
	fcmClient    *fcm.Client
	consumerName string
	log          *logger.Logger
}

// NewConsumer creates a new event Consumer.
func NewConsumer(
	redisClient *redis.Client,
	notifRepo service.NotificationRepository,
	tokenRepo service.DeviceTokenRepository,
	fcmClient *fcm.Client,
	consumerName string,
	log *logger.Logger,
) *Consumer {
	return &Consumer{
		redis:        redisClient,
		notifRepo:    notifRepo,
		tokenRepo:    tokenRepo,
		fcmClient:    fcmClient,
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
	err := c.redis.XGroupCreateMkStream(ctx, streamName, consumerGroup, "0").Err()
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
	raw, ok := msg.Values["data"].(string)
	if !ok {
		c.log.Warn().Str("msg_id", msg.ID).Msg("event consumer: missing 'data' key in message")
		c.ack(ctx, msg.ID)
		return
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

	c.dispatchPush(ctx, n)

	// Acknowledge only after DB write succeeds
	c.ack(ctx, msg.ID)
}

func (c *Consumer) dispatchPush(ctx context.Context, n *model.Notification) {
	if c.fcmClient == nil {
		c.log.Warn().Str("notification_id", n.ID).Msg("event consumer: FCM client disabled; marking notification as skipped")
		c.updateDeliveryStatus(ctx, n.ID, model.DeliverySkipped, 0, "fcm client not configured", nil, nil)
		return
	}

	tokens, err := c.tokenRepo.FindActiveByDriver(ctx, n.DriverID)
	if err != nil {
		c.log.Error().Err(err).Str("notification_id", n.ID).Msg("event consumer: failed to load device tokens")
		c.markDeliveryFailed(ctx, n.ID, 0, err.Error())
		return
	}
	if len(tokens) == 0 {
		c.log.Info().Str("notification_id", n.ID).Str("driver_id", n.DriverID).Msg("event consumer: no active device tokens; marking skipped")
		c.updateDeliveryStatus(ctx, n.ID, model.DeliverySkipped, 0, "no active device tokens", nil, nil)
		return
	}

	var (
		lastErr        string
		highestRetry   int
		atLeastOneSent bool
	)

	for _, token := range tokens {
		retryCount, err := c.sendWithRetry(ctx, token, n)
		if retryCount > highestRetry {
			highestRetry = retryCount
		}
		if err == nil {
			atLeastOneSent = true
			continue
		}

		lastErr = err.Error()
		var deliveryErr *fcm.DeliveryError
		if errors.As(err, &deliveryErr) && deliveryErr.Permanent {
			c.log.Warn().
				Str("notification_id", n.ID).
				Str("device_token_id", token.ID).
				Str("code", deliveryErr.Code).
				Msg("event consumer: deactivating stale device token")
			if deactivateErr := c.tokenRepo.Deactivate(ctx, token.ID, strings.ToLower(deliveryErr.Code)); deactivateErr != nil {
				c.log.Warn().Err(deactivateErr).Str("device_token_id", token.ID).Msg("event consumer: failed to deactivate device token")
			}
		}
	}

	if atLeastOneSent {
		now := time.Now().UTC()
		c.updateDeliveryStatus(ctx, n.ID, model.DeliverySent, highestRetry, lastErr, &now, nil)
		return
	}

	c.markDeliveryFailed(ctx, n.ID, highestRetry, lastErr)
}

func (c *Consumer) sendWithRetry(ctx context.Context, token model.DeviceToken, n *model.Notification) (int, error) {
	var lastErr error
	for attempt := 1; attempt <= maxDeliveryAttempts; attempt++ {
		err := c.fcmClient.Send(ctx, token.FCMToken, n)
		if err == nil {
			return attempt - 1, nil
		}

		lastErr = err
		var deliveryErr *fcm.DeliveryError
		if errors.As(err, &deliveryErr) && deliveryErr.Permanent {
			return attempt - 1, err
		}

		if attempt == maxDeliveryAttempts {
			break
		}

		retryingStatus := model.DeliveryRetrying
		c.updateDeliveryStatus(ctx, n.ID, retryingStatus, attempt, err.Error(), nil, nil)
		time.Sleep(time.Duration(1<<uint(attempt-1)) * time.Second)
	}

	return maxDeliveryAttempts, lastErr
}

func (c *Consumer) markDeliveryFailed(ctx context.Context, notificationID string, retryCount int, lastErr string) {
	now := time.Now().UTC()
	c.updateDeliveryStatus(ctx, notificationID, model.DeliveryFailed, retryCount, lastErr, nil, &now)
}

func (c *Consumer) updateDeliveryStatus(ctx context.Context, notificationID, status string, retryCount int, lastErr string, sentAt, failedAt *time.Time) {
	if err := c.notifRepo.UpdateDeliveryStatus(ctx, notificationID, status, retryCount, lastErr, sentAt, failedAt); err != nil {
		c.log.Warn().
			Err(err).
			Str("notification_id", notificationID).
			Str("status", status).
			Msg("event consumer: failed to update delivery status")
	}
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
