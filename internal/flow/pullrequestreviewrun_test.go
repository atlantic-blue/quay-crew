package flow_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/atlantic-blue/quay-crew/internal/job"
)

// The shipped review graph, run through the engine and the real store rather than read as a file.
//
// The unit tests next to this one hold the shape of the graph. These hold what a run of it does:
// where it stops, what the operator is shown, and what has been declared by the time they are
// asked. The difference matters, because the stop is what keeps the system from posting to somebody
// else's pull request on its own, and an edge that reads correctly is not the same as a run that
// halts.

// theReviewGraph is the file that ships, read off disk. A copy written here would prove the copy.
func theReviewGraphSource(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("../../flows/pull-request-review.yaml")
	if err != nil {
		t.Fatalf("reading the shipped graph: %v", err)
	}
	return string(body)
}

// The pull request a run of this picks, and what each of its steps answers. The first reply is the
// address, because the pick step answers with the address or with the word none, and every later
// reply carries the address on its first line the way the graph asks.
const reviewed = "https://github.com/atlantic-blue/quay-crew/pull/512"

const theDraft = reviewed + `

1. The new store call has no policy, so every request from the reader fails.
2. Is this deployed anywhere? Nothing names the module.

internal/store/reader.go:40 the call needs the policy this role does not carry.`

// worked lands one step with the answer given and carries the run on, which is what the controller
// and the poller do between them.
func worked(t *testing.T, engine *flow.Engine, it *system, run flow.Run, reply string) flow.Run {
	t.Helper()
	step := stepOf(t, it, run)
	lands(t, it, step, "session-of-"+step.Labels["flow.node"], answered(reply))
	return ticked(t, engine, it, run)
}

// declaredFor is every job the run declared for one node of the graph, whatever phase it
// reached. It is the question "has this step ever been sent" rather than "is it out now".
func declaredFor(t *testing.T, it *system, run flow.Run, node string) []*job.Job {
	t.Helper()
	listed, err := it.store.ListJobs(context.Background(), job.Filter{
		LabelKey: "flow.run", LabelValue: run.ID,
	})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	var out []*job.Job
	for _, one := range listed {
		if one.Labels["flow.node"] == node {
			out = append(out, one)
		}
	}
	return out
}

// asking drives a run of the shipped graph up to the question, which is five steps.
func asking(t *testing.T, engine *flow.Engine, it *system, workspace, project string) flow.Run {
	t.Helper()
	run := started(t, engine, it, "pull-request-review", workspace, project)
	for _, reply := range []string{reviewed, reviewed + "\nno secrets in the diff", reviewed + "\nit is not deployed", reviewed + "\nno scenario", theDraft} {
		run = worked(t, engine, it, run, reply)
	}
	return run
}

// The stop the whole graph is built around. The run reaches the question holding the draft, and
// nothing has been declared to post it: the operator has not answered yet, so there is nothing on
// the pull request.
func TestAReviewStopsAtTheQuestionWithNothingPosted(t *testing.T) {
	engine, it, workspace, project := aSystem(t, theReviewGraphSource(t))

	run := asking(t, engine, it, workspace, project)

	if run.Status != flow.StatusAsking {
		t.Fatalf("the run is %q on %q, want it asking the operator", run.Status, run.Node)
	}
	if !strings.Contains(run.Question, theDraft) {
		t.Fatalf("the operator is asked %q, and the draft is not in it", run.Question)
	}
	if posted := declaredFor(t, it, run, "post"); len(posted) != 0 {
		t.Fatalf("%d posting steps were declared while the operator is still deciding", len(posted))
	}
	// The three passes ran, in the order the graph declares, before anything was drafted.
	var passes []string
	for _, node := range []string{"security", "features", "completeness", "draft"} {
		if len(declaredFor(t, it, run, node)) != 1 {
			t.Errorf("the %s step was declared %d times, want once", node, len(declaredFor(t, it, run, node)))
		}
		passes = append(passes, node)
	}
	transitions, err := it.store.ListFlowTransitions(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ListFlowTransitions: %v", err)
	}
	var moved []string
	for _, one := range transitions {
		moved = append(moved, one.Node)
	}
	if want := strings.Join(append([]string{"pick"}, append(passes, "permit")...), " "); strings.Join(moved, " ") != want {
		t.Fatalf("the run moved through %q, want %q", strings.Join(moved, " "), want)
	}
}

