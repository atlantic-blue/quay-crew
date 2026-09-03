package job

import (
	"strings"
	"testing"
)

func TestAJobThatHasNotStartedIsInIdeation(t *testing.T) {
	one := &Job{Product: "you paste a link and get the text back", Phase: PhasePending}
	stage := StageOf(one)
	if stage.Name != StageIdeation || stage.Number != 1 {
		t.Fatalf("a job nobody has started is %q, %d of four", stage.Name, stage.Number)
	}
	if !stage.Built {
		t.Fatalf("ideation reads as not built, and it is built")
	}
	if stage.Closed != "nothing came before it, ideation is the first stage" {
		t.Fatalf("something closed a stage before the first one: %q", stage.Closed)
	}
	if stage.Opens != "design opens on your answer to what it understood" {
		t.Fatalf("ideation says the next stage opens on %q", stage.Opens)
	}
	if stage.Doing != "" {
		t.Fatalf("a job in ideation is told where it stands inside it: %q", stage.Doing)
	}
}

func TestAnAnsweredReadingPutsAJobInDesign(t *testing.T) {
	one := &Job{
		Product:        "you paste a link and get the text back",
		Ideation:       "Understood: a page that takes a link",
		IdeationAnswer: "1: on the command line",
		Phase:          PhaseRunning,
	}
	stage := StageOf(one)
	if stage.Name != StageDesign || stage.Number != 2 {
		t.Fatalf("a job whose reading was answered is %q, %d of four", stage.Name, stage.Number)
	}
	if stage.Closed != "ideation closed on your answer to what it understood" {
		t.Fatalf("design says ideation was closed by %q", stage.Closed)
	}
	if stage.Opens != "test opens on your acceptance of the list it would build" {
		t.Fatalf("design says the next stage opens on %q", stage.Opens)
	}
	// Built, and it holds one standing only. This is the way off the old reading: design used to be a
	// name with nothing behind it, and a job standing in it was told so.
	if !stage.Built || stage.Doing != "" {
		t.Fatalf("design reads as unbuilt, or says where a job stands in it: %q", stage.Doing)
	}
}

// An accepted list closes design, and the job stands in test until its requirements have failing
// tests.
func TestAnAcceptedListPutsAJobInTest(t *testing.T) {
	stage := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		Design:         "Vertical 1: a person pastes a link and gets the text back",
		DesignAccepted: true,
	})
	if stage.Name != StageTest || stage.Number != 3 {
		t.Fatalf("a job whose list was accepted is %q, %d of four", stage.Name, stage.Number)
	}
	if stage.Closed != "design closed on your acceptance of the list it would build" {
		t.Fatalf("test says design was closed by %q", stage.Closed)
	}
	if stage.Opens != "build opens on a failing test for every requirement on that list" {
		t.Fatalf("test says the next stage opens on %q", stage.Opens)
	}
	// Built, and it holds one standing only. This is the way off the old reading: test used to be a
	// name with nothing behind it, and a job standing in it was told so.
	if !stage.Built || stage.Doing != "" {
		t.Fatalf("test reads as unbuilt, or says where a job stands in it: %q", stage.Doing)
	}
}

// A red suite closes test, and the stage after it is the one nobody has written.
func TestAFailingSuitePutsAJobInBuild(t *testing.T) {
	stage := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		Design:         "Vertical 1: a person pastes a link and gets the text back",
		DesignAccepted: true,
		Tests:          "Requirement 1: a person pastes a link\nRan 1: 12\nFails 1: TestItFails",
	})
	if stage.Name != StageBuild || stage.Number != 4 {
		t.Fatalf("a job whose suite is red is %q, %d of four", stage.Name, stage.Number)
	}
	if stage.Closed != "test closed on a failing test for every requirement on that list" {
		t.Fatalf("build says test was closed by %q", stage.Closed)
	}
	if stage.Opens != "nothing, build is the last stage" {
		t.Fatalf("build says the next stage opens on %q", stage.Opens)
	}
}

