package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/atlantic-blue/krewe/internal/flow"
	"github.com/atlantic-blue/krewe/internal/store"
)

// runTriggerConformance holds both stores to what a pending trigger is.
//
// The claim is the part that has to be exact, and it is exactly the part a looser double would let
// through: a memory store that claimed a row somebody else already held would keep a suite green
// while the real system started two runs, and paid for two, from one thing happening.
func runTriggerConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a trigger is raised, read back whole, and outlives the caller", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		raised := &flow.Trigger{
			ID: store.NewID(), GraphName: "fix-red", Workspace: workspace, Project: project,
			Payload: map[string]string{"url": "https://example.test/run/9", "branch": "main"},
			Source:  "an in process caller",
		}
		if err := s.RaiseTrigger(ctx, raised); err != nil {
			t.Fatalf("RaiseTrigger: %v", err)
		}

		// Reopened, because a trigger that only exists in the caller's process is a run that never
		// starts when the caller goes away, which is the whole reason it is a row.
		found, err := open(t).GetTrigger(ctx, raised.ID)
		if err != nil {
			t.Fatalf("GetTrigger: %v", err)
		}
		if found.GraphName != "fix-red" || found.Workspace != workspace || found.Project != project {
			t.Fatalf("the trigger reads back as %q in %s/%s", found.GraphName, found.Workspace, found.Project)
		}
		if found.Status != flow.TriggerPending {
			t.Fatalf("the trigger reads back %q, want it pending", found.Status)
		}
		// The payload is the run's opening state, so a key lost here is a prompt rendered with
		// {{url}} still in it.
		if found.Payload["url"] != "https://example.test/run/9" || found.Payload["branch"] != "main" {
			t.Fatalf("the payload reads back as %v", found.Payload)
		}
		if found.Source != "an in process caller" {
			t.Errorf("the source reads back as %q", found.Source)
		}
		if found.RaisedAt.IsZero() {
			t.Error("the trigger came back with no time, so nothing can say when it arrived")
		}
		if found.Run != "" || found.Reason != "" {
			t.Errorf("a fresh trigger already names run %q and reason %q", found.Run, found.Reason)
		}
		if _, err := s.GetTrigger(ctx, "never-raised"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("a trigger nobody raised answered %v, want not found", err)
		}
	})

	t.Run("one pending trigger is claimed once, and the second poller is told so", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		raised := aTrigger(t, s, workspace, project, "fix-red")

		pending, err := s.PendingTriggers(ctx, 0)
		if err != nil {
			t.Fatalf("PendingTriggers: %v", err)
		}
		if len(pending) != 1 || pending[0].ID != raised.ID {
			t.Fatalf("%d triggers are waiting, want the one raised", len(pending))
		}

		claimed, err := s.ClaimTrigger(ctx, raised.ID, aLease("poller-one"))
		if err != nil {
			t.Fatalf("ClaimTrigger: %v", err)
		}
		if claimed.Attempts != 1 {
			t.Errorf("the claimed trigger counts %d attempts, want 1", claimed.Attempts)
		}
		// The second poller read the same row a moment earlier and is refused, rather than starting
		// a second run of one thing happening.
		if _, err := s.ClaimTrigger(ctx, raised.ID, aLease("poller-two")); !errors.Is(err, flow.ErrTriggerTaken) {
			t.Fatalf("the second claim answered %v, want it taken", err)
		}
		// And it is not offered again while the first poller holds it.
		waiting, err := s.PendingTriggers(ctx, 0)
		if err != nil {
			t.Fatalf("PendingTriggers: %v", err)
		}
		if len(waiting) != 0 {
			t.Fatalf("%d triggers are offered while one poller holds the row", len(waiting))
		}
	})

	t.Run("a claim that runs out is offered again, so a poller that died loses nothing", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		raised := aTrigger(t, s, workspace, project, "fix-red")

		// A poller that claimed the row and went away without starting anything.
		if _, err := s.ClaimTrigger(ctx, raised.ID, anExpiredLease("poller-that-died")); err != nil {
			t.Fatalf("ClaimTrigger: %v", err)
		}
		waiting, err := s.PendingTriggers(ctx, 0)
		if err != nil {
			t.Fatalf("PendingTriggers: %v", err)
		}
		if len(waiting) != 1 || waiting[0].ID != raised.ID {
			t.Fatalf("%d triggers are offered after a claim ran out, want the abandoned one", len(waiting))
		}
		taken, err := s.ClaimTrigger(ctx, raised.ID, aLease("poller-two"))
		if err != nil {
			t.Fatalf("the abandoned trigger could not be taken over: %v", err)
		}
		if taken.Attempts != 2 {
			t.Errorf("the trigger counts %d attempts, want the second claim counted", taken.Attempts)
		}
	})

	t.Run("the run a trigger starts and the trigger saying started are one write", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		if err := s.ImportFlowGraph(ctx, "fix-red", 1, "the definition"); err != nil {
			t.Fatalf("ImportFlowGraph: %v", err)
		}
		raised := aTrigger(t, s, workspace, project, "fix-red")
		if _, err := s.ClaimTrigger(ctx, raised.ID, aLease("poller-one")); err != nil {
			t.Fatalf("ClaimTrigger: %v", err)
		}

		run := &flow.Run{
			ID: "run-of-a-trigger", Workspace: workspace, Project: project,
			GraphName: "fix-red", GraphVersion: 1, Status: flow.StatusRunning,
			State: map[string]string{}, Attempts: map[string]int{},
		}
		carrier, records := carrierFor(run)
		if err := s.CreateFlowRun(ctx, run, carrier, records, raised.ID); err != nil {
			t.Fatalf("CreateFlowRun: %v", err)
		}
		started, err := s.GetTrigger(ctx, raised.ID)
		if err != nil {
			t.Fatalf("GetTrigger: %v", err)
		}
		if started.Status != flow.TriggerStarted || started.Run != run.ID {
			t.Fatalf("the trigger reads %q naming run %q, want it started naming the run it started", started.Status, started.Run)
		}

		// A second run of a trigger already acted on is refused whole. This is what makes a trigger
		// start exactly one run: a poller taking over a claim it thinks ran out cannot pay for a
		// second run of the same thing happening.
		second := *run
		second.ID = "second-run-of-one-trigger"
		secondCarrier, secondRecords := carrierFor(&second)
		if err := s.CreateFlowRun(ctx, &second, secondCarrier, secondRecords, raised.ID); err == nil {
			t.Fatal("a second run of one trigger was written")
		}
		if _, err := s.GetFlowRun(ctx, second.ID); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("the refused run reads back as %v, want nothing written", err)
		}
	})

	t.Run("a trigger that started nothing keeps the reason, and is offered no more", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		raised := aTrigger(t, s, workspace, project, "never-imported")
		if _, err := s.ClaimTrigger(ctx, raised.ID, aLease("poller-one")); err != nil {
			t.Fatalf("ClaimTrigger: %v", err)
		}

		reason := "the flow never-imported could not be read, so nothing was started"
		if err := s.FailTrigger(ctx, raised.ID, reason); err != nil {
			t.Fatalf("FailTrigger: %v", err)
		}
		failed, err := s.GetTrigger(ctx, raised.ID)
		if err != nil {
			t.Fatalf("GetTrigger: %v", err)
		}
		if failed.Status != flow.TriggerFailed || failed.Reason != reason {
			t.Fatalf("the trigger reads %q saying %q, want it failed with the reason", failed.Status, failed.Reason)
		}
		// Not offered again, and not claimable again: a trigger nobody can start must not be read,
		// refused and logged on every tick for as long as the system runs.
		waiting, err := s.PendingTriggers(ctx, 0)
		if err != nil {
			t.Fatalf("PendingTriggers: %v", err)
		}
		if len(waiting) != 0 {
			t.Fatalf("%d triggers are offered after one failed", len(waiting))
		}
		if _, err := s.ClaimTrigger(ctx, raised.ID, aLease("poller-two")); !errors.Is(err, flow.ErrTriggerTaken) {
			t.Errorf("a failed trigger was claimed again, answering %v", err)
		}
	})

	t.Run("triggers are offered oldest first, and no more than asked for", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		first := aTrigger(t, s, workspace, project, "first")
		second := aTrigger(t, s, workspace, project, "second")
		third := aTrigger(t, s, workspace, project, "third")

		offered, err := s.PendingTriggers(ctx, 2)
		if err != nil {
			t.Fatalf("PendingTriggers: %v", err)
		}
		if len(offered) != 2 {
			t.Fatalf("a tick was offered %d triggers, want the 2 it asked for", len(offered))
		}
		if offered[0].ID != first.ID || offered[1].ID != second.ID {
			t.Fatalf("the triggers came back as %q then %q, want the oldest two in order",
				offered[0].GraphName, offered[1].GraphName)
		}
		if third.ID == "" {
			t.Fatal("the third trigger was not raised")
		}
	})
}

// aTrigger raises one and answers with it, with a raised time far enough apart that the order two
// come back in is the order they were raised in rather than whatever the clock's resolution allows.
func aTrigger(t *testing.T, s store.Store, workspace, project, graph string) *flow.Trigger {
	t.Helper()
	raised := &flow.Trigger{
		ID: store.NewID(), GraphName: graph, Workspace: workspace, Project: project,
		Payload: map[string]string{"why": graph},
	}
	if err := s.RaiseTrigger(context.Background(), raised); err != nil {
		t.Fatalf("RaiseTrigger: %v", err)
	}
	// Postgres stamps raised_at from now(), which is the transaction's clock, so two raised inside
	// one millisecond can tie. A tie is broken by identifier in both stores, and this keeps the
	// order under test the one the caller can see.
	time.Sleep(2 * time.Millisecond)
	return raised
}
