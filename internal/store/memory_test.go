package store_test

import (
	"context"
	"testing"

	"github.com/atlantic-blue/krewe/internal/store"
	"github.com/atlantic-blue/krewe/internal/store/storetest"
)

// TestMemoryConformance holds the in memory store to the same contract as the Postgres one.
//
// "Reopening" an in memory store means handing out the same instance again, which proves the store
// keeps the data rather than the caller keeping it. Real durability across a process is what the
// Postgres integration test proves.
func TestMemoryConformance(t *testing.T) {
	storetest.RunConformance(t, func(*testing.T) storetest.Opener {
		memory := store.NewMemory()
		return func(*testing.T) store.Store { return memory }
	})
}

// A probe that answered without writing would make a health check agree with a system that cannot
// write, which is the fault the check exists for, so the count is what is asserted here.
func TestAProbeOnTheMemoryStoreWrites(t *testing.T) {
	memory := store.NewMemory()
	if got := memory.Probes(); got != 0 {
		t.Fatalf("a fresh store says it took %d probes", got)
	}
	if err := memory.Probe(context.Background()); err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got := memory.Probes(); got != 1 {
		t.Fatalf("the store took %d probes, want 1", got)
	}
}
