package messaging

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Client is a franz-go backed EventLog. The producer connects lazily, so constructing one does not
// require a running broker; each Consume call opens its own consumer for the group it joins.
type Client struct {
	seeds    []string
	producer *kgo.Client
	// ensured remembers the topics this client has already created or found, so the cost of the
	// check is paid once per topic per process rather than on every record.
	ensured sync.Map
}

var _ EventLog = (*Client)(nil)

// NewClient creates a client for the given seed brokers (for example "localhost:19092").
//
// The producer asks the broker to create a topic it has never seen. A workspace's streams are named
// after the workspace, so they come into existence when the first record is written rather than
// being provisioned ahead of time by somebody who has to remember to. Without this, the very first
// task in a new workspace is rejected with UNKNOWN_TOPIC_OR_PARTITION and quietly dropped, which is
// exactly the failure this option exists to stop.
func NewClient(seeds ...string) (*Client, error) {
	if len(seeds) == 0 {
		return nil, fmt.Errorf("messaging: at least one seed broker is required")
	}
	producer, err := kgo.NewClient(
		kgo.SeedBrokers(seeds...),
		kgo.AllowAutoTopicCreation(),
	)
	if err != nil {
		return nil, fmt.Errorf("messaging: new kafka client: %w", err)
	}
	return &Client{seeds: seeds, producer: producer}, nil
}

// Publish writes value to topic, keyed by key, and waits for the broker to acknowledge it.
//
// The topic is created on first use. Streams are named after the workspace they belong to, so they
// cannot be provisioned ahead of time by anyone who does not know what workspaces exist yet, and a
// broker with automatic creation turned off would otherwise reject the first record ever written to
// a new workspace.
func (c *Client) Publish(ctx context.Context, topic string, key, value []byte) error {
	if err := c.ensureTopic(ctx, topic); err != nil {
		return err
	}
	record := &kgo.Record{Topic: topic, Key: key, Value: value}
	if err := c.producer.ProduceSync(ctx, record).FirstErr(); err != nil {
		return fmt.Errorf("messaging: publish to %s: %w", topic, err)
	}
	return nil
}

// ensureTopic creates topic unless it is already there, with the broker's own defaults for
// partitions and replication, because what those should be is the cluster's business and not this
// client's.
func (c *Client) ensureTopic(ctx context.Context, topic string) error {
	if _, done := c.ensured.Load(topic); done {
		return nil
	}
	response, err := kadm.NewClient(c.producer).CreateTopic(ctx, -1, -1, nil, topic)
	if err != nil && !errors.Is(err, kerr.TopicAlreadyExists) {
		return fmt.Errorf("messaging: create topic %s: %w", topic, err)
	}
	if response.Err != nil && !errors.Is(response.Err, kerr.TopicAlreadyExists) {
		return fmt.Errorf("messaging: create topic %s: %w", topic, response.Err)
	}
	c.ensured.Store(topic, true)
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
	if len(topics) == 0 {
		return fmt.Errorf("messaging: consume requires at least one topic")
	}
	return c.consume(ctx, group, handler, kgo.ConsumeTopics(topics...))
}

// ConsumePattern consumes every topic matching a regular expression, and keeps up with topics
// created after it started, which is how a workspace made while the system is running gets read.
func (c *Client) ConsumePattern(ctx context.Context, group, pattern string, handler Handler) error {
	if pattern == "" {
		return fmt.Errorf("messaging: consume requires a pattern")
	}
	return c.consume(ctx, group, handler, kgo.ConsumeTopics(pattern), kgo.ConsumeRegex())
}

// consume is what both consumers do, differing only in how they were told which topics to read.
func (c *Client) consume(ctx context.Context, group string, handler Handler, subscription ...kgo.Opt) error {
	if group == "" {
		return fmt.Errorf("messaging: consume requires a group")
	}
	if handler == nil {
		return fmt.Errorf("messaging: consume requires a handler")
	}

	options := append([]kgo.Opt{
		kgo.SeedBrokers(c.seeds...),
		kgo.ConsumerGroup(group),
		// A group with no committed offset starts at the beginning of the log, so a new consumer
		// replays the history rather than seeing only what arrives after it starts.
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
	}, subscription...)

	consumer, err := kgo.NewClient(options...)
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