// All four stages are built now, and this is the way off the old reading. The sentence a job in build
// used to carry named a stage that did nothing, and a reader who met it again would believe the work
// had not landed.
func TestNoJobIsToldItsStageIsNotBuilt(t *testing.T) {
	stage := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		DesignAccepted: true,
		Tests:          "Requirement 1: a person pastes a link\nRan 1: 12\nFails 1: TestItFails",
	})
	if !stage.Built {
		t.Fatalf("build reads as a stage that is not built, and it is built")
	}
	if strings.Contains(stage.Doing, "not built") {
		t.Fatalf("a job in build is told %q", stage.Doing)
	}
	if stage.Where() != "stage 4 of 4: build" {
		t.Fatalf("a job in build reads as %q", stage.Where())
	}
}

// A row written before these stages existed carries an approved plan, no reading and no list. It is
// past both gates, and the reading must not say a person answered a question nobody asked or
// accepted a list nobody put to them.
func TestARowOlderThanTheseStagesIsPastThem(t *testing.T) {
	stage := StageOf(&Job{
		Product:      "you paste a link and get the text back",
		Plan:         "Step 1: read the design",
		PlanApproved: true,
	})
	if stage.Name != StageBuild {
		t.Fatalf("a job with an approved plan and no list is in %q", stage.Name)
	}
	want := "test closed on the plan itself, because this job is older than the test stage"
	if stage.Closed != want {
		t.Fatalf("it says test was closed by %q", stage.Closed)
	}
}

// A row written after ideation shipped and before the list did: it answered a reading, wrote a plan
// and is waiting on it. It is at the plan gate, so sending it back to design would take work a
// person had already agreed to back to the beginning.
func TestARowHoldingAPlanIsPastTheList(t *testing.T) {
	one := &Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		Plan:           "Step 1: read the design",
	}
	if WaitingForItsDesign(one) {
		t.Fatalf("a job holding a plan is asked for the list it would build")
	}
	if !WaitingForItsPlan(one) {
		t.Fatalf("a job holding a plan nobody approved owes no plan")
	}
	if stage := StageOf(one); stage.Name != StageBuild {
		t.Fatalf("a job holding a plan reads as stage %q", stage.Name)
	}
}

func TestAnErrandRunsNoStages(t *testing.T) {
	stage := StageOf(&Job{Title: "rotate the key"})
	if stage.Name != "" || stage.Number != 0 {
		t.Fatalf("a job that states no sentence is in stage %q", stage.Name)
	}
	if stage.Outside == "" {
		t.Fatalf("a job with no stage does not say why it has none")
	}
	if stage.Says() != "-" {
		t.Fatalf("a listing carries %q for a job with no stage", stage.Says())
	}
	if stage.Where() != "no stage" {
		t.Fatalf("a reading of an errand says %q", stage.Where())
	}
}

// A step of a flow run follows the graph a person imported, which is the plan, so it runs none of the
// four stages. What decides that is the run it belongs to and not a job above it: no job is above it.
func TestAStepOfAFlowRunCarriesNoStageOfItsOwn(t *testing.T) {
	stage := StageOf(&Job{
		Product: "you paste a link and get the text back",
		Run:     "a-run",
	})
	if stage.Name != "" {
		t.Fatalf("a step of a run is in stage %q of its own", stage.Name)
	}
	if stage.Outside == "" {
		t.Fatalf("a step of a run does not say why it has no stage of its own")
	}
}

// And a job a session declared is a job like any other. It states the sentence, so it runs the four
// stages, starting at the first: what caused it says how it came about and nothing about what it is.
func TestAJobASessionDeclaredRunsItsOwnStages(t *testing.T) {
	stage := StageOf(&Job{
		Product: "you paste a link and get the text back",
		Cause:   "the-job-whose-session-declared-it",
	})
	if stage.Outside != "" {
		t.Fatalf("a job a session declared runs no stages, saying %q", stage.Outside)
	}
	if stage.Name != StageIdeation {
		t.Fatalf("a job a session declared is in stage %q, want ideation", stage.Name)
	}
}

