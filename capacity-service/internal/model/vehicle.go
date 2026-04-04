package model

// VehicleType represents the type of vehicle making a reservation.
type VehicleType string

const (
	VehicleTypeCar        VehicleType = "car"
	VehicleTypeVan        VehicleType = "van"
	VehicleTypeMotorcycle VehicleType = "motorcycle"
	VehicleTypeTruck      VehicleType = "truck"
)

// SlotWeights maps each vehicle type to the number of capacity slots it consumes.
var SlotWeights = map[VehicleType]float64{
	VehicleTypeCar:        1.0,
	VehicleTypeVan:        1.5,
	VehicleTypeMotorcycle: 0.5,
	VehicleTypeTruck:      3.0,
}

// IsValid returns true if the vehicle type is recognised.
func (vt VehicleType) IsValid() bool {
	_, ok := SlotWeights[vt]
	return ok
}

// SlotsNeeded returns the number of capacity slots consumed by this vehicle type.
func (vt VehicleType) SlotsNeeded() float64 {
	return SlotWeights[vt]
}
