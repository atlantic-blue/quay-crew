package messaging

import "context"

// Record is one message on the event log.
type Record struct {
	Topic string
	Key   []byte
	Value []byte
}

// Handler processes a consumed record. An error stops consumption without committing, so the record
// is delivered again on the next run.
type Handler func(ctx context.Context, record Record) error

// EventLog is the asynchronous backbone. Locally it is Kafka served by Redpanda.
type EventLog interface {
	Publish(ctx context.Context, topic string, key, value []byte) error
	Consume(ctx context.Context, group string, topics []string, handler Handler) error
	// ConsumePattern includes topics created after consumption started. Streams are named after
	// workspaces, and a workspace made while the crew is running would otherwise be ignored.
	ConsumePattern(ctx context.Context, group, pattern string, handler Handler) error
	Close()
}
