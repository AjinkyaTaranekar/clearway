package service

// Tests in the same package so we can exercise unexported helpers directly.
// DB-dependent tests are guarded by the "integration" build tag so they are
// skipped in the normal `go test ./...` run but can be enabled explicitly:
//   go test -tags integration ./internal/service/...

import (
	"fmt"
	"testing"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
)

// ---------------------------------------------------------------------------
// validateReserveRequest
// ---------------------------------------------------------------------------

func TestValidateReserveRequest_MissingJourneyID(t *testing.T) {
	req := &model.ReserveRequest{
		IdempotencyKey: "idk_abc",
		VehicleType:    model.VehicleTypeCar,
		Reservations:   []model.SegmentReservation{{SegmentID: "seg_city_north"}},
	}
	if err := validateReserveRequest(req); err == nil {
		t.Error("expected error for missing journey_id, got nil")
	}
}

func TestValidateReserveRequest_MissingIdempotencyKey(t *testing.T) {
	req := &model.ReserveRequest{
		JourneyID:    "jrn_abc",
		VehicleType:  model.VehicleTypeCar,
		Reservations: []model.SegmentReservation{{SegmentID: "seg_city_north"}},
	}
	if err := validateReserveRequest(req); err == nil {
		t.Error("expected error for missing idempotency_key, got nil")
	}
}

func TestValidateReserveRequest_InvalidVehicleType(t *testing.T) {
	req := &model.ReserveRequest{
		JourneyID:      "jrn_abc",
		IdempotencyKey: "idk_abc",
		VehicleType:    "bus",
		Reservations:   []model.SegmentReservation{{SegmentID: "seg_city_north"}},
	}
	if err := validateReserveRequest(req); err == nil {
		t.Error("expected error for invalid vehicle_type, got nil")
	}
}

func TestValidateReserveRequest_EmptyReservations(t *testing.T) {
	req := &model.ReserveRequest{
		JourneyID:      "jrn_abc",
		IdempotencyKey: "idk_abc",
		VehicleType:    model.VehicleTypeCar,
		Reservations:   []model.SegmentReservation{},
	}
	if err := validateReserveRequest(req); err == nil {
		t.Error("expected error for empty reservations list, got nil")
	}
}

func TestValidateReserveRequest_MissingSegmentID(t *testing.T) {
	req := &model.ReserveRequest{
		JourneyID:      "jrn_abc",
		IdempotencyKey: "idk_abc",
		VehicleType:    model.VehicleTypeCar,
		Reservations:   []model.SegmentReservation{{SegmentID: ""}},
	}
	if err := validateReserveRequest(req); err == nil {
		t.Error("expected error for missing segment_id in reservation, got nil")
	}
}

func TestValidateReserveRequest_Valid(t *testing.T) {
	now := time.Now()
	req := &model.ReserveRequest{
		JourneyID:      "jrn_abc",
		IdempotencyKey: "idk_abc",
		VehicleType:    model.VehicleTypeTruck,
		Reservations: []model.SegmentReservation{
			{
				SegmentID:       "seg_city_north",
				TimeWindowStart: now,
				TimeWindowEnd:   now.Add(30 * time.Minute),
			},
		},
	}
	if err := validateReserveRequest(req); err != nil {
		t.Errorf("expected no error for valid request, got: %v", err)
	}
}

// All four vehicle types must pass validation.
func TestValidateReserveRequest_AllVehicleTypes(t *testing.T) {
	now := time.Now()
	seg := []model.SegmentReservation{{
		SegmentID:       "seg_city_north",
		TimeWindowStart: now,
		TimeWindowEnd:   now.Add(time.Hour),
	}}
	for _, vt := range []model.VehicleType{
		model.VehicleTypeCar,
		model.VehicleTypeVan,
		model.VehicleTypeMotorcycle,
		model.VehicleTypeTruck,
	} {
		req := &model.ReserveRequest{
			JourneyID:      "jrn_x",
			IdempotencyKey: "idk_x",
			VehicleType:    vt,
			Reservations:   seg,
		}
		if err := validateReserveRequest(req); err != nil {
			t.Errorf("validateReserveRequest with vehicle_type=%q: unexpected error: %v", vt, err)
		}
	}
}

