package job

import "fmt"

// The four stages a job moves through, and what a reader of one job is told about them.
//
// A job already says which phase it is in, and a phase says what the system is doing with the row:
// it is pending, it is running, it is asking. It does not say how far through the work the job is. A
// job waiting for an answer about what it understood and a job waiting for an answer about a failed
// build both read "asking", and those two are days apart. The stage is the other half of that pair,
// so a job stuck at the beginning reads differently from one stuck at the end.
//
// The stage is read off the row rather than written on it. Every boundary between two stages is
// already a fact the row carries, and the reading of the request is kept the same way, for the same
// reason: a third copy of something two columns already say can only disagree with them. The day a
// boundary needs a fact nothing records, that fact becomes a column and this keeps reading it.
const (
	// StageIdeation is what the job understood and assumed, and it asks a person before it plans.
	StageIdeation = "ideation"
	// StageDesign is how the work comes alive as verticals, each one a thing a person can be shown
	// working.
	StageDesign = "design"
	// StageTest is the requirements turned into failing tests.
	StageTest = "test"
	// StageBuild is the work, until those tests pass.
	StageBuild = "build"
)

// Stages are the four, in the order a job passes through them.
var Stages = []string{StageIdeation, StageDesign, StageTest, StageBuild}

// StageBuilt says whether a stage is built yet. All four are.
//
// It stays because a reader of a job asks the question, and because the day a fifth stage is named
// before it works, a named stage that does nothing has to read differently from one that works: the
// second reading is a lie the job itself tells.
func StageBuilt(stage string) bool {
	for _, one := range Stages {
		if one == stage {
			return true
		}
	}
	return false
}

// StageOpenedBy is what moves a job into this stage, as a phrase a sentence is built round. It is
// empty where nothing moves a job there yet, which is every stage after test, and empty for ideation,
// which nothing opens because it is the first.
func StageOpenedBy(stage string) string {
	switch stage {
	case StageDesign:
		return "your answer to what it understood"
	case StageTest:
		return "your acceptance of the list it would build"
	case StageBuild:
		return "a failing test for every requirement on that list"
	}
	return ""
}

// Stage is where one job stands: which of the four it is in, what closed the stage before it, and
// what opens the next one.
type Stage struct {
	// Name is the stage, and empty on a job that runs no stages.
	Name string
	// Number is which of the four it is, counting from one, and zero where there is no stage.
	Number int
	// Built says whether the stage this job is in is built yet.
	Built bool
	// Closed is what closed the stage before this one, in a sentence.
	Closed string
	// Opens is what opens the next stage, in a sentence.
	Opens string
	// Outside is why this job runs no stages at all, and empty on a job that does.
	Outside string
	// Doing is where the job stands inside the stage it is in, and empty where the stage has one
	// standing only. It is a fact about that job rather than about the stage, which is why it is here
	// rather than in the stage's own sentences: the build stage holds a job writing its plan, a job
	// whose verticals are being built in a session each, and a job waiting for somebody to accept what
	// arrived, and those three read the same off the stage alone.
	Doing string
}

