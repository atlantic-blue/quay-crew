package job_test

import (
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/krewe/internal/job"
)

func TestAStepSaysWhatWasFinishedInOneLine(t *testing.T) {
	tidy, err := job.TidyStep("  cut the worktree\n  from origin/main  ")
	if err != nil {
		t.Fatalf("a step was refused: %v", err)
	}
	if tidy != "cut the worktree from origin/main" {
		t.Fatalf("the step is kept as %q, want one line with the space taken off", tidy)
	}
}

func TestAStepWithNoWordsIsRefused(t *testing.T) {
	for _, said := range []string{"", "   ", "\n\t "} {
		_, err := job.TidyStep(said)
		if err == nil {
			t.Errorf("a step of %q was accepted, and a record of nothing tells the next attempt nothing", said)
			continue
		}
		if !strings.Contains(err.Error(), "what you finished") {
			t.Errorf("the refusal says %q, want it to say a step says what was finished", err)
		}
	}
}

func TestAStepOverTheCeilingIsRefusedAndSaysHowLongItIs(t *testing.T) {
	_, err := job.TidyStep(strings.Repeat("a", job.StepLimit+1))
	if err == nil {
		t.Fatal("a step longer than the ceiling was accepted")
	}
	for _, want := range []string{"201", "200"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal says %q, want it to name %s", err, want)
		}
	}
}

// The record is the set of what is finished. The same words twice are one step, or a session that
// says again what it said before pushes the earlier steps down a list and the record claims work
// that happened once.
func TestAStepAlreadyOnTheRecordIsRecognised(t *testing.T) {
	steps := []job.Step{{Seq: 1, Summary: "read the issue"}, {Seq: 2, Summary: "cut the worktree"}}

	if !job.Recorded(steps, "read the issue") {
		t.Fatal("a step already on the record was not recognised, so it would be written twice")
	}
	if job.Recorded(steps, "ran the tests") {
		t.Fatal("a step nothing recorded was taken for one that was")
	}
}

func TestAJobMayRecordUpToTheCeilingAndNoMore(t *testing.T) {
	steps := make([]job.Step, job.StepCount-1)
	if err := job.RoomForAStep(steps); err != nil {
		t.Fatalf("a job with room for another step was refused: %v", err)
	}

	err := job.RoomForAStep(append(steps, job.Step{Summary: "one more"}))
	if err == nil {
		t.Fatal("a job at the ceiling recorded another step")
	}
	if !strings.Contains(err.Error(), "40") {
		t.Fatalf("the refusal says %q, want it to name the ceiling", err)
	}
}

// Only a failure is continued. Done has nothing left, and stopped is somebody ending it on purpose,
// which is what refusing a failure does: continuing that would walk back a decision.
func TestOnlyAFailedJobCanBeContinued(t *testing.T) {
	for _, phase := range job.Phases() {
		if got, want := job.Resumable(phase), phase == job.PhaseFailed; got != want {
			t.Errorf("a %s job reports resumable=%v, want %v", phase, got, want)
		}
	}
}

func TestTheRefusalToContinueSaysWhatThePhaseIsAndWhatToDo(t *testing.T) {
	said := job.NotResumable("job-1", job.PhaseStopped)
	for _, want := range []string{"job-1", "stopped", "on purpose"} {
		if !strings.Contains(said, want) {
			t.Errorf("the refusal says %q, want it to say %q", said, want)
		}
	}
	if said := job.NotResumable("job-1", job.PhaseDone); !strings.Contains(said, "krewe job show job-1") {
		t.Errorf("the refusal says %q, want it to say how to read what came back", said)
	}
	if said := job.NotResumable("job-1", job.PhaseRunning); !strings.Contains(said, "running") {
		t.Errorf("the refusal says %q, want it to name the phase", said)
	}
}

// The reason a refused job carries. Both halves are on it, because a reader holding one of them goes
// looking for the other.
func TestARefusalCarriesTheOperatorsReasonAndTheFailure(t *testing.T) {
	said := job.Refused("the migration was wrong, this needs declaring again",
		"the model did not finish")
	for _, want := range []string{"the migration was wrong", "It failed with: the model did not finish"} {
		if !strings.Contains(said, want) {
			t.Errorf("the reason says %q, want it to carry %q", said, want)
		}
	}
	if said := job.Refused("   ", "the model did not finish"); !strings.Contains(said, "refused by the operator") {
		t.Errorf("a refusal with no words says %q, want it to say who refused it", said)
	}
}

