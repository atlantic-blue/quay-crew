//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/forge"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// The whole path over a real database: a job carrying a pull request address, the reading that a
// forge answers with, and what GetJob then says about it.
//
// The unit tier proves the reader and the watcher, and the conformance suite proves both stores keep
// the columns. Neither reaches this: the reading has to go through six columns of a real table, a
// partial index whose predicate decides what is read again, and the call a person actually makes.
// That is the shape a new column gets wrong, because a name left out of a select comes back empty
// rather than failing.

const theAddress = "https://github.com/atlantic-blue/quay-crew/pull/549"

// aSystemReadingAForge is the control plane on a real database, with a forge a test writes the
// answers of.
func aSystemReadingAForge(t *testing.T, reading *forge.Fake) (*controlplane.Server, store.Store) {
	t.Helper()
	truncate(t)
	kept, err := store.NewPostgres(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(kept.Close)
	return controlplane.NewServer(controlplane.Config{
		Store: kept, Runner: &model.FakeRunner{}, Provider: &sandbox.FakeProvider{},
		Secrets: secrets.NewMemory(), Forge: reading,
	}), kept
}

// aFinishedJobWithAPullRequest declares a job, lands it done, and puts the address on the row the way
// the controller does when a session answers with one.
func aFinishedJobWithAPullRequest(t *testing.T, s *controlplane.Server, kept store.Store, address string) string {
	t.Helper()
	ctx := context.Background()
	_, project := aProjectOnPostgres(t, s)
	declared, err := s.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
		Project: project, Title: "read the electricity bill", Brief: "open it and say when it is due",
		Repository: "atlantic-blue/quay-crew",
	})
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	id := declared.GetJob().GetId()
	if _, err := kept.StartJob(ctx, id, job.Lease{Owner: "a controller"}, nil); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if _, err := kept.LandJob(ctx, id, job.Landing{
		Phase: job.PhaseDone, Answer: "done, opened " + address, PullRequest: address,
	}, &job.Event{
		ID: store.NewID(), Kind: job.EventAnswered, Job: id, Detail: "answered",
	}); err != nil {
		t.Fatalf("LandJob: %v", err)
	}
	return id
}

func stateOf(t *testing.T, s *controlplane.Server, id string) *quaycrewv1.Job {
	t.Helper()
	found, err := s.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: id})
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	return found.GetJob()
}

// The refusal first. A pull request the crew could not read must say unknown through the whole path,
// database included, because a row of empty columns and a row that says unknown look the same to a
// store and different to whoever reads the answer.
func TestAPullRequestTheCrewCouldNotReadSaysUnknownThroughPostgres(t *testing.T) {
	refusing := forge.NewFake().Refuses(theAddress, "the rate limit is spent")
	s, kept := aSystemReadingAForge(t, refusing)
	id := aFinishedJobWithAPullRequest(t, s, kept, theAddress)

	// Nothing has read it yet, and that must not read as passing either.
	before := stateOf(t, s, id)
	if before.GetPullRequestStatus() != forge.StatusUnknown ||
		before.GetPullRequestChecks() != forge.ChecksUnknown {
		t.Fatalf("before any reading the job says %q and %q",
			before.GetPullRequestStatus(), before.GetPullRequestChecks())
	}
	if before.GetPullRequestReadAt() != nil {
		t.Fatalf("a pull request nothing read carries a moment: %v", before.GetPullRequestReadAt())
	}

	s.ReadPullRequests(context.Background())

	after := stateOf(t, s, id)
	if after.GetPullRequestStatus() != forge.StatusUnknown ||
		after.GetPullRequestChecks() != forge.ChecksUnknown {
		t.Fatalf("a refused reading says %q and %q",
			after.GetPullRequestStatus(), after.GetPullRequestChecks())
	}
	if !strings.Contains(after.GetPullRequestFailed(), "rate limit") {
		t.Fatalf("the job says %q about why it could not be read", after.GetPullRequestFailed())
	}
	if after.GetPullRequestReadAt() == nil {
		t.Fatal("the attempt left no moment on the row, so nothing can tell a stale reading from a fresh one")
	}
}

// A check that failed reaches the answer, with the name of the check, through the real table.
func TestARedCheckReachesTheAnswerWithItsName(t *testing.T) {
	answering := forge.NewFake().Says(theAddress, forge.Reading{
		Status: forge.StatusOpen, Checks: forge.ChecksRed, FailedCheck: "integration",
		Review: forge.ReviewChangesRequested,
	})
	s, kept := aSystemReadingAForge(t, answering)
	id := aFinishedJobWithAPullRequest(t, s, kept, theAddress)

	s.ReadPullRequests(context.Background())

	one := stateOf(t, s, id)
	if one.GetPullRequestStatus() != forge.StatusOpen || one.GetPullRequestChecks() != forge.ChecksRed {
		t.Fatalf("the job says %q and %q", one.GetPullRequestStatus(), one.GetPullRequestChecks())
	}
	if one.GetPullRequestCheck() != "integration" {
		t.Fatalf("the failed check is %q, so nobody knows what to open", one.GetPullRequestCheck())
	}
	if one.GetPullRequestReview() != forge.ReviewChangesRequested {
		t.Fatalf("the review says %q", one.GetPullRequestReview())
	}
	if one.GetPullRequestFailed() != "" {
		t.Fatalf("a reading that happened says it failed: %q", one.GetPullRequestFailed())
	}
}

// A merged pull request says merged, and is then left alone. The partial index decides this, so it is
// only true against the real database.
func TestAMergedPullRequestSaysMergedAndIsNotReadAgain(t *testing.T) {
	answering := forge.NewFake().Says(theAddress, forge.Reading{
		Status: forge.StatusMerged, Checks: forge.ChecksGreen, Review: forge.ReviewApproved,
	})
	s, kept := aSystemReadingAForge(t, answering)
	id := aFinishedJobWithAPullRequest(t, s, kept, theAddress)

	ctx := context.Background()
	s.ReadPullRequests(ctx)
	if one := stateOf(t, s, id); one.GetPullRequestStatus() != forge.StatusMerged {
		t.Fatalf("a merged pull request says %q", one.GetPullRequestStatus())
	}
	if asked := answering.Asked(theAddress); asked != 1 {
		t.Fatalf("the first reading asked %d times", asked)
	}

	s.ReadPullRequests(ctx)
	s.ReadPullRequests(ctx)
	if asked := answering.Asked(theAddress); asked != 1 {
		t.Fatalf("a settled pull request was read %d times, so the crew pays for it for ever", asked)
	}
}
