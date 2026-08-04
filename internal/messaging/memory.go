package messaging

import (
	"context"
	"fmt"
	"regexp"
	"sync"
)

// Discard is an EventLog that accepts everything and keeps nothing.
//
// It exists so a stack with no broker configured runs normally rather than making every caller check
// for a nil log. Consuming from it blocks until the context is done, which is what consuming from a
// log that never receives anything should look like.
type Discard struct{}

var _ EventLog = Discard{}

func (Discard) Publish(context.Context, string, []byte, []byte) error { return nil }

func (Discard) Consume(ctx context.Context, _ string, _ []string, _ Handler) error {
	<-ctx.Done()
	return ctx.Err()
}

func (Discard) ConsumePattern(ctx context.Context, _, _ string, _ Handler) error {
	<-ctx.Done()
	return ctx.Err()
}

func (Discard) Close() {}

// Memory is an EventLog that keeps records in memory, for tests.
//
// It is not a substitute for the real thing: there are no partitions, no offsets and no groups, so a
// behaviour that depends on any of those belongs in the integration test against a real Redpanda.
// What it is good for is asserting that something was published, and what it said.
type Memory struct {
	// StopWhenDrained makes a consumer return once it has replayed what is already there, instead of
	// blocking for more. A test that wants a consumer to catch up can then wait for it to finish
	// rather than for a timeout, which is the difference between a suite that is deterministic and
	// one that is merely usually right.
	StopWhenDrained bool

	mu       sync.Mutex
	records  []Record
	handlers []Handler
}

var _ EventLog = (*Memory)(nil)

// NewMemory returns an empty in memory log.
func NewMemory() *Memory { return &Memory{} }

// Publish appends a record and hands it to anything consuming.
func (m *Memory) Publish(ctx context.Context, topic string, key, value []byte) error {
	m.mu.Lock()
	record := Record{Topic: topic, Key: key, Value: value}
	m.records = append(m.records, record)
	handlers := make([]Handler, len(m.handlers))
	copy(handlers, m.handlers)
	m.mu.Unlock()

	for _, handler := range handlers {
		if err := handler(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

// Consume registers handler and blocks until ctx is done, replaying what is already there first, so
// a consumer that starts after a publisher still sees everything. Group is ignored: there is one
// reader of an in memory log.
func (m *Memory) Consume(ctx context.Context, group string, topics []string, handler Handler) error {
	wanted := map[string]bool{}
	for _, topic := range topics {
		wanted[topic] = true
	}
	return m.consume(ctx, group, func(topic string) bool { return wanted[topic] }, handler)
}

// consume is what both Consume and ConsumePattern do, differing only in which topics they want.
func (m *Memory) consume(ctx context.Context, _ string, wants func(string) bool, handler Handler) error {
	filtered := func(ctx context.Context, record Record) error {
		if !wants(record.Topic) {
			return nil
		}
		return handler(ctx, record)
	}

	m.mu.Lock()
	existing := make([]Record, len(m.records))
	copy(existing, m.records)
	m.handlers = append(m.handlers, filtered)
	m.mu.Unlock()

	for _, record := range existing {
		if err := filtered(ctx, record); err != nil {
			return err
		}
	}
	if m.StopWhenDrained {
		return nil
	}

	<-ctx.Done()
	return ctx.Err()
}

// ConsumePattern consumes every topic matching pattern, the same way Consume does for a list.
func (m *Memory) ConsumePattern(ctx context.Context, group, pattern string, handler Handler) error {
	matches, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("messaging: consume pattern %q: %w", pattern, err)
	}
	return m.consume(ctx, group, func(topic string) bool { return matches.MatchString(topic) }, handler)
}

// Close does nothing. The records stay readable, which is the point in a test.
func (m *Memory) Close() {}

// Records returns everything published so far, oldest first.
func (m *Memory) Records() []Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Record, len(m.records))
	copy(out, m.records)
	return out
}

// RecordsOn returns everything published to one topic, oldest first.
func (m *Memory) RecordsOn(topic string) []Record {
	out := make([]Record, 0)
	for _, record := range m.Records() {
		if record.Topic == topic {
			out = append(out, record)
		}
	}
	return out
}
