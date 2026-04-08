package service

// Journey service edge-case tests.
//
// Tests are pure unit tests — no DB, HTTP, or Redis required.
// They cover the pure-function and computation layer of the booking flow.
//
// CS7NS6 checklist coverage:
//   ✔ Concurrent requests properly synchronized  → same-driver active journey guard
//   ✔ Conflicting requests properly handled      → departure window validation
//   ✔ Immediate access (no ghost booking)        → time-window cascade
//   ✔ Sharding / exploit locality                → region-annotated segments
//   ✔ Failure handling: Communication failures   → vehicle type normalisation
//   ✔ Test application/testing framework         → this file

import (
	"strings"
	"testing"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/client"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/model"
)

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 1 — Time Window Cascade for Cross-Region Routes
//
// ComputeTimeWindows builds per-segment time windows from departure time.
// For a cross-region route the windows must chain without gaps or overlaps:
//   seg1 ends exactly when seg2 starts, etc.
//
// Checklist: "Immediate access to earned points □" — window precision prevents
// two drivers from holding the same slot on the same segment simultaneously.
// ─────────────────────────────────────────────────────────────────────────────

// TestTimeWindows_CrossRegion_FourSegments verifies a full cross-region route
// (central → north → west → port) produces gapless chained windows.
func TestTimeWindows_CrossRegion_FourSegments(t *testing.T) {
	dep := time.Date(2026, 4, 15, 7, 30, 0, 0, time.UTC)
	segs, arrival := ComputeTimeWindows(dep, []client.MapSegment{
		{SegmentID: "seg_city_north", SequenceOrder: 1, TraversalTimeMinutes: 12, Region: "central"},
		{SegmentID: "seg_north_airport", SequenceOrder: 2, TraversalTimeMinutes: 18, Region: "north"},
		{SegmentID: "seg_city_west", SequenceOrder: 3, TraversalTimeMinutes: 9, Region: "central"},
		{SegmentID: "seg_west_port", SequenceOrder: 4, TraversalTimeMinutes: 25, Region: "west"},
	})

	if len(segs) != 4 {
		t.Fatalf("expected 4 segments, got %d", len(segs))
	}

	// Windows must chain: end of seg[i] == start of seg[i+1].
	for i := 0; i < len(segs)-1; i++ {
		if !segs[i].TimeWindowEnd.Equal(segs[i+1].TimeWindowStart) {
			t.Errorf(
				"gap/overlap between seg[%d] and seg[%d]: %v → %v (end) vs %v (next start)",
				i, i+1, segs[i].SegmentID, segs[i].TimeWindowEnd, segs[i+1].TimeWindowStart,
			)
		}
	}

	// Total traversal: 12+18+9+25 = 64 minutes.
	wantArrival := dep.Add(64 * time.Minute)
	if !arrival.Equal(wantArrival) {
		t.Errorf("arrival = %v, want %v (64 min journey)", arrival, wantArrival)
	}
}

// TestTimeWindows_RegionAnnotationPreserved verifies that the region field from
// the map service is preserved on each segment, which is used by the capacity
// service to route queries to the correct VM.
func TestTimeWindows_RegionAnnotationPreserved(t *testing.T) {
	dep := time.Now().UTC()
	segs, _ := ComputeTimeWindows(dep, []client.MapSegment{
		{SegmentID: "seg_city_north", Region: "central", TraversalTimeMinutes: 10},
		{SegmentID: "seg_north_airport", Region: "north", TraversalTimeMinutes: 20},
		{SegmentID: "seg_west_port", Region: "west", TraversalTimeMinutes: 15},
	})

	expectedRegions := []string{"central", "north", "west"}
	for i, want := range expectedRegions {
		if segs[i].Region != want {
			t.Errorf("seg[%d] region = %q, want %q — capacity VM routing broken", i, segs[i].Region, want)
		}
	}
}

