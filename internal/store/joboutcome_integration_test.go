//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

// A job ends on one word, over the database that holds it.
//
// The unit tier proves the reader and the controller's decision against doubles, and the conformance
// suite proves the column survives a write and a read. What only the real engine can answer is the
// whole path: a session answers, the controller reads the word off that answer, the row keeps it, and
// a listing narrows by it in one indexed query.

// The refusal first. A gate that always passes satisfies every test about passing, and this is the
// gate: a session that states nothing has not finished the job.
func TestAJobWhoseAnswerStatesNoOutcomeDoesNotSettleInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &model.FakeRunner{
		Reply: "I read the bill and it is due on the 14th", Exact: true,
	})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	stopped := waitForJob(t, s, declared.GetJob().GetId(), job.PhaseStopped)

	if stopped.GetOutcome() != "" {
		t.Fatalf("the row says the job ended on %q, and its answer stated nothing", stopped.GetOutcome())
	}
	if !strings.Contains(stopped.GetReason(), job.OutcomeMarker) {
		t.Fatalf("the row says %q, want it to say what line was missing", stopped.GetReason())
	}
	// The end of an attempt is not the end of what it produced, so what the session said survives.
	if stopped.GetAnswer() != "I read the bill and it is due on the 14th" {
		t.Fatalf("the answer on the row is %q", stopped.GetAnswer())
	}
}

// The word comes off the answer and onto the row, and a listing narrows by it. Two jobs are done and
// one of them could not do its work, which is the reading the phase cannot make.
func TestAListingNarrowsByOutcomeInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &outcomePerBrief{})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	blocked, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the water bill", Brief: "open the water bill",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	done := waitForJob(t, s, blocked.GetJob().GetId(), job.PhaseDone)
	if done.GetOutcome() != job.OutcomeBlocked {
		t.Fatalf("the row says the job ended on %q, want %q", done.GetOutcome(), job.OutcomeBlocked)
	}

	// A second job, answered the ordinary way, so the listing has two done rows to tell apart.
	proved, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open the electricity bill",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if ended := waitForJob(t, s, proved.GetJob().GetId(), job.PhaseDone); ended.GetOutcome() != job.OutcomeProved {
		t.Fatalf("the second job ended on %q, want %q", ended.GetOutcome(), job.OutcomeProved)
	}

	listed, err := s.ListJobs(ctx, &quaycrewv1.ListJobsRequest{
		Project: project, Outcome: job.OutcomeBlocked,
	})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(listed.GetJobs()) != 1 || listed.GetJobs()[0].GetId() != blocked.GetJob().GetId() {
		t.Fatalf("the jobs that ended blocked are %d rows, want the one", len(listed.GetJobs()))
	}
	both, err := s.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Project: project, Phase: job.PhaseDone})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(both.GetJobs()) != 2 {
		t.Fatalf("the done jobs are %d rows, want both: the phase cannot tell them apart",
			len(both.GetJobs()))
	}
}

// A word nothing hands out is refused rather than answered with an empty listing, because an empty
// listing reads exactly like a database holding no such jobs.
func TestAListingAskedForAWordThatIsNotAnOutcomeIsRefusedInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &model.FakeRunner{Reply: "ok"})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	_, err := s.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Project: project, Outcome: "complete"})
	if err == nil {
		t.Fatal("a listing asked for a word nothing ends on was answered")
	}
	for _, word := range job.Outcomes() {
		if !strings.Contains(err.Error(), word) {
			t.Fatalf("the refusal says %q, want it to offer %q", err, word)
		}
	}
}

// outcomePerBrief is a model that ends on a different word depending on what it was asked, so one
// system can produce the two done rows this listing has to tell apart.
type outcomePerBrief struct{}

func (outcomePerBrief) Run(_ context.Context, _ sandbox.Sandbox, req model.Request) (model.Response, error) {
	outcome := job.OutcomeProved
	if strings.Contains(req.Text, "water") {
		outcome = job.OutcomeBlocked
	}
	return model.Response{
		Reply:          "it is done\n\n" + job.OutcomeMarker + " " + outcome,
		ModelSessionID: req.ModelSessionID,
	}, nil
}

// statingTheOutcome ends an answer the way a session that read its task ends one.
//
// Every job tells its session to state an outcome on a line of its own, and a job whose answer states
// none does not settle. A runner double that ignored the instruction would be looser than the thing
// it stands in for, so every test about a step would quietly become a test about that. A reply that
// already states one is left alone, and a test about a session that states nothing says so with
// model.FakeRunner's Exact.
func statingTheOutcome(reply, asked string) string {
	// Read rather than searched, because a double that echoes its task echoes the instruction too.
	if !strings.Contains(asked, job.OutcomeMarker) || job.OutcomeIn(reply) != "" {
		return reply
	}
	return reply + "\n\n" + job.OutcomeMarker + " " + job.OutcomeProved
}

// The word decide does not settle the job, it stops it with a person, and the row that says so is
// the row every reader of what waits on you already reads.
//
// Over the real engine because that is where the incident was: four rows read running while four
// conversations held a question, and a listing narrowing on the phase is the one query that has to
// find them.
func TestASessionThatStopsForAPersonLeavesAnAskingRowInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &model.FakeRunner{
		Reply: "One store bills nothing at rest and the other bills a minimum capacity. Which?\n\n" +
			job.OutcomeMarker + " " + job.OutcomeDecide,
		Exact: true,
	})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "choose where the transcripts are kept",
		Brief: "read what the project says about cost and pick the store",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	asking := waitForJob(t, s, declared.GetJob().GetId(), job.PhaseAsking)

	if !strings.Contains(asking.GetQuestion(), "Which?") {
		t.Fatalf("the row carries the question %q", asking.GetQuestion())
	}
	if strings.Contains(asking.GetQuestion(), job.OutcomeMarker) {
		t.Fatalf("the question a person reads carries the system's own line: %q", asking.GetQuestion())
	}
	// The one query. A person who does not know which job needs them reads this and nothing else.
	waiting, err := s.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Phase: job.PhaseAsking})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(waiting.GetJobs()) != 1 || waiting.GetJobs()[0].GetId() != declared.GetJob().GetId() {
		t.Fatalf("%d rows read as waiting on a person, want the one that stopped", len(waiting.GetJobs()))
	}
	// And the answer goes back, which is what makes the phase worth reaching: an asking row nothing
	// can answer would be a job stopped in a state with no way out of it.
	answered, err := s.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{
		Id: declared.GetJob().GetId(), Answer: "the key value store, on demand",
	})
	if err != nil {
		t.Fatalf("AnswerJob: %v", err)
	}
	if answered.GetJob().GetPhase() != job.PhasePending {
		t.Fatalf("an answered job is %q, want pending", answered.GetJob().GetPhase())
	}
}

// The quiet case over the real engine. A job that finished its work leaves no row for a person to
// answer, so a listing of what waits on you is empty.
func TestAJobThatFinishedItsWorkLeavesNothingWaitingInPostgres(t *testing.T) {
	s, _ := aSystemWithAController(t, &model.FakeRunner{
		Reply: "The bill is due on the 14th.\n\n" + job.OutcomeMarker + " " + job.OutcomeProved,
		Exact: true,
	})
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it and say when it is due",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	waitForJob(t, s, declared.GetJob().GetId(), job.PhaseDone)

	waiting, err := s.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Phase: job.PhaseAsking})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(waiting.GetJobs()) != 0 {
		t.Fatalf("%d rows read as waiting on a person, and the work finished", len(waiting.GetJobs()))
	}
}
