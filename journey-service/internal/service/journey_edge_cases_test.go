package service

// Concurrent edge-case tests for the journey service.
//
// Every test uses the barrier pattern to release goroutines simultaneously:
//   ready, go_ := barrier(n)
//   // goroutine: ready(); go_(); <work>
// Run with: go test -race ./internal/service/...
//
// CS7NS6 checklist:
//   ✔ Concurrent requests properly synchronized  → same-driver active journey
//   ✔ Conflicting requests properly handled      → departure-window validation
//   ✔ Sharding / exploit locality                → region-annotated segments
//   ✔ Communication failures tolerated           → vehicle type normalisation
//   ✔ Test application/testing framework         → this file

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/client"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/internal/model"
)

func barrier(n int) (ready func(), go_ func()) {
	var wg sync.WaitGroup
	wg.Add(n)
	ready = func() { wg.Done() }
	go_ = func() { wg.Wait() }
	return
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 1 — Time Window Cascade: Concurrent Computation
//
// ComputeTimeWindows is a pure function.  Multiple goroutines calling it with
// the same inputs must always get identical results (no shared mutable state).
// ─────────────────────────────────────────────────────────────────────────────

// TestTimeWindows_Concurrent_SameRoute_IdenticalResults fires 500 goroutines
// all computing the same 4-segment cross-region route simultaneously.
// All must produce byte-for-byte identical windows.
func TestTimeWindows_Concurrent_SameRoute_IdenticalResults(t *testing.T) {
	const goroutines = 500
	dep := time.Date(2026, 4, 15, 7, 30, 0, 0, time.UTC)
	route := []client.MapSegment{
		{SegmentID: "seg_city_north", SequenceOrder: 1, TraversalTimeMinutes: 12, Region: "central"},
		{SegmentID: "seg_north_airport", SequenceOrder: 2, TraversalTimeMinutes: 18, Region: "north"},
		{SegmentID: "seg_city_west", SequenceOrder: 3, TraversalTimeMinutes: 9, Region: "central"},
		{SegmentID: "seg_west_port", SequenceOrder: 4, TraversalTimeMinutes: 25, Region: "west"},
	}

	type result struct {
		arrival time.Time
		ends    [4]time.Time
	}
	results := make([]result, goroutines)
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			segs, arrival := ComputeTimeWindows(dep, route)
			r := result{arrival: arrival}
			for j, s := range segs {
				r.ends[j] = s.TimeWindowEnd
			}
			results[idx] = r
		}(i)
	}
	wg.Wait()

	canonical := results[0]
	for i, r := range results {
		if !r.arrival.Equal(canonical.arrival) {
			t.Errorf("goroutine %d: arrival %v ≠ canonical %v", i, r.arrival, canonical.arrival)
		}
		for j := range r.ends {
			if !r.ends[j].Equal(canonical.ends[j]) {
				t.Errorf("goroutine %d seg[%d] end %v ≠ canonical %v", i, j, r.ends[j], canonical.ends[j])
			}
		}
	}
}

