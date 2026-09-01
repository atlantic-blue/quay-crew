//go:build integration

package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// The controller over a real database.
//
// The unit tier proves the loop's decisions against doubles, and the conformance suite proves the
// store keeps its side. Neither reaches the one thing only Postgres can answer: the claim is a
// conditional update in a transaction, so two controllers asking at the same moment leave one task
// rather than two. That either holds against the real engine or it does not.

// errTheModelRefusedThisTask is a model that will not do the work, which is the sad path a
// controller has to write onto the row rather than leave running.
var errTheModelRefusedThisTask = errors.New("the model refused this task")

// aSystemWithAController stands the control plane up on a real database, with a model that answers.
func aSystemWithAController(t *testing.T, runner model.Runner) (*controlplane.Server, store.Store) {
	t.Helper()
	truncate(t)
	kept, err := store.NewPostgres(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)
	return controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: runner, Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	}), kept
}

// waitForJob reads the row until it reaches a phase, so a test never asserts on a task still in
// flight behind a dispatch that let go of it.
func waitForJob(t *testing.T, s *controlplane.Server, id, phase string) *quaycrewv1.Job {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.TickJob(ctx)
		found, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: id})
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.GetJob().GetPhase() == phase {
			return found.GetJob()
		}
		if time.Now().After(deadline) {
			t.Fatalf("the job is %q saying %q, want %q", found.GetJob().GetPhase(),
				found.GetJob().GetReason(), phase)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The whole of what this slice buys, against the database that holds it: declared job runs, and the
// answer is on the row afterwards.
func TestDeclaredJobRunsAndItsAnswerIsOnTheRowInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &model.FakeRunner{Reply: "the bill is due on the 14th"})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	done := waitForJob(t, s, declared.GetJob().GetId(), job.PhaseDone)

	if !strings.Contains(done.GetAnswer(), "the bill is due on the 14th") {
		t.Fatalf("the answer on the row is %q", done.GetAnswer())
	}
	if done.GetSession() == "" {
		t.Fatal("the job says no session, so nothing can reach the conversation that did it")
	}
	if done.GetStartedAt() == nil || done.GetFinishedAt() == nil {
		t.Fatal("the job does not carry when it started and when it finished")
	}
	if done.GetObservedVersion() != done.GetVersion() {
		t.Fatalf("the status describes version %d of a declaration at version %d",
			done.GetObservedVersion(), done.GetVersion())
	}
}

// The claim is a conditional update in one statement, which is what keeps two controllers ticking at
// the same moment from both starting the same job. A job is paid for, so twice is money.
func TestTwoControllersTickingAtOnceStartTheJobOnceInPostgres(t *testing.T) {
	s, kept := aSystemWithAController(t, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	_ = workspace

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	id := declared.GetJob().GetId()

	// The two claims race in the database rather than in a mutex, which is the property under test.
	claims := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := kept.StartJob(ctx, id,
				job.Lease{Owner: "controller-a", Until: time.Now().UTC().Add(time.Minute)},
				[]*job.Event{{
					ID: store.NewID(), Kind: job.EventStarted, Job: id,
					Workspace: declared.GetJob().GetWorkspace(), Project: project,
					Detail: "attempt 1", OccurredAt: time.Now().UTC(),
				}})
			claims <- err
		}()
	}
	won, lost := 0, 0
	for range 2 {
		if err := <-claims; err == nil {
			won++
		} else {
			lost++
		}
	}
	if won != 1 || lost != 1 {
		t.Fatalf("%d claims won and %d were refused, want exactly one of each", won, lost)
	}

	events, err := kept.ListJobEvents(ctx, id)
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	starts := 0
	for _, event := range events {
		if event.Kind == job.EventStarted {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("%d starts are on the record, want the one that won", starts)
	}
}

// Ticking over the same row again must not send a second task. The row is the guard, and the row
// lives in the database.
func TestTickingAgainSendsNoSecondTaskInPostgres(t *testing.T) {
	s, kept := aSystemWithAController(t, &model.FakeRunner{Reply: "the bill is due on the 14th"})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	done := waitForJob(t, s, declared.GetJob().GetId(), job.PhaseDone)

	for range 3 {
		s.TickJob(ctx)
	}

	tasks, err := kept.ListTasks(ctx, done.GetSession(), 0)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("%d tasks are recorded against the job, want the one it was asked to run", len(tasks))
	}
	found, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.GetJob().GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if found.GetJob().GetAttempts() != 1 {
		t.Fatalf("the job is on attempt %d, want 1", found.GetJob().GetAttempts())
	}
}

// A task the model refuses leaves the job failed with the reason on the row, rather than running
// forever with nothing behind it.
func TestJobWhoseTaskFailedIsFailedOnTheRowInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &model.FakeRunner{Err: errTheModelRefusedThisTask})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	failed := waitForJob(t, s, declared.GetJob().GetId(), job.PhaseFailed)

	if failed.GetReason() == "" {
		t.Fatal("the job failed and the row says nothing about why")
	}
}

