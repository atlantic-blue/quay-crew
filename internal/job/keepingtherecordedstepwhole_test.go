package job_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A session records what it finished, and the record keeps it whether it is one line or ten.
//
// A step is the only thing that survives a container. It is written as the work is done, so a step
// refused for its length is work that happened and that nothing now says happened: the session
// carries on, the record does not, and whoever continues the job does it again. The count has the
// same shape. A job that recorded forty steps and is refused the forty first loses every step after
// it, which is the end of the run rather than the start of it.

// aStepSaying is one step of exactly this many bytes, opening and ending with words an assertion can
// look for, so a step cut at either end shows as a cut rather than as a pass. Single spaces
// throughout, because the system tidies the whitespace in a line before it keeps it.
func aStepSaying(size int) string {
	const opens, ends = "swept every file for a cap that refuses text", "and this step ends here"
	middle := size - len(opens) - len(ends) - 2
	if middle < 1 {
		panic("a step this short cannot carry both ends")
	}
	return opens + " " + strings.Repeat("x", middle) + " " + ends
}

func TestARecordedStepOfAnyLengthIsKeptWordForWord(t *testing.T) {
	step := aStepSaying(job.StepLimit * 2)

	kept, err := job.TidyStep(step)
	if err != nil {
		t.Fatalf("a step of %d bytes was refused: %v", len(step), err)
	}
	if kept != step {
		t.Fatalf("the step was kept as %d bytes of the %d it was written with", len(kept), len(step))
	}
}

// The forty first step. A job that reaches the count is a job with more finished than the guide
// expected, and the answer to that is a person reading it rather than the record closing.
func TestAJobPastTheStepGuideStillRecordsWhatItFinished(t *testing.T) {
	steps := make([]job.Step, 0, job.StepCount)
	for at := 0; at < job.StepCount; at++ {
		steps = append(steps, job.Step{Job: "b75f5bf6", Seq: at + 1, Summary: "finished piece of the sweep"})
	}

	if err := job.RoomForAStep(steps); err != nil {
		t.Fatalf("a job that recorded %d steps was refused the next one: %v", len(steps), err)
	}
}

// Carried through to the reader. The session that continues the job is given what is already
// finished, and a step kept whole on the row and cut on the way into that task is the same loss one
// step later.
func TestTheSessionThatContinuesTheJobIsGivenTheWholeStep(t *testing.T) {
	step := aStepSaying(job.StepLimit * 2)

	kept, err := job.TidyStep(step)
	if err != nil {
		t.Fatalf("a step of %d bytes was refused: %v", len(step), err)
	}
	one := &job.Job{
		ID:      "b75f5bf6",
		Title:   "move the caps that still refuse text",
		Brief:   "the second half of the sweep",
		Product: "a session recording a long step keeps its words",
		Steps:   []job.Step{{Job: "b75f5bf6", Seq: 1, Summary: kept}},
		Handoffs: []job.Handoff{{
			Job: "b75f5bf6", Seq: 1, Left: "the rest of the sweep", Session: "session-1",
		}},
	}

	if said := job.HandedOn(one); !strings.Contains(said, step) {
		t.Fatalf("the session carrying the job on is told %q, and the step it must not do again is %d bytes",
			said, len(step))
	}
}
