package controlplane

import (
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/model"
)

// A task was recorded only when it ended, so for the minutes or the hours it ran there was nothing to
// see: the listing said idle and the history said the session had been asked nothing. Three sessions
// worked for over half an hour each and every one of them read that way. These are the cases that
// stop it coming back, on the path the operator's own terminal takes.

// tasksOf reads a session's history the way every surface reads it.
func tasksOf(t *testing.T, server *Server, session string) []*quaycrewv1.Task {
	t.Helper()
	resp, err := server.ListTasks(context.Background(), &quaycrewv1.ListTasksRequest{Session: session})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	return resp.GetTasks()
}

// waitFor blocks until a channel closes, so a test never asserts on a task that has not started.
func waitFor(t *testing.T, started <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s never happened", what)
	}
}

// The defect itself. The caller waits, which is what `quay dispatch` does, so nothing about this
// task is detached and nothing about it was visible either.
func TestAWaitedTaskIsVisibleWhileItRuns(t *testing.T) {
	runner := &model.FakeRunner{
		Reply: "it is a control plane",
		Gate:  make(chan struct{}), Started: make(chan struct{}),
	}
	server, project := detachServer(t, runner)

	// Dispatched behind the test rather than detached, because the caller waiting is the whole point.
	answered := make(chan *quaycrewv1.DispatchResponse, 1)
	go func() {
		resp, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
			Project: project, Text: "read the repository and tell me what it does",
		})
		if err != nil {
			close(answered)
			return
		}
		answered <- resp
	}()
	waitFor(t, runner.Started, "the task")

	sessions, err := server.ListSessions(context.Background(), &quaycrewv1.ListSessionsRequest{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions.GetSessions()) != 1 {
		t.Fatalf("the crew has %d sessions, want 1", len(sessions.GetSessions()))
	}
	working := sessions.GetSessions()[0]
	if working.GetStatus() != StatusRunning {
		t.Fatalf("a session with a task in it reads %q, want %q", working.GetStatus(), StatusRunning)
	}

	running := tasksOf(t, server, working.GetId())
	if len(running) != 1 {
		t.Fatalf("%d tasks are recorded while one runs, want 1", len(running))
	}
	if running[0].GetPrompt() != "read the repository and tell me what it does" {
		t.Fatalf("the recorded task says %q was asked", running[0].GetPrompt())
	}
	if running[0].GetStatus() != StatusRunning {
		t.Fatalf("the recorded task reads %q, want %q", running[0].GetStatus(), StatusRunning)
	}

	close(runner.Gate)
	resp := <-answered
	if resp == nil {
		t.Fatal("the dispatch failed")
	}

	// The same task, closed. Two rows would mean the operator reads their own prompt twice.
	landed := tasksOf(t, server, resp.GetId())
	if len(landed) != 1 {
		t.Fatalf("%d tasks are recorded after one ran, want 1", len(landed))
	}
	if landed[0].GetId() != running[0].GetId() {
		t.Fatalf("the task that landed is not the one that started: %s then %s", running[0].GetId(), landed[0].GetId())
	}
	if landed[0].GetStatus() != StatusIdle || landed[0].GetReply() != "it is a control plane" {
		t.Fatalf("the landed task came back as %+v", landed[0])
	}
	if got := sessionByID(t, server, resp.GetId()).GetStatus(); got != StatusIdle {
		t.Fatalf("the session reads %q once its task landed, want %q", got, StatusIdle)
	}
}

// The detached path marked the session running and still recorded nothing, so the console could say a
// session was working and not say what it was working on.
func TestADetachedTaskSaysWhatItWasAskedWhileItRuns(t *testing.T) {
	runner := &model.FakeRunner{
		Reply: "done", Gate: make(chan struct{}), Started: make(chan struct{}),
	}
	server, project := detachServer(t, runner)

	resp, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "order the bulbs", Detach: true,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	waitFor(t, runner.Started, "the detached task")

	running := tasksOf(t, server, resp.GetId())
	if len(running) != 1 || running[0].GetPrompt() != "order the bulbs" {
		t.Fatalf("the running task is not in the history: %+v", running)
	}

	close(runner.Gate)
	server.tasking.Wait()
	if landed := tasksOf(t, server, resp.GetId()); len(landed) != 1 || landed[0].GetReply() != "done" {
		t.Fatalf("the landed task came back as %+v", landed)
	}
}

// A task that dies with the process leaves its row open. Closing that row says which task died,
// rather than that some task did.
func TestARestartSaysWhichTaskDiedWithIt(t *testing.T) {
	runner := &model.FakeRunner{
		Reply: "done", Gate: make(chan struct{}), Started: make(chan struct{}),
	}
	server, project := detachServer(t, runner)

	resp, err := server.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "migrate the database", Detach: true,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	waitFor(t, runner.Started, "the detached task")

	server.SettleTasks(context.Background())

	settled := tasksOf(t, server, resp.GetId())
	if len(settled) != 1 {
		t.Fatalf("%d tasks are recorded after settling one, want 1", len(settled))
	}
	if settled[0].GetPrompt() != "migrate the database" {
		t.Fatalf("the settled task does not say what died: %+v", settled[0])
	}
	if !strings.Contains(settled[0].GetFailure(), "restarted") {
		t.Fatalf("the settled task does not say the crew restarted: %+v", settled[0])
	}

	close(runner.Gate)
	server.tasking.Wait()
}
