// Package broker contains the Kafka producer used by the outbox relay.
package broker

import (
	"context"

	"github.com/segmentio/kafka-go"
)

// KafkaProducer publishes raw messages to Kafka.
type KafkaProducer struct {
	writer *kafka.Writer
}

// NewKafkaProducer builds a producer for the given broker address.
func NewKafkaProducer(addr string) *KafkaProducer {
	writer := &kafka.Writer{
		Addr: kafka.TCP(addr),
		// Hash balancer keeps all messages with the same key (user ID)
		// in one partition, preserving per-user ordering.
		Balancer:               &kafka.Hash{},
		RequiredAcks:           kafka.RequireAll,
		AllowAutoTopicCreation: true,
	}

	return &KafkaProducer{writer: writer}
}

// Publish writes one message to the given topic, keyed for partitioning.
func (p *KafkaProducer) Publish(ctx context.Context, topic, key string, payload []byte) error {
	return p.writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: payload,
	})
}

// Close flushes and closes the underlying writer.
func (p *KafkaProducer) Close() error {
	return p.writer.Close()
}
