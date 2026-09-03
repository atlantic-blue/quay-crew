package job_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A long evidence label and a long step are kept whole, and the person is told how long they are.
//
// Two jobs paid for this. Job 325aa685 was held twice in one night, once on a label of 803
// characters against a ceiling of 200 and once on a label of 590. Job a171e9c4 was held on the same
// ceiling at 741 characters, and on a step of 312.
//
// The cost is not the message. A refusal in the build stage sends the whole stage back to a person
// and runs every worker again, so one long sentence costs four sessions and the words in it are
// thrown away. So the number is a guide here, the way it is a guide on a reading: the record keeps
// every word, and one line says which part is long, how many bytes it is, and what the guide is.
// That is what an operator needs to say "that is fine" or "say it shorter next time".

// theLongEvidenceLabel and theLongEvidenceStep are the two sizes those jobs were held on. They are the incidents'
// numbers rather than round ones, because what these hold is that a person is told a measurement.
const theLongEvidenceLabel, theLongEvidenceStep = 803, 312

// anEvidenceLabelOf is a label of exactly this many bytes. It opens with what was running and the command
// behind it, so it is a label a person could act on, and it ends with words an assertion can look
// for: a label cut at either end then reads as a cut rather than as a pass.
func anEvidenceLabelOf(size int) string {
	return anEvidenceLineOf(size,
		"the console at http://localhost:3000 with krewe up running behind it,",
		"the row was drawn on the third tick, after the reason column had something in it, ",
		"and this label ends here")
}

// anEvidenceStepOf is a step of exactly this many bytes. A step says one thing a person does and what they
// should see, so both halves are in it, and the padding sits between them.
func anEvidenceStepOf(size int) string {
	return anEvidenceLineOf(size,
		"open http://localhost:3000 and press j until the stopped row is under the cursor,",
		"the row is the one the third tick drew, with the reason beside it, ",
		"and you see the reason printed under the job")
}

// anEvidenceLineOf is one line of exactly this many bytes, opening and ending with words an assertion can
// find. The middle is prose rather than one repeated letter, so a case that passes says the words
// came back in the order they went in.
func anEvidenceLineOf(size int, opens, filler, ends string) string {
	if size <= len(opens)+len(ends)+2 {
		panic(fmt.Sprintf("a saying of %d bytes cannot carry both ends", size))
	}
	built := opens + " "
	for len(built) < size-len(ends)-1 {
		built += filler
	}
	built = built[:size-len(ends)-1]
	if strings.HasSuffix(built, " ") {
		built = built[:len(built)-1] + "x"
	}
	said := built + " " + ends
	if len(said) != size {
		panic(fmt.Sprintf("this saying is %d bytes and the case wants %d", len(said), size))
	}
	return said
}

// aReportShownWithAPicture is what a worker answers with for a vertical shown with a picture, with
// the label the case wants to be long.
func aReportShownWithAPicture(vertical int, passing, taken string) string {
	return fmt.Sprintf("I built vertical %d and ran the suite.\n\nRan: 14\nRed: 0\n"+
		"Passing 1: %s\nChanged 1: internal/transcript/vertical%d.go\nPicture: vertical%d.png\n"+
		"Taken: %s", vertical, passing, vertical, vertical, taken)
}

// aReportShownWithSteps is the same report for a vertical shown with steps, with the first step the
// one the case wants to be long.
func aReportShownWithSteps(vertical int, passing string, steps []string) string {
	said := fmt.Sprintf("I built vertical %d and ran the suite.\n\nRan: 9\nRed: 0\n"+
		"Passing 1: %s\nChanged 1: internal/transcript/vertical%d.go\n", vertical, passing, vertical)
	for at, step := range steps {
		said += fmt.Sprintf("Steps %d: %s\n", at+1, step)
	}
	return said + "Taken: krewe up is running and the console is open at http://localhost:3000"
}

// theStepsBesideALongOne are the steps that go beside a long one, because steps a person follows are at least
// two and this case is about the length of one of them.
var theStepsBesideALongOne = []string{
	"press j until the stopped row is under the cursor and see it highlighted",
	"press enter and see the reason printed under the job",
}

