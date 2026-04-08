package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type CapacityClient struct {
	baseURL string
	http    *http.Client
}

type capacityOccupancy struct {
	SegmentID       string  `json:"segment_id"`
	OccupancyPct    float64 `json:"occupancy_pct"`
	Level           string  `json:"level"`
	CurrentVehicles float64 `json:"current_vehicles"`
	MaxCapacity     float64 `json:"max_capacity"`
	Trend           string  `json:"trend"`
}

func NewCapacityClient(baseURL string) *CapacityClient {
	if baseURL == "" {
		return nil
	}

	return &CapacityClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *CapacityClient) GetOccupancy(ctx context.Context) ([]capacityOccupancy, error) {
	if c == nil {
		return nil, fmt.Errorf("capacity client not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/capacity/segments/occupancy", nil)
	if err != nil {
		return nil, fmt.Errorf("build capacity request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call capacity service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("capacity occupancy returned status %d", resp.StatusCode)
	}

	var payload []capacityOccupancy
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode capacity occupancy: %w", err)
	}

	return payload, nil
}
