package event

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// Publisher publishes journey events to Redis Streams
type Publisher struct {
	client *redis.Client
	log    *zerolog.Logger
}

// NewPublisher creates a new event publisher.
// If client is nil, publishing is a no-op (best-effort).
func NewPublisher(client *redis.Client, log *zerolog.Logger) *Publisher {
	return &Publisher{client: client, log: log}
}

// Publish publishes an event to the journey.events stream.
// Silently ignores errors — events are best-effort, not on the critical path.
func (p *Publisher) Publish(ctx context.Context, eventType string, payload interface{}) {
	if p.client == nil {
		return
	}

	envelope := Envelope{
		EventID:   uuid.New().String(),
		EventType: eventType,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		p.log.Warn().Err(err).Str("event_type", eventType).Msg("failed to marshal event")
		return
	}

	if err := p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: StreamName,
		Values: map[string]interface{}{"data": string(data)},
	}).Err(); err != nil {
		p.log.Warn().Err(err).Str("event_type", eventType).Msg("failed to publish event to Redis Streams")
	}
}
