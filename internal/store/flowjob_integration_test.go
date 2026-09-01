//go:build integration

package store_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/flow"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

// A flow run over a real database.
//
// The unit tier proves the engine's decisions against the in memory store, and the conformance suite
// proves both stores keep the same contract. Neither reaches what only Postgres can answer: a
// movement that writes a run row, a transition, an idempotency claim, a new job and its
// record has to be one transaction against the real engine, and the foreign key from a run to the
// job that carries it either holds or it does not.

const twoStepFlow = `
name: fix-red
version: 1
mode: edits
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

// driveFlow ticks the two loops the system runs on its own until the run reaches a status, which is
// what the system does every few seconds.
func driveFlow(t *testing.T, s *controlplane.Server, id, want string) *quaycrewv1.FlowRun {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(20 * time.Second)
	for {
		s.TickJob(ctx)
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

// echoingRunner answers with the prompt it was asked, so two steps of one run answer differently.
//
// model.FakeRunner hands back one canned reply whatever it is asked, which is right for a test about
// a phase and useless for a test about an answer: every step would carry the same string, and the
// assertion would hold just as well if the system wrote one step's answer onto the other's row. This is
// what lets the answer be traced back to the task that gave it.
type echoingRunner struct {
	mu    sync.Mutex
	asked int
}

func (e *echoingRunner) Run(_ context.Context, _ sandbox.Sandbox, req model.Request) (model.Response, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.asked++
	return model.Response{
		Reply:          "you said: " + req.Text,
		ModelSessionID: fmt.Sprintf("conversation-%d", e.asked),
	}, nil
}

// The whole of what this slice buys, against the database that holds it: a run declares its step and
// returns, a controller runs it, and the run carries on from the row.
func TestAFlowRunDeclaresItsStepsAsJobInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &echoingRunner{})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	run := startedFlow(t, s, project, twoStepFlow)
	if run.GetJob() == "" {
		t.Fatal("the run was written outside the job tree, so neither depth nor budget counts it")
	}
	carrier, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: run.GetJob()})
	if err != nil {
		t.Fatalf("the job carrying the run is not there: %v", err)
	}
	// Held back rather than pending, or a controller would send the run's own job as a task.
	if carrier.GetJob().GetPhase() != job.PhaseWaiting {
		t.Fatalf("the job carrying the run is %q, want it held back", carrier.GetJob().GetPhase())
	}

	finished := driveFlow(t, s, run.GetId(), flow.StatusDone)
	if finished.GetNode() != "done" {
		t.Fatalf("the run ended on %q, want done", finished.GetNode())
	}

	// Every step is a job under the run, one level deeper, found by the label a person
	// would search on.
	listed, err := s.ListJobs(ctx, &quaycrewv1.ListJobsRequest{
		LabelKey: "flow.run", LabelValue: run.GetId(),
	})
	if err != nil {
		t.Fatalf("ListJob: %v", err)
	}
	steps := map[string]*quaycrewv1.Job{}
	for _, one := range listed.GetJobs() {
		if node := one.GetLabels()["flow.node"]; node != "" {
			steps[node] = one
		}
	}
	if len(steps) != 2 {
		t.Fatalf("the run declared %d steps, want fix and push", len(steps))
	}
	for node, step := range steps {
		if step.GetParent() != run.GetJob() {
			t.Errorf("step %s hangs under %q, want the run's own job %q", node, step.GetParent(), run.GetJob())
		}
		if step.GetDepth() != carrier.GetJob().GetDepth()+1 {
			t.Errorf("step %s is at depth %d and the run at %d, want one deeper",
				node, step.GetDepth(), carrier.GetJob().GetDepth())
		}
		if step.GetPhase() != job.PhaseDone {
			t.Errorf("step %s is %q, want done", node, step.GetPhase())
		}
	}
	// The answer of a step is a field a caller reads, which is the other half of what this buys. Both
	// steps, each against its own prompt, because a run with two steps is where an answer written onto
	// the wrong row would show and a single step would hide it.
	for node, prompt := range map[string]string{"fix": "fix the build", "push": "push the fix"} {
		whole, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: steps[node].GetId()})
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if !strings.Contains(whole.GetJob().GetAnswer(), prompt) {
			t.Errorf("step %s answers %q, want what its own task said about %q",
				node, whole.GetJob().GetAnswer(), prompt)
		}
	}
	// And the run's own job carries what the run came to.
	ended, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: run.GetJob()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if ended.GetJob().GetPhase() != job.PhaseDone || ended.GetJob().GetAnswer() == "" {
		t.Errorf("the run finished and its own job is %q answering %q",
			ended.GetJob().GetPhase(), ended.GetJob().GetAnswer())
	}
}

// The records quay-crew#349 named, against the database that keeps them. They are job events on the
// job that carries the run, so one history covers the run and everything it did.
func TestARunsOwnRecordsAreOnItsJobInPostgres(t *testing.T) {
	s, kept := aSystemWithAController(t, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	run := startedFlow(t, s, project, twoStepFlow)
	driveFlow(t, s, run.GetId(), flow.StatusDone)

	listed, err := kept.ListJobEvents(ctx, run.GetJob())
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	kinds := make([]string, 0, len(listed))
	for _, event := range listed {
		kinds = append(kinds, event.Kind)
		if event.Workspace == "" || event.TraceID == "" {
			t.Errorf("the record %q carries workspace %q and trace %q, want both off the row it describes",
				event.Kind, event.Workspace, event.TraceID)
		}
	}
	want := []string{job.EventDeclared, flow.EventRunStarted, flow.EventRunFinished}
	if len(kinds) != len(want) {
		t.Fatalf("the run's own job records %v, want %v", kinds, want)
	}
	for at := range want {
		if kinds[at] != want[at] {
			t.Fatalf("the run's own job records %v, want %v", kinds, want)
		}
	}
}

// A run started by a session hangs under that session's job, so the workspace's depth limit covers
// the whole run. This is the case the bound exists for: a job that starts a flow that
// starts more job.
func TestARunStartedBySomethingElseHangsUnderItInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &model.FakeRunner{Reply: "done"})
	ctx := context.Background()
	workspace, project := aProjectOnPostgres(t, s)

	if _, err := s.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{
		Limits: &quaycrewv1.WorkspaceLimits{Workspace: workspace, MaxDepth: 1},
	}); err != nil {
		t.Fatalf("SetWorkspaceLimits: %v", err)
	}
	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "clear the backlog", Brief: "start the flow",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	// The credential a task of that job runs under, which is what makes the parent the system's to
	// assign rather than the caller's to claim.
	token, minted := s.JobCredentialForTest(ctx, declared.GetJob().GetId())
	if !minted {
		t.Fatal("no credential was minted for the job")
	}
	grant, recognised := s.Grants().Grant(token)
	if !recognised {
		t.Fatal("the system does not recognise the credential it minted")
	}
	// The context a task of that job runs its calls under, which is what the interceptor builds from
	// the credential in the sandbox.
	running := auth.WithGrant(ctx, grant)

	startedFlow(t, s, project, twoStepFlow)
	started, err := s.StartFlow(running, &quaycrewv1.StartFlowRequest{Graph: "fix-red", Project: project})
	if err != nil {
		t.Fatalf("StartFlow as the job's session: %v", err)
	}
	carrier, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: started.GetRun().GetJob()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if carrier.GetJob().GetParent() != declared.GetJob().GetId() {
		t.Fatalf("the run hangs under %q, want the job whose session started it %q",
			carrier.GetJob().GetParent(), declared.GetJob().GetId())
	}
	if carrier.GetJob().GetDepth() != 1 {
		t.Fatalf("the run is at depth %d, want one below the job that started it", carrier.GetJob().GetDepth())
	}
}

// modeRecordingRunner keeps the permission mode of every task beside the prompt that carried it, so
// a test can say which step ran in which mode rather than that some task somewhere did.
type modeRecordingRunner struct {
	mu    sync.Mutex
	modes map[string]string
}

func (m *modeRecordingRunner) Run(_ context.Context, _ sandbox.Sandbox, req model.Request) (model.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.modes == nil {
		m.modes = map[string]string{}
	}
	m.modes[req.Text] = req.PermissionMode
	return model.Response{Reply: "done", ModelSessionID: "conversation-" + req.Text}, nil
}

// modeOf is the mode the task carrying this prompt ran in. Matched on what the graph wrote rather
// than on the whole text, because the system adds its own lines beside a step's prompt.
func (m *modeRecordingRunner) modeOf(prompt string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for asked, mode := range m.modes {
		if strings.Contains(asked, prompt) {
			return mode
		}
	}
	return ""
}

// quay-crew#461, over the whole road rather than the reducer alone.
//
// The unit tier proves the graph carries the mode it declares. That is not the question: the mode has
// to survive the graph becoming a job row in Postgres, a controller claiming that row, and a dispatch
// building the session, and every one of those is a place it could be dropped without a test noticing.
// A run whose steps arrive in the wrong mode stops to ask a person who is not there, which is the
// failure the whole change exists to stop, and it is invisible until something reads the mode the
// model was actually handed.
//
// Both steps, because a mode read once and forgotten would pass on the first and fail on the second.
func TestEveryStepOfARunIsDispatchedInTheModeItsGraphDeclaresInPostgres(t *testing.T) {
	runner := &modeRecordingRunner{}
	s, _ := aSystemWithAController(t, runner)
	_, project := aProjectOnPostgres(t, s)

	run := startedFlow(t, s, project, `
