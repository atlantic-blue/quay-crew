package messaging_test

import (
	"context"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/messaging"
)

func TestNewClientRequiresSeeds(t *testing.T) {
	if _, err := messaging.NewClient(); err == nil {
		t.Fatal("NewClient() with no seeds = nil error, want error")
	}
}

func TestNewClientWithSeeds(t *testing.T) {
	// franz-go connects lazily, so this does not touch the network.
	c, err := messaging.NewClient("localhost:19092")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.Close()
}

// Consume rejects an incomplete request before it opens a consumer, so these need no broker.
func TestConsumeRejectsIncompleteRequests(t *testing.T) {
	client, err := messaging.NewClient("localhost:19092")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(client.Close)

	topics := []string{"acme.inbound"}
	handler := func(context.Context, messaging.Record) error { return nil }

	tests := []struct {
		name    string
		group   string
		topics  []string
		handler messaging.Handler
	}{
		{name: "no group", group: "", topics: topics, handler: handler},
		{name: "no topics", group: "workers", topics: nil, handler: handler},
		{name: "no handler", group: "workers", topics: topics, handler: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := client.Consume(context.Background(), test.group, test.topics, test.handler)
			if err == nil {
				t.Fatal("Consume() = nil error, want error")
			}
		})
	}
}
