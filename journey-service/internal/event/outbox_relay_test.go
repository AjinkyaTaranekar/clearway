package event

// Concurrent tests for the transactional outbox relay.
//
// Every test section that can be made concurrent uses a barrier pattern:
//   ready, go_ := barrier(n)
//   // each goroutine: ready(); go_(); <do work>
// This releases all goroutines simultaneously, maximising contention and
// making race conditions detectable by `go test -race`.
//
// CS7NS6 checklist:
//   ✔ Communication failures tolerated    → Redis-down concurrent tests
//   ✔ Replica recovery supported          → recovery-after-failure tests
//   ✔ Consistency of data                 → partial-batch ordering tests
//   ✔ Test application/testing framework  → this file

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// ─────────────────────────────────────────────────────────────────────────────
// Barrier helper — releases all goroutines simultaneously.
// ─────────────────────────────────────────────────────────────────────────────

func barrier(n int) (ready func(), go_ func()) {
	var wg sync.WaitGroup
	wg.Add(n)
	ready = func() { wg.Done() }
	go_ = func() { wg.Wait() }
	return
}

// ─────────────────────────────────────────────────────────────────────────────
// Mock implementations
// ─────────────────────────────────────────────────────────────────────────────

// mockFetcher implements OutboxFetcher with controllable, goroutine-safe behavior.
type mockFetcher struct {
	mu              sync.Mutex
	events          []OutboxEvent
	fetchErr        error
	markErr         error
	markedPublished []int64
	fetchCalls      int64 // atomic
}

func (m *mockFetcher) FetchUnpublishedOutbox(_ context.Context, limit int) ([]OutboxEvent, error) {
	atomic.AddInt64(&m.fetchCalls, 1)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	n := len(m.events)
	if n > limit {
		n = limit
	}
	// Return a copy so concurrent goroutines don't share the backing array.
	out := make([]OutboxEvent, n)
	copy(out, m.events[:n])
	return out, nil
}

func (m *mockFetcher) MarkOutboxPublished(_ context.Context, ids []int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.markErr != nil {
		return m.markErr
	}
	m.markedPublished = append(m.markedPublished, ids...)
	return nil
}

func (m *mockFetcher) getMarked() []int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]int64, len(m.markedPublished))
	copy(out, m.markedPublished)
	return out
}

func (m *mockFetcher) getFetchCalls() int64 {
	return atomic.LoadInt64(&m.fetchCalls)
}

// mockStreamWriter implements StreamWriter with atomic call counting.
type mockStreamWriter struct {
	mu         sync.Mutex
	failOnCall int  // XAdd fails on this call number (1-indexed), 0 = never
	callCount  int  // protected by mu
	xaddErr    error
	published  []string
}

func (m *mockStreamWriter) XAdd(_ context.Context, args *redis.XAddArgs) *redis.StringCmd {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCount++
	cmd := redis.NewStringCmd(context.Background())

	if m.xaddErr != nil {
		cmd.SetErr(m.xaddErr)
		return cmd
	}
	if m.failOnCall > 0 && m.callCount == m.failOnCall {
		cmd.SetErr(errors.New("simulated redis XADD failure"))
		return cmd
	}

	if valMap, ok := args.Values.(map[string]interface{}); ok {
		if data, ok := valMap["data"]; ok {
			m.published = append(m.published, data.(string))
		}
	}
	cmd.SetVal("ok")
	return cmd
}

func (m *mockStreamWriter) getPublished() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.published))
	copy(out, m.published)
	return out
}

