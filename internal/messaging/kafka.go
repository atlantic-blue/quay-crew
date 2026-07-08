package messaging

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Client is a franz-go backed EventLog: it publishes records and consumes them by group.
// The producer connects lazily, so constructing one does not require a running broker.
type Client struct {
	seeds    []string
	producer *kgo.Client
}

// compile time check that Client satisfies EventLog.
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

// Consume joins the given group, reads records from the topics, and calls handler for each. It
// commits offsets only after handler succeeds, and blocks until ctx is done or handler returns an
// error.
func (c *Client) Consume(ctx context.Context, group string, topics []string, handler Handler) error {
	if group == "" {
		return fmt.Errorf("messaging: consume needs a group")
	}
	if len(topics) == 0 {
		return fmt.Errorf("messaging: consume needs at least one topic")
	}
	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(c.seeds...),
		kgo.ConsumeTopics(topics...),
		kgo.ConsumerGroup(group),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return fmt.Errorf("messaging: new consumer: %w", err)
	}
	defer consumer.Close()

	for {
		fetches := consumer.PollFetches(ctx)
		if err := ctx.Err(); err != nil {
			return err
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			return fmt.Errorf("messaging: consume: %v", errs)
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
			return fmt.Errorf("messaging: commit: %w", err)
		}
	}
}

// Close releases the producer.
func (c *Client) Close() { c.producer.Close() }
