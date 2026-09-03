package controlplane_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// testWait is what these tests give a budget that is measured in a minute in production. Long enough
// that a loaded machine does not trip it, short enough that watching one run out is not a wait.
const testWait = 300 * time.Millisecond

// waitingSystem is a control plane with short budgets, so a test can watch one run out.
func waitingSystem(cfg controlplane.Config) *controlplane.Server {
	if cfg.Store == nil {
		cfg.Store = store.NewMemory()
	}
	if cfg.Runner == nil {
		cfg.Runner = &model.FakeRunner{Reply: "done"}
	}
	if cfg.Provider == nil {
		cfg.Provider = &sandbox.FakeProvider{}
	}
	if cfg.Secrets == nil {
		cfg.Secrets = secrets.NewMemory()
	}
	cfg.StartWait = testWait
	return controlplane.NewServer(cfg)
}

// A sandbox that never starts: the dispatch has to end, and it has to say which wait it gave up on,
// because "the dispatch failed" sends the reader to the whole path.
func TestASandboxThatNeverStartsFailsTheTaskByName(t *testing.T) {
	provider := &sandbox.FakeProvider{Hold: make(chan struct{})}
	server := waitingSystem(controlplane.Config{Provider: provider})
	_, project := newProject(t, server)

	_, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "read the repository",
	})
	if err == nil {
		t.Fatal("the dispatch answered, and no sandbox was ever made for it")
	}
	if !strings.Contains(err.Error(), "the sandbox to be created") {
		t.Fatalf("the system said %q, and it does not say what it waited for", err)
	}

	// And the row says so too. A row left idle with no task reads as a session waiting for work.
	session := oneSession(t, server)
	if session.GetStatus() != controlplane.StatusFailed {
		t.Fatalf("the session reads %q, want failed", session.GetStatus())
	}
	tasks, err := server.ListTasks(context.Background(), &quaycrewv1.ListTasksRequest{Session: session.GetId()})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks.GetTasks()) != 1 || tasks.GetTasks()[0].GetFailure() == "" {
		t.Fatalf("%d tasks recorded, and nothing says why the dispatch did not start", len(tasks.GetTasks()))
	}
}

// The system starts one sandbox at a time. That was a mutex held across the whole start, so a start
// that never ended held every later dispatch behind it with no way out, and nothing said so.
func TestADispatchBehindAStartThatNeverEndsSaysSoAndComesBack(t *testing.T) {
	written := &bytes.Buffer{}
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(written, nil)))
	defer slog.SetDefault(restore)

	provider := &sandbox.FakeProvider{Hold: make(chan struct{})}
	server := waitingSystem(controlplane.Config{Provider: provider})
	_, project := newProject(t, server)

	held := make(chan error, 1)
	go func() {
		_, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
			Project: project, Handle: "the-first-session", Text: "first",
		})
		held <- err
	}()
	// Wait for the first dispatch to be inside the provider, so the second is genuinely behind it
	// rather than racing it.
	waitFor(t, func() bool { return provider.Asked() > 0 })

	answered := make(chan error, 1)
	go func() {
		_, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
			Project: project, Handle: "the-second-session", Text: "second",
		})
		answered <- err
	}()

	select {
	case err := <-answered:
		if err == nil {
			t.Fatal("the second dispatch answered, and no sandbox was ever made for it")
		}
	case <-time.After(20 * testWait):
		t.Fatal("the second dispatch never came back: the start ahead of it is still holding it")
	}
	<-held

	if !strings.Contains(written.String(), "the sandbox start ahead of this one") {
		t.Fatalf("the log never says a dispatch was behind another start. It says:\n%s", written.String())
	}
}

// The fault was silent, which is what made it cost an hour. The log now says a dispatch arrived and
// what it is waiting on, so a hang is a wait with no end to it in the log rather than nothing at all.
func TestTheLogSaysADispatchArrivedAndWhatItWaitsFor(t *testing.T) {
	written := &bytes.Buffer{}
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(written, nil)))
	defer slog.SetDefault(restore)

	server := waitingSystem(controlplane.Config{})
	_, project := newProject(t, server)
	if _, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "hello",
	}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	said := written.String()
	for _, line := range []string{"a dispatch arrived", "the system is waiting", "the sandbox to be created"} {
		if !strings.Contains(said, line) {
			t.Fatalf("the log does not say %q. It says:\n%s", line, said)
		}
	}
}

// oneSession is the system's single session, which is what a test that dispatched once asserts on.
func oneSession(t *testing.T, server *controlplane.Server) *quaycrewv1.Session {
	t.Helper()
	listed, err := server.ListSessions(context.Background(), &quaycrewv1.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(listed.GetSessions()) != 1 {
		t.Fatalf("the system has %d sessions, want one", len(listed.GetSessions()))
	}
	return listed.GetSessions()[0]
}

// waitFor polls until want is true, and fails rather than hanging when it never is.
func waitFor(t *testing.T, want func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * testWait)
	for time.Now().Before(deadline) {
		if want() {
			return
		}
		time.Sleep(testWait / 20)
	}
	t.Fatal("the system never reached the state this test is about")
}
