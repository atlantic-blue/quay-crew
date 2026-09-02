package job

import "testing"

func TestAJobThatHasNotStartedIsInIdeation(t *testing.T) {
	one := &Job{Product: "you paste a link and get the text back", Phase: PhasePending}
	stage := StageOf(one)
	if stage.Name != StageIdeation || stage.Number != 1 {
		t.Fatalf("a job nobody has started is %q, %d of four", stage.Name, stage.Number)
	}
	if !stage.Built {
		t.Fatalf("ideation reads as not built, and it is the one stage that is")
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
	if stage.Opens != "nothing opens test yet, it is a later slice" {
		t.Fatalf("design says the next stage opens on %q", stage.Opens)
	}
}

// The honest reading of a stage nobody has written. A job in design is running, and saying so
// without saying design does nothing would be the job claiming work that does not exist.
func TestAJobInAnUnbuiltStageSaysSo(t *testing.T) {
	stage := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
	})
	if stage.Built {
		t.Fatalf("design reads as built, and design is a later slice")
	}
	if stage.Unbuilt == "" {
		t.Fatalf("a job in design is told nothing about design not being built")
	}
	if stage.Where() != "stage 2 of 4: design" {
		t.Fatalf("a job in design reads as %q", stage.Where())
	}
}

// A row written before the ideation stage existed carries an approved plan and no reading. It is
// past the gate, and the reading must not say a person answered a question nobody asked.
func TestARowOlderThanIdeationIsPastIt(t *testing.T) {
	stage := StageOf(&Job{
		Product:      "you paste a link and get the text back",
		Plan:         "Step 1: read the design",
		PlanApproved: true,
	})
	if stage.Name != StageDesign {
		t.Fatalf("a job with an approved plan and no reading is in %q", stage.Name)
	}
	want := "ideation closed on the plan a person approved, because this job is older than the " +
		"ideation stage"
	if stage.Closed != want {
		t.Fatalf("it says ideation was closed by %q", stage.Closed)
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

// What closed the stage before each one, and what opens the next, for all four. Two of the four are
// later slices, and a reader is told that rather than left to find out.
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
	// Nothing moves a job into test or into build, because neither is built. A phrase here would be
	// the reading claiming a boundary the code does not have.
	if StageOpenedBy(StageTest) != "" || StageOpenedBy(StageBuild) != "" {
		t.Fatalf("test opens on %q and build on %q, and neither stage is built",
			StageOpenedBy(StageTest), StageOpenedBy(StageBuild))
	}
	if !StageBuilt(StageIdeation) {
		t.Fatalf("ideation reads as not built")
	}
	for _, name := range []string{StageDesign, StageTest, StageBuild} {
		if StageBuilt(name) {
			t.Fatalf("%s reads as built, and only ideation is", name)
		}
	}
}

func TestNoJobIsInNoStage(t *testing.T) {
	if stage := StageOf(nil); stage.Name != "" || stage.Outside != "" {
		t.Fatalf("nothing at all is in stage %q", stage.Name)
	}
}

// What a job in design is told it is doing, for each of the three states it can be in. The line has
// to be true of the job in front of the reader: the moment ideation closes there is no plan at all,
// and a job cannot be carrying on under one nobody has written.
func TestAJobInDesignIsToldWhatItIsActuallyDoing(t *testing.T) {
	justAnswered := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
	})
	want := "design is not built yet, so this job writes its plan next, and a person approves it " +
		"before any work starts"
	if justAnswered.Unbuilt != want {
		t.Fatalf("a job that has just answered its reading is told %q", justAnswered.Unbuilt)
	}

	written := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		Plan:           "Step 1: read the design",
	})
	if written.Unbuilt != "design is not built yet, so this job holds a plan nobody has approved yet" {
		t.Fatalf("a job whose plan nobody answered is told %q", written.Unbuilt)
	}

	approved := StageOf(&Job{
		Product:        "you paste a link and get the text back",
		IdeationAnswer: "1: on the command line",
		Plan:           "Step 1: read the design",
		PlanApproved:   true,
	})
	working := "design is not built yet, so this job carries on under the plan a person approved"
	if approved.Unbuilt != working {
		t.Fatalf("a job working to an approved plan is told %q", approved.Unbuilt)
	}
}
