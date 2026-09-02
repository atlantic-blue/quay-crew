package job

import "testing"

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
	if stage.Unbuilt != "" {
		t.Fatalf("a job in ideation is told its stage is not built: %q", stage.Unbuilt)
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
	// Built, and it says nothing about being unbuilt. This is the way off the old reading: design used
	// to be a name with nothing behind it, and a job standing in it was told so.
	if !stage.Built || stage.Unbuilt != "" {
		t.Fatalf("design reads as unbuilt, saying %q", stage.Unbuilt)
	}
}

// An accepted list closes design, and the stage after it is one nobody has written.
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
	if stage.Opens != "nothing opens build yet, it is a later slice" {
		t.Fatalf("test says the next stage opens on %q", stage.Opens)
	}
}

// The honest reading of a stage nobody has written. A job past the list is running, and saying so
// without saying test does nothing would be the job claiming work that does not exist.
func TestAJobInAnUnbuiltStageSaysSo(t *testing.T) {
	stage := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		DesignAccepted: true,
	})
	if stage.Built {
		t.Fatalf("test reads as built, and test is a later slice")
	}
	if stage.Unbuilt == "" {
		t.Fatalf("a job in test is told nothing about test not being built")
	}
	if stage.Where() != "stage 3 of 4: test" {
		t.Fatalf("a job in test reads as %q", stage.Where())
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
	if stage.Name != StageTest {
		t.Fatalf("a job with an approved plan and no list is in %q", stage.Name)
	}
	want := "design closed on the plan itself, because this job is older than the design stage"
	if stage.Closed != want {
		t.Fatalf("it says design was closed by %q", stage.Closed)
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
	if stage := StageOf(one); stage.Name != StageTest {
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

func TestAChildJobCarriesTheStageOfTheJobAboveIt(t *testing.T) {
	stage := StageOf(&Job{
		Product: "you paste a link and get the text back",
		Parent:  "job-above",
		Depth:   1,
	})
	if stage.Name != "" {
		t.Fatalf("a child job is in stage %q of its own", stage.Name)
	}
	if stage.Outside == "" {
		t.Fatalf("a child job does not say why it has no stage of its own")
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
	// Nothing moves a job into build, because build is not built. A phrase here would be the reading
	// claiming a boundary the code does not have.
	if StageOpenedBy(StageBuild) != "" {
		t.Fatalf("build opens on %q, and build is a later slice", StageOpenedBy(StageBuild))
	}
	for _, name := range []string{StageIdeation, StageDesign} {
		if !StageBuilt(name) {
			t.Fatalf("%s reads as not built", name)
		}
	}
	for _, name := range []string{StageTest, StageBuild} {
		if StageBuilt(name) {
			t.Fatalf("%s reads as built, and it is a later slice", name)
		}
	}
}

func TestNoJobIsInNoStage(t *testing.T) {
	if stage := StageOf(nil); stage.Name != "" || stage.Outside != "" {
		t.Fatalf("nothing at all is in stage %q", stage.Name)
	}
}

// What a job past the list is told it is doing, for each of the three states it can be in. The line
// has to be true of the job in front of the reader: the moment the list is accepted there is no plan
// at all, and a job cannot be carrying on under one nobody has written.
func TestAJobInTestIsToldWhatItIsActuallyDoing(t *testing.T) {
	justAccepted := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		DesignAccepted: true,
	})
	want := "test is not built yet, so this job writes its plan next, and a person approves it " +
		"before any work starts"
	if justAccepted.Unbuilt != want {
		t.Fatalf("a job that has just had its list accepted is told %q", justAccepted.Unbuilt)
	}

	written := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		DesignAccepted: true,
		Plan:           "Step 1: read the design",
	})
	if written.Unbuilt != "test is not built yet, so this job holds a plan nobody has approved yet" {
		t.Fatalf("a job whose plan nobody answered is told %q", written.Unbuilt)
	}

	approved := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		DesignAccepted: true,
		Plan:           "Step 1: read the design",
		PlanApproved:   true,
	})
	working := "test is not built yet, so this job carries on under the plan a person approved"
	if approved.Unbuilt != working {
		t.Fatalf("a job working to an approved plan is told %q", approved.Unbuilt)
	}
}
