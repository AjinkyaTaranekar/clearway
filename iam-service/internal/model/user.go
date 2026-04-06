package model

import (
	"encoding/json"
	"time"
)

type Role string

const (
	RoleDriver Role = "driver"
	RoleAdmin  Role = "admin"
)

type VehicleType string

const (
	VehicleCar        VehicleType = "car"
	VehicleVan        VehicleType = "van"
	VehicleMotorcycle VehicleType = "motorcycle"
	VehicleTruck      VehicleType = "truck"
)

var ValidVehicleTypes = map[VehicleType]bool{
	VehicleCar: true, VehicleVan: true, VehicleMotorcycle: true, VehicleTruck: true,
}

type LicenseInfo struct {
	LicenseNumber       string `json:"license_number"`
	ExpiryDate          string `json:"expiry_date,omitempty"`
	Class               string `json:"class,omitempty"`
	IssuingJurisdiction string `json:"issuing_jurisdiction,omitempty"`
}

type User struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Email       string      `json:"email"`
	EmailLower  string      `json:"-"`
	Role        Role        `json:"role"`
	VehicleType VehicleType `json:"vehicle_type"`
	LicenseInfo LicenseInfo `json:"license_info"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type RefreshToken struct {
	ID        int64
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
	UserAgent string
	IPAddress string
}

func LicenseInfoFromJSON(data []byte) (LicenseInfo, error) {
	var li LicenseInfo
	if len(data) == 0 || string(data) == "null" || string(data) == "{}" {
		return li, nil
	}
	err := json.Unmarshal(data, &li)
	return li, err
}
