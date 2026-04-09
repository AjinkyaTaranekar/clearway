package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
)

func (h *MapHandler) computeDynamicRoute(ctx context.Context, origin, destination RoutePointRequest) (ComputeRouteResponse, error) {
	if h.geo == nil {
		return ComputeRouteResponse{}, fmt.Errorf("geo client is not configured")
	}

	origin = normalizedPoint(origin)
	destination = normalizedPoint(destination)

	if origin.Lat == destination.Lat && origin.Lng == destination.Lng {
		return ComputeRouteResponse{}, fmt.Errorf("origin and destination must be different")
	}

	originPlaceID := coordinatePlaceID(origin)
	destinationPlaceID := coordinatePlaceID(destination)

	if h.db != nil {
		cachedRoute, found, err := h.loadCachedDynamicRoute(ctx, originPlaceID, destinationPlaceID)
		if err != nil {
			logWithTrace(ctx).Warn().Err(err).Msg("failed to load cached route; continuing with live route computation")
		} else if found {
			if err := h.ensureCapacitySegments(ctx, cachedRoute.Segments); err != nil {
				logWithTrace(ctx).Warn().Err(err).Str("route_id", cachedRoute.RouteID).Msg("failed to register cached route segments in capacity service")
			}
			_ = h.touchRouteLastUsed(ctx, cachedRoute.RouteID)
			return cachedRoute, nil
		}
	}

	routeResult, err := h.geo.GetRoute(ctx, RoutePoint{Lat: origin.Lat, Lng: origin.Lng}, RoutePoint{Lat: destination.Lat, Lng: destination.Lng})
	if err != nil {
		return ComputeRouteResponse{}, fmt.Errorf("osrm route lookup failed: %w", err)
	}

	segments := mapRouteStepsToSegments(routeResult.Segments)
	if len(segments) == 0 {
		return ComputeRouteResponse{}, fmt.Errorf("osrm returned no segments")
	}

	segmentCoordinates := deriveSegmentCoordinates(origin, destination, routeResult)
	applySegmentCoordinates(segments, segmentCoordinates)

	if err := h.ensureCapacitySegments(ctx, segments); err != nil {
		logWithTrace(ctx).Warn().Err(err).Msg("failed to register route segments in capacity service")
	}

	routeID := fmt.Sprintf("rte_%s_%s", originPlaceID, destinationPlaceID)
	if h.db != nil {
		persistedRouteID, err := h.persistDynamicRoute(ctx, originPlaceID, destinationPlaceID, origin, destination, routeResult, segments)
		if err != nil {
			logWithTrace(ctx).Warn().Err(err).Msg("failed to persist dynamic route; returning non-cached route response")
		} else {
			routeID = persistedRouteID
		}
	}

	response := ComputeRouteResponse{
		RouteID:              routeID,
		OriginPlaceID:        originPlaceID,
		DestinationPlaceID:   destinationPlaceID,
		TotalDistanceKm:      roundToTwo(routeResult.DistanceMeters / 1000.0),
		TotalDurationMinutes: durationSecondsToMinutes(routeResult.DurationSeconds),
		Segments:             segments,
		Path:                 mapPathPoints(routeResult.Points),
	}
	if response.TotalDurationMinutes <= 0 {
		response.TotalDurationMinutes = sumTraversalMinutes(segments)
	}

	return response, nil
}

