package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

// A listing prints two identifiers for every thread, the id and the handle, and it prints an address
// above them. Every one of those is a thing the operator can read off the screen and type back, so
// every one of them has to reach the thread. These cases type each form at a thread scoped command.

func TestAThreadScopedCommandTakesTheIdTheListingPrints(t *testing.T) {
	client, _ := aThreadWatchingTheModel(t)
	id := onlyThread(t, client).GetId()[:8]

	said := mustRun(t, client, "mode", id)

	if !strings.Contains(said, "runs in") {
		t.Fatalf("quay mode %s said %q", id, said)
	}
}

func TestAThreadScopedCommandTakesTheHandleTheListingPrints(t *testing.T) {
	client, _ := aThreadWatchingTheModel(t)
	handle := onlyThread(t, client).GetHandle()[:8]

	said := mustRun(t, client, "mode", handle)

	if !strings.Contains(said, "runs in") {
		t.Fatalf("quay mode %s said %q", handle, said)
	}
}

func TestAThreadScopedCommandTakesAnAddress(t *testing.T) {
	client, _ := aThreadWatchingTheModel(t)
	address := addressOf(t, client)

	said := mustRun(t, client, "mode", address)

	if !strings.Contains(said, "runs in") {
		t.Fatalf("quay mode %s said %q", address, said)
	}
}

// The history command shares the resolver, so it takes the same forms. A fix that only reached mode
// would leave the operator refused by the command that reads what a thread did.
func TestTasksTakesTheHandleAndTheAddressToo(t *testing.T) {
	client, _ := aThreadWatchingTheModel(t)
	thread := onlyThread(t, client)

	for _, typed := range []string{thread.GetId()[:8], thread.GetHandle()[:8], addressOf(t, client)} {
		said := mustRun(t, client, "tasks", typed)
		if !strings.Contains(said, "hello") {
			t.Fatalf("quay tasks %s said %q, want the task that was dispatched", typed, said)
		}
	}
}

// The way off the old word, not only the way onto the new one.
//
// quay turns is in fingers, in scripts and in notes. A command that simply stops existing reads as
// the tool being broken, and the refusal has to name what to type instead or it is no better.
func TestTheOldTurnsCommandIsRefusedAndNamesTheNewOne(t *testing.T) {
	client, _ := aThreadWatchingTheModel(t)
	for _, typed := range []string{"turns", "turn"} {
		err := run(context.Background(), client, []string{typed, "anything"}, io.Discard, "")
		if err == nil {
			t.Fatalf("quay %s was accepted, and it no longer does anything", typed)
		}
		if !strings.Contains(err.Error(), "quay tasks") {
			t.Fatalf("the refusal for quay %s does not say what to type instead: %v", typed, err)
		}
	}
}

// Setting the mode through the handle has to reach the model, not only the store. Reading it back
// from the crew would pass on a command that recorded the mode and never applied it.
func TestTheNextTurnRunsInTheModeSetThroughTheHandle(t *testing.T) {
	client, runner := aThreadWatchingTheModel(t)
	handle := onlyThread(t, client).GetHandle()[:8]

	mustRun(t, client, "mode", handle, "dangerous")
	mustRun(t, client, "dispatch", addressOf(t, client), "and again")

	if was := runner.LastReq.PermissionMode; was != "bypassPermissions" {
		t.Fatalf("the turn after the change ran in %q, want bypassPermissions", was)
	}
}

// A refusal that names only what was typed leaves the operator guessing which identifier the command
// wanted. It has to say what the crew does hold.
func TestARefusedThreadNamesTheThreadsThatExist(t *testing.T) {
	client, _ := aThreadWatchingTheModel(t)
	thread := onlyThread(t, client)

	err := refused(t, client, "mode", "ffffffff")

	said := err.Error()
	if !strings.Contains(said, thread.GetHandle()[:8]) && !strings.Contains(said, thread.GetId()[:8]) {
		t.Fatalf("the refusal %q names neither identifier of the one thread that exists", said)
	}
}
