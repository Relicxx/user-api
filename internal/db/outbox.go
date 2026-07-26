package db

import (
	"context"
	"database/sql"
	"fmt"

	"user-api/internal/outbox"
)

// OutboxStorage drains the outbox table for the relay.
type OutboxStorage struct {
	DB *sql.DB
}

// ProcessPending claims up to limit unpublished messages with
// FOR UPDATE SKIP LOCKED (safe to run from several instances), publishes
// them in insertion order and marks the delivered ones, all in one
// transaction. On a publish error the batch stops at the failed message:
// everything before it is marked published, the rest is retried on the
// next poll. Delivery is therefore at-least-once — a crash between publish
// and commit re-sends the batch, so consumers must be idempotent.
func (s *OutboxStorage) ProcessPending(ctx context.Context, limit int, publish outbox.PublishFunc) (published int, err error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin outbox tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, topic, key, payload
		FROM outbox
		WHERE published_at IS NULL
		ORDER BY id
		LIMIT $1
		FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return 0, fmt.Errorf("select pending outbox messages: %w", err)
	}

	var msgs []outbox.Message
	for rows.Next() {
		var m outbox.Message
		if scanErr := rows.Scan(&m.ID, &m.Topic, &m.Key, &m.Payload); scanErr != nil {
			rows.Close()
			err = fmt.Errorf("scan outbox message: %w", scanErr)
			return 0, err
		}
		msgs = append(msgs, m)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate outbox messages: %w", err)
	}

	if len(msgs) == 0 {
		err = tx.Commit()
		return 0, err
	}

	var publishErr error
	ids := make([]int64, 0, len(msgs))
	for _, m := range msgs {
		if pubErr := publish(ctx, m); pubErr != nil {
			publishErr = fmt.Errorf("publish outbox message %d: %w", m.ID, pubErr)
			break
		}
		ids = append(ids, m.ID)
	}

	if len(ids) > 0 {
		if _, err = tx.ExecContext(ctx,
			`UPDATE outbox SET published_at = now() WHERE id = ANY($1)`, ids); err != nil {
			return 0, fmt.Errorf("mark outbox messages published: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit outbox tx: %w", err)
	}

	return len(ids), publishErr
}