// What closed the stage before each one, and what opens the next, for all four. One of the four is a
// later slice, and a reader is told that rather than left to find out.
func TestWhatClosesAndOpensEachStage(t *testing.T) {
	if len(Stages) != 4 {
		t.Fatalf("there are %d stages", len(Stages))
	}
	for i, name := range []string{StageIdeation, StageDesign, StageTest, StageBuild} {
		if Stages[i] != name {
			t.Fatalf("stage %d of four is %q and it is %q", i+1, Stages[i], name)
		}
	}
	if StageOpenedBy(StageIdeation) != "" {
		t.Fatalf("something opens the first stage: %q", StageOpenedBy(StageIdeation))
	}
	if StageOpenedBy(StageDesign) == "" {
		t.Fatalf("nothing opens design, and answering what a job understood does")
	}
	if StageOpenedBy(StageTest) == "" {
		t.Fatalf("nothing opens test, and accepting the list does")
	}
	if StageOpenedBy(StageBuild) == "" {
		t.Fatalf("nothing opens build, and a failing test for every requirement does")
	}
	for _, name := range Stages {
		if !StageBuilt(name) {
			t.Fatalf("%s reads as not built", name)
		}
	}
	// A name that is not one of the four is not a stage, so it does not read as a stage that works.
	// That is what the day a fifth is named rests on: it has to read differently until it does.
	if StageBuilt("acceptance") {
		t.Fatalf("a stage nobody has written reads as built")
	}
}

func TestNoJobIsInNoStage(t *testing.T) {
	if stage := StageOf(nil); stage.Name != "" || stage.Outside != "" {
		t.Fatalf("nothing at all is in stage %q", stage.Name)
	}
}

// What a job past its failing tests is told it is doing, for each of the three states it can be in.
// The line has to be true of the job in front of the reader: the moment the suite goes red there is
// no plan at all, and a job cannot be carrying on under one nobody has written.
func TestAJobInBuildIsToldWhatItIsActuallyDoing(t *testing.T) {
	red := "Requirement 1: a person pastes a link\nRan 1: 12\nFails 1: TestItFails"
	justRed := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		DesignAccepted: true,
		Tests:          red,
	})
	if !strings.Contains(justRed.Doing, "writes the plan that turns those tests green next") {
		t.Fatalf("a job whose suite has just gone red is told %q", justRed.Doing)
	}

	written := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		DesignAccepted: true,
		Tests:          red,
		Plan:           "Step 1: read the design",
	})
	if !strings.Contains(written.Doing, "holds a plan nobody has approved yet") {
		t.Fatalf("a job whose plan nobody answered is told %q", written.Doing)
	}

	building := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		DesignAccepted: true,
		Tests:          red,
		Plan:           "Step 1: read the design",
		PlanApproved:   true,
	})
	// The two halves of the sentence this stage serves: several sessions at once, and none of them
	// able to change a test.
	for _, want := range []string{"one session for each vertical", "none of them can change a test"} {
		if !strings.Contains(building.Doing, want) {
			t.Fatalf("a job whose verticals are being built is told %q", building.Doing)
		}
	}

	// And once they are all green the job is waiting for a person, which is a different thing again
	// from building. A reader told only "build" cannot tell those two apart.
	held := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		DesignAccepted: true,
		Tests:          red,
		Plan:           "Step 1: read the design",
		PlanApproved:   true,
		Build: "Vertical 1: a person pastes a link\nRan 1: 14\nPasses 1: TestItFails\n" +
			"Picture 1: paste.png\nTaken 1: the command line, drawn with krewe render",
	})
	if !strings.Contains(held.Doing, "look at the evidence for 1 vertical") {
		t.Fatalf("a job whose verticals are all built is told %q", held.Doing)
	}

	// And once a person has looked, the same stage holds a job that is finished, which is a fourth
	// standing. A reader told it waits for them to look, on a job they already looked at, is being
	// asked for something they gave.
	accepted := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		DesignAccepted: true,
		Tests:          red,
		Plan:           "Step 1: read the design",
		PlanApproved:   true,
		Build: "Vertical 1: a person pastes a link\nRan 1: 14\nPasses 1: TestItFails\n" +
			"Picture 1: paste.png\nTaken 1: the command line, drawn with krewe render",
		Accepted: true,
	})
	if !strings.Contains(accepted.Doing, "said the value arrived") {
		t.Fatalf("a job a person accepted is told %q", accepted.Doing)
	}
}
