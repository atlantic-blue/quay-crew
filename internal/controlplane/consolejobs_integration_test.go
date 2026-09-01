//go:build integration

package controlplane_test

import (
	"context"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/console"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
)

// The console's jobs view over the whole path: a real Postgres, the real control plane, and the real
// gRPC interface, with nothing stood in for but the model.
//
// The table tests in internal/console prove how a row is built from a job. What they cannot answer is
// whether the rows are the system's actual jobs: they are built from a double that hands back whatever
// the case wrote. So this declares jobs the way the command line does, lets the controller start one,
// and reads the listing the operator would be looking at.
func TestTheConsoleListsTheJobsTheSystemActuallyHolds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	server := controlplane.NewServer(controlplane.Config{
		Store: aRealStore(t, ctx), Runner: &model.FakeRunner{Reply: "it is due on the 14th"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := servedOver(t, server)

	workspace, err := client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: "acme"})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	elsewhere, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace.GetWorkspace().GetId(), Name: "gardening",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	started := declare(t, ctx, client, project.GetProject().GetId(), "read the electricity bill")

	// The tick sits between the two declarations because one tick starts every job it finds
	// runnable, up to a batch of twenty, rather than one of them. So a job declared before it is a
	// job with a session, and a job declared after it is a job nothing has started, which is the
	// second row this listing has to carry.
	server.TickJob(ctx)
	waiting, doneWaiting := context.WithTimeout(ctx, 60*time.Second)
	server.WaitForTasks(waiting)
	doneWaiting()

	pending := declare(t, ctx, client, project.GetProject().GetId(), "read the water bill")
	declare(t, ctx, client, elsewhere.GetProject().GetId(), "cut the hedge")

	registry, err := console.NewDefaultRegistry(client)
	if err != nil {
		t.Fatalf("building the console: %v", err)
	}
	jobs, found := registry.Get("jobs")
	if !found {
		t.Fatal("the console has no jobs view")
	}

	// Every job the system holds, which is what the view opens on.
	rows, err := jobs.List(ctx, "")
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("the console lists %d jobs, want the 3 the system holds", len(rows))
	}

	running, ok := rowFor(rows, started.GetId())
	if !ok {
		t.Fatalf("the started job is not in the listing: %v", rows)
	}
	// The session is the controller's, read back through Postgres and shortened for the cell. The
	// point of the column is that an operator can get from a job to the conversation running it.
	if running.Parent == "" {
		t.Fatalf("the started job carries no session, so there is nothing to descend into: %q", running.Cells)
	}
	if cellNamed(jobs, running, "session") == "not yet" {
		t.Fatal("the started job says it has no session yet, and the controller gave it one")
	}
	if phase := cellNamed(jobs, running, "phase"); phase != job.PhaseRunning && phase != job.PhaseDone {
		t.Fatalf("the started job reads %q, want a phase the controller moved it to", phase)
	}

	waitingRow, ok := rowFor(rows, pending.GetId())
	if !ok {
		t.Fatalf("the pending job is not in the listing: %v", rows)
	}
	if got := cellNamed(jobs, waitingRow, "session"); got != "not yet" {
		t.Fatalf("a job nothing has started says its session is %q", got)
	}

	// Scoped to one project, which is the same call the view makes when it is drilled into from one.
	inProject, err := jobs.List(ctx, project.GetProject().GetId())
	if err != nil {
		t.Fatalf("listing one project's jobs: %v", err)
	}
	if len(inProject) != 2 {
		t.Fatalf("that project holds %d jobs in the listing, want 2", len(inProject))
	}

	// What a job did, which is where enter goes: the tasks of the session the controller gave it.
	scoped, err := jobs.DrillBy(running)
	if err != nil {
		t.Fatalf("descending into what the job did: %v", err)
	}
	tasks, foundTasks := registry.Get("tasks")
	if !foundTasks {
		t.Fatal("the console has no tasks view")
	}
	ran, err := tasks.List(ctx, scoped)
	if err != nil {
		t.Fatalf("listing the job's tasks: %v", err)
	}
	if len(ran) == 0 {
		t.Fatal("the job's session ran no task, so enter opens an empty screen")
	}

	// Stopping from the view, against the real system: the key writes through the control plane and the
	// next listing says so.
	stop, foundStop := actionNamed(jobs, "Stop")
	if !foundStop {
		t.Fatal("the jobs view has no Stop action")
	}
	if err := stop.Run(ctx, waitingRow); err != nil {
		t.Fatalf("stopping a job from the console: %v", err)
	}
	after, err := jobs.List(ctx, "")
	if err != nil {
		t.Fatalf("listing jobs again: %v", err)
	}
	stopped, ok := rowFor(after, pending.GetId())
	if !ok {
		t.Fatalf("the stopped job left the listing: %v", after)
	}
	if got := cellNamed(jobs, stopped, "phase"); got != job.PhaseStopped {
		t.Fatalf("the job reads %q after being stopped from the console, want stopped", got)
	}
}

// declare writes a job the way the command line does.
func declare(t *testing.T, ctx context.Context, client quaycrewv1.ControlPlaneServiceClient,
	project, title string) *quaycrewv1.Job {
	t.Helper()
	created, err := client.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: title, Brief: "open it and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateJob %q: %v", title, err)
	}
	return created.GetJob()
}

// rowFor is the listed row for one job, found by identifier rather than by position, so the case is
// about what the row says and not about the order the store answers in.
func rowFor(rows []console.Row, id string) (console.Row, bool) {
	for _, row := range rows {
		if row.ID == id {
			return row, true
		}
	}
	return console.Row{}, false
}

// actionNamed is the action the view binds under a label.
func actionNamed(resource console.Resource, label string) (console.Action, bool) {
	for _, action := range resource.Actions {
		if action.Label == label {
			return action, true
		}
	}
	return console.Action{}, false
}

// cellNamed is what a row says under one column heading, found by the heading rather than by counting
// positions in a slice. A case that counted broke the moment a column was added in front of the one it
// was about, and it broke in this tier only, which is the tier an untagged run never compiles.
func cellNamed(view console.Resource, row console.Row, title string) string {
	for at, column := range view.Columns {
		if column.Title != title {
			continue
		}
		if at >= len(row.Cells) {
			return ""
		}
		return row.Cells[at]
	}
	return ""
}