// ---------------------------------------------------------------------------
// isUniqueViolation
// ---------------------------------------------------------------------------

func TestIsUniqueViolation_Nil(t *testing.T) {
	if isUniqueViolation(nil) {
		t.Error("isUniqueViolation(nil) = true, want false")
	}
}

func TestIsUniqueViolation_UnrelatedError(t *testing.T) {
	err := fmt.Errorf("connection refused")
	if isUniqueViolation(err) {
		t.Error("isUniqueViolation(generic error) = true, want false")
	}
}

// ---------------------------------------------------------------------------
// generateID
// ---------------------------------------------------------------------------

func TestGenerateID_Prefix(t *testing.T) {
	id := generateID("rsv")
	if len(id) < 5 {
		t.Fatalf("generateID produced unexpectedly short ID: %q", id)
	}
	if id[:4] != "rsv_" {
		t.Errorf("generateID(\"rsv\") = %q, want prefix \"rsv_\"", id)
	}
}

func TestGenerateID_Uniqueness(t *testing.T) {
	ids := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id := generateID("rsv")
		if ids[id] {
			t.Fatalf("generateID produced duplicate ID %q after %d calls", id, i)
		}
		ids[id] = true
	}
}

// ---------------------------------------------------------------------------
// availabilityCacheKey
// ---------------------------------------------------------------------------

func TestAvailabilityCacheKey_Format(t *testing.T) {
	start := time.Unix(1713168000, 0).UTC() // fixed timestamp for determinism
	end := time.Unix(1713169800, 0).UTC()
	key := availabilityCacheKey("seg_city_north", start, end)
	want := "cap:avail:seg_city_north:1713168000:1713169800"
	if key != want {
		t.Errorf("availabilityCacheKey = %q, want %q", key, want)
	}
}

func TestAvailabilityCacheKey_DifferentSegments(t *testing.T) {
	start := time.Now()
	end := start.Add(30 * time.Minute)
	k1 := availabilityCacheKey("seg_city_north", start, end)
	k2 := availabilityCacheKey("seg_north_airport", start, end)
	if k1 == k2 {
		t.Error("different segments produced the same cache key")
	}
}

// ---------------------------------------------------------------------------
// roundTwo
// ---------------------------------------------------------------------------

func TestRoundTwo(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{42.555, 42.56},
		{0, 0},
		{100.001, 100.0},
		{33.333, 33.33},
	}
	for _, c := range cases {
		got := roundTwo(c.in)
		if got != c.want {
			t.Errorf("roundTwo(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Slot weight capacity arithmetic (spec-critical)
// ---------------------------------------------------------------------------

// TestCapacityArithmetic verifies that the slot-weight arithmetic correctly
// determines whether a vehicle fits in the remaining capacity.
// This mirrors the core logic in Reserve(): available := maxCapacity - currentlyReserved
func TestCapacityArithmetic_TruckFillsSegment(t *testing.T) {
	maxCapacity := 10.0
	truckSlots := model.VehicleTypeTruck.SlotsNeeded() // 3.0
	// 3 trucks + 1 van (1.5) = 10.5 — should be at capacity
	reservedByThreeTrucks := truckSlots * 3            // 9.0
	available := maxCapacity - reservedByThreeTrucks   // 1.0
	vanSlots := model.VehicleTypeVan.SlotsNeeded()     // 1.5
	if available >= vanSlots {
		t.Errorf("van (%.1f slots) should NOT fit in %.1f remaining slots", vanSlots, available)
	}
}

func TestCapacityArithmetic_MotorcyclesFillBeforeCar(t *testing.T) {
	maxCapacity := 1.0
	// One motorcycle (0.5 slots) reserved; a car (1.0) should not fit; another motorcycle (0.5) should.
	reserved := model.VehicleTypeMotorcycle.SlotsNeeded() // 0.5
	available := maxCapacity - reserved                   // 0.5
	if available >= model.VehicleTypeCar.SlotsNeeded() {
		t.Error("car should not fit when only 0.5 slots remain")
	}
	if available < model.VehicleTypeMotorcycle.SlotsNeeded() {
		t.Error("motorcycle should fit when 0.5 slots remain")
	}
}
