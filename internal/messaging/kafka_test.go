package messaging_test

import (
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
