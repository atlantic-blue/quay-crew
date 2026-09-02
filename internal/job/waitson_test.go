package job_test

import (
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/forge"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Two readings of one question, one off a row and one off the record a caller holds, so they are
// held to the same word for the same job. A surface that decided the kind of wait for itself is how
// two surfaces come to disagree about one job, which is the failure the telling exists to end.
func TestBothReadingsOfAWaitAgree(t *testing.T) {
	for _, one := range []struct {
		name string
		row  *job.Job
	}{
		{"a job that is asking", &job.Job{Phase: job.PhaseAsking, Question: "aurora or a key value store?"}},
		{"a job that failed", &job.Job{Phase: job.PhaseFailed, Reason: "the container went away"}},
		{"a job that was stopped", &job.Job{Phase: job.PhaseStopped, Reason: "the operator stopped it"}},
		{"a job going again", &job.Job{Phase: job.PhaseFailed, Reason: "it broke", Resuming: "it broke"}},
		{"a job held for want of room", &job.Job{Phase: job.PhasePending, Reason: "there is not enough memory"}},
		{"a job waiting its turn", &job.Job{Phase: job.PhasePending}},
		{"a job that is running", &job.Job{Phase: job.PhaseRunning}},
		{"a job whose board is red", &job.Job{
			Phase:            job.PhaseDone,
			PullRequestState: forge.Reading{Checks: forge.ChecksRed, FailedCheck: "integration"},
		}},
		{"a job whose board is green", &job.Job{
			Phase:            job.PhaseDone,
			PullRequestState: forge.Reading{Checks: forge.ChecksGreen},
		}},
	} {
		t.Run(one.name, func(t *testing.T) {
			why, want, waiting := job.Waits(one.row)
			readWhy, readWant, readWaiting := job.WaitsOn(asRecord(one.row))
			if why != readWhy || want != readWant || waiting != readWaiting {
				t.Fatalf("the row reads %q/%q/%v and the record reads %q/%q/%v",
					why, want, waiting, readWhy, readWant, readWaiting)
			}
		})
	}
}

// The gap belongs to the wait a person is in now. A job that asked, was answered, ran on and then
// failed carries the moment of that first question, and dating the later wait from it reports the
// answer and the whole run as time somebody spent not knowing.
func TestTheGapIsMeasuredFromTheWaitAPersonIsIn(t *testing.T) {
	asked := time.Now().Add(-3 * time.Hour)
	stopped := time.Now().Add(-10 * time.Minute)

	began, known := job.WaitBegan(job.WaitingBlocked, &asked, stopped)
	if !known {
		t.Fatalf("a job that stopped records no start for its wait")
	}
	if !began.Equal(stopped) {
		t.Fatalf("a blocked wait begins at %s, want the moment it stopped, %s", began, stopped)
	}

	began, known = job.WaitBegan(job.WaitingAsking, &asked, stopped)
	if !known || !began.Equal(asked) {
		t.Fatalf("a question begins at %s, want the moment it was asked, %s", began, asked)
	}

	// Nothing writes the moment a board turned red, so this answers with no moment rather than the
	// nearest one lying on the row.
	if _, known := job.WaitBegan(job.WaitingChecks, &asked, stopped); known {
		t.Fatalf("a red board claims to record when its wait began")
	}
}

// asRecord is the same job as a caller reads it back, which is the shape the command line holds.
func asRecord(row *job.Job) *quaycrewv1.Job {
	one := &quaycrewv1.Job{
		Phase: row.Phase, Question: row.Question, Reason: row.Reason, Resuming: row.Resuming,
		PullRequestChecks: row.PullRequestState.Checks,
		PullRequestCheck:  row.PullRequestState.FailedCheck,
	}
	if row.AskedAt != nil {
		one.AskedAt = timestamppb.New(*row.AskedAt)
	}
	return one
}
