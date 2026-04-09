package model

import "time"

// JourneyEvent is a durable, user-visible lifecycle/audit record for a journey.
type JourneyEvent struct {
	EventID   string                 `json:"event_id"`
	JourneyID string                 `json:"journey_id"`
	EventType string                 `json:"event_type"`
	ActorType string                 `json:"actor_type"`
	ActorID   string                 `json:"actor_id,omitempty"`
	Note      string                 `json:"note,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt time.Time              `json:"timestamp"`
}

// JourneyEventInput is the payload used when writing an event inside DB transactions.
type JourneyEventInput struct {
	EventID   string
	JourneyID string
	EventType string
	ActorType string
	ActorID   string
	Note      string
	Metadata  map[string]interface{}
}
