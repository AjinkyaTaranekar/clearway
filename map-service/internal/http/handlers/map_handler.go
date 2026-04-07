package handlers

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"

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
	Sequence             int    `json:"sequence,omitempty"`
	SequenceOrder        int    `json:"sequence_order,omitempty"`
	SegmentID            string `json:"segment_id"`
	SegmentName          string `json:"segment_name"`
	FromNodeID           string `json:"from_node_id,omitempty"`
	ToNodeID             string `json:"to_node_id,omitempty"`
	TraversalTimeMinutes int    `json:"traversal_time_minutes"`
	Region               string `json:"region,omitempty"`
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

type RoutePointRequest struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

type ComputeRouteRequest struct {
	Origin      RoutePointRequest `json:"origin"`
	Destination RoutePointRequest `json:"destination"`
}

type ComputeRouteResponse struct {
	RouteID              string         `json:"route_id"`
	TotalDistanceKm      float64        `json:"total_distance_km"`
	TotalDurationMinutes int            `json:"total_duration_minutes"`
	Segments             []RouteSegment `json:"segments"`
}

type graphEdge struct {
	SegmentName          string
	Region               string
	SegmentID            string
	FromNodeID           string
	ToNodeID             string
	TraversalTimeMinutes int
	DistanceKm           float64
}

type bidirectionalRoad struct {
	ForwardSegmentID     string
	ForwardSegmentName   string
	ReverseSegmentID     string
	ReverseSegmentName   string
	Region               string
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

var hardcodedEdges = buildHardcodedEdges()

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

	route, err := buildRoute(originNodeID, destinationNodeID)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}

	response.Success(w, route, traceID)
}

// ComputeRoute godoc
// @Summary Compute route from coordinates
// @Description Returns a route response for a given origin and destination coordinate pair
// @Tags Map
// @Accept json
// @Produce json
// @Param request body ComputeRouteRequest true "Origin and destination coordinates"
// @Success 200 {object} ComputeRouteResponse
// @Router /api/v1/routes/compute [post]
func (h *MapHandler) ComputeRoute(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())

	var req ComputeRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, appErrors.BadRequest("invalid request body"), traceID)
		return
	}

	origin, ok := findNearestNode(req.Origin.Lat, req.Origin.Lng)
	if !ok {
		response.Error(w, appErrors.NotFound("origin node not found"), traceID)
		return
	}

	destination, ok := findNearestNode(req.Destination.Lat, req.Destination.Lng)
	if !ok {
		response.Error(w, appErrors.NotFound("destination node not found"), traceID)
		return
	}

	if origin.NodeID == destination.NodeID {
		response.Error(w, appErrors.BadRequest("origin and destination resolve to the same node"), traceID)
		return
	}

	computeRoute, err := buildComputeRoute(origin.NodeID, destination.NodeID)
	if err != nil {
		response.Error(w, err, traceID)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(computeRoute)
}

func findNode(nodeID string) (Node, bool) {
	for _, node := range hardcodedNodes {
		if node.NodeID == nodeID {
			return node, true
		}
	}

	return Node{}, false
}

func findNearestNode(lat, lng float64) (Node, bool) {
	if len(hardcodedNodes) == 0 {
		return Node{}, false
	}

	bestNode := hardcodedNodes[0]
	bestDistance := coordinateDistanceKm(lat, lng, bestNode.Lat, bestNode.Lng)

	for _, node := range hardcodedNodes[1:] {
		distance := coordinateDistanceKm(lat, lng, node.Lat, node.Lng)
		if distance < bestDistance {
			bestNode = node
			bestDistance = distance
		}
	}

	return bestNode, true
}

func buildRoute(originNodeID, destinationNodeID string) (RouteResponse, error) {
	origin, ok := findNode(originNodeID)
	if !ok {
		return RouteResponse{}, appErrors.NotFound("origin node not found")
	}

	destination, ok := findNode(destinationNodeID)
	if !ok {
		return RouteResponse{}, appErrors.NotFound("destination node not found")
	}

	segments, totalTraversalTime, _, ok := calculateShortestRoute(originNodeID, destinationNodeID)
	if !ok {
		return RouteResponse{}, appErrors.NotFound("route not found")
	}

	return RouteResponse{
		Origin:                    origin,
		Destination:               destination,
		TotalTraversalTimeMinutes: totalTraversalTime,
		Segments:                  segments,
	}, nil
}

