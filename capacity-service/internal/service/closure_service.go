package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
)

var (
	// ErrClosureOverlap indicates the segment already has an active overlapping closure.
	ErrClosureOverlap = errors.New("segment already has an active overlapping closure")
	// ErrUnknownSegment indicates the requested segment ID does not exist.
	ErrUnknownSegment = errors.New("segment not found")
)

const maxClosureDurationMinutes = 7 * 24 * 60

// CreateClosureInput is the admin request payload for creating a closure.
type CreateClosureInput struct {
	SegmentID       string
	DurationMinutes int
	Reason          string
	AdminID         string
}

// CreateClosure creates a new active closure starting now for the requested duration.
func (s *ReservationService) CreateClosure(ctx context.Context, input CreateClosureInput) (*model.SegmentClosure, error) {
	if err := validateCreateClosureInput(input); err != nil {
		return nil, err
	}
	if s.closureRepo == nil {
		return nil, fmt.Errorf("closure repository is not configured")
	}

	now := time.Now().UTC()
	closure := &model.SegmentClosure{
		ClosureID: generateID("cls"),
		SegmentID: input.SegmentID,
		StartTime: now,
		EndTime:   now.Add(time.Duration(input.DurationMinutes) * time.Minute),
		Reason:    strings.TrimSpace(input.Reason),
		AdminID:   input.AdminID,
		Status:    "active",
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := s.segmentRepo.LockForUpdate(ctx, tx, closure.SegmentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUnknownSegment
		}
		return nil, fmt.Errorf("lock segment %s: %w", closure.SegmentID, err)
	}

	overlap, err := s.closureRepo.FindActiveOverlapTx(ctx, tx, closure.SegmentID, closure.StartTime, closure.EndTime)
	if err != nil {
		return nil, err
	}
	if overlap != nil {
		return nil, ErrClosureOverlap
	}

	if err := s.closureRepo.InsertTx(ctx, tx, closure); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit closure tx: %w", err)
	}

	s.invalidateAvailabilityCacheBySegment(ctx, closure.SegmentID)
	return closure, nil
}

// ListActiveClosures returns active closures that have not yet ended.
func (s *ReservationService) ListActiveClosures(ctx context.Context) ([]model.SegmentClosure, error) {
	if s.closureRepo == nil {
		return nil, fmt.Errorf("closure repository is not configured")
	}
	return s.closureRepo.ListActive(ctx)
}

func validateCreateClosureInput(input CreateClosureInput) error {
	if strings.TrimSpace(input.SegmentID) == "" {
		return fmt.Errorf("segment_id is required")
	}
	if strings.TrimSpace(input.AdminID) == "" {
		return fmt.Errorf("admin identity is required")
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return fmt.Errorf("reason is required")
	}
	if input.DurationMinutes <= 0 {
		return fmt.Errorf("duration_minutes must be greater than zero")
	}
	if input.DurationMinutes > maxClosureDurationMinutes {
		return fmt.Errorf("duration_minutes must be less than or equal to %d", maxClosureDurationMinutes)
	}
	return nil
}
