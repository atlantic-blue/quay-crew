package flow_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/atlantic-blue/quay-crew/internal/work"
)

// These drive the engine against the real in memory store rather than a double of it, because what
// this slice changed is mostly what lands in a row: a run hangs under a piece of work, a step is a
// piece of work, and a movement writes both in one breath. A mock store would agree with whatever
// the engine did. The in memory store is held to the same conformance suite Postgres is.
//
// The crew below is a double, of the two things the engine asks the control plane for: preparing a
// declaration and putting a session away. It applies the same rules the control plane's PrepareWork
// does, and the whole road through the real one is proved in features/flows.feature.

// crew is the control plane as the engine sees it.
type crew struct {
	store    store.Store
	maxDepth int
	// refuse is what PrepareWork answers instead, once the run itself has been declared, for the case
	// where the crew will not take a step: too deep, a role the workspace does not hold.
	refuse   error
	prepared int
	// exported is every record offered to the log, and archived every session put away.
	exported []*work.Event
	archived []string
}

func (c *crew) PrepareWork(ctx context.Context, under string, declaration work.Declaration) (*work.Work, *work.Event, error) {
	c.prepared++
	if c.refuse != nil && c.prepared > 1 {
		return nil, nil, c.refuse
	}
	if err := declaration.Validate(); err != nil {
		return nil, nil, err
	}
	tidy := declaration.Tidied()
	declared := &work.Work{
		ID: store.NewID(), Workspace: tidy.Workspace, Project: tidy.Project,
		Title: tidy.Title, Brief: tidy.Brief, Role: tidy.Role, Mode: tidy.Mode,
		ExpectFile: tidy.ExpectFile, ExpectContains: tidy.ExpectContains, Labels: tidy.Labels,
		Version: 1, Phase: work.PhasePending, TraceID: "trace-of-the-tree",
	}
	if under != "" {
		parent, err := c.store.GetWork(ctx, under)
		if err != nil {
			return nil, nil, err
		}
		declared.Parent, declared.Depth = parent.ID, parent.Depth+1
	}
	if declared.Depth > c.maxDepth {
		return nil, nil, fmt.Errorf("this workspace allows work no deeper than %d, and this would be at depth %d",
			c.maxDepth, declared.Depth)
	}
	return declared, &work.Event{
		ID: store.NewID(), Kind: work.EventDeclared, Work: declared.ID,
		Workspace: declared.Workspace, Project: declared.Project, OccurredAt: time.Now().UTC(),
	}, nil
}

func (c *crew) ExportWork(_ context.Context, events ...*work.Event) {
	c.exported = append(c.exported, events...)
}

func (c *crew) ArchiveSession(_ context.Context, req *quaycrewv1.ArchiveSessionRequest) (*quaycrewv1.ArchiveSessionResponse, error) {
	c.archived = append(c.archived, req.GetId())
	return &quaycrewv1.ArchiveSessionResponse{}, nil
}

// aCrew stands an engine up over an empty store, with a workspace and a project to run in.
func aCrew(t *testing.T, graph string) (*flow.Engine, *crew, string, string) {
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
	// Deep enough that a run and its steps fit: the run's own work sits at the top and every step
	// one below it.
	it := &crew{store: kept, maxDepth: 4}
	return flow.NewEngine(kept, it, nil, it), it, workspace.GetId(), project.GetId()
}

// started begins a run and answers with it and the store it was written to.
func started(t *testing.T, engine *flow.Engine, it *crew, graph, workspace, project string) flow.Run {
	t.Helper()
	run, err := engine.Start(context.Background(), graph, workspace, project, nil)
	if err != nil {
		t.Fatalf("starting the run: %v", err)
	}
	return run
}

// stepOf is the piece of work a run's current step went out as. It is found by the labels every step
// carries, which is the road a person takes too: quay work list --label flow.run=<run>.
func stepOf(t *testing.T, it *crew, run flow.Run) *work.Work {
	t.Helper()
	listed, err := it.store.ListWork(context.Background(), work.Filter{
		LabelKey: "flow.run", LabelValue: run.ID,
	})
	if err != nil {
		t.Fatalf("ListWork: %v", err)
	}
	for _, one := range listed {
		if one.Labels["flow.node"] == run.Node && one.Phase == work.PhasePending {
			whole, err := it.store.GetWork(context.Background(), one.ID)
			if err != nil {
				t.Fatalf("GetWork: %v", err)
			}
			return whole
		}
	}
	t.Fatalf("the run is on %s and no piece of work is out for it", run.Node)
	return nil
}

// lands writes what came of a step, through the same three calls the work controller makes, so the
// row the engine then reads is the row a real landing leaves.
func lands(t *testing.T, it *crew, step *work.Work, session string, landed work.Landing) *work.Work {
	t.Helper()
	ctx := context.Background()
	lease := work.Lease{Owner: "a-controller", Until: time.Now().UTC().Add(time.Minute)}
	if _, err := it.store.StartWork(ctx, step.ID, lease, []*work.Event{aRecord(step, work.EventStarted)}); err != nil {
		t.Fatalf("StartWork: %v", err)
	}
	if session != "" {
		if err := it.store.RecordWorkSession(ctx, step.ID, session); err != nil {
			t.Fatalf("RecordWorkSession: %v", err)
		}
	}
	ended, err := it.store.LandWork(ctx, step.ID, landed, aRecord(step, work.EventAnswered))
	if err != nil {
		t.Fatalf("LandWork: %v", err)
	}
	return ended
}

func aRecord(of *work.Work, kind string) *work.Event {
	return &work.Event{
		ID: store.NewID(), Kind: kind, Work: of.ID,
		Workspace: of.Workspace, Project: of.Project, OccurredAt: time.Now().UTC(),
	}
}

// ticked runs one poll, which is what carries a run on from a step that ended.
func ticked(t *testing.T, engine *flow.Engine, it *crew, run flow.Run) flow.Run {
	t.Helper()
	flow.NewPoller(engine, 0, slog.New(slog.NewTextHandler(io.Discard, nil))).Tick(context.Background())
	kept, err := it.store.GetFlowRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetFlowRun: %v", err)
	}
	return *kept
}

// answered is a step that did what it was asked.
func answered(reply string) work.Landing {
	return work.Landing{Phase: work.PhaseDone, Answer: reply}
}
