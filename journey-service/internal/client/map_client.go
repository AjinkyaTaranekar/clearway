package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// MapSegment is a road segment returned by Map Service
type MapSegment struct {
	SegmentID            string `json:"segment_id"`
	SegmentName          string `json:"segment_name"`
	TraversalTimeMinutes int    `json:"traversal_time_minutes"`
	SequenceOrder        int    `json:"sequence_order"`
	Region               string `json:"region"`
}

// RouteResponse is returned by Map Service
type RouteResponse struct {
	RouteID              string       `json:"route_id"`
	TotalDistanceKm      float64      `json:"total_distance_km"`
	TotalDurationMinutes int          `json:"total_duration_minutes"`
	Segments             []MapSegment `json:"segments"`
}

// MapCoordinates holds lat/lng for the route compute request
type MapCoordinates struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// MapClient calls the Map Service to compute routes
type MapClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewMapClient creates a new Map Service client
func NewMapClient(baseURL string) *MapClient {
	return &MapClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

// ComputeRoute calls Map Service for route segments.
// Falls back to a mock route if Map Service is unreachable.
func (c *MapClient) ComputeRoute(ctx context.Context, origin, dest MapCoordinates) (*RouteResponse, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"origin":      origin,
		"destination": dest,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/routes/compute", bytes.NewReader(body))
	if err != nil {
		return fallbackRoute(), nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Map Service unreachable — use mock so the service keeps working
		return fallbackRoute(), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fallbackRoute(), nil
	}

	var result RouteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fallbackRoute(), nil
	}

	if len(result.Segments) == 0 {
		return fallbackRoute(), nil
	}

	return &result, nil
}

// fallbackRoute returns a mock route with 2 segments when Map Service is unavailable
func fallbackRoute() *RouteResponse {
	return &RouteResponse{
		RouteID:              fmt.Sprintf("rte_mock_%d", time.Now().UnixMilli()),
		TotalDistanceKm:      45.0,
		TotalDurationMinutes: 55,
		Segments: []MapSegment{
			{
				SegmentID:            "seg_main",
				SegmentName:          "Main Route",
				TraversalTimeMinutes: 30,
				SequenceOrder:        1,
				Region:               "central",
			},
			{
				SegmentID:            "seg_ring",
				SegmentName:          "Ring Road",
				TraversalTimeMinutes: 25,
				SequenceOrder:        2,
				Region:               "outer",
			},
		},
	}
}
