package event

import "time"

// Stream and event type constants
const (
	StreamName = "journey.events"

	EventJourneyBooked    = "journey.booked"
	EventJourneyRejected  = "journey.rejected"
	EventJourneyCancelled = "journey.cancelled"
	EventJourneyActivated = "journey.activated"
	EventJourneyCompleted = "journey.completed"
	EventJourneyExpired   = "journey.expired"
)

// Envelope wraps all events with common metadata
type Envelope struct {
	EventID   string      `json:"event_id"`
	EventType string      `json:"event_type"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

// BookedPayload is the payload for journey.booked and journey.rejected
type BookedPayload struct {
	JourneyID        string    `json:"journey_id"`
	DriverID         string    `json:"driver_id"`
	OriginLabel      string    `json:"origin_label,omitempty"`
	DestinationLabel string    `json:"destination_label,omitempty"`
	DepartureTime    time.Time `json:"departure_time"`
	EstimatedArrival time.Time `json:"estimated_arrival"`
	VehicleType      string    `json:"vehicle_type"`
	Status           string    `json:"status"`
	Reason           string    `json:"reason,omitempty"`
	RejectionReason  string    `json:"rejection_reason,omitempty"`
}

// CancelledPayload is the payload for journey.cancelled
type CancelledPayload struct {
	JourneyID        string `json:"journey_id"`
	DriverID         string `json:"driver_id"`
	OriginLabel      string `json:"origin_label,omitempty"`
	DestinationLabel string `json:"destination_label,omitempty"`
	Status           string `json:"status"`
	CancelledBy      string `json:"cancelled_by"`
	ReservationID    string `json:"reservation_id,omitempty"`
}

// SimplePayload is used for activate/complete/expire events
type SimplePayload struct {
	JourneyID        string `json:"journey_id"`
	DriverID         string `json:"driver_id"`
	OriginLabel      string `json:"origin_label,omitempty"`
	DestinationLabel string `json:"destination_label,omitempty"`
	Status           string `json:"status"`
	ReservationID    string `json:"reservation_id,omitempty"`
}
