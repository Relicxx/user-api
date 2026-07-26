package broker

import (
	"context"

	"github.com/segmentio/kafka-go"
)

type KafkaProducer struct {
	writer *kafka.Writer
}

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

func (p *KafkaProducer) Publish(ctx context.Context, topic, key string, payload []byte) error {
	return p.writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: payload,
	})
}

func (p *KafkaProducer) Close() error {
	return p.writer.Close()
}
