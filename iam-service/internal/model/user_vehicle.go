package model

import "time"

// UserVehicle is a driver-owned vehicle available for journey booking.
type UserVehicle struct {
	ID                 string      `json:"id"`
	VehicleType        VehicleType `json:"vehicle_type"`
	LicenseInfo        LicenseInfo `json:"license_info"`
	IsEmergencyVehicle bool        `json:"is_emergency_vehicle"`
	IsPrimary          bool        `json:"is_primary"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}
