package messaging

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Client is a thin wrapper over a franz-go Kafka client used to publish to the event log.
// It connects lazily, so constructing one does not require a running broker.
type Client struct {
	kc *kgo.Client
}

// NewClient creates a client for the given seed brokers (for example "localhost:19092").
func NewClient(seeds ...string) (*Client, error) {
	if len(seeds) == 0 {
		return nil, fmt.Errorf("messaging: at least one seed broker is required")
	}
	kc, err := kgo.NewClient(kgo.SeedBrokers(seeds...))
	if err != nil {
		return nil, fmt.Errorf("messaging: new kafka client: %w", err)
	}
	return &Client{kc: kc}, nil
}

// Publish writes value to topic, keyed by key, and waits for the broker to acknowledge it.
func (c *Client) Publish(ctx context.Context, topic string, key, value []byte) error {
	record := &kgo.Record{Topic: topic, Key: key, Value: value}
	if err := c.kc.ProduceSync(ctx, record).FirstErr(); err != nil {
		return fmt.Errorf("messaging: publish to %s: %w", topic, err)
	}
	return nil
}

// Close releases the underlying client.
func (c *Client) Close() { c.kc.Close() }
