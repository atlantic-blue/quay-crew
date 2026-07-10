package messaging

import "context"

// Record is one message on the event log.
type Record struct {
	Topic string
	Key   []byte
	Value []byte
}

// Handler processes a consumed record. Returning an error stops consumption and the record is not
// committed, so it is delivered again on the next run.
type Handler func(ctx context.Context, record Record) error

// EventLog is the asynchronous backbone: publish records to a topic and consume them as a group.
// Locally the log is Kafka served by Redpanda; a cloud implementation satisfies the same interface.
type EventLog interface {
	Publish(ctx context.Context, topic string, key, value []byte) error
	Consume(ctx context.Context, group string, topics []string, handler Handler) error
	Close()
}
