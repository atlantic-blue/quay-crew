//go:build integration

package store_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

// A session going in circles, over the whole path: the real control plane, the real controller and a
// real database, with nothing stood in for but the model.
//
// The unit tier proves the measure and the rule against text, and the conformance suite proves each
// store keeps its side. Neither answers the question this one asks, which is whether a job that keeps
// producing the same thing actually stops, and whether one that is getting somewhere is left alone.
// Both decisions cross the controller, the store and the row a reader sees.

// sameEveryTime is a model that fails with one sentence, however often it is asked. It is what a
// session that cannot get a check green looks like from outside.
type sameEveryTime struct {
	said string
	mu   sync.Mutex
	runs int
}

func (s *sameEveryTime) Run(_ context.Context, _ sandbox.Sandbox, _ model.Request) (model.Response, error) {
	s.mu.Lock()
	s.runs++
	s.mu.Unlock()
	return model.Response{}, errors.New(s.said)
}

func (s *sameEveryTime) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runs
}

// differentEveryTime is a model that fails for a new reason each time, which is what a session
// working through a problem looks like. The last one repeats, so the queue never runs dry.
type differentEveryTime struct {
	said []string
	mu   sync.Mutex
	runs int
}

func (d *differentEveryTime) Run(_ context.Context, _ sandbox.Sandbox, _ model.Request) (model.Response, error) {
	d.mu.Lock()
	at := d.runs
	d.runs++
	d.mu.Unlock()
	if at >= len(d.said) {
		at = len(d.said) - 1
	}
	return model.Response{}, errors.New(d.said[at])
}

// TestAJobThatGoesInCirclesEscalatesRatherThanSpendingTheRestOfItsBudgetInPostgres.
func TestAJobThatGoesInCirclesEscalatesRatherThanSpendingTheRestOfItsBudgetInPostgres(t *testing.T) {
	said := "the coverage check is still red, so I will try the same fix once more"
	runner := &sameEveryTime{said: said}
	s, _ := aSystemWithAController(t, runner)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "get the coverage check green", Brief: "make the coverage gate pass",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	id := declared.GetJob().GetId()

	// Two attempts, each put back by the operator the way a failure is answered today. The third is
	// the one the system has to stop.
	waitForJob(t, s, id, job.PhaseFailed)
	continueTheJobOnPostgres(t, s, id)
	waitForJob(t, s, id, job.PhaseFailed)
	continueTheJobOnPostgres(t, s, id)

	asking := waitForJob(t, s, id, job.PhaseAsking)

	switch {
	case asking.GetLoopedStep() != 1:
		t.Fatalf("the job says it went in circles on step %d, want 1", asking.GetLoopedStep())
	case asking.GetEscalatedTo() != job.RouteAsk:
		t.Fatalf("the job says it escalated to %q, want the operator", asking.GetEscalatedTo())
	case !strings.Contains(asking.GetQuestion(), said):
		t.Fatalf("the question does not carry what the attempts said:\n%s", asking.GetQuestion())
	}

	// The record the decision was made on, read back off the database rather than remembered.
	attempts := asking.GetAttempted()
	if len(attempts) != job.LoopAttempts {
		t.Fatalf("the job records %d attempts, want the %d it made", len(attempts), job.LoopAttempts)
	}
	for _, attempt := range attempts[1:] {
		if attempt.GetSimilarity() < job.LoopThreshold {
			t.Fatalf("attempt %d scores %.3f against the same words said again, and the threshold is %.2f",
				attempt.GetSeq(), attempt.GetSimilarity(), job.LoopThreshold)
		}
	}
	// The whole point of stopping: a fourth attempt is not paid for. Ticking again proves nothing
	// starts a job that is waiting to be told something.
	s.TickJob(ctx)
	if runs := runner.count(); runs != job.LoopAttempts {
		t.Fatalf("the model ran %d times, want the %d attempts before the loop was found", runs, job.LoopAttempts)
	}
}

// The case that has to fail before the one above is worth anything: a session that is getting
// somewhere must never be stopped by this.
func TestAJobWhoseAttemptsAreDifferentIsNotStoppedInPostgres(t *testing.T) {
	runner := &differentEveryTime{said: []string{
		"the parser has no case for an empty file, so I am adding one",
		"the migration runs twice against a fresh database, so the guard moves up",
		"the sandbox image carries no git, so the clone in step two cannot run",
	}}
	s, _ := aSystemWithAController(t, runner)
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "get the coverage check green", Brief: "make the coverage gate pass",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	id := declared.GetJob().GetId()

	waitForJob(t, s, id, job.PhaseFailed)
	continueTheJobOnPostgres(t, s, id)
	waitForJob(t, s, id, job.PhaseFailed)
	continueTheJobOnPostgres(t, s, id)
	failed := waitForJob(t, s, id, job.PhaseFailed)

	switch {
	case failed.GetLoopedStep() != 0:
		t.Fatalf("three attempts at different work read as a loop, on step %d", failed.GetLoopedStep())
	case failed.GetEscalatedTo() != "":
		t.Fatalf("a job making progress escalated to %q", failed.GetEscalatedTo())
	case len(failed.GetAttempted()) != 3:
		t.Fatalf("the job records %d attempts, want the three it made", len(failed.GetAttempted()))
	}
	for _, attempt := range failed.GetAttempted()[1:] {
		if attempt.GetSimilarity() >= job.LoopThreshold {
			t.Fatalf("attempt %d scores %.3f against different work, and the threshold is %.2f: this "+
				"detector would stop work that was going to finish",
				attempt.GetSeq(), attempt.GetSimilarity(), job.LoopThreshold)
		}
	}
}

// continueTheJobOnPostgres puts a failed job back the way the operator does, and waits for the
// controller to send the next attempt.
func continueTheJobOnPostgres(t *testing.T, s *controlplane.Server, id string) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.ResumeJob(ctx, &quaycrewv1.ResumeJobRequest{Id: id}); err != nil {
		t.Fatalf("ResumeJob: %v", err)
	}
	// The controller starts it again on its own tick, which waitForJob drives.
	time.Sleep(10 * time.Millisecond)
}
