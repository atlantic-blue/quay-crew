//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/auth"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/work"
)

// A flow run over a real database.
//
// The unit tier proves the engine's decisions against the in memory store, and the conformance suite
// proves both stores keep the same contract. Neither reaches what only Postgres can answer: a
// movement that writes a run row, a transition, an idempotency claim, a new piece of work and its
// record has to be one transaction against the real engine, and the foreign key from a run to the
// work that carries it either holds or it does not.

const twoStepFlow = `
name: fix-red
version: 1
nodes:
  fix:  { type: dispatch, prompt: "fix the build" }
  ok:   { type: choice, on: { result.failed: "false" } }
  push: { type: dispatch, prompt: "push the fix" }
edges:
  - [fix, ok]
  - [ok, push, "true"]
  - [ok, done, "false"]
  - [push, done]
`

// driveFlow ticks the two loops the crew runs on its own until the run reaches a status, which is
// what the crew does every few seconds.
func driveFlow(t *testing.T, s *controlplane.Server, id, want string) *quaycrewv1.FlowRun {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(20 * time.Second)
	for {
		s.TickWork(ctx)
		s.TickFlows(ctx)
		found, err := s.GetFlowRun(ctx, &quaycrewv1.GetFlowRunRequest{Id: id})
		if err != nil {
			t.Fatalf("GetFlowRun: %v", err)
		}
		if found.GetRun().GetStatus() == want {
			return found.GetRun()
		}
		if time.Now().After(deadline) {
			t.Fatalf("the run is %q on node %q saying %q, want %q", found.GetRun().GetStatus(),
				found.GetRun().GetNode(), found.GetRun().GetReason(), want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func startedFlow(t *testing.T, s *controlplane.Server, project, definition string) *quaycrewv1.FlowRun {
	t.Helper()
	ctx := context.Background()
	if _, err := s.ImportFlow(ctx, &quaycrewv1.ImportFlowRequest{Definition: definition}); err != nil {
		t.Fatalf("ImportFlow: %v", err)
	}
	graph, err := flow.Parse([]byte(definition))
	if err != nil {
		t.Fatalf("parse the graph: %v", err)
	}
	started, err := s.StartFlow(ctx, &quaycrewv1.StartFlowRequest{Graph: graph.Name, Project: project})
	if err != nil {
		t.Fatalf("StartFlow: %v", err)
	}
	return started.GetRun()
}

// The whole of what this slice buys, against the database that holds it: a run declares its step and
// returns, a controller runs it, and the run carries on from the row.
func TestAFlowRunDeclaresItsStepsAsWorkInPostgres(t *testing.T) {
	s, _ := aCrewWithAController(t, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	run := startedFlow(t, s, project, twoStepFlow)
	if run.GetWork() == "" {
		t.Fatal("the run was written outside the work tree, so neither depth nor budget counts it")
	}
	carrier, err := s.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: run.GetWork()})
	if err != nil {
		t.Fatalf("the work carrying the run is not there: %v", err)
	}
	// Held back rather than pending, or a controller would send the run's own work as a task.
	if carrier.GetWork().GetPhase() != work.PhaseWaiting {
		t.Fatalf("the work carrying the run is %q, want it held back", carrier.GetWork().GetPhase())
	}

	finished := driveFlow(t, s, run.GetId(), flow.StatusDone)
	if finished.GetNode() != "done" {
		t.Fatalf("the run ended on %q, want done", finished.GetNode())
	}

	// Every step is a piece of work under the run, one level deeper, found by the label a person
	// would search on.
	listed, err := s.ListWork(ctx, &quaycrewv1.ListWorkRequest{
		LabelKey: "flow.run", LabelValue: run.GetId(),
	})
	if err != nil {
		t.Fatalf("ListWork: %v", err)
	}
	steps := map[string]*quaycrewv1.Work{}
	for _, one := range listed.GetWork() {
		if node := one.GetLabels()["flow.node"]; node != "" {
			steps[node] = one
		}
	}
	if len(steps) != 2 {
		t.Fatalf("the run declared %d steps, want fix and push", len(steps))
	}
	for node, step := range steps {
		if step.GetParent() != run.GetWork() {
			t.Errorf("step %s hangs under %q, want the run's own work %q", node, step.GetParent(), run.GetWork())
		}
		if step.GetDepth() != carrier.GetWork().GetDepth()+1 {
			t.Errorf("step %s is at depth %d and the run at %d, want one deeper",
				node, step.GetDepth(), carrier.GetWork().GetDepth())
		}
		if step.GetPhase() != work.PhaseDone {
			t.Errorf("step %s is %q, want done", node, step.GetPhase())
		}
	}
	// The answer of a step is a field a caller reads, which is the other half of what this buys.
	whole, err := s.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: steps["fix"].GetId()})
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if !strings.Contains(whole.GetWork().GetAnswer(), "fix the build") {
		t.Errorf("the step answers %q, want what its task said", whole.GetWork().GetAnswer())
	}
	// And the run's own work carries what the run came to.
	ended, err := s.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: run.GetWork()})
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if ended.GetWork().GetPhase() != work.PhaseDone || ended.GetWork().GetAnswer() == "" {
		t.Errorf("the run finished and its own work is %q answering %q",
			ended.GetWork().GetPhase(), ended.GetWork().GetAnswer())
	}
}