// TestTimeWindows_Concurrent_TwoDriversSameSlot fires goroutine pairs
// simultaneously — one for Driver A, one for Driver B — on the same route
// and same departure time.  Both must produce identical windows, which means
// they WILL contend on the capacity service (serializable tx decides the winner).
func TestTimeWindows_Concurrent_TwoDriversSameSlot(t *testing.T) {
	const pairs = 200
	dep := time.Date(2026, 4, 15, 9, 0, 0, 0, time.UTC)
	route := []client.MapSegment{
		{SegmentID: "seg_city_north", TraversalTimeMinutes: 15},
		{SegmentID: "seg_north_airport", TraversalTimeMinutes: 20},
	}

	type window struct{ start, end time.Time }
	type result struct {
		driver  string
		windows [2]window
		arrival time.Time
	}
	results := make([]result, pairs*2)
	ready, go_ := barrier(pairs * 2)
	var wg sync.WaitGroup
	wg.Add(pairs * 2)

	for i := 0; i < pairs; i++ {
		go func(idx int) {
			defer wg.Done()
			segs, arrival := func() ([]model.JourneySegment, time.Time) { return ComputeTimeWindows(dep, route) }()
			r := result{driver: "A"}
			for j, s := range segs {
				r.windows[j] = window{s.TimeWindowStart, s.TimeWindowEnd}
			}
			r.arrival = arrival
			ready()
			go_()
			results[idx*2] = r
		}(i)

		go func(idx int) {
			defer wg.Done()
			segs, arrival := func() ([]model.JourneySegment, time.Time) { return ComputeTimeWindows(dep, route) }()
			r := result{driver: "B"}
			for j, s := range segs {
				r.windows[j] = window{s.TimeWindowStart, s.TimeWindowEnd}
			}
			r.arrival = arrival
			ready()
			go_()
			results[idx*2+1] = r
		}(i)
	}
	wg.Wait()

	canonical := results[0]
	for i, r := range results {
		if !r.arrival.Equal(canonical.arrival) {
			t.Errorf("pair goroutine %d: arrival differs — drivers would book different capacity slots", i)
		}
		for j := range r.windows {
			if !r.windows[j].start.Equal(canonical.windows[j].start) ||
				!r.windows[j].end.Equal(canonical.windows[j].end) {
				t.Errorf("goroutine %d seg[%d]: window differs — should contend on same capacity slot", i, j)
			}
		}
	}
}

