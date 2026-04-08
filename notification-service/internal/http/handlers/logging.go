package handlers

import (
	"context"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/pkg/logger"
)

func logWithTrace(ctx context.Context) *logger.Logger {
	return logger.FromContext(ctx)
}
