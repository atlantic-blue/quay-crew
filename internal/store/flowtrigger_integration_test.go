//go:build integration

package store_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/flow"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/store"
)

// A run that starts because something happened, over a real database.
//
// The unit tier proves what the engine decides and the conformance suite proves both stores keep the
// same contract. Neither reaches the thing that only Postgres can answer: two pollers claiming one
// row is a conditional update racing itself inside the database, and the in memory store answers
// that question with a mutex, which is not the same question.

const reactingFlow = `
name: fix-red
version: 1
mode: edits
nodes:
  arrived: { type: trigger }
  fix:     { type: dispatch, prompt: "the build at {{url}} is red. Fix it." }
edges:
  - [arrived, fix]
  - [fix, done]
`

// The whole of the slice against the database that holds it: something happens, nobody starts
// anything, and the system's own tick runs the job.
func TestATriggerStartsAFlowRunInPostgres(t *testing.T) {
	s, kept := aSystemWithAController(t, &echoingRunner{})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	if _, err := s.ImportFlow(ctx, &quaycrewv1.ImportFlowRequest{Definition: reactingFlow}); err != nil {
		t.Fatalf("ImportFlow: %v", err)
	}

	raised, err := anEngineOn(kept, s).Raise(ctx, flow.Trigger{
		GraphName: "fix-red", Workspace: workspace, Project: project,
		Payload: map[string]string{"url": "https://ci.test/9"},
		Source:  "a caller inside the system",
	})
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	// Nothing has run. The trigger is a row, and the row is all there is until a tick reads it.
	if runs, err := kept.ListFlowRuns(ctx, project); err != nil || len(runs) != 0 {
		t.Fatalf("%d runs exist before the system ticked (%v)", len(runs), err)
	}

	started := waitForTrigger(t, kept, raised.ID, flow.TriggerStarted, s)
	run := driveFlow(t, s, started.Run, flow.StatusDone)

	if run.GetState()["url"] != "https://ci.test/9" {
		t.Fatalf("the run opened knowing %v, want what the trigger carried", run.GetState())
	}
	if run.GetJob() == "" {
		t.Fatal("the triggered run was written outside the job tree, so neither depth nor budget counts it")
	}
	carrier, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: run.GetJob()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	// The label is how a person finds why a run nobody started exists.
	if carrier.GetJob().GetLabels()["flow.trigger"] != raised.ID {
		t.Errorf("the run's own job is labelled %v, want it to name the trigger", carrier.GetJob().GetLabels())
	}
	// And the step was asked about the thing that happened, which is what the payload becoming the
	// opening state is for.
	listed, err := s.ListJobs(ctx, &quaycrewv1.ListJobsRequest{LabelKey: "flow.run", LabelValue: run.GetId()})
	if err != nil {
		t.Fatalf("ListJob: %v", err)
	}
	asked := ""
	for _, one := range listed.GetJobs() {
		if one.GetLabels()["flow.node"] == "fix" {
			asked = one.GetBrief()
		}
	}
	if asked != "the build at https://ci.test/9 is red. Fix it." {
		t.Fatalf("the step was written down as %q, want the payload rendered into the prompt", asked)
	}
}

