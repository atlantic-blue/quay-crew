package job_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/krewe/internal/job"
)

// What the gate decides, before any controller is involved: whether a job is gated at all, what a
// verdict says, and what a reader of a settled job is told about what passed it.
//
// The fail path comes first in every pair below. A test that a passing verdict is read passes just as
// happily against a reading that says pass to everything, which is the whole defect the gate exists
// to answer.

// The case that must not be read as a pass. An answer with no verdict line has judged nothing, and a
// reading that fell through to true would settle every job while reporting it was checked.
func TestAnAnswerWithNoVerdictLineSaidNothing(t *testing.T) {
	for _, answer := range []string{
		"",
		"I read the change and it looks fine to me.",
		"The verdict is that this passes.",
		"Verdict:",
		"Verdict: ",
		"Verdict: maybe",
		"Verdict: it depends on whether the column is nullable",
	} {
		if judged := job.Verdict(answer); judged.Said {
			t.Errorf("the answer %q was read as a verdict (passed=%v, reason=%q)",
				answer, judged.Passed, judged.Reason)
		}
	}
}

func TestAFailIsReadWithWhatItSaid(t *testing.T) {
	judged := job.Verdict("I read the diff.\nVerdict: fail because the change adds a column and no " +
		"migration, so a fresh store cannot read it\n")

	if !judged.Said {
		t.Fatal("the verdict was not read at all")
	}
	if judged.Passed {
		t.Fatal("a fail was read as a pass")
	}
	if !strings.Contains(judged.Reason, "adds a column and no migration") {
		t.Fatalf("the reason is %q, want what the gate said", judged.Reason)
	}
	// The word between the verdict and the reason is not part of the reason. "fail because x" and
	// "fail: x" say the same thing, and a row that reads "because because x" is a row somebody read
	// twice.
	if strings.HasPrefix(judged.Reason, "because") {
		t.Fatalf("the reason is %q, want it to start with what was wrong", judged.Reason)
	}
}

// A session writing a report reaches for a bullet or a heading, and refusing the line over a dash
// would throw away a judgement that was made.
func TestAVerdictIsReadThroughTheMarkupAroundIt(t *testing.T) {
	for _, answer := range []string{
		"- Verdict: fail the tests do not run",
		"## Verdict: fail the tests do not run",
		"**Verdict:** fail the tests do not run",
		"  verdict: FAIL the tests do not run",
	} {
		judged := job.Verdict(answer)
		if !judged.Said || judged.Passed {
			t.Errorf("the answer %q was read as passed=%v said=%v", answer, judged.Passed, judged.Said)
		}
		if !strings.Contains(judged.Reason, "the tests do not run") {
			t.Errorf("the answer %q gave the reason %q", answer, judged.Reason)
		}
	}
}

func TestAPassIsReadAsAPass(t *testing.T) {
	judged := job.Verdict("Verdict: pass the change does what the brief asked and the suite ran 540 files")

	if !judged.Said || !judged.Passed {
		t.Fatalf("a pass was read as passed=%v said=%v", judged.Passed, judged.Said)
	}
	if !strings.Contains(judged.Reason, "540 files") {
		t.Fatalf("the reason is %q, want what the gate said", judged.Reason)
	}
}

// The first verdict wins. A gate that wrote one line and then discussed it must not have the
// discussion read as a second judgement.
func TestTheFirstVerdictInAnAnswerIsTheVerdict(t *testing.T) {
	judged := job.Verdict("Verdict: fail the migration is missing\n\nVerdict: pass if you add one")

	if judged.Passed {
		t.Fatalf("the second line was read as the verdict: %+v", judged)
	}
}

// Which jobs the gate applies to. A job that names no repository has no change for anything to read,
// and a job declared with the gate off said so where somebody was looking.
func TestOnlyAJobWithAChangeAndTheGateOnIsGated(t *testing.T) {
	for _, one := range []struct {
		what  string
		job   *job.Job
		gated bool
	}{
		{"a job in a repository", &job.Job{Repository: "atlantic-blue/quay-crew"}, true},
		{"a job in no repository", &job.Job{}, false},
		{"a job with the gate off", &job.Job{Repository: "atlantic-blue/quay-crew", Ungated: true}, false},
		{"no job at all", nil, false},
	} {
		if got := job.Gated(one.job); got != one.gated {
			t.Errorf("%s is gated=%v, want %v", one.what, got, one.gated)
		}
	}
}

