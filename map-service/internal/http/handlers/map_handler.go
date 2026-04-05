package handlers

import (
	"net/http"
	"strings"

	appErrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/tracing"
)

// MapHandler handles map-related endpoints.
type MapHandler struct{}

// NewMapHandler creates a new map handler.
func NewMapHandler() *MapHandler {
	return &MapHandler{}
}

// Node represents a selectable map node.
type Node struct {
	NodeID string  `json:"node_id"`
	Label  string  `json:"label"`
	Lat    float64 `json:"lat"`
	Lng    float64 `json:"lng"`
}

// RouteSegment represents a segment in an ordered route.
type RouteSegment struct {
	Sequence             int    `json:"sequence"`
	SegmentID            string `json:"segment_id"`
	SegmentName          string `json:"segment_name"`
	FromNodeID           string `json:"from_node_id"`
	ToNodeID             string `json:"to_node_id"`
	TraversalTimeMinutes int    `json:"traversal_time_minutes"`
}

// NodesResponse represents the response body for map nodes.
type NodesResponse struct {
	Nodes []Node `json:"nodes"`
}

// RouteResponse represents the response body for a route lookup.
type RouteResponse struct {
	Origin                    Node           `json:"origin"`
	Destination               Node           `json:"destination"`
	TotalTraversalTimeMinutes int            `json:"total_traversal_time_minutes"`
	Segments                  []RouteSegment `json:"segments"`
}

type graphEdge struct {
	SegmentID            string
	FromNodeID           string
	ToNodeID             string
	TraversalTimeMinutes int
}

var hardcodedNodes = []Node{
	{NodeID: "city", Label: "City Centre", Lat: 53.3498, Lng: -6.2603},
	{NodeID: "north", Label: "North Gate", Lat: 53.4200, Lng: -6.2603},
	{NodeID: "airport", Label: "Airport", Lat: 53.4264, Lng: -6.2499},
	{NodeID: "east", Label: "East Quay", Lat: 53.3498, Lng: -6.2100},
	{NodeID: "south", Label: "South Terminal", Lat: 53.3100, Lng: -6.2603},
	{NodeID: "industrial", Label: "Industrial Park", Lat: 53.3150, Lng: -6.2100},
	{NodeID: "west", Label: "West Depot", Lat: 53.3498, Lng: -6.3200},
	{NodeID: "port", Label: "Port Terminal", Lat: 53.3100, Lng: -6.3200},
	{NodeID: "northfield", Label: "Northfield", Lat: 53.4000, Lng: -6.3000},
	{NodeID: "riverside", Label: "Riverside", Lat: 53.3350, Lng: -6.2700},
}

var hardcodedEdges = []graphEdge{
	{SegmentID: "seg_city_north", FromNodeID: "city", ToNodeID: "north", TraversalTimeMinutes: 8},
	{SegmentID: "seg_north_airport", FromNodeID: "north", ToNodeID: "airport", TraversalTimeMinutes: 16},
	{SegmentID: "seg_city_east", FromNodeID: "city", ToNodeID: "east", TraversalTimeMinutes: 6},
	{SegmentID: "seg_east_airport", FromNodeID: "east", ToNodeID: "airport", TraversalTimeMinutes: 20},
	{SegmentID: "seg_city_riverside", FromNodeID: "city", ToNodeID: "riverside", TraversalTimeMinutes: 5},
	{SegmentID: "seg_riverside_south", FromNodeID: "riverside", ToNodeID: "south", TraversalTimeMinutes: 7},
	{SegmentID: "seg_south_industrial", FromNodeID: "south", ToNodeID: "industrial", TraversalTimeMinutes: 6},
	{SegmentID: "seg_industrial_east", FromNodeID: "industrial", ToNodeID: "east", TraversalTimeMinutes: 6},
	{SegmentID: "seg_city_west", FromNodeID: "city", ToNodeID: "west", TraversalTimeMinutes: 9},
	{SegmentID: "seg_west_port", FromNodeID: "west", ToNodeID: "port", TraversalTimeMinutes: 8},
	{SegmentID: "seg_port_south", FromNodeID: "port", ToNodeID: "south", TraversalTimeMinutes: 7},
	{SegmentID: "seg_west_northfield", FromNodeID: "west", ToNodeID: "northfield", TraversalTimeMinutes: 9},
	{SegmentID: "seg_northfield_north", FromNodeID: "northfield", ToNodeID: "north", TraversalTimeMinutes: 7},
}

// GetNodes godoc
// @Summary Get map nodes
// @Description Returns the list of selectable map nodes
// @Tags Map
// @Accept json
// @Produce json
// @Success 200 {object} NodesResponse
// @Router /api/v1/map/nodes [get]
func (h *MapHandler) GetNodes(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	response.Success(w, NodesResponse{
		Nodes: hardcodedNodes,
	}, traceID)
}

