//go:build integration

package web

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A read of jobs by the moment they finished, over the whole path: a real Postgres, the real control
// plane and the real gRPC interface, with nothing stood in for but the model. The briefing is drawn
// from the same database at the end of it.
//
// The tables beside this one prove how a block is built, against a store that keeps whatever the case
// wrote and answers from the struct. This is the part they cannot say: that a window and an order
// written as SQL come back the way the conformance suite says they do, through the interface every
// caller speaks. The jobs below are declared in the reverse of the order they ended in, because
// finished_at and created_at are not in step and a read by the wrong one answers backwards.
func TestAReadByTheMomentAJobFinishedGoesThroughTheWholeSystem(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	kept := aDurableStore(t, ctx)
	client := systemOver(t, kept, &model.FakeRunner{Reply: "it is due on the 14th"})
	// A place of this run's own, and titles marked with it. A database named by the environment is
	// reused between runs, so a case looking for a bare title would read the run before it too.
	workspace, project, address := aPlaceOfItsOwn(t, ctx, client)
	mark := func(title string) string { return title + " " + display.ShortID(project) }

	now := time.Now().UTC()
	writeJob(t, kept, &job.Job{
		Workspace: workspace, Project: project, Title: mark("deploy the pricing change"),
		Phase: job.PhaseAsking, Question: "the migration drops a column. Do I run it?",
	})
	writeJob(t, kept, &job.Job{
		Workspace: workspace, Project: project, Title: mark("migrate the ledger"),
		Phase: job.PhaseFailed, Reason: "the model gave up on the third attempt",
		FinishedAt: moment(now.Add(-2 * time.Hour)),
	})
	writeJob(t, kept, &job.Job{
		Workspace: workspace, Project: project, Title: mark("the newer piece of work"),
		Phase:     job.PhaseDone,
		CreatedAt: now.Add(-96 * time.Hour), FinishedAt: moment(now.Add(-time.Hour)),
		PullRequest: "https://github.com/atlantic-blue/quay-crew/pull/2",
	})
	writeJob(t, kept, &job.Job{
		Workspace: workspace, Project: project, Title: mark("the older piece of work"),
		Phase:     job.PhaseDone,
		CreatedAt: now.Add(-72 * time.Hour), FinishedAt: moment(now.Add(-24 * time.Hour)),
		PullRequest: "https://github.com/atlantic-blue/quay-crew/pull/1",
	})
	writeJob(t, kept, &job.Job{
		Workspace: workspace, Project: project, Title: mark("something from last month"),
		Phase:     job.PhaseDone,
		CreatedAt: now.Add(-48 * time.Hour), FinishedAt: moment(now.Add(-30 * 24 * time.Hour)),
	})

	// The window and the cap, asked for the way the tool would ask. Nothing that finished before the
	// window is in it, and a job that has not finished is not in it at all.
	week := timestamppb.New(now.Add(-7 * 24 * time.Hour))
	read, err := client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{
		Project: project, Phase: job.PhaseDone, FinishedSince: week, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	for _, one := range read.GetJobs() {
		if one.GetTitle() == mark("something from last month") {
			t.Error("work that finished before the window came back inside it")
		}
	}
	if len(read.GetJobs()) != 2 {
		t.Fatalf("the window holds %d jobs, want the two that finished inside it", len(read.GetJobs()))
	}
	if read.GetJobs()[0].GetTitle() != mark("the newer piece of work") {
		t.Errorf("the window opens with %q, want the most recently finished first", read.GetJobs()[0].GetTitle())
	}
	capped, err := client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{
		Project: project, Phase: job.PhaseDone, FinishedSince: week, Limit: 1,
	})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(capped.GetJobs()) != 1 || capped.GetJobs()[0].GetTitle() != mark("the newer piece of work") {
		t.Fatalf("a cap of one gave %d rows, want the most recently finished alone", len(capped.GetJobs()))
	}

	body, status := get(t, client, "/")
	if status != 200 {
		t.Fatalf("the briefing answered %d", status)
	}
	for _, want := range []string{
		"the migration drops a column. Do I run it?",
		"krewe job answer",
		"the model gave up on the third attempt",
		"https://github.com/atlantic-blue/quay-crew/pull/2",
		address,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the briefing does not carry %q:\n%s", want, body)
		}
	}
	newer := strings.Index(body, mark("the newer piece of work"))
	older := strings.Index(body, mark("the older piece of work"))
	if newer < 0 || older < 0 {
		t.Fatalf("both pieces of work should be on the page:\n%s", body)
	}
	if newer > older {
		t.Error("Postgres answered oldest finished first, so the top row is not the latest thing the system produced")
	}
}

// aPlaceOfItsOwn is a workspace and a project this run alone put there, so a database reused between
// runs cannot answer with the run before it.
func aPlaceOfItsOwn(t *testing.T, ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) (string, string, string) {
	t.Helper()
	name := "acme-" + display.ShortID(store.NewID())
	made, err := client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: name})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: made.GetWorkspace().GetId(), Name: "house-bills",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return made.GetWorkspace().GetId(), project.GetProject().GetId(), name + "/house-bills"
}

// aDurableStore is a migrated Postgres. It takes one that is already running where the environment
// names it, which is the same variable the store package's own tier reads, and starts a container
// otherwise. Reading it means this tier can be run on a machine with no container daemon.
func aDurableStore(t *testing.T, ctx context.Context) store.Store {
	t.Helper()
	url := os.Getenv("QC_TEST_DATABASE_URL")
	if url == "" {
		container, err := postgres.Run(ctx, "postgres:17-alpine",
			postgres.WithDatabase("quaycrew"),
			postgres.WithUsername("quaycrew"),
			postgres.WithPassword("quaycrew"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(60*time.Second),
			),
		)
		if err != nil {
			t.Fatalf("start postgres: %v", err)
		}
		t.Cleanup(func() {
			timeout := 30 * time.Second
			_ = container.Stop(context.Background(), &timeout)
		})
		if url, err = container.ConnectionString(ctx, "sslmode=disable"); err != nil {
			t.Fatalf("connection string: %v", err)
		}
	}
	durable, err := store.NewPostgres(ctx, url)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	t.Cleanup(durable.Close)
	emptyOfJobs(t, ctx, url)
	return durable
}

// emptyOfJobs clears the jobs this case reads before it writes its own.
//
// The briefing is the whole system in one page, so it narrows by no project, and a database handed to
// this tier by the environment is reused between runs. Left alone, the rows of the run before fill the
// produced block's cap and this case reads them instead of its own. A container database is already
// empty, so this is one path rather than two, and the store package's own tier truncates for the same
// reason.
func emptyOfJobs(t *testing.T, ctx context.Context, url string) {
	t.Helper()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect to empty the jobs: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, "truncate table jobs cascade"); err != nil {
		t.Fatalf("empty the jobs: %v", err)
	}
}

func moment(at time.Time) *time.Time { return &at }
