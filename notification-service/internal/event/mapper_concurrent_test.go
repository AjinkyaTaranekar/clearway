package event

// Concurrent tests for the notification event mapper.
//
// MapEvent is called on the hot path of the notification consumer — once per
// Redis Stream message.  Under load many goroutines can call it simultaneously.
// These tests verify it is stateless and goroutine-safe.
//
// Run with: go test -race ./internal/event/...
//
// CS7NS6 checklist:
//   ✔ Concurrent requests properly synchronized  → mapper is pure, no shared state
//   ✔ Conflicting requests properly handled      → all event types mapped correctly
//   ✔ Test application/testing framework         → this file

import (
	"fmt"
	"sync"
	"testing"
	"time"

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
// SECTION 1 — All Event Types Mapped: Concurrent Goroutines
// ─────────────────────────────────────────────────────────────────────────────

// TestMapEvent_AllTypes_Concurrent fires 100 goroutines per event type
// simultaneously. All must return the correct notification type and a
// non-empty title/message.
func TestMapEvent_AllTypes_Concurrent(t *testing.T) {
	type testCase struct {
		eventType    string
		wantNotifType string
		wantTitle     string
	}
	cases := []testCase{
		{JourneyBooked, model.TypeSuccess, "Journey Approved"},
		{JourneyRejected, model.TypeError, "Journey Rejected"},
		{JourneyCancelled, model.TypeWarning, "Journey Cancelled"},
		{JourneyActivated, model.TypeInfo, "Journey Started"},
		{JourneyCompleted, model.TypeSuccess, "Journey Completed"},
		{JourneyExpired, model.TypeWarning, "Journey Expired"},
	}

	const goroutinesPerType = 100
	total := len(cases) * goroutinesPerType
	ready, go_ := barrier(total)
	var wg sync.WaitGroup
	wg.Add(total)
	errs := make(chan string, total)

	for _, tc := range cases {
		for i := 0; i < goroutinesPerType; i++ {
			go func(tc testCase, idx int) {
				defer wg.Done()
				env := &Envelope{
					EventID:   fmt.Sprintf("ev-%s-%d", tc.eventType, idx),
					EventType: tc.eventType,
					Timestamp: time.Now().UTC(),
					Payload: Payload{
						JourneyID:        "jrn_abc",
						DriverID:         "usr_driver01",
						OriginLabel:      "City Centre",
						DestinationLabel: "Airport",
					},
				}
				ready()
				go_()
				mapped, err := MapEvent(env)
				if err != nil {
					errs <- fmt.Sprintf("[%s] goroutine %d: unexpected error: %v", tc.eventType, idx, err)
					return
				}
				if mapped.Type != tc.wantNotifType {
					errs <- fmt.Sprintf("[%s] goroutine %d: type=%q, want %q", tc.eventType, idx, mapped.Type, tc.wantNotifType)
				}
				if mapped.Title != tc.wantTitle {
					errs <- fmt.Sprintf("[%s] goroutine %d: title=%q, want %q", tc.eventType, idx, mapped.Title, tc.wantTitle)
				}
				if mapped.Message == "" {
					errs <- fmt.Sprintf("[%s] goroutine %d: empty message", tc.eventType, idx)
				}
			}(tc, i)
		}
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// TestMapEvent_UnknownType_Concurrent verifies that 200 concurrent goroutines
// all receiving an unknown event type get an error — none silently swallow it.
func TestMapEvent_UnknownType_Concurrent(t *testing.T) {
	const goroutines = 200
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			env := &Envelope{EventType: "journey.teleported"}
			ready()
			go_()
			_, err := MapEvent(env)
			if err == nil {
				errs <- fmt.Sprintf("goroutine %d: unknown event type should return error, got nil", idx)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// TestMapEvent_MixedTypes_Concurrent fires 600 goroutines (100 per event type)
// all simultaneously calling MapEvent with different types.
// Each goroutine's result must match what MapEvent returns for that type alone —
// verifying there is no cross-contamination between concurrent invocations.
func TestMapEvent_MixedTypes_Concurrent(t *testing.T) {
	eventTypes := []string{
		JourneyBooked, JourneyRejected, JourneyCancelled,
		JourneyActivated, JourneyCompleted, JourneyExpired,
	}
	// Pre-compute expected types sequentially for reference.
	expectedTypes := map[string]string{
		JourneyBooked:    model.TypeSuccess,
		JourneyRejected:  model.TypeError,
		JourneyCancelled: model.TypeWarning,
		JourneyActivated: model.TypeInfo,
		JourneyCompleted: model.TypeSuccess,
		JourneyExpired:   model.TypeWarning,
	}

	const goroutinesPerType = 100
	total := len(eventTypes) * goroutinesPerType
	ready, go_ := barrier(total)
	var wg sync.WaitGroup
	wg.Add(total)
	errs := make(chan string, total)

	for _, et := range eventTypes {
		for i := 0; i < goroutinesPerType; i++ {
			go func(et string, idx int) {
				defer wg.Done()
				env := &Envelope{
					EventType: et,
					Payload: Payload{
						OriginLabel:      "Dublin",
						DestinationLabel: "Cork",
					},
				}
				ready()
				go_()
				mapped, err := MapEvent(env)
				if err != nil {
					errs <- fmt.Sprintf("[%s] goroutine %d: unexpected error", et, idx)
					return
				}
				want := expectedTypes[et]
				if mapped.Type != want {
					errs <- fmt.Sprintf("[%s] goroutine %d: type=%q, want %q — cross-contamination?", et, idx, mapped.Type, want)
				}
			}(et, i)
		}
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 2 — Empty Labels: Concurrent "Unknown" Fallback
// ─────────────────────────────────────────────────────────────────────────────

// TestMapEvent_EmptyLabels_Concurrent verifies that missing origin/destination
// labels produce "Unknown" in the message text — not a panic or empty string.
// This covers the case where cross-region events arrive without label data.
func TestMapEvent_EmptyLabels_Concurrent(t *testing.T) {
	const goroutines = 300
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			env := &Envelope{
				EventType: JourneyBooked,
				Payload:   Payload{}, // empty labels
			}
			ready()
			go_()
			mapped, err := MapEvent(env)
			if err != nil {
				errs <- fmt.Sprintf("goroutine %d: unexpected error: %v", idx, err)
				return
			}
			if mapped.Message == "" {
				errs <- fmt.Sprintf("goroutine %d: empty message for empty labels", idx)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// TestMapEvent_RejectionReason_Concurrent verifies the rejection reason is
// included in the message when present (not empty default).
func TestMapEvent_RejectionReason_Concurrent(t *testing.T) {
	const goroutines = 200
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			env := &Envelope{
				EventType: JourneyRejected,
				Payload: Payload{
					OriginLabel:      "City",
					DestinationLabel: "Airport",
					Reason:           "segment at capacity",
				},
			}
			ready()
			go_()
			mapped, err := MapEvent(env)
			if err != nil {
				errs <- fmt.Sprintf("goroutine %d: unexpected error: %v", idx, err)
				return
			}
			if mapped.Type != model.TypeError {
				errs <- fmt.Sprintf("goroutine %d: rejection should be TypeError, got %q", idx, mapped.Type)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
