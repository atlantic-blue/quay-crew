package channel_test

import (
	"context"
	"errors"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/channel"
)

func TestMemoryAdapterRoundTrip(t *testing.T) {
	adapter := channel.NewMemoryAdapter("cli")
	if adapter.ID() != "cli" {
		t.Fatalf("ID() = %q, want %q", adapter.ID(), "cli")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	got := make(chan *quaycrewv1.InboundMessage, 1)
	go func() {
		_ = adapter.Start(ctx, func(_ context.Context, msg *quaycrewv1.InboundMessage) error {
			got <- msg
			return nil
		})
	}()

	in := &quaycrewv1.InboundMessage{Workspace: "acme", Channel: "cli", Text: "hello", CorrelationId: "abc"}
	if err := adapter.Inject(ctx, in); err != nil {
		t.Fatalf("Inject: %v", err)
	}

	select {
	case msg := <-got:
		if msg.GetText() != "hello" || msg.GetWorkspace() != "acme" {
			t.Fatalf("handler got %+v", msg)
		}
	case <-ctx.Done():
		t.Fatal("handler was not called before timeout")
	}
}

func TestMemoryAdapterDeliverRecords(t *testing.T) {
	adapter := channel.NewMemoryAdapter("cli")
	out := &quaycrewv1.OutboundMessage{Workspace: "acme", ThreadId: "t1", Text: "reply", CorrelationId: "abc"}
	if err := adapter.Deliver(context.Background(), out); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	delivered := adapter.Delivered()
	if len(delivered) != 1 || delivered[0].GetText() != "reply" {
		t.Fatalf("Delivered() = %+v", delivered)
	}
}

func TestMemoryAdapterRejectsNil(t *testing.T) {
	adapter := channel.NewMemoryAdapter("cli")
	if err := adapter.Deliver(context.Background(), nil); !errors.Is(err, channel.ErrNilMessage) {
		t.Fatalf("Deliver(nil) err = %v, want ErrNilMessage", err)
	}
	if err := adapter.Start(context.Background(), nil); !errors.Is(err, channel.ErrNilHandler) {
		t.Fatalf("Start(nil) err = %v, want ErrNilHandler", err)
	}
}

func TestMemoryAdapterInjectRespectsContext(t *testing.T) {
	adapter := channel.NewMemoryAdapter("cli") // never Started, so never ready
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	in := &quaycrewv1.InboundMessage{Workspace: "acme"}
	if err := adapter.Inject(ctx, in); !errors.Is(err, context.Canceled) {
		t.Fatalf("Inject on cancelled ctx err = %v, want context.Canceled", err)
	}
}
