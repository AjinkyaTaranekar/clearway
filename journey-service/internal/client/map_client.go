package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/sony/gobreaker"
)

// MapCoordinates holds lat/lng for route cache key computation.
// The journey service receives coordinates from drivers; this type is the
// cache key so the same origin/destination pair resolves from Redis without
// calling the map service again.
type MapCoordinates struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// MapSegment is a road segment returned by Map Service.
// JSON tags match the map service's actual response shape.
type MapSegment struct {
	SegmentID            string `json:"segment_id"`
	SegmentName          string `json:"segment_name"`
	TraversalTimeMinutes int    `json:"traversal_time_minutes"`
	SequenceOrder        int    `json:"sequence"` // map service returns "sequence"
	Region               string `json:"region"`   // not in map response; left empty
}

// RouteResponse is the internal representation of a route returned by the Map Service.
type RouteResponse struct {
	TotalTraversalTimeMinutes int          `json:"total_traversal_time_minutes"`
	Segments                  []MapSegment `json:"segments"`
}

// ---------------------------------------------------------------------------
// Internal types matching the Map Service's JSON envelope + route shape
// ---------------------------------------------------------------------------

// mapNode matches the Node struct returned by GET /api/v1/map/nodes.
type mapNode struct {
	NodeID string  `json:"node_id"`
	Label  string  `json:"label"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
}

// mapAPIEnvelope is the standard {"success":true,"data":...} wrapper that all
// map service endpoints return.
type mapAPIEnvelope struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
}

// mapNodesData is the inner data shape of GET /api/v1/map/nodes.
type mapNodesData struct {
	Nodes []mapNode `json:"nodes"`
}

// mapRouteData is the inner data shape of GET /api/v1/map/route.
type mapRouteData struct {
	TotalTraversalTimeMinutes int          `json:"total_traversal_time_minutes"`
	Segments                  []MapSegment `json:"segments"`
}

// ---------------------------------------------------------------------------
// MapClient
// ---------------------------------------------------------------------------

// MapClient calls the Map Service to compute routes.
type MapClient struct {
	baseURL    string
	httpClient *http.Client
	breaker    *gobreaker.CircuitBreaker

	// nodes cache — avoids fetching /api/v1/map/nodes on every request.
	nodesMu        sync.RWMutex
	nodes          []mapNode
	nodesFetchedAt time.Time
}

// NewMapClient creates a new Map Service client.
func NewMapClient(baseURL string) *MapClient {
	breaker := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        "map-service",
		MaxRequests: 1,
		Interval:    30 * time.Second,
		Timeout:     10 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 5
		},
	})

	return &MapClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		breaker: breaker,
	}
}

func isMapCircuitOpen(err error) bool {
	return errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests)
}

// fetchNodes retrieves the node list from GET /api/v1/map/nodes and stores it.
func (c *MapClient) fetchNodes(ctx context.Context) error {
	nodesCall := func() ([]mapNode, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			c.baseURL+"/api/v1/map/nodes", nil)
		if err != nil {
			return nil, fmt.Errorf("create nodes request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch nodes: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("map service nodes: unexpected status %d", resp.StatusCode)
		}

		var envelope mapAPIEnvelope
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			return nil, fmt.Errorf("decode nodes envelope: %w", err)
		}
		if !envelope.Success {
			return nil, fmt.Errorf("map service nodes: success=false in response")
		}

		var data mapNodesData
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return nil, fmt.Errorf("decode nodes data: %w", err)
		}

		return data.Nodes, nil
	}

	var nodes []mapNode
	if c.breaker == nil {
		resolved, err := nodesCall()
		if err != nil {
			return err
		}
		nodes = resolved
	} else {
		result, err := c.breaker.Execute(func() (interface{}, error) {
			return nodesCall()
		})
		if err != nil {
			if isMapCircuitOpen(err) {
				return fmt.Errorf("map service circuit open")
			}
			return err
		}

		resolved, ok := result.([]mapNode)
		if !ok {
			return fmt.Errorf("map service nodes: unexpected breaker result type %T", result)
		}
		nodes = resolved
	}

	c.nodesMu.Lock()
	c.nodes = nodes
	c.nodesFetchedAt = time.Now()
	c.nodesMu.Unlock()
	return nil
}

// getNodes returns the cached node list, refreshing if it is older than 1 hour.
// If the refresh fails but stale data is available, the stale data is returned
// so that a temporary map service outage does not break all routing.
func (c *MapClient) getNodes(ctx context.Context) ([]mapNode, error) {
	c.nodesMu.RLock()
	nodes := c.nodes
	fetchedAt := c.nodesFetchedAt
	c.nodesMu.RUnlock()

	if len(nodes) > 0 && time.Since(fetchedAt) < time.Hour {
		return nodes, nil
	}

	refreshErr := c.fetchNodes(ctx)

	c.nodesMu.RLock()
	nodes = c.nodes
	c.nodesMu.RUnlock()

	if len(nodes) > 0 {
		return nodes, nil // return even if stale when refresh failed
	}
	if refreshErr != nil {
		return nil, fmt.Errorf("map service unreachable and no cached nodes: %w", refreshErr)
	}
	return nil, fmt.Errorf("map service returned an empty node list")
}

// nearestNodeID returns the node ID of the node closest to (lat, lng) using
// Euclidean distance in the lat/lng plane (adequate for a city-scale graph).
func nearestNodeID(nodes []mapNode, lat, lng float64) string {
	best := ""
	bestDist := math.MaxFloat64
	for _, n := range nodes {
		d := math.Sqrt((n.Lat-lat)*(n.Lat-lat) + (n.Lng-lng)*(n.Lng-lng))
		if d < bestDist {
			bestDist = d
			best = n.NodeID
		}
	}
	return best
}

// ComputeRoute converts lat/lng coordinates to the nearest map node IDs and
// calls GET /api/v1/map/route?origin_node_id=X&destination_node_id=Y.
//
// Returns a non-nil error when the map service is unreachable or returns an
// unexpected response. The caller (journey service) must treat this as a hard
// failure — no silent fallback is performed here.
func (c *MapClient) ComputeRoute(ctx context.Context, origin, dest MapCoordinates) (*RouteResponse, error) {
	nodes, err := c.getNodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("map service unavailable (nodes): %w", err)
	}

	originNodeID := nearestNodeID(nodes, origin.Lat, origin.Lng)
	destNodeID := nearestNodeID(nodes, dest.Lat, dest.Lng)

	if originNodeID == "" || destNodeID == "" {
		return nil, fmt.Errorf("map service: could not resolve coordinates to nodes")
	}
	if originNodeID == destNodeID {
		return nil, fmt.Errorf("map service: origin and destination resolve to the same node (%s)", originNodeID)
	}

	url := fmt.Sprintf("%s/api/v1/map/route?origin_node_id=%s&destination_node_id=%s",
		c.baseURL, originNodeID, destNodeID)

	routeCall := func() (*RouteResponse, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("map service: build request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("map service unreachable: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("map service: unexpected status %d for route %s→%s",
				resp.StatusCode, originNodeID, destNodeID)
		}

		var envelope mapAPIEnvelope
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			return nil, fmt.Errorf("map service: decode route envelope: %w", err)
		}
		if !envelope.Success {
			return nil, fmt.Errorf("map service: success=false for route %s→%s", originNodeID, destNodeID)
		}

		var data mapRouteData
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return nil, fmt.Errorf("map service: decode route data: %w", err)
		}

		if len(data.Segments) == 0 {
			return nil, fmt.Errorf("map service: no segments returned for route %s→%s", originNodeID, destNodeID)
		}

		return &RouteResponse{
			TotalTraversalTimeMinutes: data.TotalTraversalTimeMinutes,
			Segments:                  data.Segments,
		}, nil
	}

	if c.breaker == nil {
		return routeCall()
	}

	result, err := c.breaker.Execute(func() (interface{}, error) {
		return routeCall()
	})
	if err != nil {
		if isMapCircuitOpen(err) {
			return nil, fmt.Errorf("map service circuit open")
		}
		return nil, err
	}

	route, ok := result.(*RouteResponse)
	if !ok || route == nil {
		return nil, fmt.Errorf("map service: unexpected breaker result type %T", result)
	}

	return route, nil
}
