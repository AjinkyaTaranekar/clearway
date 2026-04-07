package model_test

import (
	"testing"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
)

// TestSlotWeights verifies the vehicle-type capacity weights defined in the spec:
// car=1.0, van=1.5, motorcycle=0.5, truck=3.0
func TestSlotWeights(t *testing.T) {
	cases := []struct {
		vt   model.VehicleType
		want float64
	}{
		{model.VehicleTypeCar, 1.0},
		{model.VehicleTypeVan, 1.5},
		{model.VehicleTypeMotorcycle, 0.5},
		{model.VehicleTypeTruck, 3.0},
	}
	for _, c := range cases {
		got := c.vt.SlotsNeeded()
		if got != c.want {
			t.Errorf("VehicleType(%q).SlotsNeeded() = %v, want %v", c.vt, got, c.want)
		}
	}
}

func TestVehicleTypeIsValid(t *testing.T) {
	valid := []model.VehicleType{
		model.VehicleTypeCar,
		model.VehicleTypeVan,
		model.VehicleTypeMotorcycle,
		model.VehicleTypeTruck,
	}
	for _, vt := range valid {
		if !vt.IsValid() {
			t.Errorf("VehicleType(%q).IsValid() = false, want true", vt)
		}
	}

	invalid := []model.VehicleType{"bus", "HGV", "", "Car", "TRUCK"}
	for _, vt := range invalid {
		if vt.IsValid() {
			t.Errorf("VehicleType(%q).IsValid() = true, want false", vt)
		}
	}
}

// TestSlotWeightOrdering verifies that truck > van > car > motorcycle,
// as required by the spec's capacity enforcement logic.
func TestSlotWeightOrdering(t *testing.T) {
	if model.VehicleTypeTruck.SlotsNeeded() <= model.VehicleTypeVan.SlotsNeeded() {
		t.Error("truck should consume more slots than van")
	}
	if model.VehicleTypeVan.SlotsNeeded() <= model.VehicleTypeCar.SlotsNeeded() {
		t.Error("van should consume more slots than car")
	}
	if model.VehicleTypeCar.SlotsNeeded() <= model.VehicleTypeMotorcycle.SlotsNeeded() {
		t.Error("car should consume more slots than motorcycle")
	}
}
