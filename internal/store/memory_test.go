package store_test

import (
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/atlantic-blue/quay-crew/internal/store/storetest"
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
