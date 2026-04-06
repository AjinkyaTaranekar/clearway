package model

import "time"

// Reservation is one row per segment per journey booking.
type Reservation struct {
	ID              int64       `json:"id"`
	ReservationID   string      `json:"reservation_id"`
	JourneyID       string      `json:"journey_id"`
	SegmentID       string      `json:"segment_id"`
	TimeWindowStart time.Time   `json:"time_window_start"`
	TimeWindowEnd   time.Time   `json:"time_window_end"`
	VehicleType     VehicleType `json:"vehicle_type"`
	SlotsUsed       float64     `json:"slots_used"`
	Status          string      `json:"status"`
	CreatedAt       time.Time   `json:"created_at"`
	ReleasedAt      *time.Time  `json:"released_at,omitempty"`
}

// IdempotencyCache durably stores the full response for a given idempotency key
// so that retries return the same result without re-executing the reservation logic.
type IdempotencyCache struct {
	IdempotencyKey string    `json:"idempotency_key"`
	JourneyID      string    `json:"journey_id"`
	ReservationID  *string   `json:"reservation_id,omitempty"`
	ResponseStatus string    `json:"response_status"`
	ResponseBody   []byte    `json:"response_body"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// --- HTTP request / response types ---

// SegmentReservation is one segment + time window inside a reserve request.
type SegmentReservation struct {
	SegmentID       string    `json:"segment_id"`
	TimeWindowStart time.Time `json:"time_window_start"`
	TimeWindowEnd   time.Time `json:"time_window_end"`
}

// ReserveRequest is the body of POST /api/v1/capacity/reserve.
type ReserveRequest struct {
	JourneyID      string               `json:"journey_id"`
	IdempotencyKey string               `json:"idempotency_key"`
	VehicleType    VehicleType          `json:"vehicle_type"`
	Reservations   []SegmentReservation `json:"reservations"`
}

// ReserveSuccessResponse is returned (HTTP 201) when all segments are reserved.
type ReserveSuccessResponse struct {
	Status        string `json:"status"`
	ReservationID string `json:"reservation_id"`
	JourneyID     string `json:"journey_id"`
}

// ReserveFailResponse is returned (HTTP 200) when a segment has no capacity.
type ReserveFailResponse struct {
	Status        string         `json:"status"`
	FailedSegment *FailedSegment `json:"failed_segment"`
}

// FailedSegment describes which segment caused a reservation failure.
type FailedSegment struct {
	SegmentID       string    `json:"segment_id"`
	Reason          string    `json:"reason"`
	AvailableSlots  float64   `json:"available_slots"`
	RequestedSlots  float64   `json:"requested_slots"`
	TimeWindowStart time.Time `json:"time_window_start"`
	TimeWindowEnd   time.Time `json:"time_window_end"`
}

// CheckResponse is returned by GET /api/v1/capacity/check.
type CheckResponse struct {
	SegmentID      string  `json:"segment_id"`
	MaxCapacity    float64 `json:"max_capacity"`
	ReservedSlots  float64 `json:"reserved_slots"`
	AvailableSlots float64 `json:"available_slots"`
	CanReserve     bool    `json:"can_reserve"`
}

// OccupancyInfo is one element in the GET /api/v1/capacity/segments/occupancy list.
type OccupancyInfo struct {
	SegmentID       string  `json:"segment_id"`
	OccupancyPct    float64 `json:"occupancy_pct"`
	Level           string  `json:"level"`
	CurrentVehicles float64 `json:"current_vehicles"`
	MaxCapacity     float64 `json:"max_capacity"`
	Trend           string  `json:"trend"`
}

// OccupancyLevel converts an occupancy percentage to a human-readable level.
func OccupancyLevel(pct float64) string {
	switch {
	case pct >= 75:
		return "critical"
	case pct >= 50:
		return "high"
	case pct >= 25:
		return "moderate"
	default:
		return "low"
	}
}
