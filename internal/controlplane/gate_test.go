package controlplane_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/auth"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/secrets"
	"github.com/atlantic-blue/krewe/internal/store"
)

// The gate driven through the whole system: the control plane, its store, its sandboxes and its
// controller, with only the model substituted.
//
// The unit tier proves the loop's decisions against doubles. This is the tier that proves the two
// gates are sessions of their own with containers of their own, that a fail lands in the conversation
// that holds the branch, that a gate is handed no credential, and that what comes back off the row
// says which of them passed. Stopping at the decisions would leave all four unproved.
//
// The refusal comes first. A test that a job both gates passed reaches done passes just as happily
// against a gate that passes everything, which is the state the system was already in.

// answering is a model that answers by what it was asked.
//
// Three conversations are in flight at once here, so a queue by position would be a test about the
// order the system happens to ask in rather than about the gate.
type answering struct {
	mu sync.Mutex
	// says is a phrase against what the model answers a task carrying it, first match winning.
	says [][2]string
	// asked is every task this model was given, and whether it arrived holding a credential.
	asked []asked
}

// asked is one task as the model received it.
type asked struct {
	text string
	// credential is whether the task arrived with a token in its environment, which is what says
	// whether that session may call the system at all.
	credential bool
}

func (a *answering) when(carrying, answer string) *answering {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.says = append(a.says, [2]string{carrying, answer})
	return a
}

func (a *answering) Run(_ context.Context, _ sandbox.Sandbox, req model.Request) (model.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.asked = append(a.asked, asked{text: req.Text, credential: req.Env[auth.TokenEnv] != ""})
	for _, pair := range a.says {
		if strings.Contains(req.Text, pair[0]) {
			return model.Response{Reply: pair[1], ModelSessionID: req.ModelSessionID}, nil
		}
	}
	return model.Response{Reply: "you said: " + req.Text, ModelSessionID: req.ModelSessionID}, nil
}

// timesAsked is how many tasks this model was given carrying a phrase.
func (a *answering) timesAsked(carrying string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	count := 0
	for _, one := range a.asked {
		if strings.Contains(one.text, carrying) {
			count++
		}
	}
	return count
}

// heldACredential is whether every task carrying a phrase arrived with a token, and whether any did.
func (a *answering) heldACredential(carrying string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, one := range a.asked {
		if strings.Contains(one.text, carrying) && one.credential {
			return true
		}
	}
	return false
}

const theChange = "https://github.com/atlantic-blue/quay-crew/pull/454"

// theWork is a phrase from the brief, so a scenario can say what the session doing the work answers.
const theWork = "make the listing"

// gatedBy stands the system up with a model that answers each conversation in turn, and declares a
// job that names a repository. reachable is set on every one of them so a credential is real where the
// system mints one: a check that a gate holds none is worth nothing against a system that mints none.
func gatedBy(t *testing.T, runner model.Runner, ungated bool) (*controlplane.Server, string) {
	t.Helper()
	server := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: runner,
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Reachable: "controlplane:50051",
	})
	_, project := newProject(t, server)
	declared, err := server.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "sort the listing",
		Brief:      "make the listing sort by the clock it shows",
		Repository: "atlantic-blue/quay-crew", Ungated: ungated,
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if declared.GetJob().GetUngated() != ungated {
		t.Fatalf("the job was declared ungated=%v and the row says %v",
			ungated, declared.GetJob().GetUngated())
	}
	return server, declared.GetJob().GetId()
}

// tick keeps the controller going until something is true, or says what the row reached instead. The
// loop is driven rather than started, so what is specified is what a pass over the rows does.
func tick(t *testing.T, server *controlplane.Server, id string, until func(*quaycrewv1.Job) bool) *quaycrewv1.Job {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(30 * testWait)
	for time.Now().Before(deadline) {
		server.TickJob(ctx)
		one := jobNow(t, server, id)
		if until(one) {
			return one
		}
		time.Sleep(testWait / 20)
	}
	one := jobNow(t, server, id)
	t.Fatalf("the job is %q saying %q, and never reached what this test is about", one.GetPhase(), one.GetReason())
	return nil
}

// inPhase is the condition for a job reaching one phase.
func inPhase(phase string) func(*quaycrewv1.Job) bool {
	return func(one *quaycrewv1.Job) bool { return one.GetPhase() == phase }
}

// The refusal, through the whole system. The reviewer fails the work, and the job does not settle: the
// fail goes back to the session that did it as its next task, and nothing reaches the operator.
func TestAJobWhoseReviewerFailsItDoesNotSettle(t *testing.T) {
	runner := (&answering{}).
		when("You are the "+job.GateReviewer, "Verdict: fail the change adds a column and no migration").
		when("You are the "+job.GateTester, "Verdict: pass 540 test files ran").
		when(theWork, "I made the change and opened "+theChange)
	server, id := gatedBy(t, runner, false)

	// Ticked until the session that did the work has been given a second task, which is the fail
	// coming back. The job is still running the whole way: nothing settled it.
	one := tick(t, server, id, func(*quaycrewv1.Job) bool {
		return len(tasksOf(t, server, id)) > 1
	})

	if one.GetPhase() != job.PhaseRunning {
		t.Fatalf("the job is %q saying %q, want running: a fail is the next task, not the end",
			one.GetPhase(), one.GetReason())
	}
	if one.GetReviewed() || one.GetTested() {
		t.Fatalf("the job says reviewed=%v tested=%v with the reviewer having failed it",
			one.GetReviewed(), one.GetTested())
	}
	// What the session was handed, off the record rather than off the double, because the record is
	// what the system really sent. It carries the reason, and it asks for the address again: this
	// answer is the one that ends the job.
	sent := tasksOf(t, server, id)
	last := sent[len(sent)-1].GetPrompt()
	for _, want := range []string{job.GateReviewer, "a column and no migration", "pull request"} {
		if !strings.Contains(last, want) {
			t.Errorf("the work went back saying %q, want it to say %q", last, want)
		}
	}
	// And it went back to the conversation that holds the branch, which is the session on the row. A
	// second session would leave the work behind in the first one with nothing able to push it.
	if !job.SentBackByTheGate(last) {
		t.Fatalf("the second task of the working session is not the gate sending it back: %q", last)
	}
	// The tester was never asked. A change the reviewer failed does not need testing, and a container
	// nobody needed is a bill nobody agreed to.
	if asked := runner.timesAsked("You are the " + job.GateTester); asked != 0 {
		t.Fatalf("the tester was asked %d times about work the reviewer failed", asked)
	}
}

