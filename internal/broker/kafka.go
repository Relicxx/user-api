package broker

import (
	"context"
	"encoding/json"

	"user-api/internal/model"

	"github.com/segmentio/kafka-go"
)

type KafkaProducer struct {
	writer *kafka.Writer
}

func NewKafkaProducer(addr, topic string) *KafkaProducer {
	writer := &kafka.Writer{
		Addr:     kafka.TCP(addr),
		Topic:    topic,
		Balancer: &kafka.LeastBytes{},
	}

	return &KafkaProducer{writer: writer}
}

func (p *KafkaProducer) PublishUserCreated(ctx context.Context, user *model.User) error {
	data, err := json.Marshal(user)
	if err != nil {
		return err
	}

	return p.writer.WriteMessages(ctx, kafka.Message{
		Key:   []byte("user-created"),
		Value: data,
	})
}

func (p *KafkaProducer) Close() error {
	return p.writer.Close()
}