func buildComputeRoute(originNodeID, destinationNodeID string) (ComputeRouteResponse, error) {
	segments, totalTraversalTime, totalDistanceKm, ok := calculateShortestRoute(originNodeID, destinationNodeID)
	if !ok {
		return ComputeRouteResponse{}, appErrors.NotFound("route not found")
	}

	return ComputeRouteResponse{
		RouteID:              fmt.Sprintf("rte_%s_%s", originNodeID, destinationNodeID),
		TotalDistanceKm:      totalDistanceKm,
		TotalDurationMinutes: totalTraversalTime,
		Segments:             segments,
	}, nil
}

func calculateShortestRoute(originNodeID, destinationNodeID string) ([]RouteSegment, int, float64, bool) {
	if originNodeID == destinationNodeID {
		return []RouteSegment{}, 0, 0, true
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
		return nil, 0, 0, false
	}

	pathEdges := make([]graphEdge, 0)
	currentNodeID := destinationNodeID
	for currentNodeID != originNodeID {
		edge, exists := previous[currentNodeID]
		if !exists {
			return nil, 0, 0, false
		}
		pathEdges = append([]graphEdge{edge}, pathEdges...)
		currentNodeID = edge.FromNodeID
	}

	segments := make([]RouteSegment, 0, len(pathEdges))
	totalDistanceKm := 0.0
	for i, edge := range pathEdges {
		totalDistanceKm += edge.DistanceKm
		segments = append(segments, RouteSegment{
			Sequence:             i + 1,
			SequenceOrder:        i + 1,
			SegmentID:            edge.SegmentID,
			SegmentName:          edge.SegmentName,
			FromNodeID:           edge.FromNodeID,
			ToNodeID:             edge.ToNodeID,
			TraversalTimeMinutes: edge.TraversalTimeMinutes,
			Region:               edge.Region,
		})
	}

	return segments, totalTraversalTime, math.Round(totalDistanceKm*100) / 100, true
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

func buildHardcodedEdges() []graphEdge {
	roads := []bidirectionalRoad{
		{ForwardSegmentID: "seg_m50", ForwardSegmentName: "M50 Motorway (North-South)", ReverseSegmentID: "seg_m50", ReverseSegmentName: "M50 Motorway (North-South)", Region: "central", FromNodeID: "city", ToNodeID: "north", TraversalTimeMinutes: 8},
		{ForwardSegmentID: "seg_m1_n", ForwardSegmentName: "M1 Motorway Northbound", ReverseSegmentID: "seg_m1_s", ReverseSegmentName: "M1 Motorway Southbound", Region: "north", FromNodeID: "north", ToNodeID: "airport", TraversalTimeMinutes: 16},
		{ForwardSegmentID: "seg_quays_e", ForwardSegmentName: "Dublin Quays Eastbound", ReverseSegmentID: "seg_quays_w", ReverseSegmentName: "Dublin Quays Westbound", Region: "central", FromNodeID: "city", ToNodeID: "east", TraversalTimeMinutes: 6},
		{ForwardSegmentID: "seg_port_n", ForwardSegmentName: "Port Tunnel Northbound", ReverseSegmentID: "seg_port_s", ReverseSegmentName: "Port Tunnel Southbound", Region: "east", FromNodeID: "east", ToNodeID: "airport", TraversalTimeMinutes: 20},
		{ForwardSegmentID: "seg_n11", ForwardSegmentName: "N11 Stillorgan Road", ReverseSegmentID: "seg_n11", ReverseSegmentName: "N11 Stillorgan Road", Region: "south", FromNodeID: "city", ToNodeID: "riverside", TraversalTimeMinutes: 5},
		{ForwardSegmentID: "seg_m50_s", ForwardSegmentName: "M50 South (Sandyford Junction)", ReverseSegmentID: "seg_m50_s", ReverseSegmentName: "M50 South (Sandyford Junction)", Region: "south", FromNodeID: "riverside", ToNodeID: "south", TraversalTimeMinutes: 7},
		{ForwardSegmentID: "seg_m8", ForwardSegmentName: "M8 Cork Road", ReverseSegmentID: "seg_m8", ReverseSegmentName: "M8 Cork Road", Region: "south", FromNodeID: "south", ToNodeID: "industrial", TraversalTimeMinutes: 6},
		{ForwardSegmentID: "seg_n7", ForwardSegmentName: "N7 Naas Dual Carriageway", ReverseSegmentID: "seg_n7", ReverseSegmentName: "N7 Naas Dual Carriageway", Region: "west", FromNodeID: "industrial", ToNodeID: "east", TraversalTimeMinutes: 6},
		{ForwardSegmentID: "seg_n4", ForwardSegmentName: "N4 Galway Road", ReverseSegmentID: "seg_m4", ReverseSegmentName: "M4 Westlink Motorway", Region: "west", FromNodeID: "city", ToNodeID: "west", TraversalTimeMinutes: 9},
		{ForwardSegmentID: "seg_m7n", ForwardSegmentName: "M7 Naas Road Northbound", ReverseSegmentID: "seg_m7s", ReverseSegmentName: "M7 Naas Road Southbound", Region: "west", FromNodeID: "west", ToNodeID: "port", TraversalTimeMinutes: 8},
		{ForwardSegmentID: "seg_n81", ForwardSegmentName: "N81 Tallaght Road", ReverseSegmentID: "seg_n81", ReverseSegmentName: "N81 Tallaght Road", Region: "south", FromNodeID: "port", ToNodeID: "south", TraversalTimeMinutes: 7},
		{ForwardSegmentID: "seg_n3", ForwardSegmentName: "N3 Navan Road", ReverseSegmentID: "seg_n2", ReverseSegmentName: "N2 Finglas Road", Region: "north", FromNodeID: "west", ToNodeID: "northfield", TraversalTimeMinutes: 9},
		{ForwardSegmentID: "seg_m2", ForwardSegmentName: "M2 Motorway", ReverseSegmentID: "seg_m2", ReverseSegmentName: "M2 Motorway", Region: "north", FromNodeID: "northfield", ToNodeID: "north", TraversalTimeMinutes: 7},
	}

	edges := make([]graphEdge, 0, len(roads)*2)
	for _, road := range roads {
		edges = append(edges,
			buildGraphEdge(road.ForwardSegmentID, road.ForwardSegmentName, road.Region, road.FromNodeID, road.ToNodeID, road.TraversalTimeMinutes),
			buildGraphEdge(road.ReverseSegmentID, road.ReverseSegmentName, road.Region, road.ToNodeID, road.FromNodeID, road.TraversalTimeMinutes),
		)
	}

	return edges
}

func buildGraphEdge(segmentID, segmentName, region, fromNodeID, toNodeID string, traversalTimeMinutes int) graphEdge {
	fromNode, _ := findNode(fromNodeID)
	toNode, _ := findNode(toNodeID)

	return graphEdge{
		SegmentID:            segmentID,
		SegmentName:          segmentName,
		Region:               region,
		FromNodeID:           fromNodeID,
		ToNodeID:             toNodeID,
		TraversalTimeMinutes: traversalTimeMinutes,
		DistanceKm:           coordinateDistanceKm(fromNode.Lat, fromNode.Lng, toNode.Lat, toNode.Lng),
	}
}

func coordinateDistanceKm(lat1, lng1, lat2, lng2 float64) float64 {
	const kmPerDegreeLat = 111.32
	avgLatRadians := (lat1 + lat2) / 2 * math.Pi / 180
	latDiffKm := (lat2 - lat1) * kmPerDegreeLat
	lngDiffKm := (lng2 - lng1) * kmPerDegreeLat * math.Cos(avgLatRadians)
	return math.Sqrt((latDiffKm * latDiffKm) + (lngDiffKm * lngDiffKm))
}
