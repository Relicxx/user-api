package outbox

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// mockStore hands out pre-seeded messages in batches, mimicking the
// database-side claim-and-mark logic.
type mockStore struct {
	mu       sync.Mutex
	pending  []Message
	fail     error
	calls    int
	maxCalls int
}

func (m *mockStore) ProcessPending(ctx context.Context, limit int, publish PublishFunc) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls++
	if m.maxCalls > 0 && m.calls > m.maxCalls {
		return 0, nil
	}
	if m.fail != nil {
		return 0, m.fail
	}

	batch := m.pending
	if len(batch) > limit {
		batch = batch[:limit]
	}

	published := 0
	for _, msg := range batch {
		if err := publish(ctx, msg); err != nil {
			m.pending = m.pending[published:]
			return published, err
		}
		published++
	}
	m.pending = m.pending[published:]
	return published, nil
}

type mockPublisher struct {
	mu     sync.Mutex
	msgs   []Message
	failOn string // key that triggers an error
}

func (p *mockPublisher) Publish(_ context.Context, topic, key string, payload []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.failOn != "" && key == p.failOn {
		return errors.New("broker unavailable")
	}
	p.msgs = append(p.msgs, Message{Topic: topic, Key: key, Payload: payload})
	return nil
}

func (p *mockPublisher) published() []Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Message(nil), p.msgs...)
}

func seed(n int) []Message {
	msgs := make([]Message, 0, n)
	for i := 1; i <= n; i++ {
		msgs = append(msgs, Message{
			ID:      int64(i),
			Topic:   "user-created",
			Key:     "k",
			Payload: []byte(`{}`),
		})
	}
	return msgs
}

func TestRelayDrainsBacklogAcrossBatches(t *testing.T) {
	store := &mockStore{pending: seed(25)}
	pub := &mockPublisher{}
	relay := NewRelay(store, pub, time.Hour, 10, nil)

	relay.drain(context.Background())

	if got := len(pub.published()); got != 25 {
		t.Fatalf("expected 25 published messages, got %d", got)
	}
	if len(store.pending) != 0 {
		t.Errorf("expected empty outbox, %d messages left", len(store.pending))
	}
	// 25 messages with batch size 10: two full batches force another poll,
	// the third (partial) batch stops the loop.
	if store.calls != 3 {
		t.Errorf("expected 3 ProcessPending calls, got %d", store.calls)
	}
}

func TestRelayStopsBatchOnPublishError(t *testing.T) {
	store := &mockStore{pending: []Message{
		{ID: 1, Topic: "t", Key: "ok-1", Payload: []byte(`{}`)},
		{ID: 2, Topic: "t", Key: "bad", Payload: []byte(`{}`)},
		{ID: 3, Topic: "t", Key: "ok-2", Payload: []byte(`{}`)},
	}}
	pub := &mockPublisher{failOn: "bad"}
	relay := NewRelay(store, pub, time.Hour, 10, nil)

	relay.drain(context.Background())

	// Only the message before the failure is published; the failed one and
	// everything after it stay pending for the next poll.
	if got := len(pub.published()); got != 1 {
		t.Fatalf("expected 1 published message, got %d", got)
	}
	if len(store.pending) != 2 {
		t.Errorf("expected 2 pending messages after failure, got %d", len(store.pending))
	}
}

func TestRelayStoreErrorDoesNotLoop(t *testing.T) {
	store := &mockStore{fail: errors.New("db down")}
	relay := NewRelay(store, &mockPublisher{}, time.Hour, 10, nil)

	relay.drain(context.Background())

	if store.calls != 1 {
		t.Errorf("expected a single call on store error, got %d", store.calls)
	}
}

func TestRelayRunStopsOnContextCancel(t *testing.T) {
	store := &mockStore{pending: seed(3)}
	pub := &mockPublisher{}
	relay := NewRelay(store, pub, 5*time.Millisecond, 10, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		relay.Run(ctx)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for len(pub.published()) < 3 {
		select {
		case <-deadline:
			t.Fatal("relay did not publish seeded messages in time")
		case <-time.After(5 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not stop after context cancel")
	}
}
