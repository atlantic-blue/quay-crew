package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/secrets"
	"github.com/atlantic-blue/krewe/internal/store"
)

// aSession is a system with one session in it, since every case here needs the same three steps first.
func aSession(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, string) {
	t.Helper()
	client, _ := aSessionWatchingTheModel(t)
	return client, sessionOf(t, client)
}

// aSessionWatchingTheModel is the same system, keeping hold of the model double, for the case that has
// to see what a later task was actually run with.
func aSessionWatchingTheModel(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, *model.FakeRunner) {
	t.Helper()
	runner := &model.FakeRunner{Reply: "ok"}
	client := testClientWith(t, controlplane.Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "task", "hello")
	return client, runner
}

// sessionOf is the id of the one session the system has, which is what a session scoped command takes,
// the same as attach and tasks.
func sessionOf(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) string {
	t.Helper()
	return onlySession(t, client).GetId()[:8]
}

// addressOf is the same session written as an address, which is what dispatch takes. Without the
// third level dispatch starts a new conversation rather than continuing this one.
func addressOf(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) string {
	t.Helper()
	return "me/house-bills/" + onlySession(t, client).GetHandle()[:8]
}

func onlySession(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) *quaycrewv1.Session {
	t.Helper()
	listed, err := client.ListSessions(context.Background(), &quaycrewv1.ListSessionsRequest{})
	if err != nil || len(listed.GetSessions()) != 1 {
		t.Fatalf("want exactly one session, got %d (%v)", len(listed.GetSessions()), err)
	}
	return listed.GetSessions()[0]
}

// The one that matters: setting the mode is worth nothing unless the next task runs in it. Every
// case above stops at what the system reports about itself, and a command that recorded the mode
// without it reaching the model would pass all of them.
func TestTheNextTaskRunsInTheModeThatWasSet(t *testing.T) {
	client, runner := aSessionWatchingTheModel(t)
	session := sessionOf(t, client)

	if was := runner.LastReq.PermissionMode; was != model.PermissionAcceptEdits {
		t.Fatalf("the first task ran in %q, want the mode a session is born in", was)
	}

	mustRun(t, client, "mode", session, "dangerous")
	mustRun(t, client, "task", addressOf(t, client), "and again")

	if was := runner.LastReq.PermissionMode; was != model.PermissionBypass {
		t.Fatalf("the task after the change ran in %q, want %q", was, model.PermissionBypass)
	}
}

// refusalOf runs one invocation expecting it to fail, for the cases that are about the refusal.
func refusalOf(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, args ...string) error {
	t.Helper()
	var out bytes.Buffer
	err := run(context.Background(), client, args, &out, "")
	if err == nil {
		t.Fatalf("krewe %s was accepted, and should not have been: %q", strings.Join(args, " "), out.String())
	}
	return err
}

// A session is born in edits, and the point of reading is finding that out without changing it.
func TestModeSaysWhatASessionRunsInWithoutChangingIt(t *testing.T) {
	client, session := aSession(t)

	said := mustRun(t, client, "mode", session)
	if !strings.Contains(said, "runs in edits") {
		t.Fatalf("a fresh session does not report the mode it was born in: %q", said)
	}
	if strings.Contains(said, "now runs") {
		t.Errorf("reading the mode reported a change: %q", said)
	}
	if again := mustRun(t, client, "mode", session); !strings.Contains(again, "runs in edits") {
		t.Errorf("reading the mode changed it: %q", again)
	}
}

func TestModeSetsWhatASessionRunsIn(t *testing.T) {
	client, session := aSession(t)

	set := mustRun(t, client, "mode", session, "dangerous")
	if !strings.Contains(set, "now runs in dangerous") {
		t.Fatalf("setting the mode did not say so: %q", set)
	}
	// The state, not the answer. A command that reports a change it did not make is the failure
	// this case exists to catch.
	if said := mustRun(t, client, "mode", session); !strings.Contains(said, "runs in dangerous") {
		t.Fatalf("the session did not keep the mode it was given: %q", said)
	}
}

// The console prints "edits" and "dangerous" in its listing, and the protocol says acceptEdits and
// bypassPermissions. Somebody reading one and typing into the other should not be refused.
func TestModeTakesTheWordTheOperatorSawAndTheOneTheModelUses(t *testing.T) {
	for _, spelling := range []string{"dangerous", "bypassPermissions", "BYPASSPERMISSIONS"} {
		client, session := aSession(t)
		if set := mustRun(t, client, "mode", session, spelling); !strings.Contains(set, "dangerous") {
			t.Errorf("%q was not taken as the dangerous mode: %q", spelling, set)
		}
	}
	client, session := aSession(t)
	if set := mustRun(t, client, "mode", session, "plan"); !strings.Contains(set, "now runs in plan") {
		t.Errorf("plan was not taken: %q", set)
	}
}

func TestAModeThatDoesNotExistIsRefusedNamingTheOnesThatDo(t *testing.T) {
	client, session := aSession(t)

	err := refusalOf(t, client, "mode", session, "yolo")
	for _, want := range []string{"yolo", "plan", "edits", "dangerous"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %s", want, err)
		}
	}
	// Refused, and nothing moved underneath it.
	if said := mustRun(t, client, "mode", session); !strings.Contains(said, "runs in edits") {
		t.Errorf("a refused mode still changed the session: %q", said)
	}
}

func TestModeWithNoSessionSaysHowToUseIt(t *testing.T) {
	client, _ := aSession(t)

	err := refusalOf(t, client, "mode")
	for _, want := range []string{"krewe mode <session>", "plan", "edits", "dangerous"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the usage does not mention %q: %s", want, err)
		}
	}
}

func TestASessionThatDoesNotExistIsRefused(t *testing.T) {
	client, _ := aSession(t)

	var out bytes.Buffer
	if err := run(context.Background(), client, []string{"mode", "ffffffff", "dangerous"}, &out, ""); err == nil {
		t.Fatalf("a session that does not exist was given a mode: %q", out.String())
	}
}
