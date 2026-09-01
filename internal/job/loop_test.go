package job_test

import (
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The measure, the rule and the routes.
//
// The case that matters most is the one that must not fire. A detector that stops work which was
// going to finish is worse than no detector, so the two attempts that are genuinely different work
// are tested before the three that are one attempt said three times.

// Two attempts at the same step, doing different work. Nothing here may count.
func TestTwoAttemptsThatAreDifferentWorkAreNotAlike(t *testing.T) {
	first := "I read the issue and the design document it names. The threshold has to be measured " +
		"rather than chosen, so I wrote the harness that records every attempt with its similarity."
	second := "The migration is written and the Postgres store reads the new column. I added the " +
		"conformance case so the in memory store cannot drift from it, and both tiers are green."

	if alike := job.Similarity(first, second); alike >= job.LoopThreshold {
		t.Fatalf("two different attempts score %.3f, which is at or above the threshold of %.2f: this "+
			"detector would stop work that was going to finish", alike, job.LoopThreshold)
	}
}

// The false positive, at the level the controller reads it: three attempts that each did something
// different are not a loop, however many of them there are.
func TestThreeAttemptsThatMakeProgressAreNotALoop(t *testing.T) {
	at := attemptsSaying(
		"I read the issue and the design it names, then wrote the harness that records each attempt.",
		"The migration is written and both stores read the new table, with the conformance case beside it.",
		"The controller now escalates by the declared route, and the scenario covers the refusal first.")

	if job.Circling(at) {
		t.Fatalf("three attempts at different work read as a loop, and the similarities were %v", alikeness(at))
	}
}

// A session that says the same thing again with its measurement changed, which is what going in
// circles actually looks like on the record.
func TestAnAttemptSaidAgainWithItsNumbersChangedIsAlike(t *testing.T) {
	first := "I could not get the coverage check green. make test failed on internal/job at 78.2 per " +
		"cent against a threshold of 80. I added a table test over the parser and ran it again. Still red."
	second := "I could not get the coverage check green. make test failed on internal/job at 79.1 per " +
		"cent against a threshold of 80. I added a table test over the parser and ran it again. Still red."

	if alike := job.Similarity(first, second); alike < job.LoopThreshold {
		t.Fatalf("the same attempt with one number changed scores %.3f, under the threshold of %.2f",
			alike, job.LoopThreshold)
	}
}

func TestThreeAttemptsSayingOneThingAreALoop(t *testing.T) {
	said := "The check is still red. I ran the suite again and the same two cases fail, so I will try " +
		"the same fix once more."
	at := attemptsSaying(said, said, said)

	if !job.Circling(at) {
		t.Fatalf("three attempts saying one thing are not read as a loop: %v", alikeness(at))
	}
}

// Two is a retry. The third is what says the second changed nothing, which is why the count is three
// rather than two.
func TestTwoAttemptsSayingOneThingAreARetryAndNotALoop(t *testing.T) {
	said := "The check is still red and I will try the same fix again."
	if job.Circling(attemptsSaying(said, said)) {
		t.Fatal("a second attempt at a step is a retry, and reading it as a loop would stop every retry")
	}
}

// A session alternating between two shapes of fix is going in circles as surely as one repeating a
// single shape, so an attempt is held against the closest earlier attempt rather than the last.
func TestAnAttemptIsHeldAgainstTheClosestEarlierOneRatherThanTheLast(t *testing.T) {
	one := "I tried widening the interface, and the coverage check is still red."
	other := "I tried moving the parser out to its own file, and the coverage check is still red."

	alike := job.LikeTheOnesBefore(one, attemptsSaying(one, other))
	if alike < job.LoopThreshold {
		t.Fatalf("an attempt held against the closest earlier one scores %.3f, so an alternating loop "+
			"reads as new work every time", alike)
	}
}

// The step is what keeps a working session out of this entirely: an attempt after a finished step is
// somewhere new, and is never compared with what came before it.
func TestAttemptsAreOnlyComparedWithAttemptsAtTheSameStep(t *testing.T) {
	said := "The check is still red and I will try the same fix again."
	at := []job.Attempt{
		{Seq: 1, Step: 0, Said: said}, {Seq: 2, Step: 0, Said: said}, {Seq: 3, Step: 1, Said: said},
	}
	if held := job.AtStep(at, 0); len(held) != 2 {
		t.Fatalf("step 0 holds %d attempts, want the two that were made there", len(held))
	}
	if job.Circling(job.AtStep(at, 0)) {
		t.Fatal("two attempts at a step read as a loop")
	}
	if job.Circling(job.AtStep(at, 1)) {
		t.Fatal("one attempt at a step read as a loop")
	}
}

func TestAnAttemptWithNothingBeforeItIsLikeNothing(t *testing.T) {
	if alike := job.LikeTheOnesBefore("anything at all", nil); alike != 0 {
		t.Fatalf("the first attempt at a step scores %.3f against nothing, want 0", alike)
	}
}

// What the record keeps is what the measure reads, so the number can be worked out again by anybody
// holding the record.
func TestWhatAnAttemptSaidIsKeptToACeiling(t *testing.T) {
	kept := job.TidyAttempt("  " + strings.Repeat("word ", 4000) + "  ")
	if len(kept) > job.AttemptLimit {
		t.Fatalf("the record keeps %d bytes of what an attempt said, and the ceiling is %d",
			len(kept), job.AttemptLimit)
	}
	if strings.HasPrefix(kept, " ") || strings.HasSuffix(kept, " ") {
		t.Fatalf("what an attempt said is kept with space around it: %q", kept[:4])
	}
}

// The routes, refusals first. A route the system cannot carry out has to say so where somebody is
// typing it, because a route that quietly does nothing is a job that goes in circles with a setting
// that says otherwise.
func TestARouteOntoAnotherModelIsRefusedAndSaysWhatToDoInstead(t *testing.T) {
	_, err := job.ReadRoute("model:opus")
	if err == nil {
		t.Fatal("escalating onto another model was accepted, and this build runs one model for the whole crew")
	}
	for _, wanted := range []string{"opus", "role:<name>", "ask"} {
		if !strings.Contains(err.Error(), wanted) {
			t.Fatalf("the refusal does not say %q: %v", wanted, err)
		}
	}
}

func TestARouteToARoleWithNoRoleNamedIsRefused(t *testing.T) {
	_, err := job.ReadRoute("role")
	if err == nil {
		t.Fatal("escalating to a role that was never named was accepted")
	}
	if !strings.Contains(err.Error(), "role:<name>") {
		t.Fatalf("the refusal does not say how to write one: %v", err)
	}
}

func TestARouteThatIsNotARouteIsRefusedAndOffersTheOnesThatAre(t *testing.T) {
	_, err := job.ReadRoute("retry")
	if err == nil {
		t.Fatal("a word that is not a route was accepted")
	}
	for _, wanted := range []string{"retry", "ask", "role:<name>"} {
		if !strings.Contains(err.Error(), wanted) {
			t.Fatalf("the refusal does not say %q: %v", wanted, err)
		}
	}
}

func TestAskingNamesNobody(t *testing.T) {
	if _, err := job.ReadRoute("ask:julian"); err == nil {
		t.Fatal("asking with somebody named after it was accepted, and the question goes to the operator")
	}
}

func TestAJobThatDeclaresNoRouteAsksTheOperator(t *testing.T) {
	route, err := job.ReadRoute("")
	if err != nil {
		t.Fatalf("a job that declares no route is refused: %v", err)
	}
	if route.Word != job.RouteAsk || route.To != "" {
		t.Fatalf("a job that declares no route escalates by %q, want the operator", route)
	}
}

func TestARouteIsStoredTheWayItWasTyped(t *testing.T) {
	route, err := job.ReadRoute("  Role: architect ")
	if err != nil {
		t.Fatalf("ReadRoute: %v", err)
	}
	if route.String() != "role:architect" {
		t.Fatalf("the route reads back as %q, want role:architect", route)
	}
	if !route.Names("architect") || route.Names("archivist") {
		t.Fatalf("the route names the wrong role: %q", route)
	}
}

// attemptsSaying is a run of attempts at one step, each carrying how alike it is to the ones before
// it, which is what the store writes and what the rule reads.
func attemptsSaying(said ...string) []job.Attempt {
	at := make([]job.Attempt, 0, len(said))
	for seq, one := range said {
		at = append(at, job.Attempt{
			Seq: seq + 1, Step: 0, Said: one, Similarity: job.LikeTheOnesBefore(one, at),
			OccurredAt: time.Now().UTC(),
		})
	}
	return at
}

// alikeness is what each attempt scored, for a failure message that says why rather than that.
func alikeness(at []job.Attempt) []float64 {
	scores := make([]float64, 0, len(at))
	for _, one := range at {
		scores = append(scores, one.Similarity)
	}
	return scores
}

// A controller that reads a task another controller already recorded must not hold the attempt
// against a copy of itself. Scoring it one would make a loop out of one piece of work, which is the
// same failure the store's key on the task prevents from the other side.
func TestAnAttemptIsNotHeldAgainstItself(t *testing.T) {
	said := "the check is still red and I will try the same fix again"
	one := &job.Job{
		ID: "job-1", Attempted: []job.Attempt{{Task: "task-1", Step: 1, Said: said, Seq: 1}},
	}

	again := job.TheAttempt(one, "task-1", said)

	if again.Similarity != 0 {
		t.Fatalf("a task read a second time scores %.2f against its own record, want nothing",
			again.Similarity)
	}
}
