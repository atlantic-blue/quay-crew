package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Stopping the task one session is running, and keeping the session.
//
// The failure this answers: killing the dispatch client was what people reached for, and the same
// kill ended one task at once and left another working for sixteen more minutes.

// aWorkingSession dispatches a task that will not land until its gate is opened, and answers once the
// task is really under way rather than once the dispatch returned.
func aWorkingSession(t *testing.T, s *controlplane.Server, project string, gate, started chan struct{}) string {
	t.Helper()
	sent, err := s.Dispatch(context.Background(), &quaycrewv1.DispatchRequest{
		Project: project, Text: "do the wrong thing", Detach: true,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	<-started
	_ = gate
	return sent.GetId()
}

func TestStoppingATaskEndsItAndKeepsTheSession(t *testing.T) {
	gate, started := make(chan struct{}), make(chan struct{})
	runner := &model.FakeRunner{Reply: "done", Gate: gate, Started: started}
	s := aSystemWithProvider(runner, &sandbox.FakeProvider{})
	_, project := newProject(t, s)
	ctx := context.Background()
	session := aWorkingSession(t, s, project, gate, started)

	resp, err := s.StopTask(ctx, &quaycrewv1.StopTaskRequest{
		Id: session, Reason: "it is duplicating another pull request",
	})
	if err != nil {
		t.Fatalf("StopTask: %v", err)
	}
	if !resp.GetStopped() {
		t.Fatal("the system says there was nothing to stop, and a task was under way")
	}

	// The command answers only when the task has actually stopped, so the record is already closed
	// by the time the caller reads success.
	tasks, err := s.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks.GetTasks()) != 1 {
		t.Fatalf("the session holds %d tasks, want the one that was stopped", len(tasks.GetTasks()))
	}
	last := tasks.GetTasks()[0]
	if last.GetStatus() != controlplane.StatusTaskStopped {
		t.Fatalf("the task reads %q, and an operator asking for a stop is not a fault: "+
			"a stop reported as a crash hides the crashes", last.GetStatus())
	}
	if !strings.Contains(last.GetFailure(), "duplicating another pull request") {
		t.Fatalf("the task record says %q, and it has to carry the operator's own reason",
			last.GetFailure())
	}
	if resp.GetSession().GetStatus() != controlplane.StatusIdle {
		t.Fatalf("the session reads %q after a stop, want idle: nothing is running in it and the "+
			"next dispatch continues it", resp.GetSession().GetStatus())
	}
	close(gate)
}

// The session survives, which is the difference between this and stopping a session.
func TestAStoppedTaskLeavesTheConversationAndTheNextDispatchContinuesIt(t *testing.T) {
	gate, started := make(chan struct{}), make(chan struct{})
	runner := &model.FakeRunner{Reply: "done", Gate: gate, Started: started}
	provider := &sandbox.FakeProvider{}
	s := aSystemWithProvider(runner, provider)
	_, project := newProject(t, s)
	ctx := context.Background()
	session := aWorkingSession(t, s, project, gate, started)

	if _, err := s.StopTask(ctx, &quaycrewv1.StopTaskRequest{Id: session, Reason: "wrong branch"}); err != nil {
		t.Fatalf("StopTask: %v", err)
	}
	// The gate is opened and a fresh runner put behind the next task, the way a model that is not
	// being stopped answers.
	close(gate)
	runner.Gate = nil

	handle, _ := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: session})
	again, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Handle: handle.GetSession().GetHandle(), Text: "try again",
	})
	if err != nil {
		t.Fatalf("dispatching after a stop: %v", err)
	}
	if again.GetId() != session {
		t.Fatalf("the next task landed in %s, want the same session %s", again.GetId(), session)
	}
	if again.GetReply() != "done" {
		t.Fatalf("the session answered %q after a stop, want the new task's answer", again.GetReply())
	}
	tasks, _ := s.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session})
	if len(tasks.GetTasks()) != 2 {
		t.Fatalf("the session holds %d tasks, want the stopped one and the new one", len(tasks.GetTasks()))
	}
	if tasks.GetTasks()[0].GetStatus() != controlplane.StatusTaskStopped {
		t.Fatalf("the first task reads %q, and the record of the stop has to survive",
			tasks.GetTasks()[0].GetStatus())
	}
}