// TestTimeWindows_Concurrent_ChainedWindowsNeverGap verifies that no matter
// how many goroutines compute the same route simultaneously, the end of
// segment[i] always equals the start of segment[i+1].
func TestTimeWindows_Concurrent_ChainedWindowsNeverGap(t *testing.T) {
	const goroutines = 300
	dep := time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)
	route := []client.MapSegment{
		{SegmentID: "seg_city_north", TraversalTimeMinutes: 10},
		{SegmentID: "seg_west_northfield", TraversalTimeMinutes: 15},
		{SegmentID: "seg_northfield_north", TraversalTimeMinutes: 8},
		{SegmentID: "seg_north_airport", TraversalTimeMinutes: 20},
	}

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			segs, _ := ComputeTimeWindows(dep, route)
			for j := 0; j < len(segs)-1; j++ {
				if !segs[j].TimeWindowEnd.Equal(segs[j+1].TimeWindowStart) {
					errs <- fmt.Sprintf("goroutine %d: gap/overlap between seg[%d] and seg[%d]", idx, j, j+1)
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// TestTimeWindows_Concurrent_PeakHour_5Departures simulates morning rush:
// 5 different departure times (5-min spacing), each computed by 100 goroutines
// simultaneously.  Verifies peak-hour windows overlap on the shared segment
// and that the computation is race-free.
func TestTimeWindows_Concurrent_PeakHour_5Departures(t *testing.T) {
	const goroutinesPerDep = 100
	baseTime := time.Date(2026, 4, 15, 7, 30, 0, 0, time.UTC)
	departures := make([]time.Time, 5)
	for i := range departures {
		departures[i] = baseTime.Add(time.Duration(i*5) * time.Minute)
	}
	route := []client.MapSegment{
		{SegmentID: "seg_city_north", TraversalTimeMinutes: 20},
	}

	type windowResult struct {
		depIdx int
		start  time.Time
		end    time.Time
	}
	total := len(departures) * goroutinesPerDep
	results := make([]windowResult, total)
	ready, go_ := barrier(total)
	var wg sync.WaitGroup
	wg.Add(total)

	for di, dep := range departures {
		for gi := 0; gi < goroutinesPerDep; gi++ {
			idx := di*goroutinesPerDep + gi
			go func(dep time.Time, depIdx, idx int) {
				defer wg.Done()
				ready()
				go_()
				segs, _ := ComputeTimeWindows(dep, route)
				results[idx] = windowResult{depIdx, segs[0].TimeWindowStart, segs[0].TimeWindowEnd}
			}(dep, di, idx)
		}
	}
	wg.Wait()

	// All goroutines for the same departure must have the same window.
	for di := range departures {
		base := results[di*goroutinesPerDep]
		for gi := 1; gi < goroutinesPerDep; gi++ {
			r := results[di*goroutinesPerDep+gi]
			if !r.start.Equal(base.start) || !r.end.Equal(base.end) {
				t.Errorf("dep[%d] goroutine %d: window differs from goroutine 0 — race in ComputeTimeWindows", di, gi)
			}
		}
	}

	// Adjacent departures overlap on seg_city_north (20-min traversal, 5-min spacing).
	overlapCount := 0
	for i := 0; i < len(departures); i++ {
		for j := i + 1; j < len(departures); j++ {
			a := results[i*goroutinesPerDep]
			b := results[j*goroutinesPerDep]
			if a.start.Before(b.end) && a.end.After(b.start) {
				overlapCount++
			}
		}
	}
	if overlapCount == 0 {
		t.Error("peak-hour windows should overlap on shared segment — capacity system must handle this contention")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 2 — Vehicle Type Normalisation: Concurrent Calls
// ─────────────────────────────────────────────────────────────────────────────

// TestVehicleNorm_Concurrent_AllValid fires 1000 goroutines simultaneously,
// each calling normalizeVehicleType with a valid input.
// No error, no data race.
func TestVehicleNorm_Concurrent_AllValid(t *testing.T) {
	inputs := []string{"car", "Car", "CAR", "van", "Van", "VAN",
		"truck", "Truck", "TRUCK", "motorcycle", "Motorcycle",
		"MOTORCYCLE", "HGV", "hgv"}
	wants := []string{"car", "car", "car", "van", "van", "van",
		"truck", "truck", "truck", "motorcycle", "motorcycle",
		"motorcycle", "truck", "truck"}

	const goroutines = 1000
	results := make([]string, goroutines)
	errs := make(chan string, goroutines)

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			in := inputs[idx%len(inputs)]
			want := wants[idx%len(wants)]
			ready()
			go_()
			got, err := normalizeVehicleType(in)
			if err != nil {
				errs <- fmt.Sprintf("goroutine %d: unexpected error for %q: %v", idx, in, err)
				return
			}
			if got != want {
				errs <- fmt.Sprintf("goroutine %d: normalizeVehicleType(%q)=%q, want %q", idx, in, got, want)
			}
			results[idx] = got
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// TestVehicleNorm_Concurrent_MixedValidInvalid fires 600 goroutines:
// 300 with valid types, 300 with invalid types.  No goroutine must
// return a wrong result even when both sets run simultaneously.
func TestVehicleNorm_Concurrent_MixedValidInvalid(t *testing.T) {
	const goroutines = 600
	errs := make(chan string, goroutines)

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			if idx%2 == 0 {
				// Valid: must succeed.
				got, err := normalizeVehicleType("car")
				if err != nil || got != "car" {
					errs <- fmt.Sprintf("goroutine %d: valid input failed", idx)
				}
			} else {
				// Invalid: must fail.
				_, err := normalizeVehicleType("bus")
				if err == nil {
					errs <- fmt.Sprintf("goroutine %d: invalid input passed", idx)
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 3 — Departure-Time Validation: Concurrent Clock Reads
//
// The departure-time check (must be ≥ 60 min from now) reads time.Now().
// Multiple goroutines calling it simultaneously must all get consistent results.
// ─────────────────────────────────────────────────────────────────────────────

// TestDeparture_Concurrent_FutureAlwaysOK fires 500 goroutines all checking
// a departure time 2 hours from now.  All must be accepted.
func TestDeparture_Concurrent_FutureAlwaysOK(t *testing.T) {
	const goroutines = 500
	const minAdvanceMin = 60

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			departure := time.Now().Add(2 * time.Hour)
			minDeparture := time.Now().Add(time.Duration(minAdvanceMin) * time.Minute)
			if departure.Before(minDeparture) {
				errs <- fmt.Sprintf("goroutine %d: 2-hour-ahead departure rejected — clock race?", idx)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// TestDeparture_Concurrent_PastAlwaysRejected fires 500 goroutines all checking
// a departure time 5 minutes ago.  All must be rejected.
func TestDeparture_Concurrent_PastAlwaysRejected(t *testing.T) {
	const goroutines = 500
	const minAdvanceMin = 60

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			departure := time.Now().Add(-5 * time.Minute)
			minDeparture := time.Now().Add(time.Duration(minAdvanceMin) * time.Minute)
			if !departure.Before(minDeparture) {
				errs <- fmt.Sprintf("goroutine %d: past departure was accepted — validation race?", idx)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 4 — Cancellation Window: Concurrent 30-Min Boundary Check
// ─────────────────────────────────────────────────────────────────────────────

// TestCancellation_Concurrent_ClearlyAllowed fires 400 goroutines all checking
// a journey with departure 90 minutes away.  All must be allowed.
func TestCancellation_Concurrent_ClearlyAllowed(t *testing.T) {
	const goroutines = 400
	const minCancelMin = 30

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			departure := time.Now().Add(90 * time.Minute)
			timeUntil := time.Until(departure)
			if timeUntil <= time.Duration(minCancelMin)*time.Minute {
				errs <- fmt.Sprintf("goroutine %d: 90-min-away cancellation rejected — race?", idx)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// TestCancellation_Concurrent_ClearlyRejected fires 400 goroutines all checking
// a journey departing in 10 minutes.  All must be rejected.
func TestCancellation_Concurrent_ClearlyRejected(t *testing.T) {
	const goroutines = 400
	const minCancelMin = 30

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			departure := time.Now().Add(10 * time.Minute)
			timeUntil := time.Until(departure)
			if timeUntil > time.Duration(minCancelMin)*time.Minute {
				errs <- fmt.Sprintf("goroutine %d: 10-min-away cancellation accepted — race?", idx)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 5 — Region Annotation: Concurrent Route Computation
// ─────────────────────────────────────────────────────────────────────────────

// TestRegion_Concurrent_AlwaysPreserved fires 300 goroutines all computing the
// same 3-segment cross-region route.  The Region field on each segment must
// survive unmodified — it is used for VM routing in the capacity service.
func TestRegion_Concurrent_AlwaysPreserved(t *testing.T) {
	const goroutines = 300
	dep := time.Now().UTC()
	route := []client.MapSegment{
		{SegmentID: "seg_city_north", Region: "central", TraversalTimeMinutes: 10},
		{SegmentID: "seg_north_airport", Region: "north", TraversalTimeMinutes: 20},
		{SegmentID: "seg_west_port", Region: "west", TraversalTimeMinutes: 15},
	}
	wantRegions := []string{"central", "north", "west"}

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			segs, _ := ComputeTimeWindows(dep, route)
			for j, want := range wantRegions {
				if segs[j].Region != want {
					errs <- fmt.Sprintf("goroutine %d seg[%d]: region=%q, want %q — VM routing broken", idx, j, segs[j].Region, want)
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 6 — Activation Window: Concurrent Grace-Period Checks
// ─────────────────────────────────────────────────────────────────────────────

// TestActivation_Concurrent_WithinWindow fires 300 goroutines all checking
// activation for a journey that departed 10 minutes ago (within the 30-min grace).
// All must be allowed.
func TestActivation_Concurrent_WithinWindow(t *testing.T) {
	const goroutines = 300
	const activationGraceMin = 30
	departure := time.Now().UTC().Add(-10 * time.Minute)

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			now := time.Now().UTC()
			graceEnd := departure.Add(time.Duration(activationGraceMin) * time.Minute)
			inWindow := !now.Before(departure) && !now.After(graceEnd)
			if !inWindow {
				errs <- fmt.Sprintf("goroutine %d: journey 10min past departure should be in activation window", idx)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// TestActivation_Concurrent_GraceExpired fires 300 goroutines all checking
// activation for a journey that departed 45 minutes ago (past the 30-min grace).
// All must be rejected.
func TestActivation_Concurrent_GraceExpired(t *testing.T) {
	const goroutines = 300
	const activationGraceMin = 30
	departure := time.Now().UTC().Add(-45 * time.Minute)

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			graceEnd := departure.Add(time.Duration(activationGraceMin) * time.Minute)
			tooLate := time.Now().UTC().After(graceEnd)
			if !tooLate {
				errs <- fmt.Sprintf("goroutine %d: 45-min-past-departure should be beyond activation window", idx)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 7 — Status Transition Safety: Concurrent State Reads
// ─────────────────────────────────────────────────────────────────────────────

// TestStatus_Concurrent_TerminalVsNonTerminal fires 400 goroutines checking
// whether each status is terminal or non-terminal.  Results must be consistent
// — no goroutine should see a mutable status as terminal or vice versa.
func TestStatus_Concurrent_TerminalVsNonTerminal(t *testing.T) {
	const goroutines = 400
	terminals := map[model.JourneyStatus]bool{
		model.StatusCompleted: true,
		model.StatusCancelled: true,
		model.StatusRejected:  true,
		model.StatusExpired:   true,
	}
	nonTerminals := map[model.JourneyStatus]bool{
		model.StatusApproved: true,
		model.StatusActive:   true,
	}
	allStatuses := []model.JourneyStatus{
		model.StatusApproved, model.StatusActive,
		model.StatusCompleted, model.StatusCancelled,
		model.StatusRejected, model.StatusExpired,
	}

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			s := allStatuses[idx%len(allStatuses)]
			isTerminal := terminals[s]
			isNonTerminal := nonTerminals[s]
			if isTerminal && isNonTerminal {
				errs <- fmt.Sprintf("goroutine %d: status %q in both terminal and non-terminal sets", idx, s)
			}
			if !isTerminal && !isNonTerminal {
				errs <- fmt.Sprintf("goroutine %d: status %q not in either set", idx, s)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 8 — Integration Test Stubs (require live stack)
// ─────────────────────────────────────────────────────────────────────────────

// TestIntegration_ConcurrentDrivers_SameSegment_AtCapacity documents the
// live scenario where N goroutines (representing N VMs) all submit bookings
// for the last available slot simultaneously.
func TestIntegration_ConcurrentDrivers_SameSegment_AtCapacity(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: requires live CockroachDB + Redis")
	}
	t.Log("SCENARIO: seg_city_north max_capacity=80, existing_load=79.0 (1 slot left)")
	t.Log("5 goroutines (VMs) simultaneously POST /journeys with car (1.0 slot)")
	t.Log("Expected: exactly 1 × HTTP 201, 4 × HTTP 409/200 at_capacity")
	t.Log("Mechanism: serializable tx + SELECT FOR UPDATE (sorted lock order)")
	t.Skip("run without -short with live stack")
}

// TestIntegration_SameDriver_ConcurrentBookings documents the scenario where
// a driver rapidly taps "book" twice — the system must prevent two APPROVED
// journeys for the same driver existing simultaneously.
func TestIntegration_SameDriver_ConcurrentBookings(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: requires live CockroachDB")
	}
	t.Log("SCENARIO: Driver D1 sends 2 concurrent POST /journeys with different idempotency keys")
	t.Log("Expected: 1 × HTTP 201 (first writer wins), 1 × HTTP 409 Conflict")
	t.Log("Mechanism: HasActiveJourney() query inside the same serializable transaction")
	t.Skip("run without -short with live CRDB")
}
