package job_test

import (
	"context"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A reading is as long as the work needs, and nothing refuses it or stops the job for being long.
//
// The stage in front of the plan asks a session what it understood, and a session that read a
// repository has as much to say as the work has in it. Today the reading is held to a paragraph, so
// the long one is refused, asked for a second time, and the job stops with an operator having read
// none of it. The reading is what a person asked for and what a person reads, so its length is
// theirs and not the system's.

// aLongReading is what a session wrote when it had read the repository. It is 859 bytes, which is
// longer than a paragraph and shorter than anything a person would call long, and it is refused
// today.
const aLongReading = "Understood: " + aLongUnderstanding + "\n" +
	"Not: a shorter reading\n" +
	"Told: the operator asked for the whole reading\n" +
	"Assumed: nothing else truncates it\n" +
	"Unknown: how long the longest reading gets\n" +
	"Confidence: sure of the shape\n" +
	"Question 1: which surface does a person read this on"

// aLongUnderstanding is the paragraph inside it, which is what the ceiling refuses.
const aLongUnderstanding = "This job takes the ceiling off what a session may write when it says what " +
	"it understood. Today the reading is held to a paragraph, so a session that read the repository " +
	"and has a lot to say is refused, asked again, and then the job stops with nobody having read a " +
	"word of it. The length of a reading is not the system's to decide: a person asked for the " +
	"reading and a person reads it, so it goes on the row whole and it reaches the operator whole, " +
	"however long it is. This paragraph is longer than the ceiling the system holds today, which is " +
	"exactly what makes it the reading the operator never gets to read at all."

// theEndOfTheParagraph is the last sentence of it, which is the half a ceiling takes away. An
// assertion on the opening words passes against a reading that was cut in the middle.
const theEndOfTheParagraph = "exactly what makes it the reading the operator never gets to read at all."

// The reader keeps it, whole and byte for byte. A reader that refuses this is the reason an operator
// never sees it, and a reader that keeps a shortened copy is the same fault one step later.
func TestAReadingIsKeptWholeAtAnyLength(t *testing.T) {
	// The number the requirement names, so a reading that drifts is a failure here rather than a
	// quiet change of what is being proved.
	if len(aLongReading) != 859 {
		t.Fatalf("the reading under test is %d bytes, want the 859 the requirement names",
			len(aLongReading))
	}

	understood, err := job.ReadIdeation(aLongReading)
	if err != nil {
		t.Fatalf("a reading of %d bytes was refused: %v", len(aLongReading), err)
	}
	if understood.Understood != aLongUnderstanding {
		t.Fatalf("what it understood came back as %q, want the paragraph it wrote", understood.Understood)
	}
	kept := job.IdeationText(understood)
	if !strings.Contains(kept, theEndOfTheParagraph) {
		t.Fatalf("the record the system keeps is %q, and the end of the paragraph is not in it", kept)
	}
	if len(kept) != len(aLongReading) {
		t.Fatalf("the record the system keeps is %d bytes and the session wrote %d",
			len(kept), len(aLongReading))
	}
}

// The whole of the requirement, on the record: the long reading lands whole, the job waits for a
// person instead of stopping, and it costs the one task the stage has always cost.
func TestALongReadingLandsWholeAndTheJobWaitsForAPersonRatherThanStopping(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(readingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands(aLongReading)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase == job.PhaseStopped {
		t.Fatalf("a job stopped for the length of its reading: %s", got.Reason)
	}
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q, want asking: %s", got.Phase, got.Reason)
	}
	if !strings.Contains(got.Ideation, theEndOfTheParagraph) {
		t.Fatalf("the reading on the row is %q, and the end of what the session wrote is not in it",
			got.Ideation)
	}
	if !strings.Contains(got.Question, theEndOfTheParagraph) {
		t.Fatalf("the question put to the person is %q, and the end of the reading is not in it",
			got.Question)
	}
	// One task. A reading asked for a second time because it was long is a task somebody pays for and
	// an operator who reads the same words twice.
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1", plane.sent())
	}
	if strings.Contains(plane.lastText(), "asked what you understood once already") {
		t.Fatalf("the session was asked a second time for a reading it already wrote: %q",
			plane.lastText())
	}
}

// And it moves on. The person answers the reading they were given, and the job is asked for the list
// it would build, which is the next stage rather than the same stage again.
func TestAJobWhoseReadingWasLongMovesToTheNextStage(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(readingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands(aLongReading)
	controller.Tick(ctx)

	// What the person answers is what is on the row, so an answer to a reading that never landed is
	// an answer to nothing.
	if held := kept.get(one.ID); !strings.Contains(held.Ideation, theEndOfTheParagraph) {
		t.Fatalf("the reading the person answers is %q, and the end of what the session wrote is "+
			"not in it", held.Ideation)
	}
	kept.answerReading(one.ID, "1: on the command line, the way every other listing is read")

	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase == job.PhaseStopped {
		t.Fatalf("a job stopped after its reading was answered: %s", got.Reason)
	}
	if stage := job.StageOf(got); stage.Name != job.StageDesign {
		t.Fatalf("the job is in stage %q, want %q", stage.Name, job.StageDesign)
	}
	if !strings.Contains(plane.lastText(), "Vertical 1:") {
		t.Fatalf("the task after the answer is %q, want the ask for the list", plane.lastText())
	}
}
