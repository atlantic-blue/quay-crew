package controlplane

import (
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// A console draws a screen. An exec takes as long as the job takes. The console used to wait for one
// anyway, hold every key while it waited, give up at thirty seconds and leave behind a session with a
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

func sessionByID(t *testing.T, server *Server, id string) *quaycrewv1.Session {
	t.Helper()
	got, err := server.GetSession(context.Background(), &quaycrewv1.GetSessionRequest{Id: id})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	return got.GetSession()
}

// The whole point: the answer comes back while the model is still working. The gate holds the exec
// open, so a Dispatch that returns has provably not waited for it.
func TestADetachedDispatchAnswersWhileTheExecIsStillRunning(t *testing.T) {
	runner := &model.FakeRunner{
		Reply: "done",
		Gate:  make(chan struct{}), Started: make(chan struct{}),
	}
	server, project := detachServer(t, runner)

	resp, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "read the repository and tell me what it does", Detach: true,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if resp.GetId() == "" || resp.GetHandle() == "" {
		t.Fatalf("a detached dispatch has to name the session it started: %+v", resp)
	}
	// There is no reply yet and saying there is one is worse than saying nothing: a caller would
	// print an empty answer as though the model had given it.
	if resp.GetReply() != "" {
		t.Fatalf("a detached dispatch cannot have a reply yet, got %q", resp.GetReply())
	}

	// The exec is genuinely under way, not merely un-started.
	select {
	case <-runner.Started:
	case <-time.After(5 * time.Second):
		t.Fatal("the detached exec never reached the runner")
	}

	// And the listing the console draws next says the session is busy. Reading idle here is what
	// invites the operator to type into a session whose first exec has not landed.
	if got := sessionByID(t, server, resp.GetId()).GetStatus(); got != StatusRunning {
		t.Fatalf("while the exec runs the session should read %q, got %q", StatusRunning, got)
	}

	close(runner.Gate)
	server.runningExecs.Wait()

	if got := sessionByID(t, server, resp.GetId()).GetStatus(); got != StatusIdle {
		t.Fatalf("once the exec lands the session should read %q, got %q", StatusIdle, got)
	}
}

// The exec has to outlive the call that asked for it. The console answers a keystroke and moves on,
// so its context is cancelled the moment it does; an exec carrying that context is killed by the very
// thing that started it, which is the thirty second deadline all over again with a smaller number.
func TestADetachedExecOutlivesTheCallerThatAskedForIt(t *testing.T) {
	runner := &model.FakeRunner{
		Reply: "done",
		Gate:  make(chan struct{}), Started: make(chan struct{}),
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
		t.Fatal("the detached exec never reached the runner")
	}
	// The caller goes away, exactly as a console command does the instant it has its answer.
	cancel()

	close(runner.Gate)
	server.runningExecs.Wait()

	session := sessionByID(t, server, resp.GetId())
	if session.GetStatus() != StatusIdle {
		t.Fatalf("the exec should have landed despite the caller leaving, session reads %q", session.GetStatus())
	}
	execs, err := server.ListExecs(context.Background(), &quaycrewv1.ListExecsRequest{Session: resp.GetId()})
	if err != nil {
		t.Fatalf("ListExecs: %v", err)
	}
	if len(execs.GetExecs()) != 1 || execs.GetExecs()[0].GetReply() != "done" {
		t.Fatalf("the reply should be recorded on the session, got %+v", execs.GetExecs())
	}
}

// A dispatch that does not detach still waits and still answers with the reply. The console changing
// is not licence to change what the command line and the flow engine get.
func TestADispatchThatDoesNotDetachStillWaitsForTheReply(t *testing.T) {
	runner := &model.FakeRunner{Reply: "done"}
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
	if got := sessionByID(t, server, resp.GetId()).GetStatus(); got != StatusIdle {
		t.Fatalf("want %q after a waited exec, got %q", StatusIdle, got)
	}
}

// Every failure used to read "the model did not complete the exec", so a deadline, a crash and a
// refusal were one line with nothing to act on in it. That sentence is what sent this bug hunt to the
// wrong place.
func TestAFailedExecRecordsWhatActuallyWentWrong(t *testing.T) {
	runner := &model.FakeRunner{Err: context.DeadlineExceeded}
	server, project := detachServer(t, runner)

	resp, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "a long job", Detach: true,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	server.runningExecs.Wait()

	if got := sessionByID(t, server, resp.GetId()).GetStatus(); got != StatusFailed {
		t.Fatalf("want %q after a failed exec, got %q", StatusFailed, got)
	}
	execs, err := server.ListExecs(context.Background(), &quaycrewv1.ListExecsRequest{Session: resp.GetId()})
	if err != nil {
		t.Fatalf("ListExecs: %v", err)
	}
	if len(execs.GetExecs()) != 1 {
		t.Fatalf("want one recorded exec, got %d", len(execs.GetExecs()))
	}
	failure := execs.GetExecs()[0].GetFailure()
	if strings.Contains(failure, "the model did not complete the exec") {
		t.Fatalf("the failure still says nothing about the cause: %q", failure)
	}
	if !strings.Contains(failure, "past the time the caller allowed") {
		t.Fatalf("an exec killed by a deadline should say so, got %q", failure)
	}
}

// An exec runs in this process and nothing survives it going down, so a row still saying running on
// the way up is an exec that died with the last process. Left alone it reads as a conversation that
// has been thinking for three days.
func TestARestartSettlesASessionLeftRunning(t *testing.T) {
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
		t.Fatal("the detached exec never reached the runner")
	}

	// The system comes up and finds the row mid exec, which is all a restart can see.
	server.SettleExecs(context.Background())

	session := sessionByID(t, server, resp.GetId())
	if session.GetStatus() != StatusFailed {
		t.Fatalf("a session left running has to be settled to %q, got %q", StatusFailed, session.GetStatus())
	}
	execs, err := server.ListExecs(context.Background(), &quaycrewv1.ListExecsRequest{Session: resp.GetId()})
	if err != nil {
		t.Fatalf("ListExecs: %v", err)
	}
	if len(execs.GetExecs()) == 0 || !strings.Contains(execs.GetExecs()[0].GetFailure(), "restarted") {
		t.Fatalf("the settled exec should say the system restarted, got %+v", execs.GetExecs())
	}

	close(runner.Gate)
	server.runningExecs.Wait()
}

// Settling is for sessions that were mid exec. A session sitting idle is not one, and marking it failed
// on every boot would exec a quiet system into a wall of failures.
func TestSettlingLeavesAnIdleSessionAlone(t *testing.T) {
	runner := &model.FakeRunner{Reply: "done"}
	server, project := detachServer(t, runner)

	resp, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "hello",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	server.SettleExecs(context.Background())

	if got := sessionByID(t, server, resp.GetId()).GetStatus(); got != StatusIdle {
		t.Fatalf("an idle session should survive a restart as %q, got %q", StatusIdle, got)
	}
}