// GetRoute godoc
// @Summary Get route
// @Description Returns a route response for a given origin and destination
// @Tags Map
// @Accept json
// @Produce json
// @Param origin_node_id query string true "Origin node ID"
// @Param destination_node_id query string true "Destination node ID"
// @Success 200 {object} RouteResponse
// @Router /api/v1/map/route [get]
func (h *MapHandler) GetRoute(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	originNodeID := r.URL.Query().Get("origin_node_id")
	destinationNodeID := r.URL.Query().Get("destination_node_id")

	if originNodeID == "" || destinationNodeID == "" {
		response.Error(w, appErrors.BadRequest("origin_node_id and destination_node_id are required"), traceID)
		return
	}

	if originNodeID == destinationNodeID {
		response.Error(w, appErrors.BadRequest("origin and destination cannot be the same"), traceID)
		return
	}

	origin, ok := findNode(originNodeID)
	if !ok {
		response.Error(w, appErrors.NotFound("origin node not found"), traceID)
		return
	}

	destination, ok := findNode(destinationNodeID)
	if !ok {
		response.Error(w, appErrors.NotFound("destination node not found"), traceID)
		return
	}

	segments, totalTraversalTime, ok := calculateShortestRoute(originNodeID, destinationNodeID)
	if !ok {
		response.Error(w, appErrors.NotFound("route not found"), traceID)
		return
	}

	response.Success(w, RouteResponse{
		Origin:                    origin,
		Destination:               destination,
		TotalTraversalTimeMinutes: totalTraversalTime,
		Segments:                  segments,
	}, traceID)
}

func findNode(nodeID string) (Node, bool) {
	for _, node := range hardcodedNodes {
		if node.NodeID == nodeID {
			return node, true
		}
	}

	return Node{}, false
}

func calculateShortestRoute(originNodeID, destinationNodeID string) ([]RouteSegment, int, bool) {
	if originNodeID == destinationNodeID {
		return []RouteSegment{}, 0, true
	}

	distances := make(map[string]int, len(hardcodedNodes))
	visited := make(map[string]bool, len(hardcodedNodes))
	previous := make(map[string]graphEdge, len(hardcodedNodes))

	for _, node := range hardcodedNodes {
		distances[node.NodeID] = -1
	}
	distances[originNodeID] = 0

	for {
		currentNodeID, ok := findClosestUnvisitedNode(distances, visited)
		if !ok {
			break
		}

		if currentNodeID == destinationNodeID {
			break
		}

		visited[currentNodeID] = true

		for _, edge := range outgoingEdges(currentNodeID) {
			nextDistance := distances[currentNodeID] + edge.TraversalTimeMinutes
			currentDistance := distances[edge.ToNodeID]
			if currentDistance == -1 || nextDistance < currentDistance {
				distances[edge.ToNodeID] = nextDistance
				previous[edge.ToNodeID] = edge
			}
		}
	}

	totalTraversalTime, ok := distances[destinationNodeID]
	if !ok || totalTraversalTime == -1 {
		return nil, 0, false
	}

	pathEdges := make([]graphEdge, 0)
	currentNodeID := destinationNodeID
	for currentNodeID != originNodeID {
		edge, exists := previous[currentNodeID]
		if !exists {
			return nil, 0, false
		}
		pathEdges = append([]graphEdge{edge}, pathEdges...)
		currentNodeID = edge.FromNodeID
	}

	segments := make([]RouteSegment, 0, len(pathEdges))
	for i, edge := range pathEdges {
		segments = append(segments, RouteSegment{
			Sequence:             i + 1,
			SegmentID:            edge.SegmentID,
			SegmentName:          buildSegmentName(edge.FromNodeID, edge.ToNodeID),
			FromNodeID:           edge.FromNodeID,
			ToNodeID:             edge.ToNodeID,
			TraversalTimeMinutes: edge.TraversalTimeMinutes,
		})
	}

	return segments, totalTraversalTime, true
}

func findClosestUnvisitedNode(distances map[string]int, visited map[string]bool) (string, bool) {
	bestNodeID := ""
	bestDistance := -1

	for nodeID, distance := range distances {
		if visited[nodeID] || distance == -1 {
			continue
		}
		if bestDistance == -1 || distance < bestDistance {
			bestNodeID = nodeID
			bestDistance = distance
		}
	}

	if bestNodeID == "" {
		return "", false
	}

	return bestNodeID, true
}

func outgoingEdges(nodeID string) []graphEdge {
	edges := make([]graphEdge, 0)
	for _, edge := range hardcodedEdges {
		if edge.FromNodeID == nodeID {
			edges = append(edges, edge)
		}
	}

	return edges
}

func buildSegmentName(fromNodeID, toNodeID string) string {
	fromNode, _ := findNode(fromNodeID)
	toNode, _ := findNode(toNodeID)

	return strings.TrimSpace(fromNode.Label + " to " + toNode.Label)
}
