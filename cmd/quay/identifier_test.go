package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

// A listing prints two identifiers for every session, the id and the handle, and it prints an address
// above them. Every one of those is a thing the operator can read off the screen and type back, so
// every one of them has to reach the session. These cases type each form at a session scoped command.

func TestASessionScopedCommandTakesTheIdTheListingPrints(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)
	id := onlySession(t, client).GetId()[:8]

	said := mustRun(t, client, "mode", id)

	if !strings.Contains(said, "runs in") {
		t.Fatalf("quay mode %s said %q", id, said)
	}
}

func TestASessionScopedCommandTakesTheHandleTheListingPrints(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)
	handle := onlySession(t, client).GetHandle()[:8]

	said := mustRun(t, client, "mode", handle)

	if !strings.Contains(said, "runs in") {
		t.Fatalf("quay mode %s said %q", handle, said)
	}
}

func TestASessionScopedCommandTakesAnAddress(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)
	address := addressOf(t, client)

	said := mustRun(t, client, "mode", address)

	if !strings.Contains(said, "runs in") {
		t.Fatalf("quay mode %s said %q", address, said)
	}
}

// The history command shares the resolver, so it takes the same forms. A fix that only reached mode
// would leave the operator refused by the command that reads what a session did.
func TestTasksTakesTheHandleAndTheAddressToo(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)
	session := onlySession(t, client)

	for _, typed := range []string{session.GetId()[:8], session.GetHandle()[:8], addressOf(t, client)} {
		said := mustRun(t, client, "tasks", typed)
		if !strings.Contains(said, "hello") {
			t.Fatalf("quay tasks %s said %q, want the task that was dispatched", typed, said)
		}
	}
}

// The way off the old word, not only the way onto the new one.
//
// quay tasks is in fingers, in scripts and in notes. A command that simply stops existing reads as
// the tool being broken, and the refusal has to name what to type instead or it is no better.
func TestTheOldTurnsCommandIsRefusedAndNamesTheNewOne(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)
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

// The way off thread is tested here rather than left to the rename, because a word that is still in
// scripts and in notes has to be refused by name: a command that silently stops existing reads as
// the tool being broken.
func TestTheOldThreadsCommandIsRefusedAndNamesTheNewOne(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)
	for _, typed := range []string{"threads", "thread"} {
		err := run(context.Background(), client, []string{typed, "anything"}, io.Discard, "")
		if err == nil {
			t.Fatalf("quay %s was accepted, and it no longer does anything", typed)
		}
		if !strings.Contains(err.Error(), "quay sessions") {
			t.Fatalf("the refusal for quay %s does not say what to type instead: %v", typed, err)
		}
	}
}

// The flag the address replaced keeps being refused under its old name too, for the same reason.
func TestTheOldThreadFlagIsRefusedAndNamesTheAddress(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)
	err := run(context.Background(), client, []string{"dispatch", "--thread", "x", "hello"}, io.Discard, "")
	if err == nil {
		t.Fatal("--thread was accepted, and it no longer does anything")
	}
	if !strings.Contains(err.Error(), "an address names the session") {
		t.Fatalf("the refusal for --thread does not say what to type instead: %v", err)
	}
}

// Setting the mode through the handle has to reach the model, not only the store. Reading it back
// from the crew would pass on a command that recorded the mode and never applied it.
func TestTheNextTaskRunsInTheModeSetThroughTheHandle(t *testing.T) {
	client, runner := aSessionWatchingTheModel(t)
	handle := onlySession(t, client).GetHandle()[:8]

	mustRun(t, client, "mode", handle, "dangerous")
	mustRun(t, client, "dispatch", addressOf(t, client), "and again")

	if was := runner.LastReq.PermissionMode; was != "bypassPermissions" {
		t.Fatalf("the task after the change ran in %q, want bypassPermissions", was)
	}
}

// A refusal that names only what was typed leaves the operator guessing which identifier the command
// wanted. It has to say what the crew does hold.
func TestARefusedSessionNamesTheSessionsThatExist(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)
	session := onlySession(t, client)

	err := refused(t, client, "mode", "ffffffff")

	said := err.Error()
	if !strings.Contains(said, session.GetHandle()[:8]) && !strings.Contains(said, session.GetId()[:8]) {
		t.Fatalf("the refusal %q names neither identifier of the one session that exists", said)
	}
}

// Attach shares the resolver, and it is the command where a refusal costs most: the operator is
// looking at a session on their screen that will not open. A label is set first because that is what
// takes the handle off the screen, leaving the id as the only identifier they can read.
func TestAttachTakesEveryIdentifierTheListingPrints(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)
	session := onlySession(t, client)
	mustRun(t, client, "label", session.GetId()[:8], "the bills")

	for _, typed := range []string{
		session.GetId(), session.GetId()[:8],
		session.GetHandle(), session.GetHandle()[:8],
		"me/house-bills/" + session.GetHandle()[:8],
		"me/house-bills/" + session.GetId()[:8],
	} {
		reached, err := resolveSession(context.Background(), client, typed)
		if err != nil {
			t.Fatalf("quay attach %s was refused: %v", typed, err)
		}
		if reached != session.GetId() {
			t.Fatalf("quay attach %s reached %s, want %s", typed, reached, session.GetId())
		}
	}
}

// The listing prints the id in its own column, so the address form has to take it. Every session
// scoped command shares this, which is what makes the id typeable everywhere rather than only where
// somebody remembered to allow it.
func TestASessionScopedCommandTakesAnAddressCarryingTheId(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)
	session := onlySession(t, client)
	address := "me/house-bills/" + session.GetId()[:8]

	said := mustRun(t, client, "mode", address)

	if !strings.Contains(said, "runs in") {
		t.Fatalf("quay mode %s said %q", address, said)
	}
}