// What the session that continues a job is handed. It is the whole of what a model has, so every
// part of it is asserted: what is done, what broke, when, and what to do before anything else.
func TestAContinuedJobIsToldWhatIsFinishedWhatBrokeAndToFetchItsBase(t *testing.T) {
	stopped := time.Date(2026, 8, 30, 9, 14, 2, 0, time.UTC)
	continued := job.Continued(&job.Job{
		Brief:    "make the listing sort by the clock it shows",
		Resuming: "the model did not finish: the credential ran out",
		Steps: []job.Step{
			{Seq: 1, Summary: "read the issue"},
			{Seq: 2, Summary: "cut the worktree from origin/main"},
		},
		FinishedAt: &stopped,
	})

	for _, want := range []string{
		"1. read the issue",
		"2. cut the worktree from origin/main",
		"Do not do them again",
		"the credential ran out",
		"2026-08-30T09:14:02Z",
		"fetch the branch this work is based on",
		"what moved",
	} {
		if !strings.Contains(continued, want) {
			t.Errorf("the session is told:\n%s\nwant it to say %q", continued, want)
		}
	}
	// The brief is not sent again. Sending it is asking for the whole job a second time, which is the
	// bill this exists to stop paying.
	if strings.Contains(continued, "make the listing sort by the clock it shows") {
		t.Errorf("the session is sent its brief again:\n%s", continued)
	}
}

func TestAContinuedJobThatRecordedNothingIsToldToLookBeforeItRepeatsItself(t *testing.T) {
	continued := job.Continued(&job.Job{Brief: "sort the listing", Resuming: "the sandbox went away"})

	if !strings.Contains(continued, "Nothing was recorded as finished") {
		t.Errorf("the session is told:\n%s\nwant it to say nothing was recorded", continued)
	}
	if !strings.Contains(continued, "the sandbox went away") {
		t.Errorf("the session is told:\n%s\nwant it to say what broke", continued)
	}
}

// The sentence the job serves wins over everything else it is told, including this. A continued job
// that dropped it would be a session carrying on against the design and not against the product.
func TestAContinuedJobStillCarriesTheSentenceItServes(t *testing.T) {
	continued := job.Continued(&job.Job{
		Product: "you paste a link and get the text back", Resuming: "the sandbox went away",
	})

	if !strings.Contains(continued, "you paste a link and get the text back") {
		t.Errorf("the session is told:\n%s\nwant it to carry the sentence the job serves", continued)
	}
}

// The controller reads one field to decide what to send, and a job being continued is the newest
// thing anybody decided about it.
func TestAJobBeingContinuedIsSentTheStepsRatherThanItsBriefOrWhatItWasTold(t *testing.T) {
	asked := job.Asked(&job.Job{
		Brief: "sort the listing", Question: "which store", Told: "the key value store",
		Resuming: "the sandbox went away", Steps: []job.Step{{Seq: 1, Summary: "read the issue"}},
	})

	if !strings.Contains(asked, "1. read the issue") {
		t.Fatalf("the session is told:\n%s\nwant the steps it finished", asked)
	}
	if strings.Contains(asked, "sort the listing") {
		t.Fatalf("the session is sent its brief again:\n%s", asked)
	}
}

// Every session is asked to keep the record as it goes, whatever else the job says. Without it a job
// can only ever start again from nothing, which is what this exists to end.
func TestEverySessionIsAskedToRecordEachStepAsItFinishesIt(t *testing.T) {
	asked := job.Asked(&job.Job{Brief: "sort the listing"})

	if !strings.Contains(asked, "krewe job step") {
		t.Fatalf("the session is told:\n%s\nwant it to be asked to record each step", asked)
	}
	if !strings.Contains(asked, "sort the listing") {
		t.Fatalf("the session is told:\n%s\nwant its brief", asked)
	}
}

// A continued job that works in a repository is told again how it ends. The task that continues the
// job is the one that finishes it, and a model reads what it is handed rather than what it remembers.
func TestAContinuedJobWorkingInARepositoryIsToldAgainHowItEnds(t *testing.T) {
	continued := job.Continued(&job.Job{
		Brief: "sort the listing", Repository: "atlantic-blue/quay-crew", Resuming: "the sandbox went away",
	})

	for _, want := range []string{"atlantic-blue/quay-crew", "pull request", "Do not merge"} {
		if !strings.Contains(continued, want) {
			t.Errorf("the session is told:\n%s\nwant it to say %q", continued, want)
		}
	}
}