// The failure this exists for, against the database that holds the row: a machine with no room to
// make a container leaves the job pending rather than failed, and the job runs when the room comes
// back. Failing it is how declared work was lost. See issue 465.
func TestJobTheSystemCouldNotGiveASandboxWaitsAndRunsLaterInPostgres(t *testing.T) {
	truncate(t)
	kept, err := store.NewPostgres(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)
	// The daemon has taken the request and not answered, which is what a busy machine looks like from
	// here. The budget is short because a test that waits the real minute out is a test nobody runs.
	provider := &sandbox.FakeProvider{Hold: make(chan struct{})}
	s := controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: &model.FakeRunner{Reply: "the bill is due on the 14th"},
		Provider: provider, Secrets: secrets.NewMemory(),
		StartWait: 200 * time.Millisecond, ExportWait: 200 * time.Millisecond,
	})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	id := declared.GetJob().GetId()

	waiting := waitForJob(t, s, id, job.PhasePending)
	if !strings.Contains(waiting.GetReason(), "waits for room") {
		t.Fatalf("the job says %q, and an operator reading it cannot tell it is waiting", waiting.GetReason())
	}
	if waiting.GetFinishedAt() != nil {
		t.Fatal("a job that never started carries the moment it finished")
	}

	close(provider.Hold)

	done := waitForJob(t, s, id, job.PhaseDone)
	if !strings.Contains(done.GetAnswer(), "the bill is due on the 14th") {
		t.Fatalf("the answer on the row is %q", done.GetAnswer())
	}
	if done.GetAttempts() < 2 {
		t.Fatalf("the job ran on attempt %d, want the one after the machine had room", done.GetAttempts())
	}
	// One conversation, however many times the system had to try: the retry lands where the job has
	// been all along rather than starting a second one.
	sessions, err := s.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions.GetSessions()) != 1 {
		t.Fatalf("the system holds %d sessions, want the one the job ran in", len(sessions.GetSessions()))
	}
}

// Every movement is a row beside the row it describes, and they read in the order they happened.
func TestEveryMovementIsOnTheRecordInPostgres(t *testing.T) {
	s, kept := aSystemWithAController(t, &model.FakeRunner{Reply: "the bill is due on the 14th"})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	waitForJob(t, s, declared.GetJob().GetId(), job.PhaseDone)

	events, err := kept.ListJobEvents(ctx, declared.GetJob().GetId())
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	// The claim comes before the start: a controller takes the job in hand before it sends anything.
	want := []string{job.EventDeclared, job.EventClaimed, job.EventStarted, job.EventAnswered}
	if len(events) != len(want) {
		t.Fatalf("%d records exist, want %v", len(events), want)
	}
	for i, kind := range want {
		if events[i].Kind != kind {
			t.Fatalf("record %d is %q, want %q", i, events[i].Kind, kind)
		}
	}
}

// What the operator reads while the work is happening, over the database that holds it.
//
// The name a session carries used to be written behind a task that had already been answered, and a
// job is one long task, so a screen of running jobs was a column of blank name cells: four
// conversations burning tokens and no way to tell which was which. The title is typed at declaration,
// so it is on the row before the model has said anything.
//
// Against Postgres because the title is a column here and a struct field in memory, and a column that
// is written and never read back is exactly what the in memory store cannot notice.
func TestAJobsSessionIsNamedBeforeItsTaskAnswersInPostgres(t *testing.T) {
	held, started := make(chan struct{}), make(chan struct{})
	runner := &model.FakeRunner{Reply: "the bill is due on the 14th", Gate: held, Started: started}
	s, _ := aSystemWithAController(t, runner)
	// Closed however this test leaves, so the task behind the gate ends rather than holding the system
	// open after the assertions.
	defer close(held)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	s.TickJob(ctx)
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("no task ever started, so there is nothing to read a name off")
	}
	sessionID := waitForJobSession(t, s, declared.GetJob().GetId())

	// Still running: the model is holding at the gate, so nothing has described this conversation and
	// the name in the listing can only have come from the declaration.
	running, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.GetJob().GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if running.GetJob().GetAnswer() != "" {
		t.Fatalf("the task already answered %q, so this proves nothing about a job in flight",
			running.GetJob().GetAnswer())
	}

	listed, err := s.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	for _, session := range listed.GetSessions() {
		if session.GetId() != sessionID {
			continue
		}
		if session.GetDescription() != "" {
			t.Fatalf("the conversation was described as %q while its task was still running", session.GetDescription())
		}
		// The cell a listing draws, not the column it comes from: what was broken was the name an
		// operator reads.
		if name := display.SessionLabel(session); name != "read the electricity bill" {
			t.Fatalf("the name cell of the session doing the job says %q", name)
		}
		return
	}
	t.Fatalf("the system lists no session %s for the job that is running in it", sessionID)
}

// waitForJobSession reads the row until the controller has written which session its task went to.
// The dispatch lets go of the task, so the model starts before the row says where it is running.
func waitForJobSession(t *testing.T, s *controlplane.Server, id string) string {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for {
		found, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: id})
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if session := found.GetJob().GetSession(); session != "" {
			return session
		}
		if time.Now().After(deadline) {
			t.Fatal("the job never said which session its task went to")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
