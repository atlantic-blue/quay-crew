//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// The session lifecycle over a real database and a real control plane.
//
// The unit tier proves the loop's decisions against doubles and the conformance suite proves each
// store keeps its side. Neither reaches what this file is for: the fourth query is a `not exists`
// against the job table with a phase array in it, and whether that query means the same thing in
// Postgres as it does in the map is a question only Postgres answers.

// aCrewWithAProviderOnPostgres stands the control plane up on a real database over a provider a test
// can drive, so a case can be an operator sitting in a container the crew is about to take back.
func aCrewWithAProviderOnPostgres(t *testing.T, runner model.Runner, provider *sandbox.FakeProvider) (
	*controlplane.Server, store.Store) {
	t.Helper()
	truncate(t)
	kept, err := store.NewPostgres(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)
	return controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: runner, Provider: provider, Secrets: secrets.NewMemory(),
	}), kept
}

// reclaimingAfter gives the workspace a reclaim time, and optionally an archive time, through the
// same call an operator's `quay limits` makes.
func reclaimingAfter(t *testing.T, s *controlplane.Server, workspace string, reclaim, archive int32) {
	t.Helper()
	if _, err := s.SetWorkspaceLimits(context.Background(), &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{
			Workspace: workspace, ReclaimSeconds: reclaim, ArchiveSeconds: archive,
		},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
}

// The rule this slice ships on, against the database that holds it.
func TestWithBothTimesUnsetNothingIsReclaimedInPostgres(t *testing.T) {
	s, _ := aCrewWithAProviderOnPostgres(t, &model.FakeRunner{Reply: "done"}, &sandbox.FakeProvider{})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)
	sent, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project, Text: "hello"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	for range 20 {
		s.TickJob(ctx)
	}

	session, err := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: sent.GetId()})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.GetSession().GetStatus() != controlplane.StatusIdle {
		t.Fatalf("the session reads %q after twenty ticks, and no workspace gave the crew a time",
			session.GetSession().GetStatus())
	}
	if session.GetSession().GetReclaimedAt() != nil {
		t.Fatal("the session carries a reclaim stamp, and nothing should have written one")
	}
}

// The mechanism, end to end: a settled session gives its container back, and the next task builds a
// fresh one over the same conversation and keeps the history.
func TestASettledSessionIsReclaimedAndCarriesOnInPostgres(t *testing.T) {
	provider := &sandbox.FakeProvider{}
	runner := &model.FakeRunner{Reply: "first", SessionID: "model-1"}
	s, _ := aCrewWithAProviderOnPostgres(t, runner, provider)
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	// One second, which the test then genuinely waits out rather than moving a clock under the code.
	reclaimingAfter(t, s, workspace, 1, 0)

	sent, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project, Text: "hello"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	time.Sleep(1200 * time.Millisecond)
	s.TickJob(ctx)

	reclaimed, _ := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: sent.GetId()})
	if reclaimed.GetSession().GetStatus() != controlplane.StatusReclaimed {
		t.Fatalf("the session reads %q a second past its reclaim time, want reclaimed",
			reclaimed.GetSession().GetStatus())
	}
	if reclaimed.GetSession().GetReclaimedAt() == nil {
		t.Fatal("the reclaimed session carries no stamp, so nothing can measure how long it has been one")
	}

	runner.Reply = "second"
	again, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project, Handle: sent.GetHandle(), Text: "still there?",
	})
	if err != nil {
		t.Fatalf("dispatching to a reclaimed session: %v", err)
	}
	if again.GetReply() != "second" || again.GetId() != sent.GetId() {
		t.Fatalf("the reclaimed session answered %q in session %s, want the new answer in the same session",
			again.GetReply(), again.GetId())
	}
	tasks, err := s.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: sent.GetId()})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks.GetTasks()) != 2 || tasks.GetTasks()[0].GetPrompt() != "hello" {
		t.Fatalf("the session holds %d tasks after the reclaim, want the history whole",
			len(tasks.GetTasks()))
	}
}

// A container an operator is typing into is never taken, however long the clock says.
func TestASessionSomebodyIsInIsNotReclaimedInPostgres(t *testing.T) {
	provider := &sandbox.FakeProvider{}
	s, _ := aCrewWithAProviderOnPostgres(t, &model.FakeRunner{Reply: "done"}, provider)
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	reclaimingAfter(t, s, workspace, 1, 0)

	sent, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project, Text: "hello"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	provider.Watch(sent.GetId())
	time.Sleep(1200 * time.Millisecond)
	for range 5 {
		s.TickJob(ctx)
	}

	session, _ := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: sent.GetId()})
	if session.GetSession().GetStatus() != controlplane.StatusIdle {
		t.Fatalf("the session reads %q while somebody had it open, want it left exactly as it was",
			session.GetSession().GetStatus())
	}
}