// Two control planes over one database, ticking together. One run, or the system pays twice for one
// thing happening.
func TestConcurrentPollersStartOneRunFromOneTriggerInPostgres(t *testing.T) {
	s, kept := aSystemWithAController(t, &model.FakeRunner{Reply: "fixed"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)
	if _, err := s.ImportFlow(ctx, &quaycrewv1.ImportFlowRequest{Definition: reactingFlow}); err != nil {
		t.Fatalf("ImportFlow: %v", err)
	}
	raised, err := anEngineOn(kept, s).Raise(ctx, flow.Trigger{
		GraphName: "fix-red", Workspace: workspace, Project: project,
		Payload: map[string]string{"url": "https://ci.test/9"},
	})
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}

	// Eight of them, all reading the one pending row at the same moment, which is the shape a claim
	// that is not a conditional write gets wrong.
	const pollers = 8
	var ready, done sync.WaitGroup
	begin := make(chan struct{})
	ready.Add(pollers)
	done.Add(pollers)
	for at := range pollers {
		go func() {
			defer done.Done()
			poller := flow.NewPoller(anEngineOn(kept, s), 0,
				slog.New(slog.NewTextHandler(io.Discard, nil))).Owned(pollerName(at))
			ready.Done()
			<-begin
			poller.Tick(ctx)
		}()
	}
	ready.Wait()
	close(begin)
	done.Wait()

	runs, err := kept.ListFlowRuns(ctx, project)
	if err != nil {
		t.Fatalf("ListFlowRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("%d runs were started from one trigger by %d pollers", len(runs), pollers)
	}
	acted, err := kept.GetTrigger(ctx, raised.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if acted.Status != flow.TriggerStarted || acted.Run != runs[0].ID {
		t.Fatalf("the trigger reads %q naming run %q, want it started naming the one run", acted.Status, acted.Run)
	}
	// And one job carries it, rather than eight declared and seven orphaned.
	carriers, err := s.ListJobs(ctx, &quaycrewv1.ListJobsRequest{
		LabelKey: "flow.trigger", LabelValue: raised.ID,
	})
	if err != nil {
		t.Fatalf("ListJob: %v", err)
	}
	if len(carriers.GetJobs()) != 1 {
		t.Fatalf("%d jobs carry the run of one trigger", len(carriers.GetJobs()))
	}
}

// A trigger the system cannot start a run from keeps the sentence saying why, on the row, in the
// database. Nothing else ever reads that failure.
func TestATriggerThatStartsNothingSaysWhyOnItsRowInPostgres(t *testing.T) {
	s, kept := aSystemWithAController(t, &model.FakeRunner{Reply: "fixed"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)

	raised, err := anEngineOn(kept, s).Raise(ctx, flow.Trigger{
		GraphName: "never-imported", Workspace: workspace, Project: project,
	})
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}

	failed := waitForTrigger(t, kept, raised.ID, flow.TriggerFailed, s)
	if !strings.Contains(failed.Reason, "never-imported") || !strings.Contains(failed.Reason, "krewe flow import") {
		t.Fatalf("the row says %q, want it to name the flow and what to do about it", failed.Reason)
	}
	if runs, err := kept.ListFlowRuns(ctx, project); err != nil || len(runs) != 0 {
		t.Fatalf("%d runs were started for a flow nobody imported (%v)", len(runs), err)
	}
	// It is not offered again, so a trigger nobody can start is not read and refused on every tick
	// for as long as the system runs.
	s.TickFlows(ctx)
	again, err := kept.GetTrigger(ctx, raised.ID)
	if err != nil {
		t.Fatalf("GetTrigger: %v", err)
	}
	if again.Attempts != failed.Attempts {
		t.Errorf("a failed trigger was claimed again: %d attempts, then %d", failed.Attempts, again.Attempts)
	}
}

// anEngineOn is the engine the system builds for itself: the store it keeps, and the control plane it
// asks to prepare job and put sessions away.
func anEngineOn(kept store.Store, s *controlplane.Server) *flow.Engine {
	return flow.NewEngine(kept, s, s, s)
}

// waitForTrigger ticks the system's own poller until the row reaches a status, so a test never asserts
// on a trigger the poller has not read yet.
func waitForTrigger(t *testing.T, kept store.Store, id, want string, s *controlplane.Server) *flow.Trigger {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.TickFlows(ctx)
		found, err := kept.GetTrigger(ctx, id)
		if err != nil {
			t.Fatalf("GetTrigger: %v", err)
		}
		if found.Status == want {
			return found
		}
		if time.Now().After(deadline) {
			t.Fatalf("the trigger is %q saying %q after ten seconds, want %q", found.Status, found.Reason, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// pollerName is a name per poller, because two sharing one could each take the other's claim.
func pollerName(at int) string { return "poller-" + string(rune('a'+at)) }
