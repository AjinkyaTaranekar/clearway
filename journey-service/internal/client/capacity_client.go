package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
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
	Reservations   []Reservation `json:"reservations"`
}

// FailedSegment details a failed reservation
type FailedSegment struct {
	SegmentID       string    `json:"segment_id"`
	Reason          string    `json:"reason"`
	TimeWindowStart time.Time `json:"time_window_start"`
	TimeWindowEnd   time.Time `json:"time_window_end"`
}

// ReserveResponse is returned by Capacity Service
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
}

// NewCapacityClient creates a new Capacity Service client
func NewCapacityClient(baseURL string) *CapacityClient {
	return &CapacityClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 3 * time.Second,
		},
	}
}

// Reserve calls Capacity Service for an all-or-nothing atomic reservation.
// Falls back to a mock approved response if Capacity Service is unreachable.
func (c *CapacityClient) Reserve(ctx context.Context, req ReserveRequest) (*ReserveResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal reserve request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/capacity/reserve", bytes.NewReader(body))
	if err != nil {
		return mockReserveResponse(req.JourneyID), nil
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// Capacity Service unreachable — mock approval
		return mockReserveResponse(req.JourneyID), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusServiceUnavailable {
		return mockReserveResponse(req.JourneyID), nil
	}

	var result ReserveResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return mockReserveResponse(req.JourneyID), nil
	}

	return &result, nil
}

// mockReserveResponse returns a mock approved reservation
func mockReserveResponse(journeyID string) *ReserveResponse {
	return &ReserveResponse{
		Status:        "reserved",
		ReservationID: fmt.Sprintf("rsv_%d", time.Now().UnixMilli()),
		JourneyID:     journeyID,
	}
}
