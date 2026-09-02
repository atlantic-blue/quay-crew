package job_test

import (
	"reflect"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// An execution is one run of one stage of one job, and it is not a job.
//
// These are about the difference between the two, because that difference is what the change is: a
// run used to be a full job row, so it stood in every listing of declared work and twelve places had
// to ask whether a row was really a job.

// Everything a person writes belongs to the job. A run that carried a copy of any of it would be a
// second version of a fact the job already states, and a run that carried a gate would be a run
// somebody has to answer.
func TestARunHoldsNothingAPersonWrote(t *testing.T) {
	held := map[string]bool{}
	shape := reflect.TypeOf(job.Execution{})
	for at := 0; at < shape.NumField(); at++ {
		held[shape.Field(at).Name] = true
	}

	// The job's, every one of them. A title and a brief are a person's words; a sentence, a request,
	// a plan and a list are what the job serves and what somebody approved; the rest are the gates,
	// the ordering and the limits a caller declared.
	for _, name := range []string{
		"Title", "Brief", "Product", "Request", "Plan", "PlanApproved", "Ideation", "IdeationAnswer",
		"Design", "DesignAccepted", "Tests", "Build", "Accepted", "Steers", "Depth", "Requires",
		"After", "Deadline", "BudgetTokens", "Labels", "ExpectFile", "ExpectContains", "Role",
		"RoleVersion", "Escalation", "Handoffs", "Questions", "Steps", "Told", "Question",
		"Ungated", "Reviewed", "Tested", "Parent", "Attempted", "Resuming",
	} {
		if held[name] {
			t.Fatalf("a run carries %s, which belongs to the job it runs a stage of", name)
		}
	}

	// And it holds what a run needs, which is what the stage gathering it reads.
	for _, name := range []string{
		"ID", "Job", "Stage", "Number", "Claim", "Session", "Phase", "Attempts", "Outcome", "Reason",
		"Answer", "Branch", "PullRequest", "SpentTokens", "LeaseOwner", "LeaseUntil", "TraceID",
		"ParentSpanID", "CreatedAt", "UpdatedAt", "StartedAt", "FinishedAt",
	} {
		if !held[name] {
			t.Fatalf("a run does not carry %s, which a run of a stage needs", name)
		}
	}
}

// Nobody declares a run. What a caller writes is a declaration, and a declaration becomes a job:
// there is no field on it that names a stage or a number, so there is no road from what a person
// types to this table.
func TestARunCannotBeDeclaredByAPerson(t *testing.T) {
	written := reflect.TypeOf(job.Declaration{})
	for at := 0; at < written.NumField(); at++ {
		switch written.Field(at).Name {
		case "Stage", "Number", "Job", "Execution":
			t.Fatalf("a declaration carries %s, so a person could declare a run",
				written.Field(at).Name)
		}
	}

	// And a declaration that tries to name the row it hangs under is refused by name, which is the
	// refusal that already stands: the system assigns what a caller may not.
	if err := (job.Declaration{
		Workspace: "a", Project: "b", Title: "a run", Brief: "write the tests", Parent: "job-1",
	}).Validate(); err == nil {
		t.Fatal("a declaration naming the job it hangs under was accepted")
	}
}

// A run passes through no stage and answers no gate. The stages are read off a job, and a run cannot
// be handed to one of them: the four gates and the stage reading all take a job.
func TestARunHasNoStageAndNoGate(t *testing.T) {
	for _, gate := range []any{
		job.WaitingForItsIdeation, job.WaitingForItsDesign, job.WaitingForItsTests,
		job.WaitingForItsPlan, job.WaitingForItsBuild, job.WaitingForItsAcceptance, job.Planned,
		job.StageOf,
	} {
		takes := reflect.TypeOf(gate).In(0)
		if takes != reflect.TypeOf(&job.Job{}) {
			t.Fatalf("a stage gate takes %s, so something other than a job can be asked to pass it",
				takes)
		}
	}

	// The one thing a run says about a stage is which one it is a run of, and that is what puts a
	// build run under the boundary. It is not a stage the run has to get through.
	one := buildingJob()
	run := job.BuildExecutions(one, job.RequirementsOf(one)[:1])[0]
	if run.Stage != job.StageBuild {
		t.Fatalf("a run of the build stage says it is in stage %q", run.Stage)
	}
	if run.Job != one.ID {
		t.Fatalf("a run belongs to job %q, want %q", run.Job, one.ID)
	}
}

// A run is refused where it could never be gathered: with no job, in no stage, or for no number.
func TestARunTheStageCouldNotGatherIsRefused(t *testing.T) {
	sad := map[string]*job.Execution{
		"no identifier": {Job: "job-1", Stage: job.StageTest, Number: 1},
		"no job":        {ID: "run-1", Stage: job.StageTest, Number: 1},
		"no stage":      {ID: "run-1", Job: "job-1", Number: 1},
		"a stage nobody has built": {
			ID: "run-1", Job: "job-1", Stage: "release", Number: 1,
		},
		"no number": {ID: "run-1", Job: "job-1", Stage: job.StageTest},
	}
	for name, run := range sad {
		if err := run.Validate(); err == nil {
			t.Fatalf("a run with %s was accepted", name)
		}
	}
	if err := (&job.Execution{
		ID: "run-1", Job: "job-1", Stage: job.StageTest, Number: 1,
	}).Validate(); err != nil {
		t.Fatalf("a run of the test stage for requirement 1 was refused: %v", err)
	}
}