// theEvidenceLengthWarning is the line that says a part of the record is long, and empty where nothing says
// it.
//
// A line rather than the whole text: an operator reads this in a terminal, and a measurement spread
// over a paragraph is one they have to assemble. The line carries which part is long, how many bytes
// it is, and the guide, because two numbers with no part named leave them counting to find out which
// part to shorten. Any of the words a person reads for that part will do: the record calls the label
// Taken and the question calls it taken, and this holds neither word against the other.
func theEvidenceLengthWarning(shown string, naming []string, size, guide int) string {
	for _, line := range strings.Split(shown, "\n") {
		named := false
		for _, word := range naming {
			if strings.Contains(strings.ToLower(line), strings.ToLower(word)) {
				named = true
			}
		}
		if !named {
			continue
		}
		found := map[string]bool{}
		for _, one := range theNumbers.FindAllString(line, -1) {
			found[one] = true
		}
		if found[strconv.Itoa(size)] && found[strconv.Itoa(guide)] {
			return line
		}
	}
	return ""
}

// theWordsForAnEvidenceLabel and theWordsForAnEvidenceStep are how a person reads each part on the surfaces they meet
// it on.
var theWordsForAnEvidenceLabel = []string{"label", "taken"}
var theWordsForAnEvidenceStep = []string{"step"}

// aBuildShownWithSteps is a job whose second vertical asks to be shown with steps, so a report of
// steps for it is the kind that vertical asked for.
func aBuildShownWithSteps(t *testing.T) *job.Job {
	t.Helper()
	one := buildingJob()
	one.Design = job.DesignText(job.DesignIn(aListReply + "\nEvidence 2: steps"))
	wanted := job.RequirementsOf(one)
	if len(wanted) != 2 {
		t.Fatalf("this job carries %d verticals, and the case is about the second one", len(wanted))
	}
	if wanted[1].Evidence != job.KindSteps {
		t.Fatalf("this job's second vertical is shown with %q, and the case is about steps",
			wanted[1].Evidence)
	}
	return one
}

// The label is read back byte for byte. It is the reading, before any stage or any record: a label
// refused here never reaches either.
func TestALabelOfEightHundredAndThreeBytesIsReadAndKeptWordForWord(t *testing.T) {
	label := anEvidenceLabelOf(theLongEvidenceLabel)
	if len(label) <= job.EvidenceLimit {
		t.Fatalf("this case writes a label of %d bytes, which is inside the old ceiling of %d and "+
			"proves nothing", len(label), job.EvidenceLimit)
	}

	report, err := job.ReadBuildReport(aReportShownWithAPicture(1, theFailures[1], label), 1)
	if err != nil {
		t.Fatalf("a label of %d bytes was refused: %v", len(label), err)
	}
	if report.Taken != label {
		t.Fatalf("the label was kept as %d bytes of the %d it was written with: %q",
			len(report.Taken), len(label), report.Taken)
	}
}

// And the step, on the same reading. A step over the guide used to be refused one at a time, which
// threw away the steps beside it as well.
func TestAStepOfThreeHundredAndTwelveBytesIsReadAndKeptWordForWord(t *testing.T) {
	step := anEvidenceStepOf(theLongEvidenceStep)
	if len(step) <= job.StepsLineLimit {
		t.Fatalf("this case writes a step of %d bytes, which is inside the old ceiling of %d and "+
			"proves nothing", len(step), job.StepsLineLimit)
	}

	report, err := job.ReadBuildReport(
		aReportShownWithSteps(2, theFailures[2], append([]string{step}, theStepsBesideALongOne...)), 2)
	if err != nil {
		t.Fatalf("a step of %d bytes was refused: %v", len(step), err)
	}
	if len(report.Steps) != 3 {
		t.Fatalf("the report carries %d steps, want the 3 that were written: %v",
			len(report.Steps), report.Steps)
	}
	if report.Steps[0] != step {
		t.Fatalf("the step was kept as %d bytes of the %d it was written with: %q",
			len(report.Steps[0]), len(step), report.Steps[0])
	}
	// The steps beside it survive too. The refusal used to take the whole report, so the two short
	// steps went with the long one.
	for at, said := range theStepsBesideALongOne {
		if report.Steps[at+1] != said {
			t.Fatalf("step %d was kept as %q, and it was written as %q", at+2, report.Steps[at+1], said)
		}
	}
}

