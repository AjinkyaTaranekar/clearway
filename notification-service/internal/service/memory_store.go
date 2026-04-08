package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/model"
	"github.com/google/uuid"
)

// MemoryNotificationRepo is an in-memory implementation of NotificationRepository.
// Replace with PostgreSQL implementation later.
type MemoryNotificationRepo struct {
	mu      sync.RWMutex
	records []model.Notification
	events  map[string]bool // event_id dedup set
}

func NewMemoryNotificationRepo() *MemoryNotificationRepo {
	return &MemoryNotificationRepo{
		records: make([]model.Notification, 0),
		events:  make(map[string]bool),
	}
}

func (m *MemoryNotificationRepo) Insert(_ context.Context, n *model.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.events[n.EventID] {
		return fmt.Errorf("duplicate event_id: %s", n.EventID)
	}

	if n.ID == "" {
		n.ID = "ntf_" + uuid.New().String()[:8]
	}
	now := time.Now().UTC()
	n.CreatedAt = now
	n.UpdatedAt = now
	m.events[n.EventID] = true
	m.records = append(m.records, *n)
	return nil
}

func (m *MemoryNotificationRepo) ListByDriver(_ context.Context, f model.NotificationFilter) ([]model.Notification, int, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var filtered []model.Notification
	unread := 0
	for _, n := range m.records {
		if n.DriverID != f.DriverID {
			continue
		}
		if f.ReadFilter != nil && n.IsRead != *f.ReadFilter {
			continue
		}
		if f.TypeFilter != "" && n.Type != f.TypeFilter {
			continue
		}
		if !n.IsRead {
			unread++
		}
		filtered = append(filtered, n)
	}

	total := len(filtered)

	// pagination
	start := (f.Page - 1) * f.Limit
	if start > total {
		start = total
	}
	end := start + f.Limit
	if end > total {
		end = total
	}

	return filtered[start:end], total, unread, nil
}

func (m *MemoryNotificationRepo) ListAll(_ context.Context, f model.NotificationFilter) ([]model.Notification, int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var filtered []model.Notification
	for _, n := range m.records {
		if f.DriverID != "" && n.DriverID != f.DriverID {
			continue
		}
		if f.TypeFilter != "" && n.Type != f.TypeFilter {
			continue
		}
		if f.DeliveryStatus != "" && n.DeliveryStatus != f.DeliveryStatus {
			continue
		}
		filtered = append(filtered, n)
	}

	total := len(filtered)
	start := (f.Page - 1) * f.Limit
	if start > total {
		start = total
	}
	end := start + f.Limit
	if end > total {
		end = total
	}

	return filtered[start:end], total, nil
}

func (m *MemoryNotificationRepo) MarkRead(_ context.Context, notificationID, driverID string) (*model.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.records {
		if m.records[i].ID == notificationID && m.records[i].DriverID == driverID {
			now := time.Now().UTC()
			m.records[i].IsRead = true
			m.records[i].ReadAt = &now
			m.records[i].UpdatedAt = now
			return &m.records[i], nil
		}
	}
	return nil, nil
}

func (m *MemoryNotificationRepo) MarkAllRead(_ context.Context, driverID string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	now := time.Now().UTC()
	for i := range m.records {
		if m.records[i].DriverID == driverID && !m.records[i].IsRead {
			m.records[i].IsRead = true
			m.records[i].ReadAt = &now
			m.records[i].UpdatedAt = now
			count++
		}
	}
	return count, nil
}

// MemoryDeviceTokenRepo is an in-memory implementation of DeviceTokenRepository.
type MemoryDeviceTokenRepo struct {
	mu      sync.RWMutex
	records []model.DeviceToken
}

func NewMemoryDeviceTokenRepo() *MemoryDeviceTokenRepo {
	return &MemoryDeviceTokenRepo{
		records: make([]model.DeviceToken, 0),
	}
}

func (m *MemoryDeviceTokenRepo) Upsert(_ context.Context, t *model.DeviceToken) (*model.DeviceToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()

	// Check for existing active token for this driver+fcm combo
	for i := range m.records {
		if m.records[i].DriverID == t.DriverID && m.records[i].FCMToken == t.FCMToken && m.records[i].IsActive {
			m.records[i].Platform = t.Platform
			m.records[i].LastSeenAt = now
			m.records[i].UpdatedAt = now
			return &m.records[i], nil
		}
	}

	if t.ID == "" {
		t.ID = "dvt_" + uuid.New().String()[:8]
	}
	t.IsActive = true
	t.CreatedAt = now
	t.UpdatedAt = now
	t.LastSeenAt = now
	m.records = append(m.records, *t)
	return t, nil
}

func (m *MemoryDeviceTokenRepo) FindActiveByDriver(_ context.Context, driverID string) ([]model.DeviceToken, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []model.DeviceToken
	for _, t := range m.records {
		if t.DriverID == driverID && t.IsActive {
			result = append(result, t)
		}
	}
	return result, nil
}

func (m *MemoryDeviceTokenRepo) Deactivate(_ context.Context, tokenID, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	for i := range m.records {
		if m.records[i].ID == tokenID {
			m.records[i].IsActive = false
			m.records[i].InvalidatedAt = &now
			m.records[i].InvalidationReason = reason
			m.records[i].UpdatedAt = now
			return nil
		}
	}
	return nil
}

func (m *MemoryDeviceTokenRepo) DeactivateByDriverAndFCMToken(_ context.Context, driverID, fcmToken, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	for i := range m.records {
		if m.records[i].DriverID == driverID && m.records[i].FCMToken == fcmToken && m.records[i].IsActive {
			m.records[i].IsActive = false
			m.records[i].InvalidatedAt = &now
			m.records[i].InvalidationReason = reason
			m.records[i].UpdatedAt = now
		}
	}
	return nil
}
