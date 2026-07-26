// Package outbox implements the relay side of the transactional outbox
// pattern: events are written to an outbox table in the same database
// transaction as the state change, and a background relay drains the table
// and publishes them to the broker with at-least-once delivery.
package outbox

import (
	"context"
	"log/slog"
	"time"
)

// Message is a single event stored in the outbox table.
type Message struct {
	ID      int64
	Topic   string
	Key     string
	Payload []byte
}

// PublishFunc delivers one message to the broker. Returning an error stops
// the current batch so ordering is preserved and the message is retried on
// the next poll.
type PublishFunc func(ctx context.Context, msg Message) error

// Store claims a batch of unpublished messages, feeds them to publish and
// marks the successfully published ones, all inside one transaction.
// It returns how many messages were published.
type Store interface {
	ProcessPending(ctx context.Context, limit int, publish PublishFunc) (int, error)
}

// Publisher sends a raw event to the broker.
type Publisher interface {
	Publish(ctx context.Context, topic, key string, payload []byte) error
}

// Relay periodically drains the outbox and publishes pending events.
type Relay struct {
	store     Store
	publisher Publisher
	interval  time.Duration
	batchSize int
	log       *slog.Logger
}

func NewRelay(store Store, publisher Publisher, interval time.Duration, batchSize int, log *slog.Logger) *Relay {
	if log == nil {
		log = slog.Default()
	}
	return &Relay{
		store:     store,
		publisher: publisher,
		interval:  interval,
		batchSize: batchSize,
		log:       log,
	}
}

// Run polls the outbox until ctx is canceled. It is intended to run in its
// own goroutine; cancel ctx to stop it during graceful shutdown.
func (r *Relay) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	r.log.Info("outbox relay started", "interval", r.interval, "batch_size", r.batchSize)

	for {
		select {
		case <-ctx.Done():
			r.log.Info("outbox relay stopped")
			return
		case <-ticker.C:
			r.drain(ctx)
		}
	}
}

// drain keeps processing full batches so a backlog clears in one poll cycle
// instead of one batch per tick.
func (r *Relay) drain(ctx context.Context) {
	for {
		n, err := r.store.ProcessPending(ctx, r.batchSize, func(ctx context.Context, msg Message) error {
			return r.publisher.Publish(ctx, msg.Topic, msg.Key, msg.Payload)
		})
		if err != nil {
			if ctx.Err() == nil {
				r.log.Error("outbox batch failed, will retry", "published", n, "error", err)
			}
			return
		}
		if n < r.batchSize {
			return
		}
	}
}
