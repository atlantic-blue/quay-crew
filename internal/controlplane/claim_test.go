package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A job claims the piece of work it is doing, and the second job that claims the same piece of work
// is refused while the first still holds it.
//
// What the refusal says is the whole product of the rule. A caller told "that is claimed" goes
// looking for who has it. A caller told which job has it, and how old that claim is, opens that job
// and takes something else.

func TestASecondJobClaimingWorkAnotherJobHoldsIsRefused(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	first, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "build the claim", Brief: "build it",
		Claim: "atlantic-blue/quay-krewe#540",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	refusal := refusalOf(t, s, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "build the claim as well", Brief: "build it",
		Claim: "atlantic-blue/quay-krewe#540",
	})
	if status.Code(refusal) != codes.FailedPrecondition {
		t.Errorf("the refusal is %s, want %s", status.Code(refusal), codes.FailedPrecondition)
	}
	for _, want := range []string{
		"atlantic-blue/quay-krewe#540", first.GetJob().GetId(), "build the claim", "less than a minute",
		"krewe job show",
	} {
		if !strings.Contains(refusal.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, refusal)
		}
	}
	// Behind the refusal. A refusal that says no and writes the row anyway is two jobs on one piece
	// of work with a sentence over it.
	listed, err := s.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{Project: project})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(listed.GetJobs()) != 1 {
		t.Fatalf("the system holds %d jobs, want only the one that claimed the work", len(listed.GetJobs()))
	}
}

// The same piece of work written two ways is one claim. Two people typing it from memory is the
// ordinary case, not the edge one.
func TestAClaimWrittenAnotherWayIsTheSameClaim(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)

	if _, err := s.CreateJob(context.Background(), &quaycrewv1.CreateJobRequest{
		Project: project, Title: "build the claim", Brief: "build it",
		Claim: "atlantic-blue/quay-krewe#540",
	}); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	refusal := refusalOf(t, s, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "build the claim as well", Brief: "build it",
		Claim: "  Atlantic-Blue/Quay-Krewe#540  ",
	})
	if !strings.Contains(refusal.Error(), "atlantic-blue/quay-krewe#540") {
		t.Errorf("the refusal is %v, and the two spellings should be one claim", refusal)
	}
}

// A claim ends when its job does, so the work is there for whoever picks it up next.
func TestWorkAStoppedJobClaimedIsClaimedAgain(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	ctx := context.Background()

	first, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "build the claim", Brief: "build it",
		Claim: "atlantic-blue/quay-krewe#540",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if _, err := s.StopJob(ctx, &quaycrewv1.StopJobRequest{
		Id: first.GetJob().GetId(), Reason: "the issue was closed",
	}); err != nil {
		t.Fatalf("StopJob: %v", err)
	}

	second, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "build the claim", Brief: "build it",
		Claim: "atlantic-blue/quay-krewe#540",
	})
	if err != nil {
		t.Fatalf("work a stopped job claimed is still held by it: %v", err)
	}
	if second.GetJob().GetClaim() != "atlantic-blue/quay-krewe#540" {
		t.Fatalf("the second job claims %q", second.GetJob().GetClaim())
	}
}

func TestAJobReadsBackWhatItClaims(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	ctx := context.Background()

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "build the claim", Brief: "build it",
		Claim: "atlantic-blue/quay-krewe#540",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	read, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.GetJob().GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if read.GetJob().GetClaim() != "atlantic-blue/quay-krewe#540" {
		t.Fatalf("the job claims %q", read.GetJob().GetClaim())
	}
}

// A claim longer than a title is taken at the write, and it reads back word for word.
//
// The write refused it, so an operator who named a piece of work in a long sentence lost the job as
// well as the claim. The number is a guide now, and the surfaces that draw a claim in a column cut
// it there.
func TestAClaimLongerThanATitleIsKeptAtTheWrite(t *testing.T) {
	s := newServer(&model.FakeRunner{})
	_, project := newProject(t, s)
	ctx := context.Background()
	claim := "atlantic-blue/quay-krewe#540 " + strings.Repeat("and the piece of work beside it, ", 20)

	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "build the claim", Brief: "build it", Claim: claim,
	})
	if err != nil {
		t.Fatalf("a claim of %d bytes was refused at the write: %v", len(claim), err)
	}
	read, err := s.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: declared.GetJob().GetId()})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got := read.GetJob().GetClaim(); got != job.TidyClaim(claim) {
		t.Fatalf("the claim reads back as %d bytes and it was written as %d: %q",
			len(got), len(job.TidyClaim(claim)), got)
	}
}