// A stop while nothing is running says so and changes nothing.
func TestStoppingASessionWithNothingRunningSaysSo(t *testing.T) {
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done"}, &sandbox.FakeProvider{})
	_, project := newProject(t, s)
	ctx := context.Background()
	session := anIdleSession(t, s, project)

	resp, err := s.StopTask(ctx, &quaycrewv1.StopTaskRequest{Id: session.GetId()})
	if err != nil {
		t.Fatalf("StopTask: %v", err)
	}
	if resp.GetStopped() {
		t.Fatal("the system says it stopped a task, and the session was idle")
	}
	tasks, _ := s.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session.GetId()})
	if len(tasks.GetTasks()) != 1 || tasks.GetTasks()[0].GetStatus() != controlplane.StatusIdle {
		t.Fatalf("the history changed under a stop that stopped nothing: %v", tasks.GetTasks())
	}
}

// A stop with no words still says it was a stop, so a listing never reads it as a crash.
func TestAStopWithNoReasonStillReadsAsAStop(t *testing.T) {
	gate, started := make(chan struct{}), make(chan struct{})
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done", Gate: gate, Started: started}, &sandbox.FakeProvider{})
	_, project := newProject(t, s)
	ctx := context.Background()
	session := aWorkingSession(t, s, project, gate, started)

	if _, err := s.StopTask(ctx, &quaycrewv1.StopTaskRequest{Id: session}); err != nil {
		t.Fatalf("StopTask: %v", err)
	}

	tasks, _ := s.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session})
	last := tasks.GetTasks()[0]
	if last.GetStatus() != controlplane.StatusTaskStopped {
		t.Fatalf("the task reads %q, want stopped", last.GetStatus())
	}
	if last.GetFailure() == "" {
		t.Fatal("a stop with no reason recorded nothing at all, so nobody can tell it from a task " +
			"that ended on its own")
	}
	close(gate)
}

func TestStoppingASessionNobodyHasIsNotFound(t *testing.T) {
	s := aSystemWithProvider(&model.FakeRunner{}, &sandbox.FakeProvider{})
	_, err := s.StopTask(context.Background(), &quaycrewv1.StopTaskRequest{Id: "ghost"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("stopping a missing session answered %v, want NotFound", err)
	}
}

// Two operators reaching for the stop at once leave one stop and one reason, rather than a second
// overwriting the first.
func TestTwoStopsLeaveOneReason(t *testing.T) {
	gate, started := make(chan struct{}), make(chan struct{})
	s := aSystemWithProvider(&model.FakeRunner{Reply: "done", Gate: gate, Started: started}, &sandbox.FakeProvider{})
	_, project := newProject(t, s)
	ctx := context.Background()
	session := aWorkingSession(t, s, project, gate, started)

	if _, err := s.StopTask(ctx, &quaycrewv1.StopTaskRequest{Id: session, Reason: "the first reason"}); err != nil {
		t.Fatalf("StopTask: %v", err)
	}
	second, err := s.StopTask(ctx, &quaycrewv1.StopTaskRequest{Id: session, Reason: "the second reason"})
	if err != nil {
		t.Fatalf("the second StopTask: %v", err)
	}
	if second.GetStopped() {
		t.Fatal("the second stop says it stopped a task, and the first had already ended it")
	}

	tasks, _ := s.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session})
	if !strings.Contains(tasks.GetTasks()[0].GetFailure(), "the first reason") {
		t.Fatalf("the task record says %q, want the reason of the stop that actually ended it",
			tasks.GetTasks()[0].GetFailure())
	}
	close(gate)
}
