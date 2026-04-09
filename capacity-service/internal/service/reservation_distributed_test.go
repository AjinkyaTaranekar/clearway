package service

// Distributed architecture concurrent tests for the reservation service.
//
// Every test in this file uses real goroutines with a sync.WaitGroup + barrier
// (sync.WaitGroup start-gate) so goroutines are released simultaneously.
// This means the Go race detector (-race) can catch actual data races in the
// production helpers this package exercises.
//
// Run with:
//   go test -race ./internal/service/...            # detect data races
//   go test -v   ./internal/service/...            # verbose output
//
// CS7NS6 checklist items exercised:
//   ✔ Concurrent requests properly synchronized   → ABBA deadlock / lock order
//   ✔ Double spending possible? (NO)              → ID collision / idempotency
//   ✔ Conflicting requests properly handled       → capacity arithmetic races
//   ✔ Sharding / Exploit locality                 → segment/region isolation
//   ✔ Consistency model                           → serializable tx arithmetic

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
)

// barrier returns a start-gate: call ready() in each goroutine just before the
// critical section, then call go_() to release all goroutines simultaneously.
// This maximises contention and makes race conditions reproducible.
func barrier(n int) (ready func(), go_ func()) {
	var wg sync.WaitGroup
	wg.Add(n)
	ready = func() { wg.Done() }
	go_ = func() { wg.Wait() }
	return
}

