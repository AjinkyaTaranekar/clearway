package service

import (
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/client"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/model"
)

// ComputeTimeWindows computes cascading time windows for each segment.
// Uses full TIMESTAMPTZ arithmetic - works correctly across midnight.
func ComputeTimeWindows(departureTime time.Time, segments []client.MapSegment) ([]model.JourneySegment, time.Time) {
	cursor := departureTime
	result := make([]model.JourneySegment, 0, len(segments))

	for _, seg := range segments {
		windowStart := cursor
		windowEnd := cursor.Add(time.Duration(seg.TraversalTimeMinutes) * time.Minute)

		result = append(result, model.JourneySegment{
			SegmentID:        seg.SegmentID,
			SegmentName:      seg.SegmentName,
			SequenceOrder:    seg.SequenceOrder,
			TimeWindowStart:  windowStart,
			TimeWindowEnd:    windowEnd,
			TraversalMinutes: seg.TraversalTimeMinutes,
			Region:           seg.Region,
		})

		cursor = windowEnd
	}

	estimatedArrival := cursor
	return result, estimatedArrival
}
