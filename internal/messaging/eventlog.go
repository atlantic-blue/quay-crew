package messaging

import "context"

// Record is one message on the event log.
type Record struct {
	Topic string
	Key   []byte
	Value []byte
}

// Handler processes a consumed record. Returning an error stops consumption.
type Handler func(ctx context.Context, r Record) error

// EventLog is the async backbone: publish records and consume them by group. Locally it is Kafka via
// Redpanda; a cloud implementation satisfies the same interface.
type EventLog interface {
	Publish(ctx context.Context, topic string, key, value []byte) error
	Consume(ctx context.Context, group string, topics []string, handler Handler) error
	Close()
}
