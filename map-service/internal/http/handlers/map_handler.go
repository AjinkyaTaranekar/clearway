package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	appErrors "github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/errors"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/logger"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/response"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/tracing"
)

// MapHandler handles map-related endpoints.
type MapHandler struct {
	graph          *GraphStore
	db             *sql.DB
	geo            *GeoClient
	capacityClient *CapacityClient
	log            *logger.Logger
}

// NewMapHandler creates a new map handler.
func NewMapHandler(graph *GraphStore, db *sql.DB, geo *GeoClient, capacityClient *CapacityClient, log *logger.Logger) *MapHandler {
	return &MapHandler{
		graph:          graph,
		db:             db,
		geo:            geo,
		capacityClient: capacityClient,
		log:            log,
	}
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

type SegmentMetadata struct {
	SegmentID            string `json:"segment_id"`
	SegmentName          string `json:"segment_name"`
	Region               string `json:"region"`
	FromNodeID           string `json:"from_node_id"`
	ToNodeID             string `json:"to_node_id"`
	TraversalTimeMinutes int    `json:"traversal_time_minutes"`
}

// NodesResponse represents the response body for map nodes.
type NodesResponse struct {
	Nodes []Node `json:"nodes"`
}

type SegmentsResponse struct {
	Segments []SegmentMetadata `json:"segments"`
}

// RouteResponse represents the response body for a route lookup.
type RouteResponse struct {
	Origin                    Node           `json:"origin"`
	Destination               Node           `json:"destination"`
	TotalTraversalTimeMinutes int            `json:"total_traversal_time_minutes"`
	Segments                  []RouteSegment `json:"segments"`
}

type TrafficSegment struct {
	SegmentID    string `json:"segment_id"`
	Name         string `json:"name"`
	Region       string `json:"region"`
	Level        string `json:"level"`
	OccupancyPct int    `json:"occupancy_pct"`
	Vehicles     int    `json:"vehicles"`
	Capacity     int    `json:"capacity"`
	Trend        string `json:"trend"`
	FromNode     Node   `json:"from_node"`
	ToNode       Node   `json:"to_node"`
}

type TrafficNode struct {
	NodeID string `json:"node_id"`
	Label  string `json:"label"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
}

type TrafficResponse struct {
	Segments []TrafficSegment `json:"segments"`
	Nodes    []TrafficNode    `json:"nodes"`
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
	RouteID              string              `json:"route_id"`
	TotalDistanceKm      float64             `json:"total_distance_km"`
	TotalDurationMinutes int                 `json:"total_duration_minutes"`
	Segments             []RouteSegment      `json:"segments"`
	Path                 []RoutePointRequest `json:"path,omitempty"`
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
	SegmentID            string
	SegmentName          string
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

var hardcodedNodePositions = map[string]TrafficNode{
	"city":       {NodeID: "city", Label: "City Centre", X: 300, Y: 250},
	"north":      {NodeID: "north", Label: "North Gate", X: 300, Y: 60},
	"airport":    {NodeID: "airport", Label: "Airport", X: 500, Y: 60},
	"east":       {NodeID: "east", Label: "East Quay", X: 520, Y: 250},
	"south":      {NodeID: "south", Label: "South Terminal", X: 300, Y: 420},
	"industrial": {NodeID: "industrial", Label: "Industrial Park", X: 470, Y: 400},
	"west":       {NodeID: "west", Label: "West Depot", X: 80, Y: 250},
	"port":       {NodeID: "port", Label: "Port Terminal", X: 80, Y: 390},
	"northfield": {NodeID: "northfield", Label: "Northfield", X: 130, Y: 80},
	"riverside":  {NodeID: "riverside", Label: "Riverside", X: 200, Y: 380},
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
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "MapHandler.GetNodes").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("get nodes request received")

	graph := h.graph.Snapshot()
	log.Info().
		Str("handler", "MapHandler.GetNodes").
		Int("node_count", len(graph.Nodes)).
		Msg("get nodes request completed")

	response.Success(w, NodesResponse{
		Nodes: graph.Nodes,
	}, traceID)
}

// GetSegments godoc
// @Summary Get map segments
// @Description Returns all static segment topology metadata
// @Tags Map
// @Accept json
// @Produce json
// @Success 200 {object} SegmentsResponse
// @Router /api/v1/map/segments [get]
func (h *MapHandler) GetSegments(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "MapHandler.GetSegments").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("get segments request received")

	graph := h.graph.Snapshot()
	log.Info().
		Str("handler", "MapHandler.GetSegments").
		Int("segment_count", len(graph.Segments)).
		Msg("get segments request completed")

	response.Success(w, SegmentsResponse{
		Segments: graph.Segments,
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
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "MapHandler.GetRoute").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("get route request received")

	graph := h.graph.Snapshot()

	originNodeID := r.URL.Query().Get("origin_node_id")
	destinationNodeID := r.URL.Query().Get("destination_node_id")

	if originNodeID == "" || destinationNodeID == "" {
		log.Warn().
			Str("handler", "MapHandler.GetRoute").
			Str("origin_node_id", originNodeID).
			Str("destination_node_id", destinationNodeID).
			Msg("route request validation failed: missing node ids")
		response.Error(w, appErrors.BadRequest("origin_node_id and destination_node_id are required"), traceID)
		return
	}

	if originNodeID == destinationNodeID {
		log.Warn().
			Str("handler", "MapHandler.GetRoute").
			Str("origin_node_id", originNodeID).
			Str("destination_node_id", destinationNodeID).
			Msg("route request validation failed: identical nodes")
		response.Error(w, appErrors.BadRequest("origin and destination cannot be the same"), traceID)
		return
	}

	log.Info().
		Str("handler", "MapHandler.GetRoute").
		Str("origin_node_id", originNodeID).
		Str("destination_node_id", destinationNodeID).
		Msg("building route")

	route, err := buildRoute(graph, originNodeID, destinationNodeID)
	if err != nil {
		log.Error().
			Str("handler", "MapHandler.GetRoute").
			Err(err).
			Str("origin_node_id", originNodeID).
			Str("destination_node_id", destinationNodeID).
			Msg("route build failed")
		response.Error(w, err, traceID)
		return
	}

	log.Info().
		Str("handler", "MapHandler.GetRoute").
		Str("origin_node_id", originNodeID).
		Str("destination_node_id", destinationNodeID).
		Int("segment_count", len(route.Segments)).
		Int("total_traversal_minutes", route.TotalTraversalTimeMinutes).
		Msg("get route request completed")

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
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "MapHandler.ComputeRoute").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("compute route request received")

	var req ComputeRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Warn().
			Str("handler", "MapHandler.ComputeRoute").
			Err(err).
			Msg("compute route request validation failed: invalid body")
		response.Error(w, appErrors.BadRequest("invalid request body"), traceID)
		return
	}

	computeRoute, err := h.computeDynamicRoute(r.Context(), req.Origin, req.Destination)
	if err != nil {
		log.Error().
			Str("handler", "MapHandler.ComputeRoute").
			Err(err).
			Msg("compute route build failed")
		if strings.Contains(strings.ToLower(err.Error()), "origin and destination must be different") {
			response.Error(w, appErrors.BadRequest("origin and destination must be different"), traceID)
			return
		}
		response.Error(w, appErrors.InternalError("route computation failed", err), traceID)
		return
	}

	log.Info().
		Str("handler", "MapHandler.ComputeRoute").
		Str("route_id", computeRoute.RouteID).
		Int("segment_count", len(computeRoute.Segments)).
		Float64("total_distance_km", computeRoute.TotalDistanceKm).
		Int("total_duration_minutes", computeRoute.TotalDurationMinutes).
		Msg("compute route request completed")

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Trace-ID", traceID)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(computeRoute)
}

// GetTraffic godoc
// @Summary Get traffic map data
// @Description Returns traffic payload joined with live capacity occupancy
// @Tags Map
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} TrafficResponse
// @Failure 401 {object} response.Response
// @Failure 403 {object} response.Response
// @Router /api/v1/map/traffic [get]
func (h *MapHandler) GetTraffic(w http.ResponseWriter, r *http.Request) {
	traceID := tracing.GetTraceID(r.Context())
	log := logWithTrace(r.Context())
	log.Info().
		Str("handler", "MapHandler.GetTraffic").
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("get traffic request received")

	graph := h.graph.Snapshot()
	log.Debug().
		Str("handler", "MapHandler.GetTraffic").
		Int("segment_count", len(graph.Segments)).
		Msg("loaded graph snapshot for traffic request")

	occupancyRows, err := h.capacityClient.GetOccupancy(r.Context())
	if err != nil {
		log.Error().
			Str("handler", "MapHandler.GetTraffic").
			Err(err).
			Msg("traffic occupancy lookup failed")
		response.Error(w, appErrors.ExternalAPIError("failed to retrieve capacity occupancy", err), traceID)
		return
	}
	log.Info().
		Str("handler", "MapHandler.GetTraffic").
		Int("occupancy_row_count", len(occupancyRows)).
		Msg("received occupancy rows from capacity service")

	staticSegments := make(map[string]SegmentMetadata, len(graph.Segments))
	for _, segment := range graph.Segments {
		staticSegments[segment.SegmentID] = segment
	}

	intercitySegments, err := h.loadIntercitySegmentMetadata(r.Context(), occupancyRows)
	if err != nil {
		log.Error().
			Str("handler", "MapHandler.GetTraffic").
			Err(err).
			Msg("failed to load intercity segment metadata")
		response.Error(w, appErrors.InternalError("failed to load traffic segment metadata", err), traceID)
		return
	}

	trafficSegments := make([]TrafficSegment, 0, len(occupancyRows))
	for _, occupancy := range occupancyRows {
		segmentID := occupancy.SegmentID

		segmentName := segmentID
		region := "intercity"
		fromNode := Node{}
		toNode := Node{}

		if staticSegment, ok := staticSegments[segmentID]; ok {
			segmentName = staticSegment.SegmentName
			region = staticSegment.Region
			fromNode, _ = findNode(graph.Nodes, staticSegment.FromNodeID)
			toNode, _ = findNode(graph.Nodes, staticSegment.ToNodeID)
		} else if intercitySegment, ok := intercitySegments[segmentID]; ok {
			segmentName = intercitySegment.SegmentName
			region = intercityCountryToRegion(intercitySegment.CountryCode)
			fromNode = Node{
				NodeID: intercitySegment.SegmentID + "_from",
				Label:  segmentName + " start",
				Lat:    intercitySegment.FromLat,
				Lng:    intercitySegment.FromLng,
			}
			toNode = Node{
				NodeID: intercitySegment.SegmentID + "_to",
				Label:  segmentName + " end",
				Lat:    intercitySegment.ToLat,
				Lng:    intercitySegment.ToLng,
			}
		} else {
			// Capacity can expose segments discovered by other regions/services.
			// Skip those when map metadata is unavailable to avoid plotting 0,0 lines.
			continue
		}

		trafficSegments = append(trafficSegments, TrafficSegment{
			SegmentID:    segmentID,
			Name:         segmentName,
			Region:       region,
			Level:        normalizeTrafficLevel(occupancy.Level, occupancy.OccupancyPct),
			OccupancyPct: int(math.Round(occupancy.OccupancyPct)),
			Vehicles:     int(math.Round(occupancy.CurrentVehicles)),
			Capacity:     int(math.Round(occupancy.MaxCapacity)),
			Trend:        occupancy.Trend,
			FromNode:     fromNode,
			ToNode:       toNode,
		})
	}

	response.Success(w, TrafficResponse{
		Segments: trafficSegments,
		Nodes:    buildTrafficNodes(graph.Nodes),
	}, traceID)
	log.Info().
		Str("handler", "MapHandler.GetTraffic").
		Int("segment_count", len(trafficSegments)).
		Msg("get traffic request completed")
}

func findNode(nodes []Node, nodeID string) (Node, bool) {
	for _, node := range nodes {
		if node.NodeID == nodeID {
			return node, true
		}
	}

	return Node{}, false
}

func findNearestNode(nodes []Node, lat, lng float64) (Node, bool) {
	if len(nodes) == 0 {
		return Node{}, false
	}

	bestNode := nodes[0]
	bestDistance := coordinateDistanceKm(lat, lng, bestNode.Lat, bestNode.Lng)

	for _, node := range nodes[1:] {
		distance := coordinateDistanceKm(lat, lng, node.Lat, node.Lng)
		if distance < bestDistance {
			bestNode = node
			bestDistance = distance
		}
	}

	return bestNode, true
}

func buildRoute(graph GraphSnapshot, originNodeID, destinationNodeID string) (RouteResponse, error) {
	origin, ok := findNode(graph.Nodes, originNodeID)
	if !ok {
		return RouteResponse{}, appErrors.NotFound("origin node not found")
	}

	destination, ok := findNode(graph.Nodes, destinationNodeID)
	if !ok {
		return RouteResponse{}, appErrors.NotFound("destination node not found")
	}

	segments, totalTraversalTime, _, ok := calculateShortestRoute(graph.Nodes, graph.Edges, originNodeID, destinationNodeID)
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

func buildComputeRoute(graph GraphSnapshot, originNodeID, destinationNodeID string) (ComputeRouteResponse, error) {
	segments, totalTraversalTime, totalDistanceKm, ok := calculateShortestRoute(graph.Nodes, graph.Edges, originNodeID, destinationNodeID)
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

func calculateShortestRoute(nodes []Node, edges []graphEdge, originNodeID, destinationNodeID string) ([]RouteSegment, int, float64, bool) {
	if originNodeID == destinationNodeID {
		return []RouteSegment{}, 0, 0, true
	}

	distances := make(map[string]int, len(nodes))
	visited := make(map[string]bool, len(nodes))
	previous := make(map[string]graphEdge, len(nodes))

	for _, node := range nodes {
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

		for _, edge := range outgoingEdges(edges, currentNodeID) {
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

func outgoingEdges(edges []graphEdge, nodeID string) []graphEdge {
	matches := make([]graphEdge, 0)
	for _, edge := range edges {
		if edge.FromNodeID == nodeID {
			matches = append(matches, edge)
		}
	}

	return matches
}

func buildHardcodedEdges() []graphEdge {
	nodeLookup := make(map[string]Node, len(hardcodedNodes))
	for _, node := range hardcodedNodes {
		nodeLookup[node.NodeID] = node
	}

	roads := buildHardcodedRoads()
	edges := make([]graphEdge, 0, len(roads)*2)
	for _, road := range roads {
		edges = append(edges,
			buildGraphEdge(nodeLookup, road.SegmentID, road.SegmentName, road.Region, road.FromNodeID, road.ToNodeID, road.TraversalTimeMinutes),
			buildGraphEdge(nodeLookup, road.SegmentID, road.SegmentName, road.Region, road.ToNodeID, road.FromNodeID, road.TraversalTimeMinutes),
		)
	}

	return edges
}

func buildHardcodedRoads() []bidirectionalRoad {
	return []bidirectionalRoad{
		{SegmentID: "seg_city_north", SegmentName: "City Centre - North Gate", Region: "central", FromNodeID: "city", ToNodeID: "north", TraversalTimeMinutes: 8},
		{SegmentID: "seg_north_airport", SegmentName: "North Gate - Airport", Region: "north", FromNodeID: "north", ToNodeID: "airport", TraversalTimeMinutes: 16},
		{SegmentID: "seg_city_east", SegmentName: "City Centre - East Quay", Region: "central", FromNodeID: "city", ToNodeID: "east", TraversalTimeMinutes: 6},
		{SegmentID: "seg_east_airport", SegmentName: "East Quay - Airport", Region: "east", FromNodeID: "east", ToNodeID: "airport", TraversalTimeMinutes: 20},
		{SegmentID: "seg_city_riverside", SegmentName: "City Centre - Riverside", Region: "central", FromNodeID: "city", ToNodeID: "riverside", TraversalTimeMinutes: 5},
		{SegmentID: "seg_riverside_south", SegmentName: "Riverside - South Terminal", Region: "south", FromNodeID: "riverside", ToNodeID: "south", TraversalTimeMinutes: 7},
		{SegmentID: "seg_south_industrial", SegmentName: "South Terminal - Industrial Park", Region: "south", FromNodeID: "south", ToNodeID: "industrial", TraversalTimeMinutes: 6},
		{SegmentID: "seg_industrial_east", SegmentName: "Industrial Park - East Quay", Region: "east", FromNodeID: "industrial", ToNodeID: "east", TraversalTimeMinutes: 6},
		{SegmentID: "seg_city_west", SegmentName: "City Centre - West Depot", Region: "west", FromNodeID: "city", ToNodeID: "west", TraversalTimeMinutes: 9},
		{SegmentID: "seg_west_port", SegmentName: "West Depot - Port Terminal", Region: "west", FromNodeID: "west", ToNodeID: "port", TraversalTimeMinutes: 8},
		{SegmentID: "seg_port_south", SegmentName: "Port Terminal - South Terminal", Region: "south", FromNodeID: "port", ToNodeID: "south", TraversalTimeMinutes: 7},
		{SegmentID: "seg_west_northfield", SegmentName: "West Depot - Northfield", Region: "north", FromNodeID: "west", ToNodeID: "northfield", TraversalTimeMinutes: 9},
		{SegmentID: "seg_northfield_north", SegmentName: "Northfield - North Gate", Region: "north", FromNodeID: "northfield", ToNodeID: "north", TraversalTimeMinutes: 7},
	}
}

func buildSegmentMetadata() []SegmentMetadata {
	roads := buildHardcodedRoads()
	segments := make([]SegmentMetadata, 0, len(roads))
	for _, road := range roads {
		segments = append(segments, SegmentMetadata{
			SegmentID:            road.SegmentID,
			SegmentName:          road.SegmentName,
			Region:               road.Region,
			FromNodeID:           road.FromNodeID,
			ToNodeID:             road.ToNodeID,
			TraversalTimeMinutes: road.TraversalTimeMinutes,
		})
	}

	return segments
}

func buildTrafficNodes(nodes []Node) []TrafficNode {
	trafficNodes := make([]TrafficNode, 0, len(nodes))
	for _, node := range nodes {
		if trafficNode, ok := hardcodedNodePositions[node.NodeID]; ok {
			trafficNode.Label = node.Label
			trafficNodes = append(trafficNodes, trafficNode)
			continue
		}

		trafficNodes = append(trafficNodes, TrafficNode{
			NodeID: node.NodeID,
			Label:  node.Label,
		})
	}

	return trafficNodes
}

func normalizeTrafficLevel(raw string, occupancyPct float64) string {
	switch strings.ToLower(raw) {
	case "moderate":
		return "medium"
	case "medium", "low", "high", "critical":
		return strings.ToLower(raw)
	}

	switch {
	case occupancyPct >= 75:
		return "critical"
	case occupancyPct >= 50:
		return "high"
	case occupancyPct >= 25:
		return "medium"
	default:
		return "low"
	}
}

func buildGraphEdge(nodeLookup map[string]Node, segmentID, segmentName, region, fromNodeID, toNodeID string, traversalTimeMinutes int) graphEdge {
	fromNode := nodeLookup[fromNodeID]
	toNode := nodeLookup[toNodeID]

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
