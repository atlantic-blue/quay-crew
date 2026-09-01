//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// The lease against a real database.
//
// The unit tier proves what a controller decides, and the conformance suite proves each store keeps
// its side of the contract. What only Postgres can answer is the compare and set: the condition and
// the write are one statement, so two controllers racing over one row leave one winner. A mutex in a
// process cannot say anything about two processes.

// aSystemNamed stands the control plane up on the shared database with a named controller and a lease
// of its own, which is what two control planes over one store look like.
func aSystemNamed(t *testing.T, kept store.Store, name string, lease time.Duration, runner model.Runner) *controlplane.Server {
	t.Helper()
	return controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: runner, Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		ControllerName: name, JobLease: lease,
	})
}

// openPostgres opens the shared database, empty of rows.
func openPostgres(t *testing.T) store.Store {
	t.Helper()
	truncate(t)
	kept, err := store.NewPostgres(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)
	return kept
}

// The test that matters: the controller that started the job is killed, its task answers anyway,
// and the controller that finds it adopts that answer without sending a second task.
func TestAControllerThatDiedMidTaskLeavesItsJobToBeAdoptedOnceInPostgres(t *testing.T) {
	kept := openPostgres(t)
	ctx := context.Background()
	// A hold that runs out at once is a controller that stopped renewing.
	dying := aSystemNamed(t, kept, "controller-a", time.Millisecond, &model.FakeRunner{Reply: "the bill is due on the 14th"})
	_, project := aProjectOnPostgres(t, dying)

	declared, err := dying.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	id := declared.GetJob().GetId()

	dying.TickJob(ctx)
	started, err := kept.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if started.Phase != job.PhaseRunning || started.LeaseOwner != "controller-a" {
		t.Fatalf("the job is %q held by %q, want running under the controller that started it",
			started.Phase, started.LeaseOwner)
	}
	// That controller is gone from here on. Its task lands anyway, because the sandbox is the system's.
	waitForTheLeaseToExpire(t, kept, id)

	next := aSystemNamed(t, kept, "controller-b", time.Minute, &model.FakeRunner{Reply: "a second task nobody asked for"})
	deadline := time.Now().Add(10 * time.Second)
	for {
		next.TickJob(ctx)
		found, err := kept.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.Phase == job.PhaseDone {
			if found.Answer != "the bill is due on the 14th" {
				t.Fatalf("the answer is %q, want the one the dead controller's task left behind", found.Answer)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the job is %q saying %q after ten seconds", found.Phase, found.Reason)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// One task, so one bill. The absence of a second start is the evidence.
	found, _ := kept.GetJob(ctx, id)
	tasks, err := kept.ListTasks(ctx, found.Session, 0)
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("%d tasks are recorded against the job, want the one it was asked to run", len(tasks))
	}
	if found.Attempts != 1 {
		t.Fatalf("the job is on attempt %d, want 1", found.Attempts)
	}
	events, err := kept.ListJobEvents(ctx, id)
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	counted := map[string]int{}
	for _, event := range events {
		counted[event.Kind]++
	}
	if counted[job.EventStarted] != 1 {
		t.Fatalf("%d starts are on the record, want 1", counted[job.EventStarted])
	}
	if counted[job.EventReleased] != 1 || counted[job.EventClaimed] != 2 {
		t.Fatalf("the record says the job was released %d times and claimed %d, want 1 and 2",
			counted[job.EventReleased], counted[job.EventClaimed])
	}
}

// Two controllers claiming one pending row at the same moment. The condition is in the statement, so
// the database decides, and it decides once.
func TestTwoControllersClaimingOneRowAtOnceLeaveOneHolderInPostgres(t *testing.T) {
	kept := openPostgres(t)
	ctx := context.Background()
	system := aSystemNamed(t, kept, "controller-a", time.Minute, &model.FakeRunner{Reply: "done"})
	workspace, project := aProjectOnPostgres(t, system)

	declared, err := system.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	id := declared.GetJob().GetId()

	claims := make(chan error, 2)
	for _, owner := range []string{"controller-a", "controller-b"} {
		go func() {
			_, err := kept.StartJob(ctx, id, job.Lease{Owner: owner, Until: time.Now().UTC().Add(time.Minute)},
				[]*job.Event{{
					ID: store.NewID(), Kind: job.EventClaimed, Job: id,
					Workspace: workspace, Project: project, Detail: "lease_owner " + owner,
					OccurredAt: time.Now().UTC(),
				}})
			claims <- err
		}()
	}
	won, refused := 0, 0
	for range 2 {
		switch err := <-claims; {
		case err == nil:
			won++
		case errors.Is(err, job.ErrNotPending):
			refused++
		default:
			t.Fatalf("a claim failed for a reason that is not the race: %v", err)
		}
	}
	if won != 1 || refused != 1 {
		t.Fatalf("%d claims won and %d were refused, want one of each", won, refused)
	}
}

// Two controllers finding the same abandoned row at the same moment. The same rule, one step later
// in the life of a job.
func TestTwoControllersTakingOverOneRowAtOnceLeaveOneHolderInPostgres(t *testing.T) {
	kept := openPostgres(t)
	ctx := context.Background()
	system := aSystemNamed(t, kept, "controller-a", time.Millisecond, &model.FakeRunner{Reply: "done"})
	workspace, project := aProjectOnPostgres(t, system)

	declared, err := system.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	id := declared.GetJob().GetId()
	system.TickJob(ctx)
	waitForTheLeaseToExpire(t, kept, id)

	takes := make(chan error, 2)
	for _, owner := range []string{"controller-b", "controller-c"} {
		go func() {
			_, err := kept.TakeOverJob(ctx, id, job.Lease{Owner: owner, Until: time.Now().UTC().Add(time.Minute)},
				[]*job.Event{{
					ID: store.NewID(), Kind: job.EventClaimed, Job: id,
					Workspace: workspace, Project: project, Detail: "lease_owner " + owner,
					OccurredAt: time.Now().UTC(),
				}})
			takes <- err
		}()
	}
	won, refused := 0, 0
	for range 2 {
		switch err := <-takes; {
		case err == nil:
			won++
		case errors.Is(err, job.ErrHeld):
			refused++
		default:
			t.Fatalf("a take over failed for a reason that is not the race: %v", err)
		}
	}
	if won != 1 || refused != 1 {
		t.Fatalf("%d take overs won and %d were refused, want one of each", won, refused)
	}
}

// A controller that is alive keeps its job, however many other controllers are ticking beside it.
func TestJobUnderALeaseThatStillRunsIsNotTakenInPostgres(t *testing.T) {
	kept := openPostgres(t)
	ctx := context.Background()
	holder := aSystemNamed(t, kept, "controller-a", time.Minute,
		&model.FakeRunner{Reply: "done", Gate: make(chan struct{}), Started: make(chan struct{})})
	_, project := aProjectOnPostgres(t, holder)

	declared, err := holder.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	id := declared.GetJob().GetId()
	holder.TickJob(ctx)

	other := aSystemNamed(t, kept, "controller-b", time.Minute, &model.FakeRunner{Reply: "done"})
	for range 3 {
		other.TickJob(ctx)
	}

	found, err := kept.GetJob(ctx, id)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if found.LeaseOwner != "controller-a" {
		t.Fatalf("the job is held by %q, and its holder never went away", found.LeaseOwner)
	}
	tasks, _ := kept.ListTasks(ctx, found.Session, 0)
	if len(tasks) > 1 {
		t.Fatalf("%d tasks are recorded against the job, want at most the one its holder sent", len(tasks))
	}
}

// A controller that died before it dispatched left nothing behind, so the job goes back to pending
// and runs properly. Nothing was paid for, so nothing is lost.
func TestJobAbandonedBeforeItsTaskWasSentRunsAgainInPostgres(t *testing.T) {
	kept := openPostgres(t)
	ctx := context.Background()
	system := aSystemNamed(t, kept, "controller-a", time.Millisecond, &model.FakeRunner{Reply: "done"})
	workspace, project := aProjectOnPostgres(t, system)

	declared, err := system.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	id := declared.GetJob().GetId()
	// Claimed and never dispatched, which is a controller killed between the two.
	if _, err := kept.StartJob(ctx, id, job.Lease{Owner: "controller-a", Until: time.Now().UTC().Add(-time.Second)},
		[]*job.Event{{
			ID: store.NewID(), Kind: job.EventClaimed, Job: id, Workspace: workspace,
			Project: project, Detail: "lease_owner controller-a", OccurredAt: time.Now().UTC(),
		}}); err != nil {
		t.Fatalf("StartJob: %v", err)
	}

	next := aSystemNamed(t, kept, "controller-b", time.Minute, &model.FakeRunner{Reply: "the bill is due on the 14th"})
	deadline := time.Now().Add(10 * time.Second)
	for {
		next.TickJob(ctx)
		found, err := kept.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.Phase == job.PhaseDone {
			if found.Answer != "the bill is due on the 14th" {
				t.Fatalf("the answer is %q", found.Answer)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the job is %q saying %q after ten seconds", found.Phase, found.Reason)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitForTheLeaseToExpire waits out a hold a test made short on purpose.
func waitForTheLeaseToExpire(t *testing.T, kept store.Store, id string) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(5 * time.Second)
	for {
		found, err := kept.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.LeaseUntil == nil || found.LeaseUntil.Before(time.Now()) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the hold on %s still runs to %v", id, found.LeaseUntil)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
