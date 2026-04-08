package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/logger"
)

type CapacityClient struct {
	baseURL string
	http    *http.Client
	log     *logger.Logger
}

type capacityOccupancy struct {
	SegmentID       string  `json:"segment_id"`
	OccupancyPct    float64 `json:"occupancy_pct"`
	Level           string  `json:"level"`
	CurrentVehicles float64 `json:"current_vehicles"`
	MaxCapacity     float64 `json:"max_capacity"`
	Trend           string  `json:"trend"`
}

type capacitySegmentRegistration struct {
	SegmentID   string `json:"segment_id"`
	SegmentName string `json:"segment_name"`
	Region      string `json:"region"`
}

type registerSegmentsPayload struct {
	Segments []capacitySegmentRegistration `json:"segments"`
}

func NewCapacityClient(baseURL string, log *logger.Logger) *CapacityClient {
	if baseURL == "" {
		return nil
	}

	return &CapacityClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 5 * time.Second},
		log:     log,
	}
}

func (c *CapacityClient) GetOccupancy(ctx context.Context) ([]capacityOccupancy, error) {
	requestLog := logWithTrace(ctx)
	if c != nil && c.log != nil {
		requestLog = c.log
	}
	requestLog.Debug().
		Str("client", "CapacityClient.GetOccupancy").
		Msg("calling capacity occupancy endpoint")

	if c == nil {
		return nil, fmt.Errorf("capacity client not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/capacity/segments/occupancy", nil)
	if err != nil {
		return nil, fmt.Errorf("build capacity request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		requestLog.Error().
			Str("client", "CapacityClient.GetOccupancy").
			Err(err).
			Msg("capacity service request failed")
		return nil, fmt.Errorf("call capacity service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		requestLog.Error().
			Str("client", "CapacityClient.GetOccupancy").
			Int("status_code", resp.StatusCode).
			Msg("capacity service returned non-200 status")
		return nil, fmt.Errorf("capacity occupancy returned status %d", resp.StatusCode)
	}

	var payload []capacityOccupancy
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		requestLog.Error().
			Str("client", "CapacityClient.GetOccupancy").
			Err(err).
			Msg("failed to decode capacity occupancy response")
		return nil, fmt.Errorf("decode capacity occupancy: %w", err)
	}

	requestLog.Debug().
		Str("client", "CapacityClient.GetOccupancy").
		Int("row_count", len(payload)).
		Msg("capacity occupancy endpoint call completed")

	return payload, nil
}

// RegisterSegments ensures all provided segment IDs exist in capacity-service.
func (c *CapacityClient) RegisterSegments(ctx context.Context, segments []capacitySegmentRegistration) error {
	if len(segments) == 0 {
		return nil
	}
	if c == nil {
		return fmt.Errorf("capacity client not configured")
	}

	body, err := json.Marshal(registerSegmentsPayload{Segments: segments})
	if err != nil {
		return fmt.Errorf("encode segment registration payload: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/api/v1/capacity/segments/register",
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("build segment registration request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call segment registration endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("segment registration returned status %d", resp.StatusCode)
	}

	return nil
}
