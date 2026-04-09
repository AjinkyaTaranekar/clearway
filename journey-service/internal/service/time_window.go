package service

import (
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/client"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/model"
)

const segmentWindowBuffer = 5 * time.Minute

func copyFloatPointer(v *float64) *float64 {
	if v == nil {
		return nil
	}
	copy := *v
	return &copy
}

// ComputeTimeWindows computes cascading time windows for each segment.
// Uses full TIMESTAMPTZ arithmetic - works correctly across midnight.
func ComputeTimeWindows(departureTime time.Time, segments []client.MapSegment) ([]model.JourneySegment, time.Time) {
	cursor := departureTime
	result := make([]model.JourneySegment, 0, len(segments))

	for _, seg := range segments {
		plannedStart := cursor
		plannedEnd := cursor.Add(time.Duration(seg.TraversalTimeMinutes) * time.Minute)
		windowStart := plannedStart.Add(-segmentWindowBuffer)
		windowEnd := plannedEnd.Add(segmentWindowBuffer)

		result = append(result, model.JourneySegment{
			SegmentID:        seg.SegmentID,
			SegmentName:      seg.SegmentName,
			SequenceOrder:    seg.SequenceOrder,
			TimeWindowStart:  windowStart,
			TimeWindowEnd:    windowEnd,
			TraversalMinutes: seg.TraversalTimeMinutes,
			Region:           seg.Region,
			FromLat:          copyFloatPointer(seg.FromLat),
			FromLng:          copyFloatPointer(seg.FromLng),
			ToLat:            copyFloatPointer(seg.ToLat),
			ToLng:            copyFloatPointer(seg.ToLng),
		})

		cursor = plannedEnd
	}

	estimatedArrival := cursor
	return result, estimatedArrival
}
