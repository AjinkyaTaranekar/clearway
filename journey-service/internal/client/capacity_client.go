package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/sony/gobreaker"
)

// Reservation is a single segment reservation request
type Reservation struct {
	SegmentID       string    `json:"segment_id"`
	TimeWindowStart time.Time `json:"time_window_start"`
	TimeWindowEnd   time.Time `json:"time_window_end"`
}

// ReserveRequest is sent to Capacity Service
type ReserveRequest struct {
	JourneyID      string        `json:"journey_id"`
	IdempotencyKey string        `json:"idempotency_key"`
	VehicleType    string        `json:"vehicle_type"`
	PriorityLevel  string        `json:"priority_level,omitempty"`
	Reservations   []Reservation `json:"reservations"`
}

// FailedSegment details a failed reservation
type FailedSegment struct {
	SegmentID       string     `json:"segment_id"`
	Reason          string     `json:"reason"`
	AvailableSlots  float64    `json:"available_slots"`
	RequestedSlots  float64    `json:"requested_slots"`
	TimeWindowStart time.Time  `json:"time_window_start"`
	TimeWindowEnd   time.Time  `json:"time_window_end"`
	ClosureReason   string     `json:"closure_reason,omitempty"`
	ClosureStart    *time.Time `json:"closure_start,omitempty"`
	ClosureEnd      *time.Time `json:"closure_end,omitempty"`
}

// ReserveResponse is returned by Capacity Service.
// On success (HTTP 201): Status="reserved", ReservationID is set.
// On capacity failure (HTTP 200): Status="failed", FailedSegment is set.
type ReserveResponse struct {
	Status        string         `json:"status"` // "reserved" or "failed"
	ReservationID string         `json:"reservation_id,omitempty"`
	JourneyID     string         `json:"journey_id,omitempty"`
	FailedSegment *FailedSegment `json:"failed_segment,omitempty"`
}

// CapacityClient calls the Capacity Service
type CapacityClient struct {
	baseURL    string
	httpClient *http.Client
	breaker    *gobreaker.CircuitBreaker
}

// NewCapacityClient creates a new Capacity Service client
func NewCapacityClient(baseURL string) *CapacityClient {
	breaker := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "capacity-service",
		MaxRequests: 1,
		Interval:    30 * time.Second,
		Timeout:     10 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 5
		},
	})

	return &CapacityClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		breaker: breaker,
	}
}

func isCapacityCircuitOpen(err error) bool {
	return errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests)
}

// Reserve calls Capacity Service for an all-or-nothing atomic reservation.
//
// The Capacity Service responds with:
//   - HTTP 201 + {"status":"reserved","reservation_id":...} on success
//   - HTTP 200 + {"status":"failed","failed_segment":...} when a segment is at capacity
//   - HTTP 4xx/5xx on bad request or internal error
//
// Returns an error if the service is unreachable or returns an unexpected
// response — no silent fallback is performed.  Masking a capacity-service
// failure with a fake approval defeats the core double-booking prevention
// guarantee of the system.
func (c *CapacityClient) Reserve(ctx context.Context, req ReserveRequest) (*ReserveResponse, error) {
	reserveCall := func() (*ReserveResponse, error) {
		body, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal reserve request: %w", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.baseURL+"/api/v1/capacity/reserve", bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("capacity service: build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("capacity service unreachable: %w", err)
		}
		defer resp.Body.Close()

		// 200 = capacity failure (valid business outcome) and 201 = reserved.
		// Anything else is an unexpected error from the service.
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return nil, fmt.Errorf("capacity service error: unexpected status %d", resp.StatusCode)
		}

		var result ReserveResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("capacity service: decode response: %w", err)
		}

		if result.Status == "" {
			return nil, fmt.Errorf("capacity service: missing status field in response")
		}

		return &result, nil
	}

	if c.breaker == nil {
		return reserveCall()
	}

	value, err := c.breaker.Execute(func() (interface{}, error) {
		return reserveCall()
	})
	if err != nil {
		if isCapacityCircuitOpen(err) {
			return nil, fmt.Errorf("capacity service circuit open")
		}
		return nil, err
	}

	result, ok := value.(*ReserveResponse)
	if !ok || result == nil {
		return nil, fmt.Errorf("capacity service: unexpected breaker result type %T", value)
	}

	return result, nil
}
