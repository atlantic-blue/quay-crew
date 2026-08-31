//go:build integration

package store_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/auth"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/flow"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
)

// A run stopping at the first thing a person can open, over the real database and through the
// control plane.
//
// The unit tier proves where the reducer sends the run, and the engine tier proves the sentence
// reaches a step. Neither reaches what only Postgres can answer. The replacement is a write to the
// job carrying the run, and the step declared by the same movement reads its own sentence off that
// row through the control plane's own rules: the inheritance, the depth, the refusal of a tree with
// two products. Every one of those lives in a place a double would agree with whatever the engine
// did.

const theTranscriptFlow = `
name: transcript
version: 1
mode: edits
product: search the archive by video id
nodes:
  page:
    type: dispatch
    prompt: "put the thinnest page up and reply with its address"
    usable: true
  polish:
    type: dispatch
    prompt: "finish the page"
edges:
  - [page, polish]
  - [polish, done]
`

const theTranscriptAddress = "https://transcripts.example/videos?id=gyN9lV9QgyA"

// addressingRunner answers the first task with an address and echoes every later one, so the step
// that builds the thing a person opens says where it is and the steps after it are still told apart.
type addressingRunner struct {
	mu    sync.Mutex
	asked int
}

func (a *addressingRunner) Run(_ context.Context, _ sandbox.Sandbox, req model.Request) (model.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked++
	reply := "you said: " + req.Text
	if a.asked == 1 {
		reply = theTranscriptAddress
	}
	return model.Response{Reply: reply, ModelSessionID: fmt.Sprintf("conversation-%d", a.asked)}, nil
}

// The answer of no, over the database. It is first because it is the answer the whole gate exists
// for, and a test about yes passes whether or not this path works.
func TestARunToldNoReplacesItsSentenceInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &addressingRunner{})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	run := startedFlow(t, s, project, theTranscriptFlow)
	carrier, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: run.GetJob()})
	if err != nil {
		t.Fatalf("the job carrying the run is not there: %v", err)
	}
	if carrier.GetJob().GetProduct() != "search the archive by video id" {
		t.Fatalf("the job carrying the run serves %q, want the sentence the graph declares",
			carrier.GetJob().GetProduct())
	}

	asking := driveFlow(t, s, run.GetId(), flow.StatusAsking)
	for _, named := range []string{theTranscriptAddress, "search the archive by video id"} {
		if !strings.Contains(asking.GetQuestion(), named) {
			t.Fatalf("the run asks %q, want it to name %q", asking.GetQuestion(), named)
		}
	}
	// Only the step that built the thing has run. The point of stopping here is that an answer of no
	// costs one step.
	if steps := stepsOfRun(t, s, run.GetId()); len(steps) != 1 {
		t.Fatalf("the run declared %d steps before asking, want the one a person can open", len(steps))
	}

	if _, err := s.AnswerFlowRun(ctx, &quaycrewv1.AnswerFlowRunRequest{
		Id: run.GetId(), Answer: "paste a YouTube link and get the text back",
	}); err != nil {
		t.Fatalf("AnswerFlowRun: %v", err)
	}
	finished := driveFlow(t, s, run.GetId(), flow.StatusDone)
	if finished.GetNode() != "done" {
		t.Fatalf("the run ended on %q, want done", finished.GetNode())
	}

	ended, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: run.GetJob()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if ended.GetJob().GetProduct() != "paste a YouTube link and get the text back" {
		t.Fatalf("the job carrying the run serves %q, want the sentence the operator gave",
			ended.GetJob().GetProduct())
	}
	// The step declared by the same movement as the answer. It reads its sentence off the row above
	// it as it is written down, so this is the assertion the ordering exists for.
	steps := stepsOfRun(t, s, run.GetId())
	after, found := steps["polish"]
	if !found {
		t.Fatalf("the run declared %v, want a step after the question", nodesOf(steps))
	}
	if after.GetProduct() != "paste a YouTube link and get the text back" {
		t.Fatalf("the step after the question serves %q, want the sentence that replaced the first",
			after.GetProduct())
	}
	// The step before it keeps what it was built against. What was already done was done against the
	// old sentence, and rewriting that would be the record disagreeing with the work.
	if before := steps["page"]; before.GetProduct() != "search the archive by video id" {
		t.Errorf("the step that built the page now serves %q, want the sentence it was done against",
			before.GetProduct())
	}
}

func TestARunToldYesKeepsItsSentenceInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &addressingRunner{})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	run := startedFlow(t, s, project, theTranscriptFlow)
	driveFlow(t, s, run.GetId(), flow.StatusAsking)
	if _, err := s.AnswerFlowRun(ctx, &quaycrewv1.AnswerFlowRunRequest{
		Id: run.GetId(), Answer: "yes",
	}); err != nil {
		t.Fatalf("AnswerFlowRun: %v", err)
	}
	driveFlow(t, s, run.GetId(), flow.StatusDone)

	ended, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: run.GetJob()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if ended.GetJob().GetProduct() != "search the archive by video id" {
		t.Fatalf("the job carrying the run serves %q after a yes, want the sentence it started with",
			ended.GetJob().GetProduct())
	}
}

// An answer that is neither yes nor a sentence leaves the run where it is, so nothing carries on
// against a sentence nobody wrote.
func TestAnAnswerThatIsNotASentenceLeavesTheRunAskingInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &addressingRunner{})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	run := startedFlow(t, s, project, theTranscriptFlow)
	driveFlow(t, s, run.GetId(), flow.StatusAsking)

	_, err := s.AnswerFlowRun(ctx, &quaycrewv1.AnswerFlowRunRequest{
		Id: run.GetId(), Answer: strings.Repeat("a", job.ProductLimit+1),
	})
	if err == nil {
		t.Fatalf("an answer of %d bytes was taken as the sentence, and the ceiling is %d",
			job.ProductLimit+1, job.ProductLimit)
	}
	if !strings.Contains(err.Error(), "200") {
		t.Errorf("the refusal says %q, want it to say what the ceiling is", err)
	}
	still, err := s.GetFlowRun(ctx, &quaycrewv1.GetFlowRunRequest{Id: run.GetId()})
	if err != nil {
		t.Fatalf("GetFlowRun: %v", err)
	}
	if still.GetRun().GetStatus() != flow.StatusAsking {
		t.Fatalf("the run is %q after an answer it could not take, want it still asking",
			still.GetRun().GetStatus())
	}
	carrier, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: run.GetJob()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if carrier.GetJob().GetProduct() != "search the archive by video id" {
		t.Errorf("the job carrying the run serves %q, want the sentence untouched by an answer that was refused",
			carrier.GetJob().GetProduct())
	}
}

// A tree with two products has none, and a graph carries a sentence of its own, so a run started by
// a session whose job serves a different one is refused where somebody is looking rather than three
// steps in.
func TestARunWhoseSentenceDisagreesWithTheJobAboveItIsRefusedInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &addressingRunner{})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "build the transcript page", Brief: "start the flow",
		Product: "paste a link and get the text back",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	token, minted := s.JobCredentialForTest(ctx, declared.GetJob().GetId())
	if !minted {
		t.Fatal("no credential was minted for the job")
	}
	grant, recognised := s.Grants().Grant(token)
	if !recognised {
		t.Fatal("the system does not recognise the credential it minted")
	}

	startedFlow(t, s, project, theTranscriptFlow)
	_, err = s.StartFlow(auth.WithGrant(ctx, grant), &quaycrewv1.StartFlowRequest{
		Graph: "transcript", Project: project,
	})
	if err == nil {
		t.Fatal("a run serving one sentence started under a job serving another, so the tree has two products and no way to say which")
	}
	if !strings.Contains(err.Error(), "paste a link and get the text back") {
		t.Errorf("the refusal says %q, want it to name the sentence the job above it serves", err)
	}
}

// stepsOfRun is every step a run declared, by the node it belongs to.
func stepsOfRun(t *testing.T, s *controlplane.Server, run string) map[string]*quaycrewv1.Job {
	t.Helper()
	listed, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{
		LabelKey: "flow.run", LabelValue: run,
	})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	steps := map[string]*quaycrewv1.Job{}
	for _, one := range listed.GetJobs() {
		if node := one.GetLabels()["flow.node"]; node != "" {
			steps[node] = one
		}
	}
	return steps
}

func nodesOf(steps map[string]*quaycrewv1.Job) []string {
	named := make([]string, 0, len(steps))
	for node := range steps {
		named = append(named, node)
	}
	return named
}
