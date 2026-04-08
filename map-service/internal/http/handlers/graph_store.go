package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/map-service/pkg/logger"
)

type GraphSnapshot struct {
	Nodes    []Node
	Segments []SegmentMetadata
	Edges    []graphEdge
}

type GraphStatus struct {
	Available    bool
	LoadedFromDB bool
	Source       string
	LastError    string
}

type GraphStore struct {
	mu     sync.RWMutex
	graph  GraphSnapshot
	status GraphStatus
	log    *logger.Logger
}

func NewGraphStore(log *logger.Logger) *GraphStore {
	store := &GraphStore{log: log}
	store.useFallback(fmt.Errorf("database graph not loaded"))
	return store
}

func (s *GraphStore) loggerFor(ctx context.Context) *logger.Logger {
	if log := logger.FromContext(ctx); log != nil {
		return log
	}
	if s.log != nil {
		return s.log
	}
	return logger.Global()
}

func (s *GraphStore) LoadFromDB(ctx context.Context, db *sql.DB) error {
	log := s.loggerFor(ctx)
	log.Info().
		Str("component", "GraphStore.LoadFromDB").
		Msg("loading map graph from database")

	if db == nil {
		err := fmt.Errorf("graph load: database connection is nil")
		log.Error().
			Str("component", "GraphStore.LoadFromDB").
			Err(err).
			Msg("graph load failed")
		return err
	}

	nodes, err := loadNodes(ctx, db)
	if err != nil {
		log.Error().
			Str("component", "GraphStore.LoadFromDB").
			Err(err).
			Msg("graph load failed while loading nodes")
		return err
	}
	segments, err := loadSegments(ctx, db)
	if err != nil {
		log.Error().
			Str("component", "GraphStore.LoadFromDB").
			Err(err).
			Msg("graph load failed while loading segments")
		return err
	}
	edges, err := buildEdgesFromSegments(nodes, segments)
	if err != nil {
		log.Error().
			Str("component", "GraphStore.LoadFromDB").
			Err(err).
			Msg("graph load failed while building edges")
		return err
	}

	s.mu.Lock()
	s.graph = GraphSnapshot{
		Nodes:    append([]Node(nil), nodes...),
		Segments: append([]SegmentMetadata(nil), segments...),
		Edges:    append([]graphEdge(nil), edges...),
	}
	s.status = GraphStatus{
		Available:    true,
		LoadedFromDB: true,
		Source:       "database",
	}
	s.mu.Unlock()

	log.Info().
		Str("component", "GraphStore.LoadFromDB").
		Int("node_count", len(nodes)).
		Int("segment_count", len(segments)).
		Int("edge_count", len(edges)).
		Msg("map graph loaded from database")

	return nil
}

func (s *GraphStore) UseFallback(err error) {
	s.useFallback(err)
}

func (s *GraphStore) useFallback(err error) {
	graph := GraphSnapshot{
		Nodes:    append([]Node(nil), hardcodedNodes...),
		Segments: buildSegmentMetadata(),
		Edges:    buildHardcodedEdges(),
	}

	status := GraphStatus{
		Available: true,
		Source:    "fallback",
	}
	if err != nil {
		status.LastError = err.Error()
	}

	s.mu.Lock()
	s.graph = graph
	s.status = status
	s.mu.Unlock()

	if s.log != nil {
		event := s.log.Warn()
		if err != nil {
			event = event.Err(err)
		}
		event.
			Int("node_count", len(graph.Nodes)).
			Int("segment_count", len(graph.Segments)).
			Int("edge_count", len(graph.Edges)).
			Msg("using fallback in-memory graph")
	}
}

func (s *GraphStore) Snapshot() GraphSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return GraphSnapshot{
		Nodes:    append([]Node(nil), s.graph.Nodes...),
		Segments: append([]SegmentMetadata(nil), s.graph.Segments...),
		Edges:    append([]graphEdge(nil), s.graph.Edges...),
	}
}

func (s *GraphStore) Status() GraphStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func loadNodes(ctx context.Context, db *sql.DB) ([]Node, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT node_id, label, lat, lng
		FROM map.nodes
		ORDER BY sort_order, node_id
	`)
	if err != nil {
		return nil, fmt.Errorf("graph load nodes: %w", err)
	}
	defer rows.Close()

	nodes := make([]Node, 0)
	for rows.Next() {
		var node Node
		if err := rows.Scan(&node.NodeID, &node.Label, &node.Lat, &node.Lng); err != nil {
			return nil, fmt.Errorf("graph scan node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("graph iterate nodes: %w", err)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("graph load nodes: no rows returned")
	}

	return nodes, nil
}

func loadSegments(ctx context.Context, db *sql.DB) ([]SegmentMetadata, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT segment_id, segment_name, region, from_node_id, to_node_id, traversal_time_minutes
		FROM map.segments
		ORDER BY sort_order, segment_id
	`)
	if err != nil {
		return nil, fmt.Errorf("graph load segments: %w", err)
	}
	defer rows.Close()

	segments := make([]SegmentMetadata, 0)
	for rows.Next() {
		var segment SegmentMetadata
		if err := rows.Scan(
			&segment.SegmentID,
			&segment.SegmentName,
			&segment.Region,
			&segment.FromNodeID,
			&segment.ToNodeID,
			&segment.TraversalTimeMinutes,
		); err != nil {
			return nil, fmt.Errorf("graph scan segment: %w", err)
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("graph iterate segments: %w", err)
	}
	if len(segments) == 0 {
		return nil, fmt.Errorf("graph load segments: no rows returned")
	}

	return segments, nil
}

func buildEdgesFromSegments(nodes []Node, segments []SegmentMetadata) ([]graphEdge, error) {
	nodeLookup := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		nodeLookup[node.NodeID] = node
	}

	edges := make([]graphEdge, 0, len(segments)*2)
	for _, segment := range segments {
		if _, ok := nodeLookup[segment.FromNodeID]; !ok {
			return nil, fmt.Errorf("graph segment %s references unknown from node %s", segment.SegmentID, segment.FromNodeID)
		}
		if _, ok := nodeLookup[segment.ToNodeID]; !ok {
			return nil, fmt.Errorf("graph segment %s references unknown to node %s", segment.SegmentID, segment.ToNodeID)
		}

		edges = append(edges,
			buildGraphEdge(nodeLookup, segment.SegmentID, segment.SegmentName, segment.Region, segment.FromNodeID, segment.ToNodeID, segment.TraversalTimeMinutes),
			buildGraphEdge(nodeLookup, segment.SegmentID, segment.SegmentName, segment.Region, segment.ToNodeID, segment.FromNodeID, segment.TraversalTimeMinutes),
		)
	}

	return edges, nil
}
