package model

import "time"

// SegmentClosure represents an admin-created temporary closure for a road segment.
type SegmentClosure struct {
	ID        int64     `json:"id"`
	ClosureID string    `json:"closure_id"`
	SegmentID string    `json:"segment_id"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Reason    string    `json:"reason"`
	AdminID   string    `json:"admin_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
