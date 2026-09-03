package job_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A count is a guide like a length, so an eighth step, a seventeenth label and an eighth vertical are
// kept and warned about rather than refused.
//
// Three caps count rather than measure, and all three used to refuse the whole reply. A plan with an
// eighth step was unreadable, so the system asked for the plan again and a second reply stopped the
// job: one step too many threw away the other seven and the work with them. A list with an eighth
// vertical went the same way. A declaration carrying a seventeenth label was refused at the door,
// and the label a person cannot write is the label they had a reason to write.
//
// So the count says how many there are and what the guide is, above the text, and every one of them
// is still there underneath. The operator reads the line and decides. Nothing is thrown away for
// arriving one over a number.

// aPlanOfSteps is a plan of n steps, each one saying something different, so a step dropped anywhere
// in the middle shows as a step that is missing rather than as a shorter list of the same words.
func aPlanOfSteps(n int) string {
	lines := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		lines = append(lines, fmt.Sprintf("Step %d: read what surface %d already draws", i, i))
	}
	return strings.Join(lines, "\n")
}

// An eighth step is read, kept and put to a person, under a line that says there are eight of them
// and the guide is seven.
func TestAnEighthPlanStepIsKeptAndWarnedAbout(t *testing.T) {
	over := job.PlanSteps + 1

	steps, err := job.ReadPlan(aPlanOfSteps(over))
	if err != nil {
		t.Fatalf("a plan of %d steps was refused rather than warned about: %v", over, err)
	}
	if len(steps) != over {
		t.Fatalf("the plan reads back as %d steps and it was written with %d", len(steps), over)
	}

	kept := job.PlanText(steps)
	shown := job.AskingWhetherThisIsThePlan("you paste a link and get the text back", kept)
	for i := 1; i <= over; i++ {
		if !strings.Contains(shown, fmt.Sprintf("Step %d: read what surface %d already draws", i, i)) {
			t.Fatalf("what a person is asked lost step %d of %d: %q", i, over, shown)
		}
	}
	warning := warningAbout(shown, "steps", over, job.PlanSteps)
	if warning == "" {
		t.Fatalf("nothing says this plan has %d steps where the guide is %d: %q",
			over, job.PlanSteps, shown)
	}
	if strings.Index(shown, warning) > strings.Index(shown, "Step 1:") {
		t.Fatalf("the count is under the plan it is about, and a reader meets it too late: %q", shown)
	}
}

// A plan inside its guide is not warned about, because a warning on every plan is a warning nobody
// reads. This half holds today, and it is here so the pair cannot be answered by saying the count on
// everything.
func TestAPlanInsideItsGuideIsNotWarnedAbout(t *testing.T) {
	steps, err := job.ReadPlan(aPlanOfSteps(job.PlanSteps))
	if err != nil {
		t.Fatalf("a plan of %d steps was refused: %v", job.PlanSteps, err)
	}
	shown := job.AskingWhetherThisIsThePlan("you paste a link and get the text back", job.PlanText(steps))
	if strings.Contains(shown, "guide") {
		t.Fatalf("a plan inside its guide was warned about: %q", shown)
	}
}

// The surface the operator actually reads. A warning composed anywhere that never reaches the
// question is a warning nobody gets, so this drives the gate: the session answers with eight steps,
// the job stops for a person, and the question carries all eight.
func TestAnEighthPlanStepReachesTheQuestionAPersonAnswers(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(plannedJob())
	ctx := context.Background()
	over := job.PlanSteps + 1

	controller.Tick(ctx)
	plane.lands("Here is the plan.\n\n" + aPlanOfSteps(over))
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking {
		t.Fatalf("a plan of %d steps left the job %q rather than asking a person: %s",
			over, got.Phase, got.Reason)
	}
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1: it asked for the plan again", plane.sent())
	}
	for i := 1; i <= over; i++ {
		if !strings.Contains(got.Plan, fmt.Sprintf("Step %d:", i)) {
			t.Fatalf("the plan on the row lost step %d of %d: %q", i, over, got.Plan)
		}
	}
	if warningAbout(got.Question, "steps", over, job.PlanSteps) == "" {
		t.Fatalf("nothing in the question says this plan has %d steps where the guide is %d: %q",
			over, job.PlanSteps, got.Question)
	}
}