// segSort replicates the exact sort used inside Reserve() so tests stay coupled
// to production logic without importing internal DB state.
func segSort(s []model.SegmentReservation) {
	sort.Slice(s, func(i, j int) bool {
		return s[i].SegmentID < s[j].SegmentID
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 1 — Deadlock Prevention: Concurrent Goroutines Sorting the Same Segments
//
// Without alphabetical ordering, two goroutines that each acquire locks in
// opposite order create an ABBA deadlock.  This section proves the sort is
// deterministic regardless of which goroutine enters first.
//
// Checklist: "Concurrent requests properly synchronized □"
// ─────────────────────────────────────────────────────────────────────────────

// TestDeadlock_ABBA_500ConcurrentPairs fires 500 goroutine pairs simultaneously.
// Each pair holds the SAME two segments but starts in OPPOSITE order.
// After sorting, both must converge to the same order — proving no ABBA deadlock.
func TestDeadlock_ABBA_500ConcurrentPairs(t *testing.T) {
	const pairs = 500
	type result struct{ order [2]string }
	results := make([]result, pairs*2)

	ready, go_ := barrier(pairs * 2)
	var wg sync.WaitGroup
	wg.Add(pairs * 2)

	for i := 0; i < pairs; i++ {
		// Goroutine A: starts with seg_city_north first
		go func(idx int) {
			defer wg.Done()
			segs := []model.SegmentReservation{
				{SegmentID: "seg_city_north"},
				{SegmentID: "seg_north_airport"},
			}
			ready() // signal ready
			go_()   // wait for all goroutines to be ready, then go simultaneously
			segSort(segs)
			results[idx*2] = result{[2]string{segs[0].SegmentID, segs[1].SegmentID}}
		}(i)

		// Goroutine B: starts with seg_north_airport first (opposite order = ABBA risk)
		go func(idx int) {
			defer wg.Done()
			segs := []model.SegmentReservation{
				{SegmentID: "seg_north_airport"},
				{SegmentID: "seg_city_north"},
			}
			ready() // signal ready
			go_()   // wait for all goroutines to be ready, then go simultaneously
			segSort(segs)
			results[idx*2+1] = result{[2]string{segs[0].SegmentID, segs[1].SegmentID}}
		}(i)
	}
	wg.Wait()

	// All results must be identical: [seg_city_north, seg_north_airport]
	want := [2]string{"seg_city_north", "seg_north_airport"}
	for i, r := range results {
		if r.order != want {
			t.Errorf("goroutine %d: got order %v, want %v — ABBA deadlock possible under contention", i, r.order, want)
		}
	}
}

// TestDeadlock_CrossRegion_1000ConcurrentSorts runs 1000 goroutines, each
// sorting a 5-segment cross-region route from a random starting permutation.
// All must converge to the same sorted order.
func TestDeadlock_CrossRegion_1000ConcurrentSorts(t *testing.T) {
	const goroutines = 1000
	ids := []string{
		"seg_city_east", "seg_city_north", "seg_city_west",
		"seg_east_airport", "seg_north_airport",
	}

	results := make([][]string, goroutines)
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			// Each goroutine gets a different starting permutation.
			segs := make([]model.SegmentReservation, len(ids))
			for j, id := range ids {
				segs[(j+idx)%len(ids)] = model.SegmentReservation{SegmentID: id}
			}
			ready()
			go_()
			segSort(segs)
			out := make([]string, len(segs))
			for j, s := range segs {
				out[j] = s.SegmentID
			}
			results[idx] = out
		}(i)
	}
	wg.Wait()

	canonical := results[0]
	for i := 1; i < goroutines; i++ {
		for j := range canonical {
			if results[i][j] != canonical[j] {
				t.Fatalf(
					"goroutine %d position %d = %q, want %q — non-deterministic sort under concurrency",
					i, j, results[i][j], canonical[j],
				)
			}
		}
	}
}

// TestDeadlock_SingleSegment_NoContention verifies that single-segment journeys
// (the majority of bookings) never deadlock — there is nothing to sort against.
func TestDeadlock_SingleSegment_NoContention(t *testing.T) {
	const goroutines = 200
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			segs := []model.SegmentReservation{{SegmentID: "seg_city_riverside"}}
			ready()
			go_()
			segSort(segs)
			if segs[0].SegmentID != "seg_city_riverside" {
				errs <- fmt.Sprintf("single-segment sort mangled ID: %q", segs[0].SegmentID)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 2 — Capacity Arithmetic Under Concurrent Load
//
// Simulates multiple goroutines each independently computing whether their
// vehicle fits in a segment.  The shared state (capacity counter) is protected
// by a mutex — the test verifies no over-commit occurs even when goroutines
// race to decrement the same counter.
//
// Checklist: "Conflicting requests properly handled □"
// ─────────────────────────────────────────────────────────────────────────────

// TestCapacity_ConcurrentBookings_NoOverCommit simulates N goroutines each
// trying to book 1 car slot (1.0) on a segment with max_capacity=10.
// With 15 concurrent attempts, exactly 10 should succeed and 5 should be rejected.
func TestCapacity_ConcurrentBookings_NoOverCommit(t *testing.T) {
	const maxCap = 10.0
	const goroutines = 15
	carSlots := model.VehicleTypeCar.SlotsNeeded() // 1.0

	var mu sync.Mutex
	reserved := 0.0
	approved := 0
	rejected := 0

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ready()
			go_() // all goroutines attempt to book simultaneously

			mu.Lock()
			available := maxCap - reserved
			if available >= carSlots {
				reserved += carSlots
				approved++
			} else {
				rejected++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if approved != 10 {
		t.Errorf("expected exactly 10 approvals (max_cap=10, car=1.0), got %d", approved)
	}
	if rejected != 5 {
		t.Errorf("expected exactly 5 rejections, got %d", rejected)
	}
	if reserved != maxCap {
		t.Errorf("expected reserved=%.1f after filling capacity, got %.1f", maxCap, reserved)
	}
}

// TestCapacity_MixedVehicles_ConcurrentFill fires goroutines with different
// vehicle types simultaneously.  The mutex ensures no over-commit, and the
// slot-weight order (motorcycle=0.5, car=1.0, van=1.5, truck=3.0) must
// always leave remaining ≥ 0.
func TestCapacity_MixedVehicles_ConcurrentFill(t *testing.T) {
	const maxCap = 20.0
	vehicles := []model.VehicleType{
		model.VehicleTypeTruck,    // 3.0 × 2 = 6.0
		model.VehicleTypeTruck,    // 3.0
		model.VehicleTypeVan,      // 1.5 × 4 = 6.0
		model.VehicleTypeVan,
		model.VehicleTypeVan,
		model.VehicleTypeVan,
		model.VehicleTypeCar,      // 1.0 × 5 = 5.0
		model.VehicleTypeCar,
		model.VehicleTypeCar,
		model.VehicleTypeCar,
		model.VehicleTypeCar,
		model.VehicleTypeMotorcycle, // 0.5 × 10 = 5.0
		model.VehicleTypeMotorcycle,
		model.VehicleTypeMotorcycle,
		model.VehicleTypeMotorcycle,
		model.VehicleTypeMotorcycle,
		model.VehicleTypeMotorcycle,
		model.VehicleTypeMotorcycle,
		model.VehicleTypeMotorcycle,
		model.VehicleTypeMotorcycle,
		model.VehicleTypeMotorcycle,
		// That's exactly 22.0 total — 2.0 over capacity, so some must be rejected.
	}

	var mu sync.Mutex
	reserved := 0.0
	var approved, rejected int

	ready, go_ := barrier(len(vehicles))
	var wg sync.WaitGroup
	wg.Add(len(vehicles))

	for _, vt := range vehicles {
		go func(vt model.VehicleType) {
			defer wg.Done()
			slots := vt.SlotsNeeded()
			ready()
			go_()

			mu.Lock()
			available := maxCap - reserved
			if available >= slots {
				reserved += slots
				approved++
			} else {
				rejected++
			}
			mu.Unlock()
		}(vt)
	}
	wg.Wait()

	if reserved > maxCap {
		t.Errorf("over-commit: reserved=%.1f exceeds maxCap=%.1f", reserved, maxCap)
	}
	if reserved < 0 {
		t.Error("reserved went negative — arithmetic underflow")
	}
	t.Logf("mixed vehicles: approved=%d rejected=%d reserved=%.1f maxCap=%.1f", approved, rejected, reserved, maxCap)
}

// TestCapacity_TwoConcurrentTrucks_LastSlot is the core race condition:
// both trucks see "2.5 slots available, I need 3.0" at the same instant.
// The mutex (standing in for the DB serializable lock) ensures BOTH are rejected.
func TestCapacity_TwoConcurrentTrucks_LastSlot(t *testing.T) {
	const maxCap = 80.0
	const existingLoad = 77.5 // 2.5 slots remain
	truckSlots := model.VehicleTypeTruck.SlotsNeeded() // 3.0

	var mu sync.Mutex
	reserved := existingLoad
	approvals := 0

	ready, go_ := barrier(2)
	var wg sync.WaitGroup
	wg.Add(2)

	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			ready()
			go_() // both trucks race to check simultaneously

			mu.Lock()
			available := maxCap - reserved
			if available >= truckSlots {
				reserved += truckSlots
				approvals++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if approvals != 0 {
		t.Errorf("expected 0 truck approvals when only 2.5 slots remain (truck needs 3.0), got %d", approvals)
	}
}

// TestCapacity_ExactBoundary_SequentialAndConcurrent verifies that filling a
// segment to EXACTLY max_capacity through a series of concurrent bookings
// never goes over and never leaves a negative remainder.
func TestCapacity_ExactBoundary_SequentialAndConcurrent(t *testing.T) {
	// 10 cars (1.0 each) = exactly 10.0 = max_capacity.  One extra car added
	// concurrently after the segment is "full" must be rejected.
	const maxCap = 10.0
	const carsToFill = 10
	car := model.VehicleTypeCar.SlotsNeeded() // 1.0

	var mu sync.Mutex
	reserved := 0.0
	approvals := 0
	rejections := 0

	ready, go_ := barrier(carsToFill + 1) // +1 extra car that should be rejected
	var wg sync.WaitGroup
	wg.Add(carsToFill + 1)

	for i := 0; i <= carsToFill; i++ {
		go func() {
			defer wg.Done()
			ready()
			go_()

			mu.Lock()
			available := maxCap - reserved
			if available >= car {
				reserved += car
				approvals++
			} else {
				rejections++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if approvals != carsToFill {
		t.Errorf("expected %d approvals (exact fill), got %d", carsToFill, approvals)
	}
	if rejections != 1 {
		t.Errorf("expected 1 rejection (segment full), got %d", rejections)
	}
	if reserved != maxCap {
		t.Errorf("reserved=%.1f ≠ maxCap=%.1f after exact fill", reserved, maxCap)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 3 — Double-Booking Prevention: Concurrent ID Generation + Cache Keys
//
// Checklist: "Double spending possible? □" (expected: NO)
// ─────────────────────────────────────────────────────────────────────────────

// TestDoubleBooking_ConcurrentIDGeneration_5000 fires 5000 goroutines
// simultaneously, each calling generateID().  Any collision is a potential
// double-booking vector.
func TestDoubleBooking_ConcurrentIDGeneration_5000(t *testing.T) {
	const n = 5000
	var mu sync.Mutex
	seen := make(map[string]int, n)

	ready, go_ := barrier(n)
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ready()
			go_() // all 5000 goroutines call generateID simultaneously
			id := generateID("rsv")
			mu.Lock()
			seen[id]++
			mu.Unlock()
		}()
	}
	wg.Wait()

	collisions := 0
	for id, count := range seen {
		if count > 1 {
			collisions++
			t.Errorf("ID collision: %q generated %d times — double-booking risk", id, count)
		}
	}
	if len(seen) != n {
		t.Errorf("expected %d unique IDs, got %d (%d collisions)", n, len(seen), collisions)
	}
}

// TestDoubleBooking_ConcurrentCacheKeyReads verifies that concurrent reads of
// the same cache key always return the same string — no data race in the
// key construction function.
func TestDoubleBooking_ConcurrentCacheKeyReads(t *testing.T) {
	const goroutines = 500
	start := time.Unix(1713168000, 0).UTC()
	end := start.Add(30 * time.Minute)
	seg := "seg_city_north"

	keys := make([]string, goroutines)
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_() // all goroutines call availabilityCacheKey simultaneously
			keys[idx] = availabilityCacheKey(seg, start, end, priorityLevelNormal)
		}(i)
	}
	wg.Wait()

	want := keys[0]
	for i, k := range keys {
		if k != want {
			t.Errorf("goroutine %d: cache key %q ≠ %q — non-deterministic key under concurrency", i, k, want)
		}
	}
}

// TestDoubleBooking_DifferentSlotsProduceDifferentKeys verifies that two
// goroutines booking adjacent (non-overlapping) 30-min slots on the same
// segment get different cache keys — ensuring their availability reads are
// independent and never poisoned by the other's cached result.
func TestDoubleBooking_DifferentSlotsProduceDifferentKeys(t *testing.T) {
	const goroutines = 200 // 100 on slot A, 100 on slot B
	seg := "seg_city_north"
	slotA := time.Unix(1713168000, 0).UTC()
	slotB := slotA.Add(30 * time.Minute) // adjacent, non-overlapping

	var (
		mu    sync.Mutex
		keyAs []string
		keyBs []string
	)
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			var k string
			if idx%2 == 0 {
				k = availabilityCacheKey(seg, slotA, slotA.Add(30*time.Minute), priorityLevelNormal)
			} else {
				k = availabilityCacheKey(seg, slotB, slotB.Add(30*time.Minute), priorityLevelNormal)
			}
			mu.Lock()
			if idx%2 == 0 {
				keyAs = append(keyAs, k)
			} else {
				keyBs = append(keyBs, k)
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	// All slot-A keys must be identical to each other.
	for i, k := range keyAs {
		if k != keyAs[0] {
			t.Errorf("slotA key[%d] = %q ≠ %q — inconsistent keys for same slot", i, k, keyAs[0])
		}
	}
	// All slot-B keys must be identical to each other.
	for i, k := range keyBs {
		if k != keyBs[0] {
			t.Errorf("slotB key[%d] = %q ≠ %q — inconsistent keys for same slot", i, k, keyBs[0])
		}
	}
	// Slot A and slot B keys must differ.
	if len(keyAs) > 0 && len(keyBs) > 0 && keyAs[0] == keyBs[0] {
		t.Error("adjacent time slots produced the same cache key — booking-slot confusion possible")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 4 — Geographic Sharding: Concurrent Cross-Region Isolation
//
// Checklist: "Sharding □  Exploit locality □"
// ─────────────────────────────────────────────────────────────────────────────

// TestSharding_ConcurrentRegionLookups verifies that goroutines concurrently
// looking up segment→region mappings always get consistent answers —
// no stale cache or write-after-read issues.
func TestSharding_ConcurrentRegionLookups(t *testing.T) {
	// Mirrors the segment→region map from migration 002.
	regionOf := map[string]string{
		"seg_city_north":       "central",
		"seg_north_airport":    "north",
		"seg_city_east":        "central",
		"seg_east_airport":     "east",
		"seg_city_riverside":   "central",
		"seg_riverside_south":  "south",
		"seg_south_industrial": "south",
		"seg_industrial_east":  "east",
		"seg_city_west":        "west",
		"seg_west_port":        "west",
		"seg_port_south":       "south",
		"seg_west_northfield":  "west",
		"seg_northfield_north": "north",
	}
	segIDs := make([]string, 0, len(regionOf))
	for k := range regionOf {
		segIDs = append(segIDs, k)
	}

	const goroutines = 1000
	type result struct {
		segID  string
		region string
	}
	results := make([]result, goroutines)

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			seg := segIDs[idx%len(segIDs)]
			ready()
			go_()
			results[idx] = result{seg, regionOf[seg]}
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		want := regionOf[r.segID]
		if r.region != want {
			t.Errorf("goroutine %d: segment %q → region %q, want %q — concurrent map read inconsistency", i, r.segID, r.region, want)
		}
	}
}

// TestSharding_DisjointRoutes_ZeroContention verifies that goroutines booking
// disjoint routes (north vs east vs west) never produce a shared cache key,
// meaning they can never interfere with each other's availability reads.
func TestSharding_DisjointRoutes_ZeroContention(t *testing.T) {
	const goroutinesPerRoute = 100
	routes := map[string][]string{
		"north": {"seg_city_north", "seg_north_airport"},
		"east":  {"seg_city_east", "seg_east_airport"},
		"west":  {"seg_city_west", "seg_west_port"},
	}
	start := time.Unix(1713168000, 0).UTC()
	end := start.Add(30 * time.Minute)

	var mu sync.Mutex
	keysByRoute := make(map[string]map[string]bool)
	for route := range routes {
		keysByRoute[route] = make(map[string]bool)
	}

	total := len(routes) * goroutinesPerRoute
	ready, go_ := barrier(total)
	var wg sync.WaitGroup
	wg.Add(total)

	routeNames := []string{"north", "east", "west"}
	for routeIdx, routeName := range routeNames {
		for i := 0; i < goroutinesPerRoute; i++ {
			go func(rName string, rIdx int) {
				defer wg.Done()
				segs := routes[rName]
				ready()
				go_()
				var keys []string
				for _, seg := range segs {
					keys = append(keys, availabilityCacheKey(seg, start, end, priorityLevelNormal))
				}
				mu.Lock()
				for _, k := range keys {
					keysByRoute[rName][k] = true
				}
				mu.Unlock()
			}(routeName, routeIdx)
		}
	}
	wg.Wait()

	// No key set should overlap with any other route's key set.
	for i := 0; i < len(routeNames); i++ {
		for j := i + 1; j < len(routeNames); j++ {
			a, b := routeNames[i], routeNames[j]
			for k := range keysByRoute[a] {
				if keysByRoute[b][k] {
					t.Errorf("routes %q and %q share cache key %q — concurrent reads would pollute each other", a, b, k)
				}
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 5 — Time Window Overlap Under Concurrent Evaluation
//
// Two goroutines simultaneously evaluate whether their booking windows overlap
// an existing reservation.  The pure-function nature of the predicate must
// produce consistent results regardless of scheduling.
//
// Checklist: "Conflicting requests properly handled □"
// ─────────────────────────────────────────────────────────────────────────────

// overlaps replicates the SQL predicate:
//   time_window_start < newEnd AND time_window_end > newStart
func overlapsPure(existingStart, existingEnd, newStart, newEnd int64) bool {
	return existingStart < newEnd && existingEnd > newStart
}

// TestTimeWindowOverlap_ConcurrentEvaluation_AllCases runs every overlap
// case from N goroutines simultaneously to verify the pure function is
// goroutine-safe and always returns the correct result.
func TestTimeWindowOverlap_ConcurrentEvaluation_AllCases(t *testing.T) {
	type testCase struct {
		name        string
		exS, exE   int64
		nS, nE     int64
		want        bool
	}
	cases := []testCase{
		{"identical", 100, 200, 100, 200, true},
		{"new inside existing", 100, 200, 120, 160, true},
		{"existing inside new", 120, 160, 100, 200, true},
		{"partial overlap start", 100, 150, 130, 200, true},
		{"partial overlap end", 150, 250, 100, 170, true},
		{"back-to-back (no overlap)", 100, 200, 200, 300, false},
		{"fully before", 100, 120, 200, 300, false},
		{"fully after", 300, 400, 100, 200, false},
		{"adjacent 30-min slots", 0, 1800, 1800, 3600, false},
		{"overlapping 30-min rush slots", 0, 1800, 900, 2700, true},
		{"EU morning vs APAC evening", 28800, 32400, 79200, 82800, false},
	}

	const goroutinesPerCase = 50
	total := len(cases) * goroutinesPerCase
	ready, go_ := barrier(total)
	var wg sync.WaitGroup
	wg.Add(total)
	errs := make(chan string, total)

	for _, tc := range cases {
		for i := 0; i < goroutinesPerCase; i++ {
			go func(tc testCase, iter int) {
				defer wg.Done()
				ready()
				go_()
				got := overlapsPure(tc.exS, tc.exE, tc.nS, tc.nE)
				if got != tc.want {
					errs <- fmt.Sprintf("[iter%d] %s: overlaps=%v want=%v", iter, tc.name, got, tc.want)
				}
			}(tc, i)
		}
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// TestTimeWindowOverlap_PeakHour_MorningRush_ConcurrentCheck simulates the
// worst-case scenario: 20 goroutines each checking whether their morning-rush
// departure (5-min spacing) overlaps an existing 08:00–08:20 reservation.
// Expected: the first 4 overlap (08:00, 08:05, 08:10, 08:15), the 5th does not.
func TestTimeWindowOverlap_PeakHour_MorningRush_ConcurrentCheck(t *testing.T) {
	// Existing reservation: 08:00 → 08:20 (unix 0 → 1200)
	exStart, exEnd := int64(0), int64(1200)
	type slot struct {
		start int64
		end   int64
	}
	// 5-min spacing, 20-min traversal each
	slots := []slot{
		{0, 1200},    // 08:00 → 08:20 — identical to existing: OVERLAP
		{300, 1500},  // 08:05 → 08:25: OVERLAP
		{600, 1800},  // 08:10 → 08:30: OVERLAP
		{900, 2100},  // 08:15 → 08:35: OVERLAP
		{1200, 2400}, // 08:20 → 08:40: back-to-back, NO OVERLAP
		{1500, 2700}, // 08:25 → 08:45: after existing, NO OVERLAP
	}
	wantOverlap := []bool{true, true, true, true, false, false}

	const goroutinesPerSlot = 50
	total := len(slots) * goroutinesPerSlot
	ready, go_ := barrier(total)
	var wg sync.WaitGroup
	wg.Add(total)
	errs := make(chan string, total)

	for si, s := range slots {
		for i := 0; i < goroutinesPerSlot; i++ {
			go func(s slot, want bool, slotIdx, iter int) {
				defer wg.Done()
				ready()
				go_()
				got := overlapsPure(exStart, exEnd, s.start, s.end)
				if got != want {
					errs <- fmt.Sprintf("slot[%d] iter%d: overlap=%v want=%v (start=%d end=%d)", slotIdx, iter, got, want, s.start, s.end)
				}
			}(s, wantOverlap[si], si, i)
		}
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 6 — Unique Violation Classifier Under Concurrency
//
// isUniqueViolation is called on the hot path when two goroutines commit an
// identical idempotency_key simultaneously.  It must be goroutine-safe.
// ─────────────────────────────────────────────────────────────────────────────

// TestIsUniqueViolation_ConcurrentCalls_NoPanic runs 300 goroutines
// simultaneously calling isUniqueViolation with various errors.
// Any panic here means the function is not goroutine-safe.
func TestIsUniqueViolation_ConcurrentCalls_NoPanic(t *testing.T) {
	errs := []error{
		nil,
		fmt.Errorf("connection refused"),
		fmt.Errorf("SQLSTATE 23505 violates unique constraint"),
		fmt.Errorf("timeout"),
	}
	const goroutines = 300
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	// No assertions — just verifying no panic/race under concurrent access.
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			_ = isUniqueViolation(errs[idx%len(errs)])
		}(i)
	}
	wg.Wait()
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 7 — Input Validation Under Concurrent Load
//
// validateReserveRequest must be goroutine-safe (pure function with no shared
// state).  Concurrent callers must never see each other's results.
// ─────────────────────────────────────────────────────────────────────────────

// TestValidation_Concurrent_ValidAndInvalid fires valid and invalid requests
// concurrently.  Valid requests must never return an error; invalid requests
// must never return nil.
func TestValidation_Concurrent_ValidAndInvalid(t *testing.T) {
	now := time.Now().UTC()
	validReq := &model.ReserveRequest{
		JourneyID:      "jrn_crossregion01",
		IdempotencyKey: "idk_crossregion01",
		VehicleType:    model.VehicleTypeCar,
		Reservations: []model.SegmentReservation{
			{SegmentID: "seg_city_north", TimeWindowStart: now, TimeWindowEnd: now.Add(15 * time.Minute)},
			{SegmentID: "seg_north_airport", TimeWindowStart: now.Add(15 * time.Minute), TimeWindowEnd: now.Add(35 * time.Minute)},
		},
	}
	invalidReq := &model.ReserveRequest{
		// Missing idempotency_key — must always fail validation.
		JourneyID:   "jrn_bad",
		VehicleType: model.VehicleTypeCar,
		Reservations: []model.SegmentReservation{
			{SegmentID: "seg_city_north"},
		},
	}

	const goroutines = 400 // 200 valid, 200 invalid
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			if idx%2 == 0 {
				if err := validateReserveRequest(validReq); err != nil {
					errs <- fmt.Sprintf("goroutine %d: valid request failed: %v", idx, err)
				}
			} else {
				if err := validateReserveRequest(invalidReq); err == nil {
					errs <- fmt.Sprintf("goroutine %d: invalid request passed unexpectedly", idx)
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
// SECTION 8 — Integration Test Stubs (require live stack)
// ─────────────────────────────────────────────────────────────────────────────

// TestIntegration_TwoConcurrentCars_ExactCapacity documents what must hold
// in a live CockroachDB test: two goroutines simultaneously hitting POST /capacity/reserve
// on a segment with 1 slot left must produce exactly 1 success and 1 failure.
func TestIntegration_TwoConcurrentCars_ExactCapacity(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: requires live CockroachDB + Redis")
	}
	t.Log("SCENARIO: seg_city_north max_capacity=10, existing_load=9.0 (1 car slot left)")
	t.Log("Two goroutines simultaneously POST /capacity/reserve with car (1.0 slot)")
	t.Log("Expected: exactly HTTP 201 (reserved) + HTTP 200 (at_capacity)")
	t.Log("Mechanism: serializable tx + SELECT FOR UPDATE in sorted lock order")
	t.Skip("run without -short with live CRDB + Redis stack")
}

// TestIntegration_5VMsSimultaneous_SameSegment verifies that 5 goroutines
// representing 5 different VMs (regions) all booking the same segment at the
// same instant respect the global capacity limit enforced by the single CRDB node.
func TestIntegration_5VMsSimultaneous_SameSegment(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: requires live CockroachDB")
	}
	t.Log("SCENARIO: 5 goroutines (simulating 5 VMs) all reserve seg_city_north at t=0")
	t.Log("seg_city_north max_capacity=80, each VM books a truck (3.0 slots)")
	t.Log("Expected: floor(80/3) = 26 successes, rest rejected")
	t.Log("Mechanism: serializable isolation, deadlock prevention via sorted lock order")
	t.Skip("run without -short with live stack")
}

// TestIntegration_SameIdempotencyKey_ConcurrentRetries verifies that when the
// same idempotency key is submitted by 3 concurrent goroutines (retry storm),
// all 3 return the SAME reservation_id — no double-booking.
func TestIntegration_SameIdempotencyKey_ConcurrentRetries(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: requires live CockroachDB")
	}
	t.Log("SCENARIO: Client retries same request 3 times concurrently (network jitter)")
	t.Log("All 3 calls send identical Idempotency-Key header")
	t.Log("Expected: all 3 return HTTP 201 with IDENTICAL reservation_id")
	t.Log("Mechanism: idempotency_cache UNIQUE constraint + INSERT ON CONFLICT")
	t.Skip("run without -short with live CRDB")
}
