//go:build integration

package messaging_test

import (
	"context"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

// TestPublishConsume starts a real Redpanda in a container, publishes through the messaging client,
// and consumes the record back. This validates the Kafka client and the networking end to end.
func TestPublishConsume(t *testing.T) {
	ctx := context.Background()

	container, err := redpanda.Run(ctx, "redpandadata/redpanda:v24.2.7")
	if err != nil {
		t.Fatalf("start redpanda: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = container.Terminate(ctx)
	})

	seed, err := container.KafkaSeedBroker(ctx)
	if err != nil {
		t.Fatalf("seed broker: %v", err)
	}

	topic, err := messaging.Topic("acme", "inbound")
	if err != nil {
		t.Fatalf("topic: %v", err)
	}

	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(seed),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		t.Fatalf("consumer: %v", err)
	}
	t.Cleanup(consumer.Close)

	if _, err := kadm.NewClient(consumer).CreateTopic(ctx, 1, 1, nil, topic); err != nil {
		t.Fatalf("create topic: %v", err)
	}

	producer, err := messaging.NewClient(seed)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(producer.Close)

	if err := producer.Publish(ctx, topic, []byte("k"), []byte("hello")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	pollCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	fetches := consumer.PollFetches(pollCtx)
	if errs := fetches.Errors(); len(errs) > 0 {
		t.Fatalf("poll: %v", errs)
	}

	var got string
	fetches.EachRecord(func(r *kgo.Record) { got = string(r.Value) })
	if got != "hello" {
		t.Fatalf("consumed %q, want %q", got, "hello")
	}
}

// TestConsumeGroup exercises the client's own Consume (group based) path against a real Redpanda.
func TestConsumeGroup(t *testing.T) {
	ctx := context.Background()

	container, err := redpanda.Run(ctx, "redpandadata/redpanda:v24.2.7")
	if err != nil {
		t.Fatalf("start redpanda: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = container.Terminate(ctx)
	})

	seed, err := container.KafkaSeedBroker(ctx)
	if err != nil {
		t.Fatalf("seed broker: %v", err)
	}
	topic, err := messaging.Topic("acme", "inbound")
	if err != nil {
		t.Fatalf("topic: %v", err)
	}

	admin, err := kgo.NewClient(kgo.SeedBrokers(seed))
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	if _, err := kadm.NewClient(admin).CreateTopic(ctx, 1, 1, nil, topic); err != nil {
		t.Fatalf("create topic: %v", err)
	}
	admin.Close()

	client, err := messaging.NewClient(seed)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(client.Close)

	if err := client.Publish(ctx, topic, []byte("k"), []byte("hello")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	consumeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	got := make(chan string, 1)
	err = client.Consume(consumeCtx, "test-group", []string{topic}, func(_ context.Context, r messaging.Record) error {
		got <- string(r.Value)
		cancel() // stop after the first record
		return nil
	})
	if err != nil && consumeCtx.Err() == nil {
		t.Fatalf("consume: %v", err)
	}

	select {
	case v := <-got:
		if v != "hello" {
			t.Fatalf("consumed %q, want hello", v)
		}
	default:
		t.Fatal("handler was not called")
	}
}
