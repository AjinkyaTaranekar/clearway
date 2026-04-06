package event

import "time"

// Event types published by Journey Service.
const (
	JourneyBooked    = "journey.booked"
	JourneyRejected  = "journey.rejected"
	JourneyCancelled = "journey.cancelled"
	JourneyActivated = "journey.activated"
	JourneyCompleted = "journey.completed"
	JourneyExpired   = "journey.expired"
)

// Envelope is the Redis Streams event envelope published by Journey Service.
type Envelope struct {
	EventID   string    `json:"event_id"`
	EventType string    `json:"event_type"`
	Timestamp time.Time `json:"timestamp"`
	SourceVM  string    `json:"source_vm"`
	Payload   Payload   `json:"payload"`
}

// Payload carries journey-specific data inside the event envelope.
type Payload struct {
	JourneyID        string `json:"journey_id"`
	DriverID         string `json:"driver_id"`
	OriginLabel      string `json:"origin_label"`
	DestinationLabel string `json:"destination_label"`
	DepartureTime    string `json:"departure_time"`
	Status           string `json:"status"`
	Reason           string `json:"reason,omitempty"` // e.g. rejection reason
}