// Answering no ends the run and posts nothing, which is what makes the question real.
func TestAReviewToldNoPostsNothing(t *testing.T) {
	engine, it, workspace, project := aSystem(t, theReviewGraphSource(t))
	run := asking(t, engine, it, workspace, project)

	ended, err := engine.Answer(context.Background(), run, "no")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if ended.Status != flow.StatusDone {
		t.Fatalf("the run is %q on %q after a no, want it done", ended.Status, ended.Node)
	}
	if posted := declaredFor(t, it, run, "post"); len(posted) != 0 {
		t.Fatalf("%d posting steps were declared after the operator said no", len(posted))
	}
}

// The other direction, without which the test above would pass on a graph that can never post at
// all. A yes declares the posting step, and it is handed the draft the operator read rather than a
// fresh one, so what is posted is what they said yes to.
func TestAReviewToldYesPostsWhatTheOperatorRead(t *testing.T) {
	engine, it, workspace, project := aSystem(t, theReviewGraphSource(t))
	run := asking(t, engine, it, workspace, project)

	carried, err := engine.Answer(context.Background(), run, "yes")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if carried.Node != "post" {
		t.Fatalf("the run is on %q after a yes, want the posting step", carried.Node)
	}
	posted := declaredFor(t, it, carried, "post")
	if len(posted) != 1 {
		t.Fatalf("%d posting steps were declared after a yes, want one", len(posted))
	}
	if !strings.Contains(posted[0].Brief, theDraft) {
		t.Fatalf("the posting step was asked %q, and the draft the operator read is not in it", posted[0].Brief)
	}
	if !strings.Contains(posted[0].Brief, "answered yes") {
		t.Errorf("the posting step was asked %q, and it does not carry the answer that let it go ahead", posted[0].Brief)
	}
}

// A run that finds nothing to review ends after the one step it took to find that out, rather than
// reviewing whatever the pick step was looking at.
func TestAReviewWithNothingToReviewEndsAfterOneStep(t *testing.T) {
	engine, it, workspace, project := aSystem(t, theReviewGraphSource(t))

	run := started(t, engine, it, "pull-request-review", workspace, project)
	run = worked(t, engine, it, run, "none")

	if run.Status != flow.StatusDone {
		t.Fatalf("the run is %q on %q, want it done", run.Status, run.Node)
	}
	for _, node := range []string{"security", "features", "completeness", "draft", "post"} {
		if declared := declaredFor(t, it, run, node); len(declared) != 0 {
			t.Errorf("the %s step was declared for a run that found nothing to review", node)
		}
	}
}

// Every step of the run is put away as it ends, so a run waiting on the operator holds no
// container. A review can sit unanswered for a day, and a day of held containers is the cost this
// checks against.
func TestAReviewWaitingOnTheOperatorHoldsNoSession(t *testing.T) {
	engine, it, workspace, project := aSystem(t, theReviewGraphSource(t))

	run := asking(t, engine, it, workspace, project)

	sessions := flow.SessionsIn(run.State)
	if len(sessions) != 5 {
		t.Fatalf("the run records %d step sessions, want one per step it took: %v", len(sessions), sessions)
	}
	for _, session := range sessions {
		if !archivedHolds(it.archived, session) {
			t.Errorf("session %s is still live while the run waits to be told whether to post", session)
		}
	}
	open, err := it.store.ListJobs(context.Background(), job.Filter{Phase: job.PhaseRunning})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(open) != 0 {
		t.Errorf("%d jobs are still running while the run asks", len(open))
	}
}

func archivedHolds(archived []string, session string) bool {
	for _, one := range archived {
		if one == session {
			return true
		}
	}
	return false
}
