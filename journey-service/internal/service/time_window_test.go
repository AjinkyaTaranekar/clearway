package service

import (
	"testing"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/client"
)

func TestComputeTimeWindows_ZeroSegments(t *testing.T) {
	dep := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	segs, arrival := ComputeTimeWindows(dep, nil)
	if len(segs) != 0 {
		t.Errorf("expected 0 segments, got %d", len(segs))
	}
	if !arrival.Equal(dep) {
		t.Errorf("expected arrival == departure for zero segments, got %v", arrival)
	}
}

func TestComputeTimeWindows_SingleSegment(t *testing.T) {
	dep := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	segs, arrival := ComputeTimeWindows(dep, []client.MapSegment{
		{SegmentID: "seg_1", TraversalTimeMinutes: 20},
	})
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	startWant := dep.Add(-5 * time.Minute)
	if !segs[0].TimeWindowStart.Equal(startWant) {
		t.Errorf("window start: want %v, got %v", startWant, segs[0].TimeWindowStart)
	}
	want := dep.Add(25 * time.Minute)
	if !segs[0].TimeWindowEnd.Equal(want) {
		t.Errorf("window end: want %v, got %v", want, segs[0].TimeWindowEnd)
	}
	arrivalWant := dep.Add(20 * time.Minute)
	if !arrival.Equal(arrivalWant) {
		t.Errorf("arrival: want %v, got %v", arrivalWant, arrival)
	}
}

func TestComputeTimeWindows_CascadingSegments(t *testing.T) {
	dep := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	segs, arrival := ComputeTimeWindows(dep, []client.MapSegment{
		{SegmentID: "seg_1", SequenceOrder: 1, TraversalTimeMinutes: 15},
		{SegmentID: "seg_2", SequenceOrder: 2, TraversalTimeMinutes: 10},
		{SegmentID: "seg_3", SequenceOrder: 3, TraversalTimeMinutes: 25},
	})
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segs))
	}

	// seg_1: 07:55 → 08:20 (planned 08:00 → 08:15 + 5 min buffer)
	// seg_2: 08:10 → 08:30 (planned 08:15 → 08:25 + 5 min buffer)
	// seg_3: 08:20 → 08:55 (planned 08:25 → 08:50 + 5 min buffer)
	cases := []struct{ start, end time.Time }{
		{dep.Add(-5 * time.Minute), dep.Add(20 * time.Minute)},
		{dep.Add(10 * time.Minute), dep.Add(30 * time.Minute)},
		{dep.Add(20 * time.Minute), dep.Add(55 * time.Minute)},
	}
	for i, c := range cases {
		if !segs[i].TimeWindowStart.Equal(c.start) {
			t.Errorf("seg[%d] start: want %v, got %v", i, c.start, segs[i].TimeWindowStart)
		}
		if !segs[i].TimeWindowEnd.Equal(c.end) {
			t.Errorf("seg[%d] end: want %v, got %v", i, c.end, segs[i].TimeWindowEnd)
		}
	}
	if !arrival.Equal(dep.Add(50 * time.Minute)) {
		t.Errorf("arrival: want %v, got %v", dep.Add(50*time.Minute), arrival)
	}
}

func TestComputeTimeWindows_MidnightCrossover(t *testing.T) {
	dep := time.Date(2026, 4, 15, 23, 50, 0, 0, time.UTC)
	segs, arrival := ComputeTimeWindows(dep, []client.MapSegment{
		{SegmentID: "seg_1", TraversalTimeMinutes: 20},
	})
	want := time.Date(2026, 4, 16, 0, 10, 0, 0, time.UTC)
	if !arrival.Equal(want) {
		t.Errorf("midnight crossover arrival: want %v, got %v", want, arrival)
	}
	endWant := time.Date(2026, 4, 16, 0, 15, 0, 0, time.UTC)
	if !segs[0].TimeWindowEnd.Equal(endWant) {
		t.Errorf("midnight crossover segment end: want %v, got %v", endWant, segs[0].TimeWindowEnd)
	}
}
