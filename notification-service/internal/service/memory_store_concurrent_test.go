package service

// Concurrent tests for MemoryNotificationRepo and MemoryDeviceTokenRepo.
//
// These are the in-memory implementations used in tests and local dev.
// They use sync.RWMutex internally. These tests verify correct behaviour under
// concurrent reads and writes — including the critical dedup guarantee that
// prevents the same journey event from creating two notifications.
//
// Run with: go test -race ./internal/service/...
//
// CS7NS6 checklist:
//   ✔ Concurrent requests properly synchronized  → mutex protection
//   ✔ Double spending possible? (NO)             → Insert dedup
//   ✔ Consistency of data                        → MarkAllRead under concurrency

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/AjinkyaTaranekar/distributed-vehicle-capacity-system/notification-service/internal/model"
)

func barrier(n int) (ready func(), go_ func()) {
	var wg sync.WaitGroup
	wg.Add(n)
	ready = func() { wg.Done() }
	go_ = func() { wg.Wait() }
	return
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 1 — Insert Deduplication Under Concurrency
//
// The most important invariant: the same event_id must never produce two
// notification rows.  This is the notification-service equivalent of the
// capacity service's idempotency_cache.
// ─────────────────────────────────────────────────────────────────────────────

// TestMemoryRepo_ConcurrentInsert_SameEventID_OnlyOneSucceeds fires 50
// goroutines all simultaneously trying to insert a notification with the
// same event_id.  Exactly 1 must succeed; the other 49 must get an error.
func TestMemoryRepo_ConcurrentInsert_SameEventID_OnlyOneSucceeds(t *testing.T) {
	const goroutines = 50
	repo := NewMemoryNotificationRepo()
	ctx := context.Background()

	var successes, failures int64
	var mu sync.Mutex

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			n := &model.Notification{
				ID:       fmt.Sprintf("ntf_%03d", idx),
				EventID:  "ev-duplicate-001", // same event_id for all goroutines
				DriverID: "usr_driver01",
				Title:    "Journey Approved",
				Type:     model.TypeSuccess,
			}
			ready()
			go_()
			err := repo.Insert(ctx, n)
			mu.Lock()
			if err == nil {
				successes++
			} else {
				failures++
			}
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	if successes != 1 {
		t.Errorf("expected exactly 1 insert success for duplicate event_id, got %d", successes)
	}
	if failures != goroutines-1 {
		t.Errorf("expected %d duplicate-rejection failures, got %d", goroutines-1, failures)
	}
}

// TestMemoryRepo_ConcurrentInsert_UniqueEventIDs_AllSucceed verifies that
// 200 goroutines inserting notifications with DIFFERENT event_ids all succeed.
func TestMemoryRepo_ConcurrentInsert_UniqueEventIDs_AllSucceed(t *testing.T) {
	const goroutines = 200
	repo := NewMemoryNotificationRepo()
	ctx := context.Background()

	errs := make(chan string, goroutines)
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			n := &model.Notification{
				EventID:  fmt.Sprintf("ev-unique-%04d", idx),
				DriverID: fmt.Sprintf("usr_driver%03d", idx),
				Title:    "Journey Approved",
				Type:     model.TypeSuccess,
			}
			ready()
			go_()
			if err := repo.Insert(ctx, n); err != nil {
				errs <- fmt.Sprintf("goroutine %d: unexpected error inserting unique event: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 2 — Concurrent Reads While Writing
//
// Checklist: "Consistency of data maintained across failures/recoveries □"
// ─────────────────────────────────────────────────────────────────────────────

// TestMemoryRepo_ConcurrentReadWrite_NoDeadlock fires 100 writer goroutines
// and 100 reader goroutines simultaneously.  The test verifies no deadlock
// occurs and all reads return without error.
func TestMemoryRepo_ConcurrentReadWrite_NoDeadlock(t *testing.T) {
	const writers = 100
	const readers = 100
	repo := NewMemoryNotificationRepo()
	ctx := context.Background()
	driverID := "usr_driver_shared"

	// Pre-populate one notification for the driver so readers have something to find.
	_ = repo.Insert(ctx, &model.Notification{
		EventID:  "ev-seed",
		DriverID: driverID,
		Title:    "Seed",
		Type:     model.TypeInfo,
	})

	total := writers + readers
	ready, go_ := barrier(total)
	var wg sync.WaitGroup
	wg.Add(total)
	errs := make(chan string, total)

	for i := 0; i < writers; i++ {
		go func(idx int) {
			defer wg.Done()
			n := &model.Notification{
				EventID:  fmt.Sprintf("ev-write-%04d", idx),
				DriverID: driverID,
				Title:    "Test",
				Type:     model.TypeInfo,
			}
			ready()
			go_()
			if err := repo.Insert(ctx, n); err != nil {
				errs <- fmt.Sprintf("writer %d: insert error: %v", idx, err)
			}
		}(i)
	}
	for i := 0; i < readers; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			_, _, _, err := repo.ListByDriver(ctx, model.NotificationFilter{
				DriverID: driverID,
				Page:     1,
				Limit:    10,
			})
			if err != nil {
				errs <- fmt.Sprintf("reader %d: list error: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 3 — MarkAllRead: Concurrent Calls on Same Driver
//
// Multiple goroutines (e.g. two browser tabs) marking all notifications as
// read simultaneously must be idempotent — all calls succeed, and the final
// unread count is 0.
// ─────────────────────────────────────────────────────────────────────────────

// TestMemoryRepo_ConcurrentMarkAllRead_Idempotent inserts 20 unread notifications
// then fires 50 goroutines simultaneously calling MarkAllRead for the same driver.
// All must succeed; unread count after must be 0.
func TestMemoryRepo_ConcurrentMarkAllRead_Idempotent(t *testing.T) {
	const unreadCount = 20
	const goroutines = 50
	repo := NewMemoryNotificationRepo()
	ctx := context.Background()
	driverID := "usr_driver_reads"

	// Insert unread notifications.
	for i := 0; i < unreadCount; i++ {
		_ = repo.Insert(ctx, &model.Notification{
			EventID:  fmt.Sprintf("ev-unread-%04d", i),
			DriverID: driverID,
			Title:    "Test",
			Type:     model.TypeInfo,
		})
	}

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			_, err := repo.MarkAllRead(ctx, driverID)
			if err != nil {
				errs <- fmt.Sprintf("goroutine %d: MarkAllRead error: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}

	// Verify final state: unread count must be 0.
	_, _, unread, _ := repo.ListByDriver(ctx, model.NotificationFilter{
		DriverID: driverID,
		Page:     1,
		Limit:    100,
	})
	if unread != 0 {
		t.Errorf("after concurrent MarkAllRead: unread=%d, want 0", unread)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 4 — Device Token Upsert: Concurrent Registration from Same Driver
//
// A driver may open the app on two devices simultaneously.  Both register
// their FCM tokens at the same time.  Both must succeed, and both tokens must
// be stored as separate active tokens.
// ─────────────────────────────────────────────────────────────────────────────

// TestMemoryDeviceTokenRepo_ConcurrentUpsert_TwoDevices registers two
// different FCM tokens for the same driver concurrently.  Both must be
// stored and retrievable.
func TestMemoryDeviceTokenRepo_ConcurrentUpsert_TwoDevices(t *testing.T) {
	repo := NewMemoryDeviceTokenRepo()
	ctx := context.Background()
	driverID := "usr_driver_multidevice"

	ready, go_ := barrier(2)
	var wg sync.WaitGroup
	wg.Add(2)
	errs := make(chan string, 2)

	tokens := []string{"fcm_token_phone", "fcm_token_tablet"}
	for _, tok := range tokens {
		go func(fcmToken string) {
			defer wg.Done()
			ready()
			go_()
			_, err := repo.Upsert(ctx, &model.DeviceToken{
				DriverID: driverID,
				FCMToken: fcmToken,
				Platform: "android",
			})
			if err != nil {
				errs <- fmt.Sprintf("upsert %q: error %v", fcmToken, err)
			}
		}(tok)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}

	active, _ := repo.FindActiveByDriver(ctx, driverID)
	if len(active) != 2 {
		t.Errorf("expected 2 active tokens for dual-device driver, got %d", len(active))
	}
}

// TestMemoryDeviceTokenRepo_ConcurrentUpsert_SameToken_Idempotent fires 50
// goroutines all upserting the SAME FCM token for the same driver.
// The result must be exactly 1 active token (upsert is idempotent).
func TestMemoryDeviceTokenRepo_ConcurrentUpsert_SameToken_Idempotent(t *testing.T) {
	const goroutines = 50
	repo := NewMemoryDeviceTokenRepo()
	ctx := context.Background()
	driverID := "usr_driver_repeated"

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			_, err := repo.Upsert(ctx, &model.DeviceToken{
				DriverID: driverID,
				FCMToken: "fcm_same_token",
				Platform: "ios",
			})
			if err != nil {
				errs <- fmt.Sprintf("goroutine %d: upsert error: %v", idx, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}

	active, _ := repo.FindActiveByDriver(ctx, driverID)
	if len(active) != 1 {
		t.Errorf("expected exactly 1 active token after concurrent upsert of same token, got %d", len(active))
	}
}

// TestMemoryDeviceTokenRepo_ConcurrentDeactivate_ThenFind fires 100 goroutines
// concurrently deactivating a token while 100 goroutines simultaneously call
// FindActiveByDriver.  The test verifies no panic or deadlock.
func TestMemoryDeviceTokenRepo_ConcurrentDeactivate_ThenFind(t *testing.T) {
	repo := NewMemoryDeviceTokenRepo()
	ctx := context.Background()
	driverID := "usr_driver_deactivate"

	// Register a token to deactivate.
	tok, _ := repo.Upsert(ctx, &model.DeviceToken{
		DriverID: driverID,
		FCMToken: "fcm_to_deactivate",
		Platform: "android",
	})

	const goroutines = 200 // 100 writers + 100 readers
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			if idx%2 == 0 {
				if err := repo.Deactivate(ctx, tok.ID, "stale_token"); err != nil {
					errs <- fmt.Sprintf("goroutine %d: deactivate error: %v", idx, err)
				}
			} else {
				if _, err := repo.FindActiveByDriver(ctx, driverID); err != nil {
					errs <- fmt.Sprintf("goroutine %d: find error: %v", idx, err)
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