// The gate at the close of the stage. A report the reading accepts and the gate refuses is a stage
// that still sends every worker back, which is the cost this whole change is about.
func TestTheStageDoesNotRefuseAVerticalForTheLengthOfItsLabelOrItsStep(t *testing.T) {
	one := aBuildShownWithSteps(t)
	wanted := job.RequirementsOf(one)
	failing := job.FailuresOn(one.Tests)
	label, step := anEvidenceLabelOf(theLongEvidenceLabel), anEvidenceStepOf(theLongEvidenceStep)

	reports := map[int]job.BuildReport{}
	first, err := job.ReadBuildReport(aReportShownWithAPicture(1, theFailures[1], label), 1)
	if err != nil {
		t.Fatalf("the report on vertical 1 was refused: %v", err)
	}
	second, err := job.ReadBuildReport(
		aReportShownWithSteps(2, theFailures[2], append([]string{step}, theStepsBesideALongOne...)), 2)
	if err != nil {
		t.Fatalf("the report on vertical 2 was refused: %v", err)
	}
	reports[1], reports[2] = first, second

	if err := job.BuiltGreen(wanted, failing, reports); err != nil {
		t.Fatalf("the stage refused two built verticals for the length of their evidence: %v", err)
	}
	// And the same report read the way the stage reads it, off the run that answered it, because that
	// is the road the refusal was on.
	if _, why := job.BuiltBy([]*job.Execution{{Phase: job.PhaseDone, Number: 1,
		Answer: aReportShownWithAPicture(1, theFailures[1], label)}}, wanted[0], failing[1]); why != "" {
		t.Fatalf("the run holding vertical 1 was refused for a label of %d bytes: %s", len(label), why)
	}
	if _, why := job.BuiltBy([]*job.Execution{{Phase: job.PhaseDone, Number: 2,
		Answer: aReportShownWithSteps(2, theFailures[2], append([]string{step}, theStepsBesideALongOne...))}},
		wanted[1], failing[2]); why != "" {
		t.Fatalf("the run holding vertical 2 was refused for a step of %d bytes: %s", len(step), why)
	}
}

// The record the job keeps carries every word of both. It is what a person reads later with krewe
// job show, and it is what the acceptance stage reads its evidence out of, so a label cut here is a
// label nobody can reproduce from.
func TestTheRecordKeepsTheWholeLabelAndTheWholeStep(t *testing.T) {
	one := aBuildShownWithSteps(t)
	wanted := job.RequirementsOf(one)
	label, step := anEvidenceLabelOf(theLongEvidenceLabel), anEvidenceStepOf(theLongEvidenceStep)

	first, err := job.ReadBuildReport(aReportShownWithAPicture(1, theFailures[1], label), 1)
	if err != nil {
		t.Fatalf("the report on vertical 1 was refused: %v", err)
	}
	second, err := job.ReadBuildReport(
		aReportShownWithSteps(2, theFailures[2], append([]string{step}, theStepsBesideALongOne...)), 2)
	if err != nil {
		t.Fatalf("the report on vertical 2 was refused: %v", err)
	}

	kept := job.BuiltText(wanted, map[int]job.BuildReport{1: first, 2: second})
	if !strings.Contains(kept, label) {
		t.Fatalf("the record lost the %d bytes of the label: %q", len(label), kept)
	}
	if !strings.Contains(kept, step) {
		t.Fatalf("the record lost the %d bytes of the step: %q", len(step), kept)
	}
	// And it reads back off the record whole, because the acceptance stage reads the evidence out of
	// the record and not out of the report.
	shown := job.EvidenceFor(kept, 1)
	if shown.Taken != label {
		t.Fatalf("the label reads back off the record as %d bytes of %d", len(shown.Taken), len(label))
	}
	steps := job.EvidenceFor(kept, 2).Steps
	if len(steps) == 0 || steps[0] != step {
		t.Fatalf("the step reads back off the record as %v", steps)
	}
}