// The base a continued attempt stands on. The silence is tested first, because a reading that finds a
// report in everything satisfies every test about finding one.
func TestAnAnswerThatSaysNothingAboutItsBaseReportsNothing(t *testing.T) {
	for _, said := range []string{
		"",
		"I carried on from the worktree and opened the pull request",
		// The word without the answer. A session that wrote the marker and stopped has said nothing
		// about the base, and this is the case the whole check turns on.
		"Base:",
		"Base:    ",
		// Two words that begin the same way and mean something else.
		"Based on the issue, the listing sorts by the clock it shows",
		"Database: postgres, and the migration ran",
		// The marker inside a sentence rather than opening the line. A reader that matched anywhere
		// would read a mention of the base as a report on it.
		"I rebased and the base: was fine",
	} {
		if moved := job.MovedUnderIt(said); moved != "" {
			t.Errorf("the answer %q reports %q moved, and it says nothing about the base", said, moved)
		}
	}
}

func TestAnAnswerThatSaysWhatMovedIsRead(t *testing.T) {
	for _, said := range []struct {
		answer string
		want   string
	}{
		{answer: "Base: nothing moved", want: "nothing moved"},
		{answer: "I finished the work.\nBase: origin/main moved on by 4 commits\nOpened the pull request",
			want: "origin/main moved on by 4 commits"},
		// The shapes a model reaches for when it writes a report.
		{answer: "- Base: nothing moved", want: "nothing moved"},
		{answer: "**Base:** nothing moved", want: "nothing moved"},
		{answer: "  base: nothing moved  ", want: "nothing moved"},
	} {
		if moved := job.MovedUnderIt(said.answer); moved != said.want {
			t.Errorf("the answer %q reports %q moved, want %q", said.answer, moved, said.want)
		}
	}
}

// The ask and the reading of it must not drift. A bound that stops matching asks forever, and every
// ask is a task somebody pays for.
func TestTheAskAboutTheBaseIsRecognisedAndTheTaskThatContinuedTheJobIsNot(t *testing.T) {
	if !job.AskingWhatMoved(job.AskedWhatMoved("atlantic-blue/quay-crew")) {
		t.Fatal("the system does not recognise its own ask, so it would ask forever")
	}
	continued := job.Continued(&job.Job{
		Repository: "atlantic-blue/quay-crew", Resuming: "the sandbox went away",
	})
	if job.AskingWhatMoved(continued) {
		t.Fatal("the task that continued the job reads as the ask, so the session would never be asked")
	}
}

// The session is told the shape to answer in, because the system reads a shape rather than the prose.
// A session asked for a report and never told how to write one is a session the reading will refuse.
func TestAContinuedSessionIsToldTheShapeToSayWhatMovedIn(t *testing.T) {
	continued := job.Continued(&job.Job{
		Repository: "atlantic-blue/quay-crew", Resuming: "the sandbox went away",
	})

	for _, want := range []string{"Base:", "nothing moved"} {
		if !strings.Contains(continued, want) {
			t.Errorf("the session is told:\n%s\nwant it to say %q", continued, want)
		}
	}
	// And what it is told satisfies the reading, so the example is not one the system would refuse.
	if moved := job.MovedUnderIt("Base: nothing moved"); moved == "" {
		t.Error("the shape the session is asked for reads as saying nothing")
	}
}

func TestTheSecondAskAndTheReasonBothNameTheRepositoryAndWhatIsMissing(t *testing.T) {
	asked := job.AskedWhatMoved("atlantic-blue/quay-crew")
	for _, want := range []string{"atlantic-blue/quay-crew", "Base:", "Fetch the branch"} {
		if !strings.Contains(asked, want) {
			t.Errorf("the session is asked:\n%s\nwant it to say %q", asked, want)
		}
	}
	stopped := job.NothingSaidAboutTheBase("atlantic-blue/quay-crew")
	for _, want := range []string{"atlantic-blue/quay-crew", "asked twice", "pull request"} {
		if !strings.Contains(stopped, want) {
			t.Errorf("the job stops saying:\n%s\nwant it to say %q", stopped, want)
		}
	}
}

// And a continued job that names no repository is told none of it. The system reads the answer only
// where a repository says what the work stands on, so a session told its answer would not end the job
// would be told something the system does not do.
func TestAContinuedJobInNoRepositoryIsNotAskedForTheLine(t *testing.T) {
	continued := job.Continued(&job.Job{Brief: "read the electricity bill", Resuming: "the sandbox went away"})

	if strings.Contains(continued, job.BaseMarker) {
		t.Errorf("the session is told:\n%s\nand nothing will read that line", continued)
	}
	// It is still told to look, because the working directory it is standing in is not where it left it.
	if !strings.Contains(continued, "what moved") {
		t.Errorf("the session is told:\n%s\nwant it to say to look at what moved", continued)
	}
}
