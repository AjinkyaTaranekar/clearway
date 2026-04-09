package service

import (
	"context"
	"fmt"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/model"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/internal/repository"
	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/capacity-service/pkg/logger"
)

// ClosureService manages the segment closure lifecycle.
type ClosureService struct {
	closureRepo *repository.ClosureRepo
	log         *logger.Logger
}

// NewClosureService creates a new ClosureService.
func NewClosureService(closureRepo *repository.ClosureRepo, log *logger.Logger) *ClosureService {
	return &ClosureService{closureRepo: closureRepo, log: log}
}

// ListClosures returns all closures (active first, then by creation time desc).
func (s *ClosureService) ListClosures(ctx context.Context) ([]model.Closure, error) {
	closures, err := s.closureRepo.ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("list closures: %w", err)
	}
	if closures == nil {
		closures = []model.Closure{}
	}
	return closures, nil
}

// CreateClosure validates and persists a new segment closure.
func (s *ClosureService) CreateClosure(ctx context.Context, req *model.CreateClosureRequest) (*model.Closure, error) {
	if req.SegmentID == "" {
		return nil, fmt.Errorf("segment_id is required")
	}
	if req.Reason == "" {
		return nil, fmt.Errorf("reason is required")
	}
	if req.StartsAt.IsZero() {
		return nil, fmt.Errorf("starts_at is required")
	}
	if req.EndsAt != nil && !req.EndsAt.After(req.StartsAt) {
		return nil, fmt.Errorf("ends_at must be after starts_at")
	}

	closure := &model.Closure{
		ClosureID: generateID("cls"),
		SegmentID: req.SegmentID,
		Reason:    req.Reason,
		StartsAt:  req.StartsAt.UTC(),
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
		CreatedBy: "admin",
	}
	if req.EndsAt != nil {
		t := req.EndsAt.UTC()
		closure.EndsAt = &t
	}

	if err := s.closureRepo.Insert(ctx, closure); err != nil {
		return nil, fmt.Errorf("insert closure: %w", err)
	}
	return closure, nil
}
