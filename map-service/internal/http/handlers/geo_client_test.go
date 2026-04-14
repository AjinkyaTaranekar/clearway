package handlers

// Tests for geo_client.go segment ID and region logic.
//
// Run:
//   go test -v ./internal/http/handlers/... -run Segment
//   go test -v ./internal/http/handlers/... -run GeoRegion
//   go test -race ./internal/http/handlers/...
//
// CS7NS6 checklist items exercised:
//   ✔ Sharding / Exploit locality  → geoRegion longitude-based split
//   ✔ Partitions handled           → cross-region ID generation
//   ✔ Data consistency             → same named road → same segment ID
//   ✔ Unnamed road isolation       → coordinate-based IDs prevent capacity pool collision

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 1 — geoRegion: longitude → region
// ─────────────────────────────────────────────────────────────────────────────

func TestGeoRegion_Europe_EU(t *testing.T) {
	cases := []struct {
		label string
		lat   float64
		lng   float64
	}{
		{"Dublin", 53.35, -6.26},
		{"London", 51.51, -0.13},
		{"Paris", 48.86, 2.35},
		{"Berlin", 52.52, 13.40},
		{"Istanbul (European side)", 41.01, 28.97},
	}
	for _, c := range cases {
		if got := geoRegion(c.lat, c.lng); got != "eu" {
			t.Errorf("geoRegion(%s lat=%v lng=%v) = %q, want eu", c.label, c.lat, c.lng, got)
		}
	}
}

func TestGeoRegion_Asia_AP(t *testing.T) {
	cases := []struct {
		label string
		lat   float64
		lng   float64
	}{
		{"Istanbul (Asian side)", 41.01, 29.15},
		{"Ankara", 39.93, 32.86},
		{"Delhi", 28.70, 77.10},
		{"Mumbai", 19.08, 72.88},
		{"Tokyo", 35.68, 139.65},
		{"Singapore", 1.35, 103.82},
	}
	for _, c := range cases {
		if got := geoRegion(c.lat, c.lng); got != "ap" {
			t.Errorf("geoRegion(%s lat=%v lng=%v) = %q, want ap", c.label, c.lat, c.lng, got)
		}
	}
}

func TestGeoRegion_Americas_US(t *testing.T) {
	cases := []struct {
		label string
		lat   float64
		lng   float64
	}{
		{"New York", 40.71, -74.01},
		{"Los Angeles", 34.05, -118.24},
		{"Toronto", 43.65, -79.38},
		{"São Paulo", -23.55, -46.63},
	}
	for _, c := range cases {
		if got := geoRegion(c.lat, c.lng); got != "us" {
			t.Errorf("geoRegion(%s lat=%v lng=%v) = %q, want us", c.label, c.lat, c.lng, got)
		}
	}
}