// The records quay-crew#349 named, against the database that keeps them. They are work events on the
// piece of work that carries the run, so one history covers the run and everything it did.
func TestARunsOwnRecordsAreOnItsWorkInPostgres(t *testing.T) {
	s, kept := aCrewWithAController(t, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	run := startedFlow(t, s, project, twoStepFlow)
	driveFlow(t, s, run.GetId(), flow.StatusDone)

	listed, err := kept.ListWorkEvents(ctx, run.GetWork())
	if err != nil {
		t.Fatalf("ListWorkEvents: %v", err)
	}
	kinds := make([]string, 0, len(listed))
	for _, event := range listed {
		kinds = append(kinds, event.Kind)
		if event.Workspace == "" || event.TraceID == "" {
			t.Errorf("the record %q carries workspace %q and trace %q, want both off the row it describes",
				event.Kind, event.Workspace, event.TraceID)
		}
	}
	want := []string{work.EventDeclared, flow.EventRunStarted, flow.EventRunFinished}
	if len(kinds) != len(want) {
		t.Fatalf("the run's own work records %v, want %v", kinds, want)
	}
	for at := range want {
		if kinds[at] != want[at] {
			t.Fatalf("the run's own work records %v, want %v", kinds, want)
		}
	}
}

// A run started by a session hangs under that session's work, so the workspace's depth limit covers
// the whole run. This is the case the bound exists for: a piece of work that starts a flow that
// starts more work.
func TestARunStartedBySomethingElseHangsUnderItInPostgres(t *testing.T) {
	s, _ := aCrewWithAController(t, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)

	if _, err := s.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: workspace, MaxDepth: 1},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
	declared, err := s.CreateWork(ctx, &quaycrewv1.CreateWorkRequest{
		Project: project, Title: "clear the backlog", Brief: "start the flow",
	})
	if err != nil {
		t.Fatalf("CreateWork: %v", err)
	}
	// The credential a task of that work runs under, which is what makes the parent the crew's to
	// assign rather than the caller's to claim.
	token, minted := s.WorkCredentialForTest(ctx, declared.GetWork().GetId())
	if !minted {
		t.Fatal("no credential was minted for the work")
	}
	grant, recognised := s.Grants().Grant(token)
	if !recognised {
		t.Fatal("the crew does not recognise the credential it minted")
	}
	// The context a task of that work runs its calls under, which is what the interceptor builds from
	// the credential in the sandbox.
	running := auth.WithGrant(ctx, grant)

	startedFlow(t, s, project, twoStepFlow)
	started, err := s.StartFlow(running, &quaycrewv1.StartFlowRequest{Graph: "fix-red", Project: project})
	if err != nil {
		t.Fatalf("StartFlow as the work's session: %v", err)
	}
	carrier, err := s.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: started.GetRun().GetWork()})
	if err != nil {
		t.Fatalf("GetWork: %v", err)
	}
	if carrier.GetWork().GetParent() != declared.GetWork().GetId() {
		t.Fatalf("the run hangs under %q, want the work whose session started it %q",
			carrier.GetWork().GetParent(), declared.GetWork().GetId())
	}
	if carrier.GetWork().GetDepth() != 1 {
		t.Fatalf("the run is at depth %d, want one below the work that started it", carrier.GetWork().GetDepth())
	}
}
