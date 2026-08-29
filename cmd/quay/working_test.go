package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// What the operator actually does while a session works: type quay sessions, then quay tasks. Both
// screens used to say a session had been asked nothing, for as long as the job took.
//
// The task is held open rather than timed. What is being tested is what is true while one runs, and a
// test that waits a duration for that passes on a slow machine by accident.
func TestWhatTheOperatorSeesWhileATaskRuns(t *testing.T) {
	runner := &model.FakeRunner{
		Reply: "the electricity bill is due on the ninth",
		Gate:  make(chan struct{}), Started: make(chan struct{}),
	}
	client := testClientWith(t, controlplane.Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	// The caller waits for the answer, which is the path that recorded nothing.
	dispatched := make(chan error, 1)
	go func() {
		var out bytes.Buffer
		dispatched <- run(context.Background(), client,
			[]string{"task", "when is the electricity bill due"}, &out, "")
	}()
	select {
	case <-runner.Started:
	case <-time.After(5 * time.Second):
		t.Fatal("the task never reached the model")
	}

	listed := mustRun(t, client, "sessions")
	if !strings.Contains(listed, "running") {
		t.Fatalf("the listing does not say the session is working:\n%s", listed)
	}

	// The identifier is copied off the screen, the way the operator copies it.
	identifier := identifierFromListing(t, listed)
	history := mustRun(t, client, "task", "list", identifier)
	if !strings.Contains(history, "when is the electricity bill due") {
		t.Fatalf("the history does not say what the session was asked:\n%s", history)
	}
	if !strings.Contains(history, "still running") {
		t.Fatalf("the history does not say the task is still working:\n%s", history)
	}

	close(runner.Gate)
	if err := <-dispatched; err != nil {
		t.Fatalf("the dispatch failed: %v", err)
	}

	// And it is the same task, with its answer in it, rather than a second one underneath.
	landed := mustRun(t, client, "task", "list", identifier)
	if strings.Contains(landed, "still running") {
		t.Fatalf("the task still reads as working after it landed:\n%s", landed)
	}
	if !strings.Contains(landed, "the electricity bill is due on the ninth") {
		t.Fatalf("the history does not carry the answer:\n%s", landed)
	}
	if asked := strings.Count(landed, "when is the electricity bill due"); asked != 1 {
		t.Fatalf("the prompt appears %d times, want 1: the landing wrote a second task\n%s", asked, landed)
	}
	if got := onlySession(t, client).GetStatus(); got != "idle" {
		t.Fatalf("the session reads %q once its task landed, want idle", got)
	}
}

// identifierFromListing takes the first column of the first session in a listing, which is what the
// operator has on screen and types back.
func identifierFromListing(t *testing.T, listed string) string {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(listed), "\n")
	if len(lines) < 2 {
		t.Fatalf("the listing has no session in it:\n%s", listed)
	}
	fields := strings.Fields(lines[1])
	if len(fields) == 0 {
		t.Fatalf("the listing's first row is empty:\n%s", listed)
	}
	return fields[0]
}

// A session with nothing in it must not read as one that is working, or the mark means nothing.
func TestASessionWithNoTaskInItReadsIdle(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "task", "hello")

	if got := onlySession(t, client).GetStatus(); got != "idle" {
		t.Fatalf("a session between tasks reads %q, want idle", got)
	}
	if listed := mustRun(t, client, "sessions"); strings.Contains(listed, "running") {
		t.Fatalf("the listing calls a finished session working:\n%s", listed)
	}
}