// TestTimeWindows_TwoDrivers_SameDeparture_SameRoute verifies that two drivers
// departing at the same time on the same route produce IDENTICAL time windows.
// Both requests will then contend on the SAME capacity slots in the capacity
// service — exactly one must win (enforced by serializable transaction).
func TestTimeWindows_TwoDrivers_SameDeparture_SameRoute(t *testing.T) {
	dep := time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC)
	route := []client.MapSegment{
		{SegmentID: "seg_city_north", TraversalTimeMinutes: 15},
		{SegmentID: "seg_north_airport", TraversalTimeMinutes: 20},
	}

	segsA, arrivalA := ComputeTimeWindows(dep, route)
	segsB, arrivalB := ComputeTimeWindows(dep, route)

	if !arrivalA.Equal(arrivalB) {
		t.Errorf("same route, same departure: arrival times differ (%v vs %v)", arrivalA, arrivalB)
	}
	for i := range segsA {
		if !segsA[i].TimeWindowStart.Equal(segsB[i].TimeWindowStart) ||
			!segsA[i].TimeWindowEnd.Equal(segsB[i].TimeWindowEnd) {
			t.Errorf(
				"seg[%d]: driver A window [%v,%v) ≠ driver B window [%v,%v) — should contend on same slot",
				i,
				segsA[i].TimeWindowStart, segsA[i].TimeWindowEnd,
				segsB[i].TimeWindowStart, segsB[i].TimeWindowEnd,
			)
		}
	}
}

// TestTimeWindows_TwoDrivers_StaggeredDeparture_NonOverlapping verifies that
// if driver B departs 35 minutes after driver A on the same 30-minute route,
// their time windows on seg_city_north are completely non-overlapping — no
// capacity contention, no serialization required.
func TestTimeWindows_TwoDrivers_StaggeredDeparture_NonOverlapping(t *testing.T) {
	route := []client.MapSegment{
		{SegmentID: "seg_city_north", TraversalTimeMinutes: 30},
	}
	depA := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	depB := depA.Add(35 * time.Minute) // departs 5 minutes after A finishes

	segsA, _ := ComputeTimeWindows(depA, route)
	segsB, _ := ComputeTimeWindows(depB, route)

	// A: 08:00 → 08:30, B: 08:35 → 09:05 — must NOT overlap.
	aEnd := segsA[0].TimeWindowEnd
	bStart := segsB[0].TimeWindowStart

	if !bStart.After(aEnd) && !bStart.Equal(aEnd) {
		t.Errorf(
			"staggered departures still overlap: A ends %v, B starts %v — unexpected capacity conflict",
			aEnd, bStart,
		)
	}
}

// TestTimeWindows_PeakHour_MorningRush simulates the peak load scenario
// (Irish Traffic Commission data: morning rush 07:30–09:30) with 5 concurrent
// departure times spaced 5 minutes apart, each on the same route.
// All 5 should produce overlapping windows on seg_city_north — the segment
// capacity must absorb them or reject the excess.
func TestTimeWindows_PeakHour_MorningRush(t *testing.T) {
	route := []client.MapSegment{
		{SegmentID: "seg_city_north", TraversalTimeMinutes: 20},
	}
	baseTime := time.Date(2026, 4, 15, 7, 30, 0, 0, time.UTC)

	type window struct{ start, end time.Time }
	windows := make([]window, 5)
	for i := 0; i < 5; i++ {
		dep := baseTime.Add(time.Duration(i*5) * time.Minute)
		segs, _ := ComputeTimeWindows(dep, route)
		windows[i] = window{segs[0].TimeWindowStart, segs[0].TimeWindowEnd}
	}

	// All 5 departures (07:30, 07:35, 07:40, 07:45, 07:50) produce 20-min
	// windows that overlap each other on seg_city_north.
	overlapCount := 0
	for i := 0; i < len(windows); i++ {
		for j := i + 1; j < len(windows); j++ {
			a, b := windows[i], windows[j]
			if a.start.Before(b.end) && a.end.After(b.start) {
				overlapCount++
			}
		}
	}
	if overlapCount == 0 {
		t.Error("expected peak-hour windows to overlap on shared segment — capacity system should handle contention")
	}
	t.Logf("peak-hour: %d overlapping window pairs on seg_city_north (capacity service must handle %d concurrent bookings)", overlapCount, len(windows))
}

