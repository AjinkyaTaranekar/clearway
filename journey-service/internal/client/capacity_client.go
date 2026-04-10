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
		Name: "capacity-service",
		// Allow 3 probe requests in half-open state; with only 1 probe a single
		// slow response during recovery causes immediate re-opening.
		MaxRequests: 3,
		// Widen the counting window: cross-region CockroachDB writes can be
		// transiently slow without the service being truly down.
		Interval: 60 * time.Second,
		// Stay open longer before probing; 10s is too short when CRDB write
		// latency in US/APAC already saturates the 5s client timeout.
		Timeout: 30 * time.Second,
		// Require more consecutive failures before opening; avoids locking out
		// all journey creation on a brief latency spike.
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 10
		},
	})

	return &CapacityClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			// 15s gives cross-region CockroachDB writes (US/APAC) enough headroom.
			// The previous 5s timeout was too tight: APAC p95 capacity latency
			// already reached 4.7s under load, causing routine timeouts that
			// tripped the circuit breaker and blocked all journey creation.
			Timeout: 15 * time.Second,
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
//   - HTTP 4xx on bad request
//   - HTTP 5xx on internal / DB error (not counted toward circuit breaker)
//
// Returns an error if the service is unreachable or returns an unexpected
// response - no silent fallback is performed.  Masking a capacity-service
// failure with a fake approval defeats the core double-booking prevention
// guarantee of the system.
//
// Circuit-breaker policy: only network-level failures (timeouts, connection
// refused) trip the breaker.  HTTP 5xx responses mean the capacity-service is
// reachable but temporarily overloaded (e.g. CockroachDB query cancellations
// under cross-region load).  Counting those as circuit failures caused the
// breaker to stay open permanently in US/APAC during load tests.
func (c *CapacityClient) Reserve(ctx context.Context, req ReserveRequest) (*ReserveResponse, error) {
	// networkCall wraps only the HTTP round-trip. It returns an error only on
	// connection failures so the circuit breaker counts solely those events.
	// HTTP 5xx from the capacity service is returned as a non-nil status code
	// without an error, keeping it out of the breaker's failure count.
	type callResult struct {
		resp       *ReserveResponse
		httpStatus int // non-zero only for unexpected HTTP status codes
	}

	networkCall := func() (callResult, error) {
		body, err := json.Marshal(req)
		if err != nil {
			return callResult{}, fmt.Errorf("failed to marshal reserve request: %w", err)
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.baseURL+"/api/v1/capacity/reserve", bytes.NewReader(body))
		if err != nil {
			return callResult{}, fmt.Errorf("capacity service: build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			// Network error: counts toward the circuit breaker.
			return callResult{}, fmt.Errorf("capacity service unreachable: %w", err)
		}
		defer resp.Body.Close()

		// 200 = capacity failure (valid business outcome), 201 = reserved.
		// 5xx means the capacity service is up but its DB is struggling —
		// return the status without an error so the breaker is not tripped.
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return callResult{httpStatus: resp.StatusCode}, nil
		}

		var result ReserveResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return callResult{}, fmt.Errorf("capacity service: decode response: %w", err)
		}
		if result.Status == "" {
			return callResult{}, fmt.Errorf("capacity service: missing status field in response")
		}
		return callResult{resp: &result}, nil
	}

	var cr callResult
	if c.breaker == nil {
		var err error
		cr, err = networkCall()
		if err != nil {
			return nil, err
		}
	} else {
		raw, err := c.breaker.Execute(func() (interface{}, error) {
			r, e := networkCall()
			return r, e
		})
		if err != nil {
			if isCapacityCircuitOpen(err) {
				return nil, fmt.Errorf("capacity service circuit open")
			}
			return nil, err
		}
		cr = raw.(callResult)
	}

	if cr.httpStatus != 0 {
		return nil, fmt.Errorf("capacity service error: unexpected status %d", cr.httpStatus)
	}

	return cr.resp, nil
}
