package flow_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/flow"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// These drive the engine against the real in memory store rather than a double of it, because what
// this slice changed is mostly what lands in a row: a run hangs under a job, a step is a
// job, and a movement writes both in one breath. A mock store would agree with whatever
// the engine did. The in memory store is held to the same conformance suite Postgres is.
//
// The system below is a double, of the two things the engine asks the control plane for: preparing a
// declaration and putting a session away. It applies the same rules the control plane's PrepareJob
// does, and the whole road through the real one is proved in features/flows.feature.

// system is the control plane as the engine sees it.
type system struct {
	store store.Store
	// maxDeclared is how many jobs one session may declare here, which the run's own job is held to
	// and a step of the run is not: a step belongs to the run.
	maxDeclared int
	// refuse is what PrepareJob answers instead, once the run itself has been declared, for the case
	// where the system will not take a step: a role the workspace does not hold, a ceiling reached.
	refuse   error
	prepared int
	// exported is every record offered to the log, and archived every session put away.
	exported []*job.Event
	archived []string
}

func (c *system) PrepareJob(ctx context.Context, at job.Placement, declaration job.Declaration) (*job.Job, *job.Event, error) {
	c.prepared++
	if c.refuse != nil && c.prepared > 1 {
		return nil, nil, c.refuse
	}
	if err := declaration.Validate(); err != nil {
		return nil, nil, err
	}
	tidy := declaration.Tidied()
	declared := &job.Job{
		ID: store.NewID(), Workspace: tidy.Workspace, Project: tidy.Project,
		Title: tidy.Title, Brief: tidy.Brief, Role: tidy.Role, Mode: tidy.Mode,
		ExpectFile: tidy.ExpectFile, ExpectContains: tidy.ExpectContains, Labels: tidy.Labels,
		Product: tidy.Product,
		Version: 1, Phase: job.PhasePending, TraceID: "trace-of-the-tree",
	}
	// The same rules the control plane keeps. A step of a flow run belongs to the run and answers to
	// no ceiling, because the ceiling was asked of whoever started the run; anything else records what
	// caused it and counts against what one session may declare. A double looser than the real thing
	// manufactures a green run.
	declared.Run, declared.Cause = at.Run, at.Cause
	if at.Trace != "" {
		declared.TraceID = at.Trace
	}
	if at.Run != "" {
		return declared, c.declaredEvent(declared), nil
	}
	if at.Cause != "" {
		cause, err := c.store.GetJob(ctx, at.Cause)
		if err != nil {
			return nil, nil, err
		}
		already, err := c.store.JobsCausedBy(ctx, cause.ID)
		if err != nil {
			return nil, nil, err
		}
		if already >= c.maxDeclared {
			return nil, nil, fmt.Errorf("this workspace lets one session declare %d jobs, and this session "+
				"declared %d already", c.maxDeclared, already)
		}
	}
	return declared, c.declaredEvent(declared), nil
}

func (c *system) declaredEvent(declared *job.Job) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventDeclared, Job: declared.ID,
		Workspace: declared.Workspace, Project: declared.Project, OccurredAt: time.Now().UTC(),
	}
}

func (c *system) ExportJob(_ context.Context, events ...*job.Event) {
	c.exported = append(c.exported, events...)
}

func (c *system) ArchiveSession(_ context.Context, req *quaycrewv1.ArchiveSessionRequest) (*quaycrewv1.ArchiveSessionResponse, error) {
	c.archived = append(c.archived, req.GetId())
	return &quaycrewv1.ArchiveSessionResponse{}, nil
}

// aSystem stands an engine up over an empty store, with a workspace and a project to run in.
func aSystem(t *testing.T, graph string) (*flow.Engine, *system, string, string) {
	t.Helper()
	ctx := context.Background()
	kept := store.NewMemory()
	workspace, err := kept.CreateWorkspace(ctx, "acme")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := kept.CreateProject(ctx, workspace.GetId(), "house-bills")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	parsed, err := flow.Parse([]byte(graph))
	if err != nil {
		t.Fatalf("parse the graph: %v", err)
	}
	if err := kept.ImportFlowGraph(ctx, parsed.Name, parsed.Version, graph); err != nil {
		t.Fatalf("ImportFlowGraph: %v", err)
	}
	// Deep enough that a run and its steps fit: the run's own job sits at the top and every step
	// one below it.
	it := &system{store: kept, maxDeclared: 4}
	return flow.NewEngine(kept, it, nil, it), it, workspace.GetId(), project.GetId()
}

// started begins a run and answers with it and the store it was written to.
func started(t *testing.T, engine *flow.Engine, it *system, graph, workspace, project string) flow.Run {
	t.Helper()
	run, err := engine.Start(context.Background(), graph, workspace, project, nil)
	if err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	return run
}

// stepOf is the job a run's current step went out as. It is found by the labels every step
// carries, which is the road a person takes too: krewe job list --label flow.run=<run>.
func stepOf(t *testing.T, it *system, run flow.Run) *job.Job {
	t.Helper()
	listed, err := it.store.ListJobs(context.Background(), job.Filter{
		LabelKey: "flow.run", LabelValue: run.ID,
	})
	if err != nil {
		t.Fatalf("ListJob: %v", err)
	}
	for _, one := range listed {
		if one.Labels["flow.node"] == run.Node && one.Phase == job.PhasePending {
			whole, err := it.store.GetJob(context.Background(), one.ID)
			if err != nil {
				t.Fatalf("GetJob: %v", err)
			}
			return whole
		}
	}
	t.Fatalf("the run is on %s and no job is out for it", run.Node)
	return nil
}

// lands writes what came of a step, through the same three calls the job controller makes, so the
// row the engine then reads is the row a real landing leaves.
func lands(t *testing.T, it *system, step *job.Job, session string, landed job.Landing) *job.Job {
	t.Helper()
	ctx := context.Background()
	lease := job.Lease{Owner: "a-controller", Until: time.Now().UTC().Add(time.Minute)}
	if _, err := it.store.StartJob(ctx, step.ID, lease, []*job.Event{aRecord(step, job.EventStarted)}); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if session != "" {
		if err := it.store.RecordJobSession(ctx, step.ID, session); err != nil {
			t.Fatalf("RecordJobSession: %v", err)
		}
	}
	ended, err := it.store.LandJob(ctx, step.ID, landed, aRecord(step, job.EventAnswered))
	if err != nil {
		t.Fatalf("LandJob: %v", err)
	}
	return ended
}

func aRecord(of *job.Job, kind string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: kind, Job: of.ID,
		Workspace: of.Workspace, Project: of.Project, OccurredAt: time.Now().UTC(),
	}
}

// ticked runs one poll, which is what carries a run on from a step that ended.
func ticked(t *testing.T, engine *flow.Engine, it *system, run flow.Run) flow.Run {
	t.Helper()
	flow.NewPoller(engine, 0, slog.New(slog.NewTextHandler(io.Discard, nil))).Tick(context.Background())
	kept, err := it.store.GetFlowRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetFlowRun: %v", err)
	}
	return *kept
}

// answered is a step that did what it was asked.
func answered(reply string) job.Landing {
	return job.Landing{Phase: job.PhaseDone, Answer: reply}
}
