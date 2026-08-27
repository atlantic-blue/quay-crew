package messaging_test

import (
	"context"
	"net"
	"testing"
	"time"

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

// A broker that accepts the connection and never answers is what wedged a whole crew. The producer
// keeps a record for as long as it is given, because franz-go leaves delivery unlimited by default,
// so the only thing that ends the wait is the caller's own budget. This proves the budget works,
// which is what the control plane's export now relies on.
func TestAPublishComesBackWhenTheBrokerNeverAnswers(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	// A broker that takes the connection and says nothing on it, held open so the client keeps
	// waiting on an answer rather than seeing the socket close.
	accepted := make(chan net.Conn, 8)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			accepted <- conn
		}
	}()
	t.Cleanup(func() {
		close(accepted)
		for conn := range accepted {
			_ = conn.Close()
		}
	})

	client, err := messaging.NewClient(listener.Addr().String())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(client.Close)

	budget, giveUp := context.WithTimeout(context.Background(), publishBudget)
	defer giveUp()
	answered := make(chan error, 1)
	go func() { answered <- client.Publish(budget, "acme.tasks", []byte("key"), []byte("value")) }()

	select {
	case err := <-answered:
		if err == nil {
			t.Fatal("the publish said it landed, and nothing was listening to take it")
		}
	case <-time.After(20 * publishBudget):
		t.Fatal("the publish never came back, so a budget on it means nothing")
	}
}

// publishBudget is what the test gives the publish above. The control plane gives an export five
// seconds; this only has to be long enough to be a budget rather than an instant refusal.
const publishBudget = time.Second
