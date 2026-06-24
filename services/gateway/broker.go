package main

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitBroker struct {
	conn *amqp.Connection
	ch   *amqp.Channel
}

func NewBroker(url string) (*RabbitBroker, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("amqp.Dial: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("Channel: %w", err)
	}
	_, err = ch.QueueDeclare(workQueue, true, false, false, false, nil)
	if err != nil {
		ch.Close()
		conn.Close()
		return nil, fmt.Errorf("QueueDeclare: %w", err)
	}
	return &RabbitBroker{conn: conn, ch: ch}, nil
}

func (b *RabbitBroker) PublishJob(ctx context.Context, body []byte) error {
	return b.ch.PublishWithContext(ctx, "", workQueue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Body:         body,
	})
}

func (b *RabbitBroker) Ping() error {
	if b.conn.IsClosed() {
		return fmt.Errorf("connection closed")
	}
	return nil
}