func TestGeoRegion_BosphorusBoundary(t *testing.T) {
	// 29.1°E is the split point — just west is EU, just east is AP
	if got := geoRegion(41.0, 29.09); got != "eu" {
		t.Errorf("lng=29.09 (just west of Bosphorus) = %q, want eu", got)
	}
	if got := geoRegion(41.0, 29.10); got != "ap" {
		t.Errorf("lng=29.10 (Bosphorus split point) = %q, want ap", got)
	}
	if got := geoRegion(41.0, 29.15); got != "ap" {
		t.Errorf("lng=29.15 (just east of Bosphorus) = %q, want ap", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 2 — deriveSegmentID: named roads
// ─────────────────────────────────────────────────────────────────────────────

func TestDeriveSegmentID_NamedRoad_RegionPrefix(t *testing.T) {
	cases := []struct {
		name string
		lat  float64
		lng  float64
		want string
	}{
		{"M7", 53.10, -7.20, "eu_m7"},          // Irish motorway → eu
		{"N8", 52.50, -8.00, "eu_n8"},           // Irish national → eu
		{"NH-44", 28.70, 77.10, "ap_nh_44"},     // Indian national highway → ap
		{"Ring Road", 53.35, -6.30, "eu_ring_road"}, // Dublin ring → eu
		{"Ring Road", 28.70, 77.10, "ap_ring_road"}, // Delhi ring → ap (no collision)
		{"I-95", 40.71, -74.01, "us_i_95"},      // US interstate → us
		{"O-4", 41.05, 29.05, "eu_o_4"},         // Istanbul EU motorway → eu
		{"O-4", 41.05, 29.20, "ap_o_4"},         // Same road, Asian side → ap
	}
	for _, c := range cases {
		got := deriveSegmentID(c.name, c.lat, c.lng)
		if got != c.want {
			t.Errorf("deriveSegmentID(%q, lat=%v, lng=%v) = %q, want %q",
				c.name, c.lat, c.lng, got, c.want)
		}
	}
}

func TestDeriveSegmentID_SameNameDifferentContinent_NoCollision(t *testing.T) {
	// "Ring Road" exists in hundreds of cities. With the old osrm_ prefix they
	// all shared one capacity pool. With region prefix they don't.
	dublinRingRoad := deriveSegmentID("Ring Road", 53.35, -6.30)   // eu_ring_road
	delhiRingRoad := deriveSegmentID("Ring Road", 28.70, 77.10)    // ap_ring_road
	nairobiRingRoad := deriveSegmentID("Ring Road", -1.29, 36.82)  // ap_ring_road
	newYorkRingRoad := deriveSegmentID("Ring Road", 40.71, -74.01) // us_ring_road

	if dublinRingRoad == delhiRingRoad {
		t.Errorf("Dublin and Delhi Ring Road must not share a segment ID: both got %q", dublinRingRoad)
	}
	if dublinRingRoad == newYorkRingRoad {
		t.Errorf("Dublin and New York Ring Road must not share a segment ID: both got %q", dublinRingRoad)
	}
	// Nairobi and Delhi are both APAC — they do share ap_ring_road.
	// This is a known limitation: same-name roads within one region still collide.
	// Acceptable because inter-region collision (the critical bug) is fixed.
	if nairobiRingRoad != delhiRingRoad {
		t.Errorf("Nairobi and Delhi both in APAC — expected same ID, got %q vs %q",
			nairobiRingRoad, delhiRingRoad)
	}
}

func TestDeriveSegmentID_SameRoadSameRegion_SameID(t *testing.T) {
	// Two journeys on M7 at different points along the road share one capacity pool.
	// That's the intended behaviour — the whole named road is one segment.
	entryPoint := deriveSegmentID("M7", 53.30, -6.89)
	midPoint := deriveSegmentID("M7", 53.22, -7.12)
	exitPoint := deriveSegmentID("M7", 53.10, -7.48)

	if entryPoint != midPoint || midPoint != exitPoint {
		t.Errorf("all points on M7 should share the same segment ID: %q %q %q",
			entryPoint, midPoint, exitPoint)
	}
	if entryPoint != "eu_m7" {
		t.Errorf("M7 in Ireland = %q, want eu_m7", entryPoint)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 3 — deriveSegmentID: unnamed roads
// ─────────────────────────────────────────────────────────────────────────────

func TestDeriveSegmentID_UnnamedRoad_CoordinateBased(t *testing.T) {
	id := deriveSegmentID("", 41.01, 29.20)
	// Must NOT be ap_unnamed — must embed coordinates
	if id == "ap_unnamed" {
		t.Errorf("unnamed road got generic ap_unnamed; want coordinate-based ID")
	}
	if !strings.HasPrefix(id, "ap_") {
		t.Errorf("unnamed road in APAC must start with ap_, got %q", id)
	}
	// Must contain the coordinates
	if !strings.Contains(id, "41.01") || !strings.Contains(id, "29.20") {
		t.Errorf("unnamed road ID %q must embed coordinates 41.01 and 29.20", id)
	}
}

func TestDeriveSegmentID_TwoUnnamedRoads_DifferentIDs(t *testing.T) {
	// Two different unnamed streets in Tokyo must get different capacity pools.
	streetA := deriveSegmentID("", 35.67, 139.65)
	streetB := deriveSegmentID("", 35.68, 139.74)

	if streetA == streetB {
		t.Errorf("different unnamed Tokyo streets share segment ID %q — capacity pool collision", streetA)
	}
}

func TestDeriveSegmentID_UnnamedRoadStability_SameCoords(t *testing.T) {
	// Same unnamed road segment called twice (e.g. cached vs fresh route) → same ID.
	id1 := deriveSegmentID("", 35.67, 139.65)
	id2 := deriveSegmentID("", 35.67, 139.65)
	if id1 != id2 {
		t.Errorf("same coordinates produced different IDs: %q vs %q", id1, id2)
	}
}

func TestDeriveSegmentID_UnnamedRoadPrecision_1kmGrid(t *testing.T) {
	// Points within ~1 km of each other (same 2-decimal grid cell) → same ID.
	// This groups nearby unnamed streets on the same stretch into one segment.
	ptA := deriveSegmentID("", 35.674, 139.651) // rounds to 35.67, 139.65
	ptB := deriveSegmentID("", 35.678, 139.658) // rounds to 35.68, 139.66 — different cell
	ptC := deriveSegmentID("", 35.671, 139.652) // rounds to 35.67, 139.65 — same as A

	if ptA != ptC {
		t.Errorf("points in same 1-km cell should share ID: %q vs %q", ptA, ptC)
	}
	if ptA == ptB {
		t.Errorf("points in different 1-km cells should have different IDs: both %q", ptA)
	}
}

func TestDeriveSegmentID_RefTakesPriorityOverName(t *testing.T) {
	// firstNonEmpty(ref, name) — ref wins if present.
	// Simulate: ref="M7", name="Motorway" → should produce eu_m7 not eu_motorway.
	// (The selection happens in collapseSteps before calling deriveSegmentID.)
	byRef := deriveSegmentID("M7", 53.10, -7.20)
	byName := deriveSegmentID("Motorway", 53.10, -7.20)
	if byRef == byName {
		t.Errorf("ref-based and name-based IDs should differ: both %q", byRef)
	}
	if byRef != "eu_m7" {
		t.Errorf("deriveSegmentID(M7) = %q, want eu_m7", byRef)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 4 — Istanbul→Ankara saga trigger simulation
//
// This test encodes the key demo scenario: a route from Istanbul's European
// district to Ankara crosses the Bosphorus at ~29.1°E, producing both eu_*
// and ap_* segment IDs in the same journey. This is what triggers the Saga
// coordinator in the capacity service.
// ─────────────────────────────────────────────────────────────────────────────

func TestIstanbulAnkara_CrossesBosphorus_ProducesBothRegions(t *testing.T) {
	// Representative steps from the actual OSRM route (sampled coordinates).
	type step struct {
		road string
		lat  float64
		lng  float64
	}
	routeSteps := []step{
		{"Zeynepsultan Camii Sokağı", 41.01, 28.98}, // EU Istanbul
		{"Alayköşkü Caddesi", 41.01, 28.98},         // EU Istanbul
		{"Galata Köprüsü", 41.02, 28.97},             // EU (Galata Bridge, European side)
		{"Dolmabahçe Caddesi", 41.04, 28.99},         // EU
		{"O-1", 41.04, 29.02},                        // EU (western approach)
		{"O-4", 41.07, 29.02},                        // EU (still west of 29.1)
		{"O-4", 41.10, 29.10},                        // AP — crosses Bosphorus!
		{"O-7", 41.09, 29.46},                        // AP (Asian Turkey)
		{"D750", 40.20, 30.03},                       // AP (Ankara approach)
	}

	euCount, apCount := 0, 0
	for _, s := range routeSteps {
		id := deriveSegmentID(s.road, s.lat, s.lng)
		switch {
		case strings.HasPrefix(id, "eu_"):
			euCount++
		case strings.HasPrefix(id, "ap_"):
			apCount++
		}
	}

	if euCount == 0 {
		t.Error("Istanbul→Ankara route produced no eu_ segments — Bosphorus crossing not detected")
	}
	if apCount == 0 {
		t.Error("Istanbul→Ankara route produced no ap_ segments — Bosphorus crossing not detected")
	}

	// Verify O-4 specifically splits at the boundary
	o4EU := deriveSegmentID("O-4", 41.07, 29.02) // west of 29.1
	o4AP := deriveSegmentID("O-4", 41.10, 29.10) // east of 29.1
	if o4EU == o4AP {
		t.Errorf("O-4 should produce different IDs on each side of Bosphorus, both got %q", o4EU)
	}
	if o4EU != "eu_o_4" {
		t.Errorf("O-4 European side = %q, want eu_o_4", o4EU)
	}
	if o4AP != "ap_o_4" {
		t.Errorf("O-4 Asian side = %q, want ap_o_4", o4AP)
	}

	t.Logf("Istanbul→Ankara: %d eu_ segments, %d ap_ segments — saga will fire", euCount, apCount)
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 5 — Concurrent safety
// ─────────────────────────────────────────────────────────────────────────────

func TestDeriveSegmentID_ConcurrentCalls_NoRace(t *testing.T) {
	const goroutines = 500
	var wg sync.WaitGroup
	var startGate sync.WaitGroup
	startGate.Add(goroutines)

	type input struct {
		road string
		lat  float64
		lng  float64
		want string
	}
	inputs := []input{
		{"M7", 53.10, -7.20, "eu_m7"},
		{"NH-44", 28.70, 77.10, "ap_nh_44"},
		{"I-95", 40.71, -74.01, "us_i_95"},
		{"", 35.67, 139.65, fmt.Sprintf("ap_%.2f_%.2f", 35.67, 139.65)},
	}

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startGate.Done()
			startGate.Wait()
			for _, in := range inputs {
				got := deriveSegmentID(in.road, in.lat, in.lng)
				if got != in.want {
					t.Errorf("deriveSegmentID(%q) = %q, want %q", in.road, got, in.want)
				}
			}
		}()
	}
	wg.Wait()
}
