package controlplane

import (
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// A console draws a screen. A turn takes as long as the work takes. The console used to wait for one
// anyway, hold every key while it waited, give up at thirty seconds and leave behind a thread with a
// container, a row, and no conversation in it. These are the cases that stop that coming back.

func detachServer(t *testing.T, runner model.Runner) (*Server, string) {
	t.Helper()
	server := NewServer(Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	ctx := context.Background()
	workspace, err := server.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return server, project.GetProject().GetId()
}

func threadByID(t *testing.T, server *Server, id string) *quaycrewv1.Thread {
	t.Helper()
	got, err := server.GetThread(context.Background(), &quaycrewv1.GetThreadRequest{Id: id})
	if err != nil {
		t.Fatalf("GetThread: %v", err)
	}
	return got.GetThread()
}

// The whole point: the answer comes back while the model is still working. The gate holds the turn
// open, so a Dispatch that returns has provably not waited for it.
func TestADetachedDispatchAnswersWhileTheTurnIsStillRunning(t *testing.T) {
	runner := &model.FakeRunner{
		Reply: "done", SessionID: "model-1",
		Gate: make(chan struct{}), Started: make(chan struct{}),
	}
	server, project := detachServer(t, runner)

	resp, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "read the repository and tell me what it does", Detach: true,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if resp.GetId() == "" || resp.GetHandle() == "" {
		t.Fatalf("a detached dispatch has to name the thread it started: %+v", resp)
	}
	// There is no reply yet and saying there is one is worse than saying nothing: a caller would
	// print an empty answer as though the model had given it.
	if resp.GetReply() != "" {
		t.Fatalf("a detached dispatch cannot have a reply yet, got %q", resp.GetReply())
	}

	// The turn is genuinely under way, not merely un-started.
	select {
	case <-runner.Started:
	case <-time.After(5 * time.Second):
		t.Fatal("the detached turn never reached the runner")
	}

	// And the listing the console draws next says the thread is busy. Reading idle here is what
	// invites the operator to type into a thread whose first turn has not landed.
	if got := threadByID(t, server, resp.GetId()).GetStatus(); got != StatusRunning {
		t.Fatalf("while the turn runs the thread should read %q, got %q", StatusRunning, got)
	}

	close(runner.Gate)
	server.turning.Wait()

	if got := threadByID(t, server, resp.GetId()).GetStatus(); got != StatusIdle {
		t.Fatalf("once the turn lands the thread should read %q, got %q", StatusIdle, got)
	}
}

// The turn has to outlive the call that asked for it. The console answers a keystroke and moves on,
// so its context is cancelled the moment it does; a turn carrying that context is killed by the very
// thing that started it, which is the thirty second deadline all over again with a smaller number.
func TestADetachedTurnOutlivesTheCallerThatAskedForIt(t *testing.T) {
	runner := &model.FakeRunner{
		Reply: "done", SessionID: "model-1",
		Gate: make(chan struct{}), Started: make(chan struct{}),
	}
	server, project := detachServer(t, runner)

	ctx, cancel := context.WithCancel(context.Background())
	resp, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Text: "a long job", Detach: true,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	select {
	case <-runner.Started:
	case <-time.After(5 * time.Second):
		t.Fatal("the detached turn never reached the runner")
	}
	// The caller goes away, exactly as a console command does the instant it has its answer.
	cancel()

	close(runner.Gate)
	server.turning.Wait()

	thread := threadByID(t, server, resp.GetId())
	if thread.GetStatus() != StatusIdle {
		t.Fatalf("the turn should have landed despite the caller leaving, thread reads %q", thread.GetStatus())
	}
	turns, err := server.ListTurns(context.Background(), &quaycrewv1.ListTurnsRequest{Thread: resp.GetId()})
	if err != nil {
		t.Fatalf("ListTurns: %v", err)
	}
	if len(turns.GetTurns()) != 1 || turns.GetTurns()[0].GetReply() != "done" {
		t.Fatalf("the reply should be recorded on the thread, got %+v", turns.GetTurns())
	}
}

// A dispatch that does not detach still waits and still answers with the reply. The console changing
// is not licence to change what the command line and the flow engine get.
func TestADispatchThatDoesNotDetachStillWaitsForTheReply(t *testing.T) {
	runner := &model.FakeRunner{Reply: "done", SessionID: "model-1"}
	server, project := detachServer(t, runner)

	resp, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "hello",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if resp.GetReply() != "done" {
		t.Fatalf("a waited dispatch answers with the reply, got %q", resp.GetReply())
	}
	if got := threadByID(t, server, resp.GetId()).GetStatus(); got != StatusIdle {
		t.Fatalf("want %q after a waited turn, got %q", StatusIdle, got)
	}
}

// Every failure used to read "the model did not complete the turn", so a deadline, a crash and a
// refusal were one line with nothing to act on in it. That sentence is what sent this bug hunt to the
// wrong place.
func TestAFailedTurnRecordsWhatActuallyWentWrong(t *testing.T) {
	runner := &model.FakeRunner{Err: context.DeadlineExceeded}
	server, project := detachServer(t, runner)

	resp, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "a long job", Detach: true,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	server.turning.Wait()

	if got := threadByID(t, server, resp.GetId()).GetStatus(); got != StatusFailed {
		t.Fatalf("want %q after a failed turn, got %q", StatusFailed, got)
	}
	turns, err := server.ListTurns(context.Background(), &quaycrewv1.ListTurnsRequest{Thread: resp.GetId()})
	if err != nil {
		t.Fatalf("ListTurns: %v", err)
	}
	if len(turns.GetTurns()) != 1 {
		t.Fatalf("want one recorded turn, got %d", len(turns.GetTurns()))
	}
	failure := turns.GetTurns()[0].GetFailure()
	if strings.Contains(failure, "the model did not complete the turn") {
		t.Fatalf("the failure still says nothing about the cause: %q", failure)
	}
	if !strings.Contains(failure, "past the time the caller allowed") {
		t.Fatalf("a turn killed by a deadline should say so, got %q", failure)
	}
}

// A turn runs in this process and nothing survives it going down, so a row still saying running on
// the way up is a turn that died with the last process. Left alone it reads as a conversation that
// has been thinking for three days.
func TestARestartSettlesAThreadLeftRunning(t *testing.T) {
	runner := &model.FakeRunner{
		Reply: "done", Gate: make(chan struct{}), Started: make(chan struct{}),
	}
	server, project := detachServer(t, runner)

	resp, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "a long job", Detach: true,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	select {
	case <-runner.Started:
	case <-time.After(5 * time.Second):
		t.Fatal("the detached turn never reached the runner")
	}

	// The crew comes up and finds the row mid turn, which is all a restart can see.
	server.SettleTurns(context.Background())

	thread := threadByID(t, server, resp.GetId())
	if thread.GetStatus() != StatusFailed {
		t.Fatalf("a thread left running has to be settled to %q, got %q", StatusFailed, thread.GetStatus())
	}
	turns, err := server.ListTurns(context.Background(), &quaycrewv1.ListTurnsRequest{Thread: resp.GetId()})
	if err != nil {
		t.Fatalf("ListTurns: %v", err)
	}
	if len(turns.GetTurns()) == 0 || !strings.Contains(turns.GetTurns()[0].GetFailure(), "restarted") {
		t.Fatalf("the settled turn should say the crew restarted, got %+v", turns.GetTurns())
	}

	close(runner.Gate)
	server.turning.Wait()
}

// Settling is for threads that were mid turn. A thread sitting idle is not one, and marking it failed
// on every boot would turn a quiet crew into a wall of failures.
func TestSettlingLeavesAnIdleThreadAlone(t *testing.T) {
	runner := &model.FakeRunner{Reply: "done"}
	server, project := detachServer(t, runner)

	resp, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "hello",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	server.SettleTurns(context.Background())

	if got := threadByID(t, server, resp.GetId()).GetStatus(); got != StatusIdle {
		t.Fatalf("an idle thread should survive a restart as %q, got %q", StatusIdle, got)
	}
}
