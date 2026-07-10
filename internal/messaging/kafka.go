package messaging

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Client is a franz-go backed EventLog. The producer connects lazily, so constructing one does not
// require a running broker; each Consume call opens its own consumer for the group it joins.
type Client struct {
	seeds    []string
	producer *kgo.Client
}

var _ EventLog = (*Client)(nil)

// NewClient creates a client for the given seed brokers (for example "localhost:19092").
func NewClient(seeds ...string) (*Client, error) {
	if len(seeds) == 0 {
		return nil, fmt.Errorf("messaging: at least one seed broker is required")
	}
	producer, err := kgo.NewClient(kgo.SeedBrokers(seeds...))
	if err != nil {
		return nil, fmt.Errorf("messaging: new kafka client: %w", err)
	}
	return &Client{seeds: seeds, producer: producer}, nil
}

// Publish writes value to topic, keyed by key, and waits for the broker to acknowledge it.
func (c *Client) Publish(ctx context.Context, topic string, key, value []byte) error {
	record := &kgo.Record{Topic: topic, Key: key, Value: value}
	if err := c.producer.ProduceSync(ctx, record).FirstErr(); err != nil {
		return fmt.Errorf("messaging: publish to %s: %w", topic, err)
	}
	return nil
}

// Consume joins group, reads records from topics, and calls handler for each one.
//
// Offsets are committed only once handler has returned nil for every record in a fetched batch. A
// batch whose handler fails is therefore never committed and is delivered again, which makes
// delivery at least once: handlers must be idempotent.
//
// Consume blocks. It returns ctx.Err() when ctx is done, and returns a handler's error unchanged so
// callers can match it with errors.Is.
func (c *Client) Consume(ctx context.Context, group string, topics []string, handler Handler) error {
	if group == "" {
		return fmt.Errorf("messaging: consume requires a group")
	}
	if len(topics) == 0 {
		return fmt.Errorf("messaging: consume requires at least one topic")
	}
	if handler == nil {
		return fmt.Errorf("messaging: consume requires a handler")
	}

	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(c.seeds...),
		kgo.ConsumeTopics(topics...),
		kgo.ConsumerGroup(group),
		// A group with no committed offset starts at the beginning of the log, so a new consumer
		// replays the history rather than seeing only what arrives after it starts.
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return fmt.Errorf("messaging: new consumer for group %s: %w", group, err)
	}
	defer consumer.Close()

	for {
		fetches := consumer.PollFetches(ctx)
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fetches.Err(); err != nil {
			return fmt.Errorf("messaging: consume group %s: %w", group, err)
		}
		if fetches.NumRecords() == 0 {
			continue
		}

		var handleErr error
		fetches.EachRecord(func(r *kgo.Record) {
			if handleErr != nil {
				return
			}
			handleErr = handler(ctx, Record{Topic: r.Topic, Key: r.Key, Value: r.Value})
		})
		if handleErr != nil {
			return handleErr
		}

		if err := consumer.CommitUncommittedOffsets(ctx); err != nil {
			return fmt.Errorf("messaging: commit offsets for group %s: %w", group, err)
		}
	}
}

// Close releases the producer.
func (c *Client) Close() { c.producer.Close() }
