package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// aThread is a crew with one thread in it, since every case here needs the same three steps first.
func aThread(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, string) {
	t.Helper()
	client, _ := aThreadWatchingTheModel(t)
	return client, threadOf(t, client)
}

// aThreadWatchingTheModel is the same crew, keeping hold of the model double, for the case that has
// to see what a later turn was actually run with.
func aThreadWatchingTheModel(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, *model.FakeRunner) {
	t.Helper()
	runner := &model.FakeRunner{Reply: "ok"}
	client := testClientWith(t, controlplane.Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "dispatch", "hello")
	return client, runner
}

// threadOf is the id of the one thread the crew has, which is what a thread scoped command takes,
// the same as attach and turns.
func threadOf(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) string {
	t.Helper()
	return onlyThread(t, client).GetId()[:8]
}

// addressOf is the same thread written as an address, which is what dispatch takes. Without the
// third level dispatch starts a new conversation rather than continuing this one.
func addressOf(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) string {
	t.Helper()
	return "me/house-bills/" + onlyThread(t, client).GetHandle()[:8]
}

func onlyThread(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) *quaycrewv1.Thread {
	t.Helper()
	listed, err := client.ListThreads(context.Background(), &quaycrewv1.ListThreadsRequest{})
	if err != nil || len(listed.GetThreads()) != 1 {
		t.Fatalf("want exactly one thread, got %d (%v)", len(listed.GetThreads()), err)
	}
	return listed.GetThreads()[0]
}

// The one that matters: setting the mode is worth nothing unless the next turn runs in it. Every
// case above stops at what the crew reports about itself, and a command that recorded the mode
// without it reaching the model would pass all of them.
func TestTheNextTurnRunsInTheModeThatWasSet(t *testing.T) {
	client, runner := aThreadWatchingTheModel(t)
	thread := threadOf(t, client)

	if was := runner.LastReq.PermissionMode; was != model.PermissionAcceptEdits {
		t.Fatalf("the first turn ran in %q, want the mode a thread is born in", was)
	}

	mustRun(t, client, "mode", thread, "dangerous")
	mustRun(t, client, "dispatch", addressOf(t, client), "and again")

	if was := runner.LastReq.PermissionMode; was != model.PermissionBypass {
		t.Fatalf("the turn after the change ran in %q, want %q", was, model.PermissionBypass)
	}
}

// refusalOf runs one invocation expecting it to fail, for the cases that are about the refusal.
func refusalOf(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, args ...string) error {
	t.Helper()
	var out bytes.Buffer
	err := run(context.Background(), client, args, &out, "")
	if err == nil {
		t.Fatalf("quay %s was accepted, and should not have been: %q", strings.Join(args, " "), out.String())
	}
	return err
}

// A thread is born in edits, and the point of reading is finding that out without changing it.
func TestModeSaysWhatAThreadRunsInWithoutChangingIt(t *testing.T) {
	client, thread := aThread(t)

	said := mustRun(t, client, "mode", thread)
	if !strings.Contains(said, "runs in edits") {
		t.Fatalf("a fresh thread does not report the mode it was born in: %q", said)
	}
	if strings.Contains(said, "now runs") {
		t.Errorf("reading the mode reported a change: %q", said)
	}
	if again := mustRun(t, client, "mode", thread); !strings.Contains(again, "runs in edits") {
		t.Errorf("reading the mode changed it: %q", again)
	}
}

func TestModeSetsWhatAThreadRunsIn(t *testing.T) {
	client, thread := aThread(t)

	set := mustRun(t, client, "mode", thread, "dangerous")
	if !strings.Contains(set, "now runs in dangerous") {
		t.Fatalf("setting the mode did not say so: %q", set)
	}
	// The state, not the answer. A command that reports a change it did not make is the failure
	// this case exists to catch.
	if said := mustRun(t, client, "mode", thread); !strings.Contains(said, "runs in dangerous") {
		t.Fatalf("the thread did not keep the mode it was given: %q", said)
	}
}

// The console prints "edits" and "dangerous" in its listing, and the protocol says acceptEdits and
// bypassPermissions. Somebody reading one and typing into the other should not be refused.
func TestModeTakesTheWordTheOperatorSawAndTheOneTheModelUses(t *testing.T) {
	for _, spelling := range []string{"dangerous", "bypassPermissions", "BYPASSPERMISSIONS"} {
		client, thread := aThread(t)
		if set := mustRun(t, client, "mode", thread, spelling); !strings.Contains(set, "dangerous") {
			t.Errorf("%q was not taken as the dangerous mode: %q", spelling, set)
		}
	}
	client, thread := aThread(t)
	if set := mustRun(t, client, "mode", thread, "plan"); !strings.Contains(set, "now runs in plan") {
		t.Errorf("plan was not taken: %q", set)
	}
}

func TestAModeThatDoesNotExistIsRefusedNamingTheOnesThatDo(t *testing.T) {
	client, thread := aThread(t)

	err := refusalOf(t, client, "mode", thread, "yolo")
	for _, want := range []string{"yolo", "plan", "edits", "dangerous"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %s", want, err)
		}
	}
	// Refused, and nothing moved underneath it.
	if said := mustRun(t, client, "mode", thread); !strings.Contains(said, "runs in edits") {
		t.Errorf("a refused mode still changed the thread: %q", said)
	}
}

func TestModeWithNoThreadSaysHowToUseIt(t *testing.T) {
	client, _ := aThread(t)

	err := refusalOf(t, client, "mode")
	for _, want := range []string{"quay mode <thread>", "plan", "edits", "dangerous"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the usage does not mention %q: %s", want, err)
		}
	}
}

func TestAThreadThatDoesNotExistIsRefused(t *testing.T) {
	client, _ := aThread(t)

	var out bytes.Buffer
	if err := run(context.Background(), client, []string{"mode", "ffffffff", "dangerous"}, &out, ""); err == nil {
		t.Fatalf("a thread that does not exist was given a mode: %q", out.String())
	}
}