name: fix-red
version: 1
mode: dangerous
nodes:
  fix:  { type: dispatch, prompt: "fix the build" }
  ok:   { type: choice, on: { result.failed: "false" } }
  push: { type: dispatch, prompt: "push the fix" }
edges:
  - [fix, ok]
  - [ok, push, "true"]
  - [ok, done, "false"]
  - [push, done]
`)
	driveFlow(t, s, run.GetId(), flow.StatusDone)

	for _, prompt := range []string{"fix the build", "push the fix"} {
		if got := runner.modeOf(prompt); got != model.PermissionBypass {
			t.Errorf("the step %q ran in permission mode %q, want %q: a step in a narrower mode stops to ask a person the run does not have",
				prompt, got, model.PermissionBypass)
		}
	}
}

// The other mode, or a system that handed every task bypassPermissions whatever the graph said would
// pass the test above and be the more dangerous of the two faults.
func TestARunInANarrowerModeGetsThatModeInPostgres(t *testing.T) {
	runner := &modeRecordingRunner{}
	s, _ := aSystemWithAController(t, runner)
	_, project := aProjectOnPostgres(t, s)

	run := startedFlow(t, s, project, `
name: reading-only
version: 1
mode: plan
nodes:
  look: { type: dispatch, prompt: "read the repository and say what it does" }
edges:
  - [look, done]
`)
	driveFlow(t, s, run.GetId(), flow.StatusDone)

	if got := runner.modeOf("read the repository and say what it does"); got != model.PermissionPlan {
		t.Errorf("the step ran in permission mode %q, want %q", got, model.PermissionPlan)
	}
}