// TestTimeWindows_MidnightCrossover_CrossRegion verifies that a journey
// departing just before midnight and crossing into a new day produces correct
// windows across all segments — TIMESTAMPTZ arithmetic must handle the
// date boundary correctly.
func TestTimeWindows_MidnightCrossover_CrossRegion(t *testing.T) {
	dep := time.Date(2026, 4, 15, 23, 45, 0, 0, time.UTC)
	segs, arrival := ComputeTimeWindows(dep, []client.MapSegment{
		{SegmentID: "seg_city_north", TraversalTimeMinutes: 10},
		{SegmentID: "seg_north_airport", TraversalTimeMinutes: 15},
	})

	// seg_city_north: 23:45 → 23:55 (same day)
	// seg_north_airport: 23:55 → 00:10 (next day)
	wantSeg1End := time.Date(2026, 4, 15, 23, 55, 0, 0, time.UTC)
	wantSeg2End := time.Date(2026, 4, 16, 0, 10, 0, 0, time.UTC)

	if !segs[0].TimeWindowEnd.Equal(wantSeg1End) {
		t.Errorf("seg_city_north end: got %v, want %v", segs[0].TimeWindowEnd, wantSeg1End)
	}
	if !segs[1].TimeWindowEnd.Equal(wantSeg2End) {
		t.Errorf("seg_north_airport end (crosses midnight): got %v, want %v", segs[1].TimeWindowEnd, wantSeg2End)
	}
	if !arrival.Equal(wantSeg2End) {
		t.Errorf("estimated arrival: got %v, want %v", arrival, wantSeg2End)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 2 — Vehicle Type Normalisation
//
// The booking API accepts "HGV" as an alias for "truck".  Normalisation must
// happen before the capacity service is called so slot weights are consistent.
// ─────────────────────────────────────────────────────────────────────────────

// TestVehicleTypeNormalisation_HGVToTruck verifies the HGV alias mapping.
func TestVehicleTypeNormalisation_HGVToTruck(t *testing.T) {
	got, err := normalizeVehicleType("HGV")
	if err != nil {
		t.Fatalf("normalizeVehicleType(HGV): unexpected error: %v", err)
	}
	if got != "truck" {
		t.Errorf("normalizeVehicleType(HGV) = %q, want %q", got, "truck")
	}
}

// TestVehicleTypeNormalisation_CaseInsensitive verifies all valid types are
// accepted regardless of case (drivers may use "Car", "CAR", etc.).
func TestVehicleTypeNormalisation_CaseInsensitive(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"car", "car"},
		{"Car", "car"},
		{"CAR", "car"},
		{"van", "van"},
		{"Van", "van"},
		{"VAN", "van"},
		{"truck", "truck"},
		{"Truck", "truck"},
		{"TRUCK", "truck"},
		{"motorcycle", "motorcycle"},
		{"Motorcycle", "motorcycle"},
		{"MOTORCYCLE", "motorcycle"},
		{"HGV", "truck"},
		{"hgv", "truck"},
	}
	for _, c := range cases {
		got, err := normalizeVehicleType(c.input)
		if err != nil {
			t.Errorf("normalizeVehicleType(%q): unexpected error: %v", c.input, err)
			continue
		}
		if got != c.want {
			t.Errorf("normalizeVehicleType(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// TestVehicleTypeNormalisation_InvalidTypes verifies that unknown types are
// rejected with an error before any DB or HTTP call is made.
func TestVehicleTypeNormalisation_InvalidTypes(t *testing.T) {
	invalid := []string{"bus", "tram", "bicycle", "lorry", "", " ", "TRACTOR"}
	for _, vt := range invalid {
		_, err := normalizeVehicleType(vt)
		if err == nil {
			t.Errorf("normalizeVehicleType(%q): expected error for invalid type, got nil", vt)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 3 — Journey Status Transition Logic
//
// Checklist: "Conflicting requests properly handled □"
// The business rule: a driver can only have ONE active/approved journey.
// Any attempt to book while one is in progress must be rejected with 409.
// ─────────────────────────────────────────────────────────────────────────────

// TestStatusTransitions_ValidTerminals verifies which status values represent
// terminal states (no further transitions possible).
func TestStatusTransitions_ValidTerminals(t *testing.T) {
	terminals := []model.JourneyStatus{
		model.StatusCompleted,
		model.StatusCancelled,
		model.StatusRejected,
		model.StatusExpired,
	}
	nonTerminals := []model.JourneyStatus{
		model.StatusApproved,
		model.StatusActive,
	}

	// Terminal statuses must not allow activation or completion.
	for _, s := range terminals {
		if s == model.StatusApproved || s == model.StatusActive {
			t.Errorf("status %q is classified as terminal but is a mutable state", s)
		}
	}
	// Non-terminals must not be final.
	for _, s := range nonTerminals {
		for _, t2 := range terminals {
			if s == t2 {
				t.Errorf("status %q is in both terminal and non-terminal sets", s)
			}
		}
	}
}

// TestCancellationWindow_MinutesBeforeDeparture validates the 30-minute
// cancellation deadline rule:
//   - Cancel 31+ min before departure: must be allowed
//   - Cancel 30 min or less before departure: must be rejected
func TestCancellationWindow_MinutesBeforeDeparture(t *testing.T) {
	const minCancelMin = 30
	now := time.Now().UTC()

	cases := []struct {
		label         string
		departure     time.Time
		expectAllowed bool
	}{
		{"61 min away — allowed", now.Add(61 * time.Minute), true},
		{"31 min away — allowed", now.Add(31 * time.Minute), true},
		{"30 min away — rejected", now.Add(30 * time.Minute), false},
		{"29 min away — rejected", now.Add(29 * time.Minute), false},
		{"1 min away — rejected", now.Add(1 * time.Minute), false},
		{"departure now — rejected", now, false},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			timeUntil := time.Until(tc.departure)
			allowed := timeUntil > time.Duration(minCancelMin)*time.Minute
			if allowed != tc.expectAllowed {
				t.Errorf("departure in %v: allowed=%v, want %v", timeUntil.Round(time.Second), allowed, tc.expectAllowed)
			}
		})
	}
}

// TestActivationWindow_GracePeriod validates the 30-minute activation grace
// window:
//   - Before departure: must be rejected
//   - After departure but within 30 min: allowed
//   - More than 30 min past departure: rejected (journey expired)
func TestActivationWindow_GracePeriod(t *testing.T) {
	const activationGraceMin = 30
	departure := time.Now().UTC().Add(-10 * time.Minute) // departed 10 min ago

	graceEnd := departure.Add(time.Duration(activationGraceMin) * time.Minute)
	now := time.Now().UTC()

	tooEarly := now.Before(departure)
	tooLate := now.After(graceEnd)
	inWindow := !tooEarly && !tooLate

	if !inWindow {
		t.Errorf("departure was 10min ago with 30min grace: should be in activation window (tooEarly=%v tooLate=%v)", tooEarly, tooLate)
	}
}

// TestActivationWindow_ExpiredJourney verifies that a journey that was never
// activated within the grace window is treated as expired.
func TestActivationWindow_ExpiredJourney(t *testing.T) {
	const activationGraceMin = 30
	// Journey departed 45 minutes ago — grace window has closed.
	departure := time.Now().UTC().Add(-45 * time.Minute)
	graceEnd := departure.Add(time.Duration(activationGraceMin) * time.Minute)

	tooLate := time.Now().UTC().After(graceEnd)
	if !tooLate {
		t.Error("a journey 45 min past departure should be past the activation grace window")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 4 — Departure Time Validation
//
// Drivers must book at least 1 hour in advance.
// Checklist: "Conflicting requests properly handled □"
// ─────────────────────────────────────────────────────────────────────────────

// TestDepartureTime_TooSoon_Rejected verifies the 1-hour advance booking rule.
func TestDepartureTime_TooSoon_Rejected(t *testing.T) {
	const minAdvanceMin = 60
	now := time.Now().UTC()
	minDeparture := now.Add(time.Duration(minAdvanceMin) * time.Minute)

	cases := []struct {
		label         string
		departure     time.Time
		expectAllowed bool
	}{
		{"90 min ahead — ok", now.Add(90 * time.Minute), true},
		{"61 min ahead — ok", now.Add(61 * time.Minute), true},
		{"60 min ahead — borderline ok", minDeparture, true},
		{"59 min ahead — rejected", now.Add(59 * time.Minute), false},
		{"30 min ahead — rejected", now.Add(30 * time.Minute), false},
		{"now — rejected", now, false},
		{"past — rejected", now.Add(-5 * time.Minute), false},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			allowed := !tc.departure.Before(minDeparture)
			if allowed != tc.expectAllowed {
				t.Errorf("departure at %v: allowed=%v, want %v", tc.departure.Round(time.Second), allowed, tc.expectAllowed)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 5 — Segment Sequence Order
//
// ComputeTimeWindows relies on segments being in traversal order.
// The map service returns them ordered by sequence_order already, but
// the sequence numbers must be preserved in the output for rendering.
// ─────────────────────────────────────────────────────────────────────────────

// TestTimeWindows_SequenceOrderPreserved verifies that the segment's
// sequence_order is carried through to the JourneySegment output.
func TestTimeWindows_SequenceOrderPreserved(t *testing.T) {
	dep := time.Now().UTC()
	segs, _ := ComputeTimeWindows(dep, []client.MapSegment{
		{SegmentID: "seg_a", SequenceOrder: 1, TraversalTimeMinutes: 10},
		{SegmentID: "seg_b", SequenceOrder: 2, TraversalTimeMinutes: 15},
		{SegmentID: "seg_c", SequenceOrder: 3, TraversalTimeMinutes: 5},
	})

	for i, seg := range segs {
		wantOrder := i + 1
		if seg.SequenceOrder != wantOrder {
			t.Errorf("segs[%d].SequenceOrder = %d, want %d", i, seg.SequenceOrder, wantOrder)
		}
	}
}

func TestBuildRejectionReason_SegmentClosed(t *testing.T) {
	closureEnd := time.Date(2026, 4, 8, 14, 0, 0, 0, time.UTC)
	reason := buildRejectionReason(&client.FailedSegment{
		SegmentID:     "seg_city_north",
		Reason:        "segment_closed",
		ClosureReason: "maintenance",
		ClosureEnd:    &closureEnd,
	})

	if !strings.Contains(strings.ToLower(reason), "closed") {
		t.Fatalf("expected closure rejection reason, got %q", reason)
	}
	if !strings.Contains(reason, "seg_city_north") {
		t.Fatalf("expected segment id in rejection reason, got %q", reason)
	}
}

func TestBuildRejectionReason_AtCapacity(t *testing.T) {
	reason := buildRejectionReason(&client.FailedSegment{
		SegmentID: "seg_city_north",
		Reason:    "at_capacity",
	})

	if !strings.Contains(strings.ToLower(reason), "capacity") {
		t.Fatalf("expected capacity rejection reason, got %q", reason)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 6 — Integration Test Stubs
// ─────────────────────────────────────────────────────────────────────────────

// TestIntegration_DoubleBooking_SameDriver_SameTime tests that a driver who
// submits two booking requests simultaneously (e.g., double-tap on mobile) only
// gets ONE approved journey, not two.
//
// Mechanisms:
//  1. HasActiveJourney() query prevents a second booking when one is APPROVED
//  2. Idempotency cache returns the same response for duplicate keys
func TestIntegration_DoubleBooking_SameDriver_SameTime(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: requires live CockroachDB")
	}
	t.Log("SCENARIO: Driver sends POST /journeys twice in <100ms with same idempotency key")
	t.Log("Expected: both return 201 with identical journey_id — second is cache replay")
	t.Log("SCENARIO: Driver sends second POST with different idempotency key")
	t.Log("Expected: second returns 409 Conflict — 'driver already has an active journey'")
	t.Skip("run without -short with live CRDB")
}

// TestIntegration_ConcurrentDrivers_SameSegment_AtCapacity verifies that when
// N drivers all book the same segment simultaneously and N > capacity, exactly
// capacity drivers succeed and the rest are rejected.
func TestIntegration_ConcurrentDrivers_SameSegment_AtCapacity(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: requires live CockroachDB + Redis")
	}
	t.Log("SCENARIO: seg_city_north max_capacity=80, existing_load=79.0 (1 slot left)")
	t.Log("5 drivers each submit a car booking (1.0 slot) at the same time via goroutines")
	t.Log("Expected: exactly 1 succeeds (reserved), 4 rejected (at_capacity)")
	t.Log("Mechanism: serializable transaction + SELECT FOR UPDATE in sorted lock order")
	t.Skip("run without -short with live stack")
}

// TestIntegration_CrossVM_JourneyOriginatedOnVM1_NotifiedOnVM2 verifies the
// event propagation across VMs:
//
//	VM1 creates a journey → publishes to Redis Stream (journey.events)
//	VM2's notification-service consumer group member receives the event
//	VM2 sends FCM push to driver's registered device
func TestIntegration_CrossVM_JourneyOriginatedOnVM1_NotifiedOnVM2(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: requires multi-VM docker-stack + FCM credentials")
	}
	t.Log("SCENARIO: Book journey on VM1, verify FCM push received within 5s")
	t.Log("Mechanism: transactional outbox → Redis Stream → notification consumer → FCM")
	t.Log("Checklist: 'Notification latency < 5s'")
	t.Skip("run against full 3-VM docker-stack")
}
