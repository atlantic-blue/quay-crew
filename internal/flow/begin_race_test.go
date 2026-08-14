package flow

import (
	"testing"
)

// Begin answers with the run and drives it behind that answer, so the caller holds one value while a
// goroutine advances another. A struct copy is not enough: State and Attempts are maps, so the copy
// shares them, and the caller's run is written to while it is being read.
//
// What it looked like from outside was `quay flow start` failing with "grpc: error while marshaling:
// size mismatch, calculated=110, measured=169". The response was being marshaled while the goroutine
// wrote to a map inside it, so the message grew between the two passes protobuf makes over it. It
// failed about one run in six, and the same shape can corrupt any read of a run in flight.
//
// This case is about the copy rather than about the timing, because a test that raced would be a test
// that passes most of the time.
func TestACopiedRunSharesNothingWritable(t *testing.T) {
	original := Run{
		ID:       "run-1",
		State:    map[string]string{"trigger": "the operator"},
		Attempts: map[string]int{"greet": 1},
	}

	copied := original.copy()

	copied.State["reply"] = "written by the goroutine"
	copied.Attempts["greet"] = 2

	if _, written := original.State["reply"]; written {
		t.Error("writing the copy's state wrote the original's, so the answer changes under whoever is reading it")
	}
	if original.Attempts["greet"] != 1 {
		t.Errorf("writing the copy's attempts wrote the original's: %d", original.Attempts["greet"])
	}
}

// A copy of a run that carries no maps must not hand back nil ones, because the run that gets driven
// writes into them on its first transition.
func TestACopiedRunAlwaysHasSomewhereToWrite(t *testing.T) {
	copied := Run{ID: "run-2"}.copy()

	copied.State["trigger"] = "a schedule"
	copied.Attempts["greet"] = 1

	if len(copied.State) != 1 || len(copied.Attempts) != 1 {
		t.Fatalf("a copied run has state %v and attempts %v", copied.State, copied.Attempts)
	}
}