// A gate that answered without a verdict judged nothing, so the job stops rather than settling while
// reporting that it was checked.
func TestAJobWhoseGateSaysNothingStops(t *testing.T) {
	runner := (&answering{}).
		when("You are the "+job.GateReviewer, "I read the change and it seems reasonable enough.").
		when(theWork, "I made the change and opened "+theChange)
	server, id := gatedBy(t, runner, false)

	stopped := tick(t, server, id, inPhase(job.PhaseStopped))

	for _, want := range []string{job.GateReviewer, "without a verdict"} {
		if !strings.Contains(stopped.GetReason(), want) {
			t.Errorf("the job stopped saying %q, want it to say %q", stopped.GetReason(), want)
		}
	}
	// What it produced is still on the row: the end of the job is not the end of the work.
	if stopped.GetPullRequest() != theChange {
		t.Fatalf("the stopped job names the pull request %q, want %s", stopped.GetPullRequest(), theChange)
	}
}

// And the road through. Both gates pass, the job settles, and the row says what passed it.
func TestAJobBothGatesPassSettlesSayingWhatPassedIt(t *testing.T) {
	runner := (&answering{}).
		when("You are the "+job.GateReviewer, "Verdict: pass it does what the brief asked").
		when("You are the "+job.GateTester, "Verdict: pass 540 test files ran, 6034 tests, all green").
		when(theWork, "I made the change and opened "+theChange)
	server, id := gatedBy(t, runner, false)

	done := tick(t, server, id, inPhase(job.PhaseDone))

	if !done.GetReviewed() || !done.GetTested() {
		t.Fatalf("the settled job says reviewed=%v tested=%v, want both",
			done.GetReviewed(), done.GetTested())
	}
	if done.GetPullRequest() != theChange {
		t.Fatalf("the settled job names the pull request %q, want %s", done.GetPullRequest(), theChange)
	}
	// The answer is still the work's own. The gates are the evidence beside it, not a replacement.
	if !strings.Contains(done.GetAnswer(), theChange) {
		t.Fatalf("the answer is %q, want what the session that did the work said", done.GetAnswer())
	}
	// Each gate read the change once, and each read it somewhere that is not the working session.
	for _, gate := range []string{job.GateReviewer, job.GateTester} {
		if asked := runner.timesAsked("You are the " + gate); asked != 1 {
			t.Fatalf("the %s was asked %d times, want once", gate, asked)
		}
	}
	held := map[string]bool{}
	listed, err := server.ListSessions(context.Background(), &quaycrewv1.ListSessionsRequest{
		Project: done.GetProject(),
	})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	for _, session := range listed.GetSessions() {
		held[session.GetHandle()] = true
	}
	for _, handle := range []string{job.SessionFor(id), job.ReviewerFor(id), job.TesterFor(id)} {
		if !held[handle] {
			t.Fatalf("the system holds no session %q, so a gate ran in the session that did the work", handle)
		}
	}
}

// A job declared with the gate off settles on its own answer, and the row reads differently from one
// two sessions passed. That is what makes the gate refusable rather than optional.
func TestAJobDeclaredWithTheGateOffSettlesAndSaysSo(t *testing.T) {
	runner := (&answering{}).when(theWork, "I made the change and opened "+theChange)
	server, id := gatedBy(t, runner, true)

	done := tick(t, server, id, inPhase(job.PhaseDone))

	if done.GetReviewed() || done.GetTested() {
		t.Fatalf("a job with the gate off says reviewed=%v tested=%v",
			done.GetReviewed(), done.GetTested())
	}
	// Nothing was asked to read it, so nothing was paid for.
	for _, gate := range []string{job.GateReviewer, job.GateTester} {
		if asked := runner.timesAsked("You are the " + gate); asked != 0 {
			t.Fatalf("the %s was asked %d times about a job declared with the gate off", gate, asked)
		}
	}
}

// The boundary, read where it is real. A session is granted verbs by the job it runs, and a gate runs
// no job, so its task arrives with no credential in it. The working session is the control: the same
// system, the same moment, and its task does carry one.
func TestAGateHoldsNoCredential(t *testing.T) {
	runner := (&answering{}).
		when("You are the "+job.GateReviewer, "Verdict: pass it does what the brief asked").
		when("You are the "+job.GateTester, "Verdict: pass 540 test files ran, all green").
		when(theWork, "I made the change and opened "+theChange)
	server, id := gatedBy(t, runner, false)

	tick(t, server, id, inPhase(job.PhaseDone))

	if !runner.heldACredential(theWork) {
		t.Fatal("the session doing the work held no credential, so this proves nothing about the gate")
	}
	for _, gate := range []string{job.GateReviewer, job.GateTester} {
		if runner.heldACredential("You are the " + gate) {
			t.Fatalf("the %s was handed a credential, so it may call the system", gate)
		}
	}
}