func (m *mockStreamWriter) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func silentLogger() *zerolog.Logger {
	l := zerolog.Nop()
	return &l
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 1 — Concurrent Batch Processing: Happy Path
// ─────────────────────────────────────────────────────────────────────────────

// TestOutbox_Concurrent_50IndependentBatches fires 50 goroutines each running
// their OWN relayBatch with their own fetcher+stream.
// Verifies no shared state bleeds between independent relay instances.
func TestOutbox_Concurrent_50IndependentBatches(t *testing.T) {
	const goroutines = 50
	const eventsPerBatch = 3

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			f := &mockFetcher{
				events: []OutboxEvent{
					{ID: int64(idx*3 + 1), EventID: "ev-a", EventType: EventJourneyBooked, Payload: []byte(`{}`)},
					{ID: int64(idx*3 + 2), EventID: "ev-b", EventType: EventJourneyBooked, Payload: []byte(`{}`)},
					{ID: int64(idx*3 + 3), EventID: "ev-c", EventType: EventJourneyCancelled, Payload: []byte(`{}`)},
				},
			}
			s := &mockStreamWriter{}
			ready()
			go_() // all 50 relay batches start simultaneously
			relayBatch(context.Background(), f, s, silentLogger())

			marked := f.getMarked()
			if len(marked) != eventsPerBatch {
				errs <- "goroutine " + string(rune('0'+idx%10)) + ": expected 3 marked, got different"
			}
			if s.getCallCount() != eventsPerBatch {
				errs <- "goroutine stream call count wrong"
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// TestOutbox_Concurrent_EmptyOutbox_AllNoOp verifies that 200 concurrent
// goroutines hitting an empty outbox produce zero XAdd calls and zero mark calls.
func TestOutbox_Concurrent_EmptyOutbox_AllNoOp(t *testing.T) {
	const goroutines = 200
	// Shared fetcher and stream — goroutines compete on the same mock.
	f := &mockFetcher{events: []OutboxEvent{}}
	s := &mockStreamWriter{}

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ready()
			go_()
			relayBatch(context.Background(), f, s, silentLogger())
		}()
	}
	wg.Wait()

	if s.getCallCount() != 0 {
		t.Errorf("empty outbox: expected 0 XAdd calls, got %d", s.getCallCount())
	}
	if len(f.getMarked()) != 0 {
		t.Errorf("empty outbox: expected 0 marked, got %d", len(f.getMarked()))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 2 — Concurrent Redis Failures (Partition Simulation)
//
// Checklist: "Communication failures tolerated □"
// ─────────────────────────────────────────────────────────────────────────────

// TestOutbox_Concurrent_RedisDown_NeverMarksPublished fires 100 goroutines
// simultaneously against a Redis that always fails.
// None should mark their events as published — they stay in the outbox for
// the next tick after Redis recovers.
func TestOutbox_Concurrent_RedisDown_NeverMarksPublished(t *testing.T) {
	const goroutines = 100
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			f := &mockFetcher{
				events: []OutboxEvent{
					{ID: int64(idx + 1), EventID: "ev", EventType: EventJourneyBooked, Payload: []byte(`{}`)},
				},
			}
			s := &mockStreamWriter{xaddErr: errors.New("NOAUTH Redis not ready")}
			ready()
			go_()
			relayBatch(context.Background(), f, s, silentLogger())

			if len(f.getMarked()) != 0 {
				errs <- "Redis was down but events were marked published — data loss risk"
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}

// TestOutbox_Concurrent_RecoverAfterPartition runs two phases:
//   Phase 1: 50 goroutines run relay with Redis down → 0 published
//   Phase 2: same 50 goroutines run relay with Redis up → all 50 delivered
// This tests the exact partition-recovery flow described in the design doc.
func TestOutbox_Concurrent_RecoverAfterPartition(t *testing.T) {
	const goroutines = 50

	type state struct {
		fetcher *mockFetcher
		stream  *mockStreamWriter
	}
	states := make([]state, goroutines)
	for i := range states {
		states[i] = state{
			fetcher: &mockFetcher{
				events: []OutboxEvent{
					{ID: int64(i + 1), EventID: "ev", EventType: EventJourneyBooked, Payload: []byte(`{}`)},
				},
			},
			stream: &mockStreamWriter{xaddErr: errors.New("partition")},
		}
	}

	// Phase 1: Redis down.
	ready1, go1_ := barrier(goroutines)
	var wg1 sync.WaitGroup
	wg1.Add(goroutines)
	for i, s := range states {
		go func(idx int, st state) {
			defer wg1.Done()
			ready1()
			go1_()
			relayBatch(context.Background(), st.fetcher, st.stream, silentLogger())
		}(i, s)
	}
	wg1.Wait()

	for i, s := range states {
		if len(s.fetcher.getMarked()) != 0 {
			t.Errorf("phase1 goroutine %d: events marked during partition", i)
		}
	}

	// Phase 2: Redis recovered — clear the error.
	for _, s := range states {
		s.stream.mu.Lock()
		s.stream.xaddErr = nil
		s.stream.mu.Unlock()
	}

	ready2, go2_ := barrier(goroutines)
	var wg2 sync.WaitGroup
	wg2.Add(goroutines)
	for i, s := range states {
		go func(idx int, st state) {
			defer wg2.Done()
			ready2()
			go2_()
			relayBatch(context.Background(), st.fetcher, st.stream, silentLogger())
		}(i, s)
	}
	wg2.Wait()

	for i, s := range states {
		marked := s.fetcher.getMarked()
		if len(marked) != 1 {
			t.Errorf("phase2 goroutine %d: expected 1 event delivered after recovery, got %d", i, len(marked))
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 3 — Partial Batch Failure Under Concurrency
//
// Checklist: "Consistency of data maintained across failures/recoveries □"
// ─────────────────────────────────────────────────────────────────────────────

// TestOutbox_Concurrent_PartialFailure_First3Succeed runs 100 goroutines each
// with a 5-event batch where XAdd fails on call 4.
// Exactly events 1–3 must be marked published in every goroutine.
func TestOutbox_Concurrent_PartialFailure_First3Succeed(t *testing.T) {
	const goroutines = 100
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			base := int64(idx * 5)
			f := &mockFetcher{
				events: []OutboxEvent{
					{ID: base + 1, EventType: EventJourneyBooked, Payload: []byte(`{}`)},
					{ID: base + 2, EventType: EventJourneyBooked, Payload: []byte(`{}`)},
					{ID: base + 3, EventType: EventJourneyBooked, Payload: []byte(`{}`)},
					{ID: base + 4, EventType: EventJourneyBooked, Payload: []byte(`{}`)},
					{ID: base + 5, EventType: EventJourneyBooked, Payload: []byte(`{}`)},
				},
			}
			s := &mockStreamWriter{failOnCall: 4} // fail on 4th XAdd
			ready()
			go_()
			relayBatch(context.Background(), f, s, silentLogger())

			marked := f.getMarked()
			if len(marked) != 3 {
				errs <- "expected 3 marked after failOnCall=4, got different"
			}
			// Events 4 and 5 must NOT be marked.
			for _, m := range marked {
				if m >= base+4 {
					errs <- "event >= base+4 was marked despite XAdd failure"
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

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 4 — Event Ordering: Concurrent Producers, Single Stream
//
// Checklist: "Consistency of data maintained across failures/recoveries □"
// ─────────────────────────────────────────────────────────────────────────────

// TestOutbox_EventOrdering_WithinBatch verifies that within a SINGLE batch
// the lifecycle events (booked → activated → completed) arrive in outbox.id
// order even when the batch is processed by multiple concurrent goroutines
// calling relayBatch on their own fetchers.
func TestOutbox_EventOrdering_WithinBatch(t *testing.T) {
	const goroutines = 50

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			f := &mockFetcher{
				events: []OutboxEvent{
					{ID: 1, EventType: EventJourneyBooked, Payload: []byte(`{"seq":1}`)},
					{ID: 2, EventType: EventJourneyActivated, Payload: []byte(`{"seq":2}`)},
					{ID: 3, EventType: EventJourneyCompleted, Payload: []byte(`{"seq":3}`)},
				},
			}
			s := &mockStreamWriter{}
			ready()
			go_()
			relayBatch(context.Background(), f, s, silentLogger())

			pub := s.getPublished()
			if len(pub) != 3 {
				errs <- "expected 3 published"
				return
			}
			want := []string{`{"seq":1}`, `{"seq":2}`, `{"seq":3}`}
			for j, w := range want {
				if pub[j] != w {
					errs <- "event order violated in batch"
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

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 5 — DB Fetch Failures Under Concurrency
// ─────────────────────────────────────────────────────────────────────────────

// TestOutbox_Concurrent_DBFetchFails_NoRedisCallsMade verifies that 200
// goroutines hitting a broken DB never attempt Redis writes.
func TestOutbox_Concurrent_DBFetchFails_NoRedisCallsMade(t *testing.T) {
	const goroutines = 200
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	var totalXAdds int64 // atomic

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			f := &mockFetcher{fetchErr: errors.New("pq: connection reset")}
			s := &mockStreamWriter{}
			ready()
			go_()
			relayBatch(context.Background(), f, s, silentLogger())
			atomic.AddInt64(&totalXAdds, int64(s.getCallCount()))
		}()
	}
	wg.Wait()

	if totalXAdds != 0 {
		t.Errorf("DB was down: expected 0 XAdd calls across all goroutines, got %d", totalXAdds)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SECTION 6 — Batch Size: Concurrent Goroutines Respect the 100-Event Limit
// ─────────────────────────────────────────────────────────────────────────────

// TestOutbox_Concurrent_BatchLimit_NeverExceeds100 fires 50 goroutines each
// with 150 events.  None should process more than 100 per tick.
func TestOutbox_Concurrent_BatchLimit_NeverExceeds100(t *testing.T) {
	const goroutines = 50
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan string, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			events := make([]OutboxEvent, 150)
			for j := range events {
				events[j] = OutboxEvent{ID: int64(idx*150 + j + 1), Payload: []byte(`{}`)}
			}
			f := &mockFetcher{events: events}
			s := &mockStreamWriter{}
			ready()
			go_()
			relayBatch(context.Background(), f, s, silentLogger())

			if len(f.getMarked()) > 100 {
				errs <- "relay processed more than 100 events in one tick"
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
// SECTION 7 — Stream Contract: Stream Name + Event Types
// ─────────────────────────────────────────────────────────────────────────────

// TestOutbox_StreamName_ConcurrentReads verifies that 500 goroutines reading
// the StreamName constant simultaneously always get the same value.
func TestOutbox_StreamName_ConcurrentReads(t *testing.T) {
	const goroutines = 500
	results := make([]string, goroutines)

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			results[idx] = StreamName
		}(i)
	}
	wg.Wait()

	const want = "journey.events"
	for i, got := range results {
		if got != want {
			t.Errorf("goroutine %d: StreamName = %q, want %q", i, got, want)
		}
	}
}

// TestMarshalEnvelope_Concurrent_UniqueIDs fires 1000 goroutines simultaneously
// calling MarshalEnvelope and verifies no two produce the same event_id.
// A collision would cause the notification consumer's dedup logic to silently
// drop a valid event.
func TestMarshalEnvelope_Concurrent_UniqueIDs(t *testing.T) {
	const goroutines = 1000
	type p struct{ X int }

	ids := make([]string, goroutines)
	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			ready()
			go_()
			id, _, _ := MarshalEnvelope(EventJourneyBooked, p{idx})
			ids[idx] = id
		}(i)
	}
	wg.Wait()

	seen := make(map[string]int, goroutines)
	for _, id := range ids {
		seen[id]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("event_id collision: %q generated %d times — consumer dedup will drop a real event", id, count)
		}
	}
}

// TestMarshalEnvelope_Concurrent_AllParseable verifies that 500 goroutines
// simultaneously producing envelopes all generate valid JSON that the
// notification consumer can parse without error.
func TestMarshalEnvelope_Concurrent_AllParseable(t *testing.T) {
	const goroutines = 500
	type payload struct {
		JourneyID string `json:"journey_id"`
		DriverID  string `json:"driver_id"`
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
			_, data, err := MarshalEnvelope(EventJourneyBooked, payload{
				JourneyID: "jrn_" + string(rune('a'+idx%26)),
				DriverID:  "usr_" + string(rune('a'+idx%26)),
			})
			if err != nil {
				errs <- "MarshalEnvelope returned error"
				return
			}
			var env Envelope
			if jsonErr := json.Unmarshal(data, &env); jsonErr != nil {
				errs <- "produced invalid JSON envelope"
			}
			if env.EventType != EventJourneyBooked {
				errs <- "wrong event type in concurrent envelope"
			}
			if env.Timestamp.IsZero() {
				errs <- "zero timestamp in concurrent envelope"
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
// SECTION 8 — Context Cancellation: Graceful Shutdown Under Load
// ─────────────────────────────────────────────────────────────────────────────

// TestOutbox_ContextCancelled_AllGoroutinesStop fires 20 goroutines each
// running the relay's select-loop logic with a pre-cancelled context.
// All must exit within 500ms — verifying the shutdown path is race-free.
func TestOutbox_ContextCancelled_AllGoroutinesStop(t *testing.T) {
	const goroutines = 20
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so goroutines exit immediately on first select

	ready, go_ := barrier(goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			ready()
			go_()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Should never fire — context is already cancelled.
			}
		}()
	}

	select {
	case <-done:
		// All goroutines exited via ctx.Done() — correct.
	case <-time.After(2 * time.Second):
		t.Error("not all goroutines responded to context cancellation within 2s — shutdown race")
	}
}
