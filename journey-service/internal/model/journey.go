package model

import "time"

// JourneyStatus represents the state machine status of a journey
type JourneyStatus string

const (
	StatusPending   JourneyStatus = "PENDING"
	StatusApproved  JourneyStatus = "APPROVED"
	StatusRejected  JourneyStatus = "REJECTED"
	StatusCancelled JourneyStatus = "CANCELLED"
	StatusActive    JourneyStatus = "ACTIVE"
	StatusCompleted JourneyStatus = "COMPLETED"
	StatusExpired   JourneyStatus = "EXPIRED"
)

// Coordinates holds a geographic position
type Coordinates struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Journey is the main domain model
type Journey struct {
	JourneyID            string           `json:"journey_id"`
	DriverID             string           `json:"driver_id"`
	IdempotencyKey       string           `json:"-"`
	Origin               Coordinates      `json:"origin"`
	Destination          Coordinates      `json:"destination"`
	OriginPlaceID        string           `json:"origin_place_id,omitempty"`
	DestinationPlaceID   string           `json:"destination_place_id,omitempty"`
	DepartureTime        time.Time        `json:"departure_time"`
	EstimatedArrival     time.Time        `json:"estimated_arrival"`
	VehicleType          string           `json:"vehicle_type"`
	TotalDistanceKm      float64          `json:"total_distance_km,omitempty"`
	TotalDurationMinutes int              `json:"total_duration_minutes,omitempty"`
	Status               JourneyStatus    `json:"status"`
	RejectionReason      string           `json:"rejection_reason,omitempty"`
	ReservationID        string           `json:"reservation_id,omitempty"`
	Version              int              `json:"-"`
	Segments             []JourneySegment `json:"segments,omitempty"`
	CreatedAt            time.Time        `json:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
	CancelledAt          *time.Time       `json:"cancelled_at,omitempty"`
	ActivatedAt          *time.Time       `json:"activated_at,omitempty"`
	CompletedAt          *time.Time       `json:"completed_at,omitempty"`
	ExpiredAt            *time.Time       `json:"expired_at,omitempty"`
}

// JourneySegment is a road segment assigned a time window for a specific journey
type JourneySegment struct {
	SegmentID        string    `json:"segment_id"`
	SegmentName      string    `json:"segment_name"`
	SequenceOrder    int       `json:"sequence_order"`
	TimeWindowStart  time.Time `json:"time_window_start"`
	TimeWindowEnd    time.Time `json:"time_window_end"`
	TraversalMinutes int       `json:"traversal_minutes"`
	Region           string    `json:"region,omitempty"`
	FromLat          *float64  `json:"from_lat,omitempty"`
	FromLng          *float64  `json:"from_lng,omitempty"`
	ToLat            *float64  `json:"to_lat,omitempty"`
	ToLng            *float64  `json:"to_lng,omitempty"`
}
