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

// StageBuilt says whether a stage is built yet.
//
// Only ideation is. A reader of a job in design has to be told that, because a stage that is named
// and does nothing reads exactly like a stage that works, and the second reading is a lie the job
// itself tells.
func StageBuilt(stage string) bool { return stage == StageIdeation }

// StageOpenedBy is what moves a job into this stage, as a phrase a sentence is built round. It is
// empty where nothing moves a job there yet, which is every stage after design, and empty for
// ideation, which nothing opens because it is the first.
func StageOpenedBy(stage string) string {
	if stage == StageDesign {
		return "your answer to what it understood"
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
	// Unbuilt is what a reader of a job in a stage that is not built is told, and empty where the
	// stage works. It names what the job is doing instead, which is a fact about that job rather than
	// about the stage: a job that has just left ideation has no plan at all, and one further on is
	// working to a plan a person approved.
	Unbuilt string
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
	design := stageStanding(StageDesign, closedOn)
	design.Unbuilt = whatItDoesWhileDesignIsNotBuilt(one)
	return design
}

// whatItDoesWhileDesignIsNotBuilt is what a job in design is actually doing.
//
// The moment ideation closes there is no plan, and a job that said it was carrying on under a plan a
// person approved would be describing a state no job is in yet: the plan is written next, and a
// person approves it before any work starts. So this reads the two plan columns rather than the
// stage, for the reason the stage itself is read off the row: what a reader is told has to be true of
// the job in front of them.
func whatItDoesWhileDesignIsNotBuilt(one *Job) string {
	const notBuilt = "design is not built yet, so this job "
	switch {
	case one.PlanApproved:
		return notBuilt + "carries on under the plan a person approved"
	case one.Plan != "":
		return notBuilt + "holds a plan nobody has approved yet"
	default:
		return notBuilt + "writes its plan next, and a person approves it before any work starts"
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
