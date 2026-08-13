package main

import (
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
func TestTurnsTakesTheHandleAndTheAddressToo(t *testing.T) {
	client, _ := aThreadWatchingTheModel(t)
	thread := onlyThread(t, client)

	for _, typed := range []string{thread.GetId()[:8], thread.GetHandle()[:8], addressOf(t, client)} {
		said := mustRun(t, client, "turns", typed)
		if !strings.Contains(said, "hello") {
			t.Fatalf("quay turns %s said %q, want the turn that was dispatched", typed, said)
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