// What a reader of a settled job is told. A job nothing passed and a job two sessions passed must
// never read the same, which is the whole of the record being worth keeping.
func TestASettledJobSaysWhatPassedItAndWhatDidNot(t *testing.T) {
	for _, one := range []struct {
		what string
		gate job.Gate
		says []string
	}{
		{"a job with the gate off", job.Gate{
			Repository: "atlantic-blue/quay-crew", Ungated: true, Phase: job.PhaseDone,
		}, []string{"gate off", "nothing independent"}},
		{"a job nothing passed", job.Gate{
			Repository: "atlantic-blue/quay-crew", Phase: job.PhaseStopped,
		}, []string{"neither", job.GateReviewer, job.GateTester}},
		{"a job both passed", job.Gate{
			Repository: "atlantic-blue/quay-crew", Reviewed: true, Tested: true, Phase: job.PhaseDone,
		}, []string{"passed by", job.GateReviewer, job.GateTester, "did not do the work"}},
		{"a job halfway through the gate", job.Gate{
			Repository: "atlantic-blue/quay-crew", Reviewed: true, Phase: job.PhaseRunning,
		}, []string{job.GateReviewer, "has not passed it"}},
	} {
		said := one.gate.PassedBy()
		for _, want := range one.says {
			if !strings.Contains(said, want) {
				t.Errorf("%s reads %q, want it to say %q", one.what, said, want)
			}
		}
	}
	// And a job with no change to read says nothing at all, rather than a sentence about a gate that
	// was never going to run.
	if said := (job.Gate{Phase: job.PhaseDone}).PassedBy(); said != "" {
		t.Errorf("a job in no repository says %q about its gate", said)
	}
}

// The two gates run in conversations of their own. A second opinion from the session that formed the
// first is not a second opinion, and this is the line that decides it.
func TestEachGateRunsInItsOwnConversation(t *testing.T) {
	const id = "b7c1de9f"
	working, reviewer, tester := job.SessionFor(id), job.ReviewerFor(id), job.TesterFor(id)

	for _, pair := range [][2]string{{working, reviewer}, {working, tester}, {reviewer, tester}} {
		if pair[0] == pair[1] {
			t.Errorf("%q and %q are the same conversation", pair[0], pair[1])
		}
	}
	// Named after the job rather than minted, so a controller that comes back to the row finds them.
	for _, handle := range []string{reviewer, tester} {
		if !strings.Contains(handle, id) {
			t.Errorf("the handle %q does not name the job, so it cannot be found again", handle)
		}
	}
}

// What each gate is asked. The reviewer is asked about what the job wanted; the tester is asked to
// read output rather than an exit status, which is the instruction the whole tier turns on.
func TestWhatEachGateIsAskedToDo(t *testing.T) {
	one := &job.Job{
		Title: "sort the listing", Brief: "make the listing sort by the clock it shows",
		Product:    "you open the listing and the newest row is at the top",
		Repository: "atlantic-blue/quay-crew",
	}
	const address = "https://github.com/atlantic-blue/quay-crew/pull/454"

	review := job.AskedToReview(one, address)
	for _, want := range []string{
		"You are the " + job.GateReviewer, "you did not do it", address, one.Title, one.Brief, one.Product,
		"Change no file", job.VerdictMarker,
	} {
		if !strings.Contains(review, want) {
			t.Errorf("the reviewer is asked %q, want it to say %q", review, want)
		}
	}

	test := job.AskedToTest(one, address)
	for _, want := range []string{
		"You are the " + job.GateTester, "you did not do it", address, "exit status", "ran nothing exits zero",
		"whole suite", job.VerdictMarker,
	} {
		if !strings.Contains(test, want) {
			t.Errorf("the tester is asked %q, want it to say %q", test, want)
		}
	}
}

// A fail goes back carrying what the gate said, and asking for the address again: this answer is the
// one that ends the job, and a job in a repository is held to naming its pull request in it.
func TestWorkSentBackCarriesTheReasonAndAsksForTheAddressAgain(t *testing.T) {
	one := &job.Job{Title: "sort the listing", Repository: "atlantic-blue/quay-crew"}

	sent := job.SentBack(job.GateReviewer, "the migration is missing", one)

	for _, want := range []string{
		job.GateReviewer, "the migration is missing", "pull request", "second fail ends the job",
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("the work went back saying %q, want it to say %q", sent, want)
		}
	}
	// And the system can recognise its own ask, which is what bounds the asking to one round.
	if !job.SentBackByTheGate(sent) {
		t.Fatal("the system cannot recognise the task it just wrote, so it would send work back forever")
	}
	if job.SentBackByTheGate("I fixed the migration and pushed again") {
		t.Fatal("an ordinary answer was read as the gate sending work back")
	}
}

// A gate that gave no reason still fails the work, and the session it goes back to is told where to
// read rather than handed an empty sentence.
func TestAFailWithNoReasonStillSaysSomethingUseful(t *testing.T) {
	one := &job.Job{Title: "sort the listing", Repository: "atlantic-blue/quay-crew"}

	for _, said := range []string{
		job.SentBack(job.GateTester, "", one),
		job.FailedTheGate(job.GateTester, "", one),
	} {
		if !strings.Contains(said, "no reason") {
			t.Errorf("a fail with nothing after it reads %q", said)
		}
	}
}