// StageOf is the stage a job is in, read off what the job has done.
//
// The gate in front of the plan is the boundary. A job that owes a person what it understood is in
// ideation. A job whose reading a person answered is past it, and every later boundary is a later
// slice, so it reads as being in design with design not yet built. That is the honest reading rather
// than the flattering one: nothing here pretends the work reached a stage nobody has written.
//
// A job that runs no stages says so, and says why. An errand states no sentence, so there is nothing
// to write a plan from and nothing to hold it against, and it never enters this at all. A job
// declared under another is one part of a plan a person already approved on the job above it, and
// the stage of that work is that job's.
func StageOf(one *Job) Stage {
	if one == nil {
		return Stage{}
	}
	if one.Product == "" {
		return Stage{Outside: "this job states no sentence, so it is an errand and runs no stages"}
	}
	if one.Parent != "" {
		return Stage{Outside: "this job is one part of a plan approved on the job above it, " +
			"so its stage is that job's"}
	}
	if WaitingForItsIdeation(one) {
		return stageStanding(StageIdeation, "")
	}
	// A row written before ideation existed carries an approved plan and no reading. It is past the
	// gate, and saying a person answered a question nobody asked would be a false record.
	closedOn := StageOpenedBy(StageDesign)
	if !Ideated(one) {
		closedOn = "the plan a person approved, because this job is older than the ideation stage"
	}
	if WaitingForItsDesign(one) {
		return stageStanding(StageDesign, closedOn)
	}
	// Past the list. A row written before the list existed reads the same way and says why: it carries
	// a plan and no list, and saying a person accepted a list nobody put to them would be a false
	// record.
	accepted := StageOpenedBy(StageTest)
	if !Designed(one) {
		accepted = "the plan itself, because this job is older than the design stage"
	}
	if WaitingForItsTests(one) {
		return stageStanding(StageTest, accepted)
	}
	// Past the tests, so the stage is build, which is the last one. A row that never went through the
	// test stage says so rather than claiming a red suite nobody ran.
	red := StageOpenedBy(StageBuild)
	if !TestsWritten(one) {
		red = "the plan itself, because this job is older than the test stage"
	}
	build := stageStanding(StageBuild, red)
	build.Doing = whereItStandsInTheBuild(one)
	return build
}

// whereItStandsInTheBuild is what a job in the last stage is actually doing.
//
// Four standings, and they are days apart. The moment the suite is red there is no plan, so a job
// that said it was building would be describing a state it is not in: the plan is written first, and
// a person approves it before any building starts. Once they have, the row itself does nothing and
// one session for each vertical does the work. Once every vertical is green, the job holds and the
// only thing that moves it is a person.
//
// It reads the columns rather than the stage, for the reason the stage itself is read off the row:
// what a reader is told has to be true of the job in front of them.
func whereItStandsInTheBuild(one *Job) string {
	switch {
	case Accepted(one):
		return "a person looked at every vertical of this job running and said the value arrived, " +
			"and what is left is the pull request the work is read in"
	case Built(one):
		return fmt.Sprintf("every vertical is built and its tests pass, and it waits for you to look at "+
			"%s and say whether the value arrived",
			theEvidenceFor(len(EvidenceIn(one.Build))))
	case one.PlanApproved:
		return "one session for each vertical is building against those tests, and none of them can " +
			"change a test"
	case one.Plan != "":
		return "it holds a plan nobody has approved yet, and nothing is built until somebody does"
	default:
		return "it writes the plan that turns those tests green next, and a person approves it before " +
			"any building starts"
	}
}

// stageStanding says what is true of the stage itself, so StageOf says only what is true of the job.
func stageStanding(stage, closedOn string) Stage {
	standing := Stage{Name: stage, Built: StageBuilt(stage)}
	for i, one := range Stages {
		if one != stage {
			continue
		}
		standing.Number = i + 1
		standing.Closed = "nothing came before it, ideation is the first stage"
		if i > 0 {
			standing.Closed = fmt.Sprintf("%s closed on %s", Stages[i-1], closedOn)
		}
		standing.Opens = "nothing, build is the last stage"
		if i+1 < len(Stages) {
			next := Stages[i+1]
			standing.Opens = fmt.Sprintf("nothing opens %s yet, it is a later slice", next)
			if opened := StageOpenedBy(next); opened != "" {
				standing.Opens = fmt.Sprintf("%s opens on %s", next, opened)
			}
		}
	}
	return standing
}

// Says is the stage in one word, for a listing, and a dash for a job that runs no stages. A dash
// rather than a blank, for the reason the outcome column already uses one: an empty cell reads as a
// column that failed to fill.
func (s Stage) Says() string {
	if s.Name == "" {
		return "-"
	}
	return s.Name
}

// Where is the stage with its place in the four, for the top of a reading.
func (s Stage) Where() string {
	if s.Name == "" {
		return "no stage"
	}
	return fmt.Sprintf("stage %d of %d: %s", s.Number, len(Stages), s.Name)
}