// The fourth query against the real engine: job still open holds its session out of the settled
// ones, and a job that ended stops holding it.
func TestJobStillOpenHoldsItsSessionAliveInPostgres(t *testing.T) {
	gate := make(chan struct{})
	provider := &sandbox.FakeProvider{}
	s, _ := aCrewWithAProviderOnPostgres(t, &model.FakeRunner{Reply: "done", Gate: gate}, provider)
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	reclaimingAfter(t, s, workspace, 1, 0)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	s.TickJob(ctx)
	time.Sleep(1200 * time.Millisecond)
	s.TickJob(ctx)

	running, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.GetJob().GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if running.GetJob().GetSession() == "" {
		t.Fatal("the job says no session, so this case is not testing what it says it is")
	}
	held, _ := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: running.GetJob().GetSession()})
	if held.GetSession().GetStatus() == controlplane.StatusReclaimed {
		t.Fatal("the crew took the container of a session a job is still running in")
	}

	// The job ends, and the same session becomes the crew's to take back.
	close(gate)
	waitForJob(t, s, declared.GetJob().GetId(), job.PhaseDone)
	time.Sleep(1200 * time.Millisecond)
	s.TickJob(ctx)

	after, _ := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: running.GetJob().GetSession()})
	if after.GetSession().GetStatus() != controlplane.StatusReclaimed {
		t.Fatalf("the session reads %q once its job had ended, want reclaimed",
			after.GetSession().GetStatus())
	}
}

// The second step, measured against the reclaim stamp rather than the row's last write.
func TestAReclaimedSessionIsArchivedInPostgres(t *testing.T) {
	s, _ := aCrewWithAProviderOnPostgres(t, &model.FakeRunner{Reply: "done"}, &sandbox.FakeProvider{})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	reclaimingAfter(t, s, workspace, 1, 1)

	sent, err := s.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: project, Text: "hello"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	time.Sleep(1200 * time.Millisecond)
	s.TickJob(ctx)
	time.Sleep(1200 * time.Millisecond)
	s.TickJob(ctx)

	session, _ := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: sent.GetId()})
	if session.GetSession().GetArchivedAt() == nil {
		t.Fatalf("the session reads %q and was never filed away", session.GetSession().GetStatus())
	}
	// Filed away and not deleted: the conversation handle is the only pointer to the transcript.
	if session.GetSession().GetModelSessionId() == "" {
		t.Fatal("the archived session lost its conversation handle")
	}
}

// Stopping the task one session is running, over the real store: the record closes as a stop with the
// operator's own reason, and the job that was running in it is stopped rather than failed.
func TestStoppingASessionStopsItsJobInPostgres(t *testing.T) {
	gate := make(chan struct{})
	s, _ := aCrewWithAProviderOnPostgres(t, &model.FakeRunner{Reply: "done", Gate: gate}, &sandbox.FakeProvider{})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	s.TickJob(ctx)
	running, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.GetJob().GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if running.GetJob().GetSession() == "" {
		t.Fatal("the job says no session, so there is nothing to stop")
	}

	stopped, err := s.StopTask(ctx, &quaycrewv1.StopTaskRequest{
		Id: running.GetJob().GetSession(), Reason: "the bill is not due yet",
	})
	if err != nil {
		t.Fatalf("StopTask: %v", err)
	}
	if !stopped.GetStopped() {
		t.Fatal("the crew says there was nothing to stop, and a task was under way")
	}
	close(gate)

	halted := waitForJob(t, s, declared.GetJob().GetId(), job.PhaseStopped)
	if halted.GetReason() == "" || halted.GetAnswer() != "" {
		t.Fatalf("the stopped job says %q and answers %q, want the operator's reason and no answer",
			halted.GetReason(), halted.GetAnswer())
	}
	tasks, err := s.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: running.GetJob().GetSession()})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if got := tasks.GetTasks()[0].GetStatus(); got != controlplane.StatusTaskStopped {
		t.Fatalf("the task reads %q on the row, want stopped: a stop reported as a crash hides "+
			"the crashes", got)
	}
	// The session survives, which is the whole difference between this and stopping a session.
	session, _ := s.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: running.GetJob().GetSession()})
	if session.GetSession().GetStatus() != controlplane.StatusIdle {
		t.Fatalf("the session reads %q after its task was stopped, want idle",
			session.GetSession().GetStatus())
	}
}
