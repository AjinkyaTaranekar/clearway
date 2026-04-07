package service

import (
	"context"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/journey-service/pkg/logger"
)

func (s *JourneyService) logWithTrace(ctx context.Context) *logger.Logger {
	if log := logger.FromContext(ctx); log != nil {
		return log
	}
	if s.log != nil {
		return s.log
	}
	return logger.Global()
}