func (h *MapHandler) loadCachedDynamicRoute(ctx context.Context, originPlaceID, destinationPlaceID string) (ComputeRouteResponse, bool, error) {
	const routeQuery = `
		SELECT route_id, distance_m, duration_s
		FROM map.routes
		WHERE origin_place_id = $1 AND dest_place_id = $2`

	var routeID string
	var distanceMeters int
	var durationSeconds int
	if err := h.db.QueryRowContext(ctx, routeQuery, originPlaceID, destinationPlaceID).Scan(&routeID, &distanceMeters, &durationSeconds); err != nil {
		if err == sql.ErrNoRows {
			return ComputeRouteResponse{}, false, nil
		}
		return ComputeRouteResponse{}, false, fmt.Errorf("load cached route header: %w", err)
	}

	const segmentsQuery = `
		SELECT
			rs.sequence,
			rs.segment_id,
			COALESCE(seg.segment_name, rs.segment_id) AS segment_name,
			rs.traversal_time_minutes,
			seg.from_lat,
			seg.from_lng,
			seg.to_lat,
			seg.to_lng
		FROM map.route_segments rs
		LEFT JOIN map.intercity_segments seg ON seg.segment_id = rs.segment_id
		WHERE rs.route_id = $1
		ORDER BY rs.sequence`

	rows, err := h.db.QueryContext(ctx, segmentsQuery, routeID)
	if err != nil {
		return ComputeRouteResponse{}, false, fmt.Errorf("load cached route segments: %w", err)
	}
	defer rows.Close()

	segments := make([]RouteSegment, 0)
	for rows.Next() {
		var sequence int
		var segmentID string
		var segmentName string
		var traversalMinutes int
		var fromLat, fromLng, toLat, toLng sql.NullFloat64
		if err := rows.Scan(&sequence, &segmentID, &segmentName, &traversalMinutes, &fromLat, &fromLng, &toLat, &toLng); err != nil {
			return ComputeRouteResponse{}, false, fmt.Errorf("scan cached route segment: %w", err)
		}

		if traversalMinutes < 1 {
			traversalMinutes = 1
		}
		segment := RouteSegment{
			Sequence:             sequence,
			SequenceOrder:        sequence,
			SegmentID:            segmentID,
			SegmentName:          segmentName,
			TraversalTimeMinutes: traversalMinutes,
			Region:               "intercity",
		}
		if fromLat.Valid {
			segment.FromLat = floatPointer(fromLat.Float64)
		}
		if fromLng.Valid {
			segment.FromLng = floatPointer(fromLng.Float64)
		}
		if toLat.Valid {
			segment.ToLat = floatPointer(toLat.Float64)
		}
		if toLng.Valid {
			segment.ToLng = floatPointer(toLng.Float64)
		}
		segments = append(segments, segment)
	}
	if err := rows.Err(); err != nil {
		return ComputeRouteResponse{}, false, fmt.Errorf("iterate cached route segments: %w", err)
	}
	if len(segments) == 0 {
		return ComputeRouteResponse{}, false, nil
	}

	resp := ComputeRouteResponse{
		RouteID:              routeID,
		OriginPlaceID:        originPlaceID,
		DestinationPlaceID:   destinationPlaceID,
		TotalDistanceKm:      roundToTwo(float64(distanceMeters) / 1000.0),
		TotalDurationMinutes: durationSecondsToMinutes(float64(durationSeconds)),
		Segments:             segments,
	}
	if resp.TotalDurationMinutes <= 0 {
		resp.TotalDurationMinutes = sumTraversalMinutes(segments)
	}

	return resp, true, nil
}

func (h *MapHandler) touchRouteLastUsed(ctx context.Context, routeID string) error {
	if routeID == "" {
		return nil
	}
	_, err := h.db.ExecContext(ctx, `UPDATE map.routes SET last_used_at = NOW() WHERE route_id = $1`, routeID)
	if err != nil {
		return fmt.Errorf("touch route last_used_at: %w", err)
	}
	return nil
}