// The whole requirement at the surface a person reads: the question the build stage stops on.
//
// It is driven through the controller rather than through the rendering alone, because what is held
// here is that the person is told. A measurement composed anywhere that never reaches the question
// is a measurement nobody gets, and a stage that sends the verticals back instead has spent four
// sessions on a sentence.
func TestALongLabelReachesAPersonWholeWithAWarningRatherThanSendingTheStageBack(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(buildingJob())
	ctx := context.Background()
	label := anEvidenceLabelOf(theLongEvidenceLabel)

	controller.Tick(ctx)
	controller.Tick(ctx)
	for _, run := range kept.executionsIn(one.ID, job.StageBuild) {
		plane.landsIn(job.SessionForExecution(run),
			landed(aReportShownWithAPicture(run.Number, theFailures[run.Number], label)))
	}
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Build == "" {
		t.Fatalf("every vertical was built and the job holds no record: %s", got.Reason)
	}
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q, want it asking a person to accept what was built: %s",
			got.Phase, got.Reason)
	}
	if !strings.Contains(got.Question, job.TheAcceptanceAsk) {
		t.Fatalf("the question is not the acceptance, so the length sent the stage back: %s", got.Question)
	}
	if !strings.Contains(got.Question, label) {
		t.Fatalf("the question a person is put lost the %d bytes of the label: %s",
			len(label), got.Question)
	}
	if !strings.Contains(got.Build, label) {
		t.Fatalf("the record lost the %d bytes of the label: %s", len(label), got.Build)
	}
	warning := theEvidenceLengthWarning(got.Question, theWordsForAnEvidenceLabel, theLongEvidenceLabel, job.EvidenceLimit)
	if warning == "" {
		t.Fatalf("nothing in the question says the label is %d bytes where the guide is %d: %s",
			theLongEvidenceLabel, job.EvidenceLimit, got.Question)
	}
	// The two verticals were built once. A stage that went back would run each of them again, which
	// is what the long sentence used to cost.
	if runs := kept.executionsIn(one.ID, job.StageBuild); len(runs) != 2 {
		t.Fatalf("the build stage wrote %d runs for 2 verticals", len(runs))
	}

	// And a label inside the guide is not warned about, on the same road. A warning on everything is
	// a warning nobody reads, and it would say every job in this system is long.
	short := "the console at http://localhost:3000, drawn with krewe render while krewe up was running"
	if len(short) > job.EvidenceLimit {
		t.Fatalf("this case writes a label of %d bytes, which is past the guide of %d and proves "+
			"nothing about a label inside it", len(short), job.EvidenceLimit)
	}
	quiet, held, machine := aController(t)
	another := held.add(buildingJob())
	quiet.Tick(ctx)
	quiet.Tick(ctx)
	for _, run := range held.executionsIn(another.ID, job.StageBuild) {
		machine.landsIn(job.SessionForExecution(run),
			landed(aReportShownWithAPicture(run.Number, theFailures[run.Number], short)))
	}
	quiet.Tick(ctx)
	if warning := theEvidenceLengthWarning(held.get(another.ID).Question, theWordsForAnEvidenceLabel,
		len(short), job.EvidenceLimit); warning != "" {
		t.Fatalf("a label of %d bytes inside the guide of %d was warned about: %q",
			len(short), job.EvidenceLimit, warning)
	}
}

// The same road for a step, on the vertical that asked to be shown with steps.
func TestALongStepReachesAPersonWholeWithAWarningRatherThanSendingTheStageBack(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(aBuildShownWithSteps(t))
	ctx := context.Background()
	step := anEvidenceStepOf(theLongEvidenceStep)

	controller.Tick(ctx)
	controller.Tick(ctx)
	for _, run := range kept.executionsIn(one.ID, job.StageBuild) {
		answer := aReportShownWithAPicture(run.Number, theFailures[run.Number], anEvidenceLabelOf(120))
		if run.Number == 2 {
			answer = aReportShownWithSteps(run.Number, theFailures[run.Number],
				append([]string{step}, theStepsBesideALongOne...))
		}
		plane.landsIn(job.SessionForExecution(run), landed(answer))
	}
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseAsking || !strings.Contains(got.Question, job.TheAcceptanceAsk) {
		t.Fatalf("the job is %q and the question is not the acceptance, so the length sent the stage "+
			"back: %s", got.Phase, got.Question)
	}
	if !strings.Contains(got.Question, step) {
		t.Fatalf("the question a person is put lost the %d bytes of the step: %s", len(step), got.Question)
	}
	if !strings.Contains(got.Build, step) {
		t.Fatalf("the record lost the %d bytes of the step: %s", len(step), got.Build)
	}
	warning := theEvidenceLengthWarning(got.Question, theWordsForAnEvidenceStep, theLongEvidenceStep, job.StepsLineLimit)
	if warning == "" {
		t.Fatalf("nothing in the question says the step is %d bytes where the guide is %d: %s",
			theLongEvidenceStep, job.StepsLineLimit, got.Question)
	}
}
