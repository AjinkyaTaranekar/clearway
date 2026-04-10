package model

import "time"

// Segment represents a road segment with a maximum vehicle capacity.
type Segment struct {
	SegmentID   string    `json:"segment_id"`
	SegmentName string    `json:"segment_name"`
	Region      string    `json:"region"`
	CRDBRegion  string    `json:"crdb_region"` // geo-partition key: eu | us | apac
	MaxCapacity float64   `json:"max_capacity"`
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