// An eighth vertical is read, kept and put to a person, under the same kind of line.
func TestAnEighthVerticalIsKeptAndWarnedAbout(t *testing.T) {
	over := job.DesignVerticals + 1

	read, err := job.ReadDesign(manyVerticals(over))
	if err != nil {
		t.Fatalf("a list of %d verticals was refused rather than warned about: %v", over, err)
	}
	if len(read.Verticals) != over {
		t.Fatalf("the list reads back as %d verticals and it was written with %d",
			len(read.Verticals), over)
	}

	kept := job.DesignText(read)
	shown := job.AskingWhetherThisIsTheList("you paste a link and get the text back", kept)
	for i := 1; i <= over; i++ {
		for _, line := range []string{
			fmt.Sprintf("Vertical %d: a person reads the transcript on surface %d", i, i),
			fmt.Sprintf("Shown %d: the person opens surface %d and the text is there", i, i),
		} {
			if !strings.Contains(shown, line) {
				t.Fatalf("what a person is asked lost %q", line)
			}
		}
	}
	warning := warningAbout(shown, "verticals", over, job.DesignVerticals)
	if warning == "" {
		t.Fatalf("nothing says this list has %d verticals where the guide is %d: %q",
			over, job.DesignVerticals, shown)
	}
	if strings.Index(shown, warning) > strings.Index(shown, "Vertical 1:") {
		t.Fatalf("the count is under the list it is about, and a reader meets it too late: %q", shown)
	}
}

// A list inside its guide is not warned about, for the reason a plan inside its guide is not.
func TestAListInsideItsGuideIsNotWarnedAbout(t *testing.T) {
	read, err := job.ReadDesign(manyVerticals(job.DesignVerticals))
	if err != nil {
		t.Fatalf("a list of %d verticals was refused: %v", job.DesignVerticals, err)
	}
	shown := job.AskingWhetherThisIsTheList("you paste a link and get the text back", job.DesignText(read))
	if strings.Contains(shown, "guide") {
		t.Fatalf("a list inside its guide was warned about: %q", shown)
	}
}

// The same, at the surface the operator reads: the session says it would build eight things, the job
// stops, and the question carries every one of them.
func TestAnEighthVerticalReachesTheQuestionAPersonAnswers(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(listingJob())
	ctx := context.Background()
	over := job.DesignVerticals + 1

	controller.Tick(ctx)
	plane.lands("Here is what I would build.\n\n" + manyVerticals(over))
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking {
		t.Fatalf("a list of %d verticals left the job %q rather than asking a person: %s",
			over, got.Phase, got.Reason)
	}
	for i := 1; i <= over; i++ {
		if !strings.Contains(got.Design, fmt.Sprintf("Vertical %d:", i)) {
			t.Fatalf("the list on the row lost vertical %d of %d: %q", i, over, got.Design)
		}
	}
	if warningAbout(got.Question, "verticals", over, job.DesignVerticals) == "" {
		t.Fatalf("nothing in the question says this list has %d verticals where the guide is %d: %q",
			over, job.DesignVerticals, got.Question)
	}
}

// A seventeenth label is accepted, where the declaration used to refuse the job for it.
//
// The length of a label stays a ceiling, because Kubernetes refuses a value over 63 characters and
// no warning here changes what the cluster does. How many of them there are is this system's own
// number, so it is a guide.
func TestASeventeenthLabelIsAccepted(t *testing.T) {
	over := job.LabelCount + 1
	d := declared()
	d.Labels = map[string]string{}
	for i := 1; i <= over; i++ {
		d.Labels[fmt.Sprintf("key-%d", i)] = fmt.Sprintf("value-%d", i)
	}

	if err := d.Validate(); err != nil {
		t.Fatalf("a declaration carrying %d labels was refused rather than warned about: %v", over, err)
	}
}