func (h *MapHandler) persistDynamicRoute(
	ctx context.Context,
	originPlaceID,
	destinationPlaceID string,
	origin,
	destination RoutePointRequest,
	routeResult *RouteResult,
	segments []RouteSegment,
) (string, error) {
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	const upsertPlace = `
		INSERT INTO map.places (place_id, name, address, lat, lng, country_code)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (place_id)
		DO UPDATE SET
			name = EXCLUDED.name,
			address = EXCLUDED.address,
			lat = EXCLUDED.lat,
			lng = EXCLUDED.lng,
			country_code = EXCLUDED.country_code`

	if _, err := tx.ExecContext(
		ctx,
		upsertPlace,
		originPlaceID,
		"Origin",
		fmt.Sprintf("%.4f, %.4f", origin.Lat, origin.Lng),
		origin.Lat,
		origin.Lng,
		"IE",
	); err != nil {
		return "", fmt.Errorf("upsert origin place: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		upsertPlace,
		destinationPlaceID,
		"Destination",
		fmt.Sprintf("%.4f, %.4f", destination.Lat, destination.Lng),
		destination.Lat,
		destination.Lng,
		"IE",
	); err != nil {
		return "", fmt.Errorf("upsert destination place: %w", err)
	}

	const upsertRoute = `
		INSERT INTO map.routes (origin_place_id, dest_place_id, distance_m, duration_s, last_used_at, created_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (origin_place_id, dest_place_id)
		DO UPDATE SET
			distance_m = EXCLUDED.distance_m,
			duration_s = EXCLUDED.duration_s,
			last_used_at = NOW()
		RETURNING route_id`

	var routeID string
	if err := tx.QueryRowContext(
		ctx,
		upsertRoute,
		originPlaceID,
		destinationPlaceID,
		int(math.Round(routeResult.DistanceMeters)),
		int(math.Round(routeResult.DurationSeconds)),
	).Scan(&routeID); err != nil {
		return "", fmt.Errorf("upsert route: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM map.route_segments WHERE route_id = $1`, routeID); err != nil {
		return "", fmt.Errorf("clear existing route segments: %w", err)
	}

	segmentCoordinates := deriveSegmentCoordinates(origin, destination, routeResult)
	const upsertIntercitySegment = `
		INSERT INTO map.intercity_segments (
			segment_id,
			segment_name,
			road_ref,
			country_code,
			from_lat,
			from_lng,
			to_lat,
			to_lng
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (segment_id)
		DO UPDATE SET
			segment_name = EXCLUDED.segment_name,
			road_ref = EXCLUDED.road_ref,
			country_code = EXCLUDED.country_code,
			from_lat = EXCLUDED.from_lat,
			from_lng = EXCLUDED.from_lng,
			to_lat = EXCLUDED.to_lat,
			to_lng = EXCLUDED.to_lng`

	const insertRouteSegment = `
		INSERT INTO map.route_segments (route_id, sequence, segment_id, traversal_time_minutes)
		VALUES ($1, $2, $3, $4)`

	for idx, segment := range segments {
		coordinates := segmentCoordinates[idx]
		roadRef := normalizeRoadRef(segment.SegmentName)
		if _, err := tx.ExecContext(
			ctx,
			upsertIntercitySegment,
			segment.SegmentID,
			segment.SegmentName,
			roadRef,
			"IE",
			coordinates.FromLat,
			coordinates.FromLng,
			coordinates.ToLat,
			coordinates.ToLng,
		); err != nil {
			return "", fmt.Errorf("upsert intercity segment %s: %w", segment.SegmentID, err)
		}

		if _, err := tx.ExecContext(
			ctx,
			insertRouteSegment,
			routeID,
			segment.Sequence,
			segment.SegmentID,
			segment.TraversalTimeMinutes,
		); err != nil {
			return "", fmt.Errorf("insert route segment %s: %w", segment.SegmentID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit route tx: %w", err)
	}

	return routeID, nil
}

func (h *MapHandler) ensureCapacitySegments(ctx context.Context, segments []RouteSegment) error {
	if h.capacityClient == nil {
		return fmt.Errorf("capacity client is not configured")
	}

	registrations := make([]capacitySegmentRegistration, 0, len(segments))
	seen := make(map[string]struct{}, len(segments))
	for _, segment := range segments {
		segmentID := strings.TrimSpace(segment.SegmentID)
		if segmentID == "" {
			continue
		}
		if _, ok := seen[segmentID]; ok {
			continue
		}
		seen[segmentID] = struct{}{}

		registrations = append(registrations, capacitySegmentRegistration{
			SegmentID:   segmentID,
			SegmentName: strings.TrimSpace(segment.SegmentName),
			Region:      normalizeMapSegmentRegion(segment.Region),
		})
	}

	if len(registrations) == 0 {
		return fmt.Errorf("no segment ids available for registration")
	}

	return h.capacityClient.RegisterSegments(ctx, registrations)
}

func mapRouteStepsToSegments(steps []RouteStep) []RouteSegment {
	segments := make([]RouteSegment, 0, len(steps))
	for idx, step := range steps {
		segmentID := strings.TrimSpace(step.SegmentID)
		if segmentID == "" {
			continue
		}

		segmentName := strings.TrimSpace(step.SegmentName)
		if segmentName == "" {
			segmentName = segmentID
		}

		segments = append(segments, RouteSegment{
			Sequence:             idx + 1,
			SequenceOrder:        idx + 1,
			SegmentID:            segmentID,
			SegmentName:          segmentName,
			TraversalTimeMinutes: durationSecondsToMinutes(step.DurationSeconds),
			Region:               "intercity",
		})
	}

	return segments
}

func floatPointer(value float64) *float64 {
	v := value
	return &v
}

func applySegmentCoordinates(segments []RouteSegment, coords []segmentCoordinate) {
	for idx := range segments {
		if idx >= len(coords) {
			break
		}
		segments[idx].FromLat = floatPointer(coords[idx].FromLat)
		segments[idx].FromLng = floatPointer(coords[idx].FromLng)
		segments[idx].ToLat = floatPointer(coords[idx].ToLat)
		segments[idx].ToLng = floatPointer(coords[idx].ToLng)
	}
}

type segmentCoordinate struct {
	FromLat float64
	FromLng float64
	ToLat   float64
	ToLng   float64
}

func deriveSegmentCoordinates(origin, destination RoutePointRequest, routeResult *RouteResult) []segmentCoordinate {
	segmentCount := len(routeResult.Segments)
	coords := make([]segmentCoordinate, segmentCount)
	if segmentCount == 0 {
		return coords
	}

	if len(routeResult.Points) < 2 {
		for i := range coords {
			coords[i] = segmentCoordinate{
				FromLat: origin.Lat,
				FromLng: origin.Lng,
				ToLat:   destination.Lat,
				ToLng:   destination.Lng,
			}
		}
		return coords
	}

	totalDuration := routeResult.DurationSeconds
	if totalDuration <= 0 {
		for _, step := range routeResult.Segments {
			totalDuration += step.DurationSeconds
		}
	}
	if totalDuration <= 0 {
		totalDuration = float64(segmentCount)
	}

	startIndex := 0
	cumulativeDuration := 0.0
	lastPointIndex := len(routeResult.Points) - 1

	for idx, step := range routeResult.Segments {
		cumulativeDuration += step.DurationSeconds
		if idx == segmentCount-1 {
			cumulativeDuration = totalDuration
		}

		endRatio := cumulativeDuration / totalDuration
		if endRatio < 0 {
			endRatio = 0
		}
		if endRatio > 1 {
			endRatio = 1
		}

		endIndex := int(math.Round(endRatio * float64(lastPointIndex)))
		if endIndex < startIndex {
			endIndex = startIndex
		}
		if endIndex > lastPointIndex {
			endIndex = lastPointIndex
		}

		from := routeResult.Points[startIndex]
		to := routeResult.Points[endIndex]
		coords[idx] = segmentCoordinate{
			FromLat: from[0],
			FromLng: from[1],
			ToLat:   to[0],
			ToLng:   to[1],
		}

		startIndex = endIndex
	}

	return coords
}

func mapPathPoints(points [][2]float64) []RoutePointRequest {
	path := make([]RoutePointRequest, 0, len(points))
	for _, point := range points {
		path = append(path, RoutePointRequest{Lat: point[0], Lng: point[1]})
	}
	return path
}

func sumTraversalMinutes(segments []RouteSegment) int {
	total := 0
	for _, segment := range segments {
		total += segment.TraversalTimeMinutes
	}
	return total
}

func normalizedPoint(point RoutePointRequest) RoutePointRequest {
	return RoutePointRequest{
		Lat: roundCoordinate(point.Lat),
		Lng: roundCoordinate(point.Lng),
	}
}

func roundCoordinate(value float64) float64 {
	return math.Round(value*10000.0) / 10000.0
}

func coordinatePlaceID(point RoutePointRequest) string {
	return fmt.Sprintf("coord_%.4f_%.4f", point.Lat, point.Lng)
}

func normalizeRoadRef(segmentName string) string {
	ref := strings.TrimSpace(segmentName)
	if ref == "" {
		return "unknown"
	}
	return strings.ToUpper(ref)
}

func normalizeMapSegmentRegion(region string) string {
	switch strings.ToLower(strings.TrimSpace(region)) {
	case "north", "south", "east", "west", "central", "intercity":
		return strings.ToLower(strings.TrimSpace(region))
	default:
		return "intercity"
	}
}

func roundToTwo(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}
