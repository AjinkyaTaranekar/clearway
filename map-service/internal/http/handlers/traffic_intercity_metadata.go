package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

type intercitySegmentMetadata struct {
	SegmentID   string
	SegmentName string
	CountryCode string
	FromLat     float64
	FromLng     float64
	ToLat       float64
	ToLng       float64
}

func (h *MapHandler) loadIntercitySegmentMetadata(ctx context.Context, occupancyRows []capacityOccupancy) (map[string]intercitySegmentMetadata, error) {
	metadata := make(map[string]intercitySegmentMetadata)
	if h.db == nil || len(occupancyRows) == 0 {
		return metadata, nil
	}

	ids := make([]string, 0, len(occupancyRows))
	seen := make(map[string]struct{}, len(occupancyRows))
	for _, occupancy := range occupancyRows {
		segmentID := strings.TrimSpace(occupancy.SegmentID)
		if segmentID == "" {
			continue
		}
		if _, ok := seen[segmentID]; ok {
			continue
		}
		seen[segmentID] = struct{}{}
		ids = append(ids, segmentID)
	}

	if len(ids) == 0 {
		return metadata, nil
	}

	const q = `
		SELECT segment_id, segment_name, country_code, from_lat, from_lng, to_lat, to_lng
		FROM map.intercity_segments
		WHERE segment_id = ANY($1)`

	rows, err := h.db.QueryContext(ctx, q, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("query intercity segment metadata: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var segment intercitySegmentMetadata
		if err := rows.Scan(
			&segment.SegmentID,
			&segment.SegmentName,
			&segment.CountryCode,
			&segment.FromLat,
			&segment.FromLng,
			&segment.ToLat,
			&segment.ToLng,
		); err != nil {
			return nil, fmt.Errorf("scan intercity segment metadata: %w", err)
		}
		metadata[segment.SegmentID] = segment
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate intercity segment metadata: %w", err)
	}

	return metadata, nil
}

func intercityCountryToRegion(countryCode string) string {
	switch strings.ToUpper(strings.TrimSpace(countryCode)) {
	case "IE", "GB", "EU", "US":
		return "intercity"
	default:
		return "intercity"
	}
}
