package job

import (
	"fmt"
	"time"
)

// An execution is one run of one stage of one job, and it is not a job.
//
// The fault it answers: a stage that fans out used to declare a full job row for each requirement.
// Those rows carried a title, a brief, a sentence, a request, a plan, gates and an acceptance, none
// of which a run of a stage has, and they stood in the jobs listing beside the work somebody asked
// for. Twelve places then asked whether a row had a parent to decide whether it was really a job.
//
// The difference is who declares it. A person declares a job: it states one sentence, it passes
// through four stages, and a person answers its gates. Nobody declares an execution. The system
// makes one because a stage of a job fans out, it has no sentence of its own, it runs no stages, and
// it has no gates. It holds what a run needs and nothing a person wrote.
//
// What a run needs of the job it belongs to is read off that job at the moment of the dispatch: the
// project, the mode, the repository, the sentence and the words the session is asked. A second copy
// of any of those on this row could only disagree with the job.

// Execution is one run of one stage of one job.
type Execution struct {
	// ID is this run's own identifier, and Job is the job it belongs to. Every execution belongs to
	// exactly one job and to exactly one stage of that job.
	ID  string
	Job string
	// Stage is which stage of that job this run is for, from the four in stage.go. Two of them fan
	// out today: the test stage and the build stage.
	Stage string
	// Number is the requirement or the vertical this run is for, counting from one. It is the number
	// the stage gathers its reports under.
	Number int
	// Claim is the piece of work this run holds, which is the requirement or the vertical. A second
	// live execution claiming it is refused, so a stage ticked twice runs one session and not two.
	Claim string

	// What the controller writes, and nobody else.
	Phase    string
	Session  string
	Attempts int
	// Answer is what the session said, whole. The stage reads its report out of it.
	Answer string
	// Outcome is the one word the answer stated, and Reason is what stopped a run that did not
	// answer.
	Outcome string
	Reason  string
	// Branch is where this run's commits ended up, written when the system puts them there, and
	// empty for a run whose job names no repository. PullRequest is the address the answer named.
	Branch      string
	PullRequest string
	// SpentTokens is what this run's session cost.
	SpentTokens int64

	// LeaseOwner is the controller holding this run, and LeaseUntil is when its hold runs out.
	LeaseOwner string
	LeaseUntil *time.Time

	// TraceID and ParentSpanID are the job's, inherited unchanged, so one trace covers the job and
	// every run of every stage under it.
	TraceID      string
	ParentSpanID string

	CreatedAt  time.Time
	UpdatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// ExecutionFilter narrows a listing of executions. One of Job and Project is required: an execution
// is only ever read as one of the runs of one stage of one job, which is the whole reason it is not
// a job, and a listing of one project's jobs reads every run in that project to draw each job's runs
// beneath it.
type ExecutionFilter struct {
	Job string
	// Stage narrows to one stage of that job, and empty is every stage.
	Stage string
	// Project is every run of every job in one project. Job wins where both are set, being the
	// narrower.
	Project string
}

// Live says whether this run is still going.
func (e *Execution) Live() bool { return e != nil && !Terminal(e.Phase) }

// Holding says whether this run still holds the piece of work it claims, as of now. The same three
// endings a job's claim has, for the same reasons: see claim.go.
func (e *Execution) Holding(now time.Time) bool {
	if e == nil || e.Claim == "" || Terminal(e.Phase) {
		return false
	}
	return now.Sub(e.UpdatedAt) < ClaimLife
}

// Validate refuses an execution the system could not run.
//
// It is not Declaration.Validate. That one refuses what a person wrote, with a sentence saying what
// to type instead. Nobody writes this one, so every refusal here is the system having built
// something it cannot run, and what it protects is the store from a row no stage could gather.
func (e *Execution) Validate() error {
	switch {
	case e == nil:
		return fmt.Errorf("execution: nothing to write")
	case e.ID == "":
		return fmt.Errorf("execution: no identifier")
	case e.Job == "":
		return fmt.Errorf("execution: no job, and an execution belongs to exactly one job")
	case !StageBuilt(e.Stage):
		return fmt.Errorf("execution: %q is not a stage, and an execution runs one stage of one job",
			e.Stage)
	case e.Number < 1:
		return fmt.Errorf("execution: number %d, and an execution runs one requirement or one "+
			"vertical, counted from one", e.Number)
	}
	return nil
}

// LiveExecution says whether any of these runs is still going.
func LiveExecution(runs []*Execution) bool {
	for _, run := range runs {
		if run.Live() {
			return true
		}
	}
	return false
}

// ExecutionsByNumber groups the runs of one stage under the requirement or vertical each one is for,
// oldest first.
//
// A list rather than one run, because a number whose first run died can have a second, and how many
// it has is the bound on how many more it gets.
func ExecutionsByNumber(runs []*Execution) map[int][]*Execution {
	held := map[int][]*Execution{}
	for _, run := range runs {
		held[run.Number] = append(held[run.Number], run)
	}
	return held
}

// SessionForExecution is the conversation one run happens in.
//
// Named after the run, the way a job's is named after the job, so a dispatch made twice lands where
// the run has been all along rather than starting a second conversation.
func SessionForExecution(run *Execution) string {
	if run == nil {
		return ""
	}
	return "run-" + run.ID
}

// ExecutionLanding is what came of one run, written in one movement with the record of it.
//
// It is Landing without the halves a run does not have. There is no gate to have passed, because an
// execution is never gated: what checks it is the stage that gathers it. There is no attempt to
// record, because nothing escalates a run: a number whose run died gets one more run and then a
// person is asked.
type ExecutionLanding struct {
	Phase       string
	Answer      string
	Outcome     string
	Reason      string
	SpentTokens int64
	PullRequest string
}

// RunCalled is what to call one run of a stage where there is no title to print, which is every
// surface that lists runs: nobody declared a run, so nobody wrote it a title.
//
// The words are the stage's own, so a run reads the way the stage that made it reads. A stage this
// does not know says its own name and the number, which is a run named honestly rather than a row
// with an empty cell.
func RunCalled(stage string, number int) string {
	switch stage {
	case StageTest:
		return fmt.Sprintf("tests for requirement %d", number)
	case StageBuild:
		return fmt.Sprintf("build vertical %d", number)
	default:
		return fmt.Sprintf("%s %d", stage, number)
	}
}
