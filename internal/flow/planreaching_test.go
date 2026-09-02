package flow_test

import (
	"context"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/flow"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// A graph whose readings read a plan, which is the shape flows/plan-reading.yaml has: the plan is
// rendered into the prompt, because a step is a new session with an empty working directory and a
// plan is a column on a row rather than a file.
const readingGraphReachingForAPlan = `
name: read-the-plan
version: 1
mode: edits
nodes:
  arrived: { type: trigger }
  critic:  { type: dispatch, prompt: "Read this plan.\n{{plan}}\nSay what you cannot settle." }
edges:
  - [arrived, critic]
  - [critic, done]
`

// The defect this answers: the shipped graph told three readings to read the plan of the job above
// them and nothing put that plan in front of them, so every lens read nothing and the run walked its
// whole success path reporting on it.
//
// It goes through the trigger road because that is the road that names the job a run hangs under: a
// job that finished raises a trigger, the run is carried by a job under that one, and the plan is on
// it. A session that starts a flow reaches the same place through its credential.
func TestAReadingIsHandedThePlanOfTheJobTheRunHangsUnder(t *testing.T) {
	engine, it, workspace, project := aSystem(t, readingGraphReachingForAPlan)
	ctx := context.Background()
	plan := "Step 1: read the design\nStep 2: build the page a person pastes a link into"
	planned := aPlannedJob(t, it, workspace, project, plan)

	if _, err := engine.Raise(ctx, flow.Trigger{
		GraphName: "read-the-plan", Workspace: workspace, Project: project,
		Cause: planned, Source: "the job that wrote the plan",
	}); err != nil {
		t.Fatalf("Raise: %v", err)
	}
	tick(t, engine)

	runs := runsOf(t, it, "read-the-plan")
	if len(runs) != 1 {
		t.Fatalf("%d runs exist after the tick, want the one the trigger asked for", len(runs))
	}
	run := runs[0]
	if run.State[flow.PlanKey] != plan {
		t.Fatalf("the run opened holding %q as the plan, want the plan of the job it hangs under", run.State[flow.PlanKey])
	}
	// And what the reading is actually handed, which is the half that stopping at the run's state
	// never sees. The brief is what the session is given.
	step := stepOf(t, it, *run)
	if !strings.Contains(step.Brief, "Step 2: build the page a person pastes a link into") {
		t.Fatalf("the reading was written down as %q, and it was told to read a plan it was never given", step.Brief)
	}
}

// A run started about a plan of its own keeps that one. A caller that passed a plan was asking for a
// reading of that plan, and the job above is not it.
func TestAPlanPassedAtTheStartIsTheOneTheReadingIsHanded(t *testing.T) {
	engine, it, workspace, project := aSystem(t, readingGraphReachingForAPlan)
	ctx := context.Background()
	above := aPlannedJob(t, it, workspace, project, "Step 1: the plan of the job above")

	if _, err := engine.Raise(ctx, flow.Trigger{
		GraphName: "read-the-plan", Workspace: workspace, Project: project,
		Cause:   above,
		Payload: map[string]string{flow.PlanKey: "Step 1: the plan the caller asked about"},
		Source:  "a caller naming the plan",
	}); err != nil {
		t.Fatalf("Raise: %v", err)
	}
	tick(t, engine)

	runs := runsOf(t, it, "read-the-plan")
	if len(runs) != 1 {
		t.Fatalf("%d runs exist after the tick", len(runs))
	}
	step := stepOf(t, it, *runs[0])
	if !strings.Contains(step.Brief, "the plan the caller asked about") {
		t.Fatalf("the reading was handed %q, want the plan the caller passed", step.Brief)
	}
	if strings.Contains(step.Brief, "the plan of the job above") {
		t.Fatalf("the reading was handed the plan of the job above rather than the one asked about: %q", step.Brief)
	}
}

// A run under nothing renders the template as typed, which is the graph author's signal that the run
// was started with no plan. It reads as nothing rather than as a plan, which is the whole difference.
func TestARunUnderNoJobIsHandedNoPlanRatherThanAnEmptyOne(t *testing.T) {
	engine, it, workspace, project := aSystem(t, readingGraphReachingForAPlan)
	ctx := context.Background()

	if _, err := engine.Raise(ctx, flow.Trigger{
		GraphName: "read-the-plan", Workspace: workspace, Project: project,
		Source: "a schedule, under nothing",
	}); err != nil {
		t.Fatalf("Raise: %v", err)
	}
	tick(t, engine)

	runs := runsOf(t, it, "read-the-plan")
	if len(runs) != 1 {
		t.Fatalf("%d runs exist after the tick", len(runs))
	}
	if held := runs[0].State[flow.PlanKey]; held != "" {
		t.Fatalf("the run holds %q as a plan, and no job above it wrote one", held)
	}
	step := stepOf(t, it, *runs[0])
	if !strings.Contains(step.Brief, "{{"+flow.PlanKey+"}}") {
		t.Fatalf("the reading was handed %q, want the template as typed so the lens can say it got nothing", step.Brief)
	}
}

// aPlannedJob is a job a person approved a plan on, which is what a reading of a plan reads.
func aPlannedJob(t *testing.T, it *system, workspace, project, plan string) string {
	t.Helper()
	planned := &job.Job{
		ID: store.NewID(), Workspace: workspace, Project: project,
		Title: "build the page a person pastes a link into",
		Brief: "the work the plan is for", Version: 1, Phase: job.PhaseRunning,
		Plan: plan, PlanApproved: true,
	}
	if err := it.store.CreateJob(context.Background(), planned, &job.Event{
		ID: store.NewID(), Kind: job.EventDeclared, Job: planned.ID,
		Workspace: workspace, Project: project,
	}); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return planned.ID
}
