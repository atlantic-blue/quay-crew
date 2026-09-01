//go:build integration

package messaging_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/messaging"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

// seedBroker addresses the Redpanda container shared by every test in this file.
var seedBroker string

// TestMain starts one real Redpanda for the package. Each test uses its own topic and group so they
// do not observe each other's records.
func TestMain(m *testing.M) {
	code, err := runWithRedpanda(m)
	if err != nil {
		fmt.Fprintf(os.Stderr, "redpanda: %v\n", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func runWithRedpanda(m *testing.M) (int, error) {
	ctx := context.Background()

	container, err := redpanda.Run(ctx, "redpandadata/redpanda:v24.2.7")
	if err != nil {
		return 0, fmt.Errorf("start: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = container.Terminate(ctx)
	}()

	seedBroker, err = container.KafkaSeedBroker(ctx)
	if err != nil {
		return 0, fmt.Errorf("seed broker: %w", err)
	}
	return m.Run(), nil
}

// TestPublishConsume publishes through the messaging client and reads the record back with a plain
// consumer, validating the Kafka client and the networking end to end.
func TestPublishConsume(t *testing.T) {
	ctx := context.Background()
	topic := createTopic(t, "publish")

	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(seedBroker),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		t.Fatalf("consumer: %v", err)
	}
	t.Cleanup(consumer.Close)

	producer := newClient(t)
	if err := producer.Publish(ctx, topic, []byte("k"), []byte("hello")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	pollCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	fetches := consumer.PollFetches(pollCtx)
	if err := fetches.Err(); err != nil {
		t.Fatalf("poll: %v", err)
	}

	var got string
	fetches.EachRecord(func(r *kgo.Record) { got = string(r.Value) })
	if got != "hello" {
		t.Fatalf("consumed %q, want %q", got, "hello")
	}
}

// TestConsumeCommitsAfterHandling proves the commit-after-handle contract: the group's offset only
// advances past records the handler has already accepted, and a new consumer in the same group then
// starts after them rather than replaying them.
func TestConsumeCommitsAfterHandling(t *testing.T) {
	ctx := context.Background()
	topic := createTopic(t, "commits")
	group := "commits-group"

	client := newClient(t)
	for _, value := range []string{"one", "two"} {
		if err := client.Publish(ctx, topic, []byte("k"), []byte(value)); err != nil {
			t.Fatalf("publish %s: %v", value, err)
		}
	}

	consumeCtx, stopConsuming := context.WithTimeout(ctx, 30*time.Second)
	defer stopConsuming()

	handled := make(chan string, 2)
	consumeDone := make(chan error, 1)
	go func() {
		consumeDone <- client.Consume(consumeCtx, group, []string{topic}, func(_ context.Context, r messaging.Record) error {
			handled <- string(r.Value)
			return nil
		})
	}()

	for _, want := range []string{"one", "two"} {
		select {
		case got := <-handled:
			if got != want {
				t.Fatalf("handled %q, want %q", got, want)
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("handler was not called with %q", want)
		}
	}

	// Both records were handled, so the group must end up committed past them. The commit happens
	// after the handler returns, so wait for it rather than reading once.
	waitForCommittedOffset(t, group, topic, 2)

	stopConsuming()
	if err := <-consumeDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Consume() = %v, want context.Canceled", err)
	}
}

// TestConsumeStopsOnHandlerError checks that a failing handler stops consumption, surfaces its own
// error to the caller, and leaves the record uncommitted so it is delivered again.
func TestConsumeStopsOnHandlerError(t *testing.T) {
	ctx := context.Background()
	topic := createTopic(t, "failures")
	group := "failures-group"

	client := newClient(t)
	if err := client.Publish(ctx, topic, []byte("k"), []byte("boom")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	consumeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	handlerErr := errors.New("handler rejected the record")
	calls := 0
	err := client.Consume(consumeCtx, group, []string{topic}, func(context.Context, messaging.Record) error {
		calls++
		return handlerErr
	})
	if !errors.Is(err, handlerErr) {
		t.Fatalf("Consume() = %v, want %v", err, handlerErr)
	}
	if calls != 1 {
		t.Fatalf("handler called %d times, want 1", calls)
	}

	if offset, ok := committedOffset(t, group, topic); ok && offset > 0 {
		t.Fatalf("committed offset %d after a failing handler, want the record left uncommitted", offset)
	}
}

func newClient(t *testing.T) *messaging.Client {
	t.Helper()
	client, err := messaging.NewClient(seedBroker)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(client.Close)
	return client
}

// createTopic names a topic for the calling test and creates it with a single partition.
func createTopic(t *testing.T, stream string) string {
	t.Helper()
	topic, err := messaging.Topic("acme", stream)
	if err != nil {
		t.Fatalf("topic: %v", err)
	}
	if _, err := admin(t).CreateTopic(context.Background(), 1, 1, nil, topic); err != nil {
		t.Fatalf("create topic %s: %v", topic, err)
	}
	return topic
}

func admin(t *testing.T) *kadm.Client {
	t.Helper()
	kc, err := kgo.NewClient(kgo.SeedBrokers(seedBroker))
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	t.Cleanup(kc.Close)
	return kadm.NewClient(kc)
}

// committedOffset reports the group's committed offset for the topic's only partition, and whether
// the group has committed one at all.
func committedOffset(t *testing.T, group, topic string) (int64, bool) {
	t.Helper()
	offsets, err := admin(t).FetchOffsets(context.Background(), group)
	if err != nil {
		t.Fatalf("fetch offsets for group %s: %v", group, err)
	}
	offset, ok := offsets.Lookup(topic, 0)
	if !ok {
		return 0, false
	}
	return offset.At, true
}

func waitForCommittedOffset(t *testing.T, group, topic string, want int64) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last int64
	for time.Now().Before(deadline) {
		offset, ok := committedOffset(t, group, topic)
		if ok && offset >= want {
			return
		}
		last = offset
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("committed offset for group %s reached %d, want %d", group, last, want)
}
