package model

import "time"

// Closure represents a planned or active road segment closure.
type Closure struct {
	ClosureID   string     `json:"closure_id"`
	SegmentID   string     `json:"segment_id"`
	SegmentName string     `json:"segment_name"`
	Reason      string     `json:"reason"`
	StartsAt    time.Time  `json:"starts_at"`
	EndsAt      *time.Time `json:"ends_at,omitempty"`
	IsActive    bool       `json:"is_active"`
	CreatedAt   time.Time  `json:"created_at"`
	CreatedBy   string     `json:"created_by"`
}

// CreateClosureRequest is the body for POST /api/v1/capacity/closures.
type CreateClosureRequest struct {
	SegmentID string     `json:"segment_id"`
	Reason    string     `json:"reason"`
	StartsAt  time.Time  `json:"starts_at"`
	EndsAt    *time.Time `json:"ends_at,omitempty"`
}
