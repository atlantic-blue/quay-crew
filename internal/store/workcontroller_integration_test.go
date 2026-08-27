//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/secrets"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/atlantic-blue/quay-crew/internal/work"
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

// aCrewWithAController stands the control plane up on a real database, with a model that answers.
func aCrewWithAController(t *testing.T, runner model.Runner) (*controlplane.Server, store.Store) {
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

// waitForWork reads the row until it reaches a phase, so a test never asserts on a task still in
// flight behind a dispatch that let go of it.
func waitForWork(t *testing.T, s *controlplane.Server, id, phase string) *quaycrewv1.Work {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.TickWork(ctx)
		found, err := s.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: id})
		if err != nil {
			t.Fatalf("GetWork: %v", err)
		}
		if found.GetWork().GetPhase() == phase {
			return found.GetWork()
		}
		if time.Now().After(deadline) {
			t.Fatalf("the work is %q saying %q, want %q", found.GetWork().GetPhase(),
				found.GetWork().GetReason(), phase)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The whole of what this slice buys, against the database that holds it: declared work runs, and the
// answer is on the row afterwards.
func TestDeclaredWorkRunsAndItsAnswerIsOnTheRowInPostgres(t *testing.T) {
	s, _ := aCrewWithAController(t, &model.FakeRunner{Reply: "the bill is due on the 14th"})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}

	done := waitForWork(t, s, declared.GetWork().GetId(), work.PhaseDone)

	if done.GetAnswer() != "the bill is due on the 14th" {
		t.Fatalf("the answer on the row is %q", done.GetAnswer())
	}
	if done.GetSession() == "" {
		t.Fatal("the work says no session, so nothing can reach the conversation that did it")
	}
	if done.GetStartedAt() == nil || done.GetFinishedAt() == nil {
		t.Fatal("the work does not carry when it started and when it finished")
	}
	if done.GetObservedVersion() != done.GetVersion() {
		t.Fatalf("the status describes version %d of a declaration at version %d",
			done.GetObservedVersion(), done.GetVersion())
	}
}

// The claim is a conditional update in one statement, which is what keeps two controllers ticking at
// the same moment from both starting the same work. Work is paid for, so twice is money.
func TestTwoControllersTickingAtOnceStartTheWorkOnceInPostgres(t *testing.T) {
	s, kept := aCrewWithAController(t, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	_ = workspace

	declared, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}
	id := declared.GetWork().GetId()

	// The two claims race in the database rather than in a mutex, which is the property under test.
	claims := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := kept.StartWork(ctx, id,
				work.Lease{Owner: "controller-a", Until: time.Now().UTC().Add(time.Minute)},
				[]*work.Event{{
					ID: store.NewID(), Kind: work.EventStarted, Work: id,
					Workspace: declared.GetWork().GetWorkspace(), Project: project,
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

	events, err := kept.ListWorkEvents(ctx, id)
	if err != nil {
		t.Fatalf("ListWorkEvents: %v", err)
	}
	starts := 0
	for _, event := range events {
		if event.Kind == work.EventStarted {
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
	s, kept := aCrewWithAController(t, &model.FakeRunner{Reply: "the bill is due on the 14th"})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}
	done := waitForWork(t, s, declared.GetWork().GetId(), work.PhaseDone)

	for range 3 {
		s.TickWork(ctx)
	}

	tasks, err := kept.ListTasks(ctx, done.GetSession(), 0)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("%d tasks are recorded against the work, want the one it was asked to run", len(tasks))
	}
	found, err := s.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: declared.GetWork().GetId()})
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if found.GetWork().GetAttempts() != 1 {
		t.Fatalf("the work is on attempt %d, want 1", found.GetWork().GetAttempts())
	}
}

// A task the model refuses leaves the work failed with the reason on the row, rather than running
// forever with nothing behind it.
func TestWorkWhoseTaskFailedIsFailedOnTheRowInPostgres(t *testing.T) {
	s, _ := aCrewWithAController(t, &model.FakeRunner{Err: errTheModelRefusedThisTask})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}

	failed := waitForWork(t, s, declared.GetWork().GetId(), work.PhaseFailed)

	if failed.GetReason() == "" {
		t.Fatal("the work failed and the row says nothing about why")
	}
}

// Every movement is a row beside the row it describes, and they read in the order they happened.
func TestEveryMovementIsOnTheRecordInPostgres(t *testing.T) {
	s, kept := aCrewWithAController(t, &model.FakeRunner{Reply: "the bill is due on the 14th"})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}
	waitForWork(t, s, declared.GetWork().GetId(), work.PhaseDone)

	events, err := kept.ListWorkEvents(ctx, declared.GetWork().GetId())
	if err != nil {
		t.Fatalf("ListWorkEvents: %v", err)
	}
	// The claim comes before the start: a controller takes the work in hand before it sends anything.
	want := []string{work.EventDeclared, work.EventClaimed, work.EventStarted, work.EventAnswered}
	if len(events) != len(want) {
		t.Fatalf("%d records exist, want %v", len(events), want)
	}
	for i, kind := range want {
		if events[i].Kind != kind {
			t.Fatalf("record %d is %q, want %q", i, events[i].Kind, kind)
		}
	}
}
