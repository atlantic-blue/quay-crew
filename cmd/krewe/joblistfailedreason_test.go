package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// Why a failed job failed, read off krewe job list.
//
// The reason is already on the record and already on the wire: krewe job show prints it. A listing
// that says failed and nothing else sends a person into one job after another to learn which failure
// was the work and which was the machine. Nothing in this file opens a job, because opening one is the
// cost these tests exist to remove.

// runnerThatFails answers every task with an error, and picks the error by what the task asks for, so
// one system can hold two failures that read differently. An error is how a job reaches failed: the
// controller writes what the task died with onto the row.
type runnerThatFails struct{ because map[string]string }

func (r *runnerThatFails) Run(_ context.Context, _ sandbox.Sandbox, req model.Request) (model.Response, error) {
	for word, reason := range r.because {
		if strings.Contains(req.Text, word) {
			return model.Response{}, errors.New(reason)
		}
	}
	return model.Response{}, errors.New("the model refused this task")
}

// aSystemWhereWorkFails is a system with one project to stand in, whose every task dies.
func aSystemWhereWorkFails(t *testing.T, because map[string]string) (quaycrewv1.ControlPlaneServiceClient,
	*controlplane.Server) {
	t.Helper()
	srv := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &runnerThatFails{because: because},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := testClientFor(t, srv)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	return client, srv
}

// aJobThatFailed declares one job, runs it until the row says failed, and answers with the identifier
// the listing prints, so a test can find that job's own row among the others.
func aJobThatFailed(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, srv *controlplane.Server,
	title, brief string, more ...string) string {
	t.Helper()
	mustRun(t, client, append([]string{"job", "create", "--title", title, "--brief", brief}, more...)...)
	listed, err := client.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	for _, one := range listed.GetJobs() {
		if one.GetTitle() == title {
			waitForPhase(t, srv, client, one.GetId(), job.PhaseFailed)
			return display.ShortID(one.GetId())
		}
	}
	t.Fatalf("the system holds no job called %q", title)
	return ""
}

// rowOf is the one line of a listing that belongs to a job. A reason printed under the listing, or on
// a line of its own, is not on the row, and this is what says so.
func rowOf(t *testing.T, listing, id string) string {
	t.Helper()
	found := ""
	for _, line := range strings.Split(listing, "\n") {
		if strings.Contains(line, id) {
			if found != "" {
				t.Fatalf("job %s has more than one line in the listing:\n%s", id, listing)
			}
			found = line
		}
	}
	if found == "" {
		t.Fatalf("job %s has no row in the listing:\n%s", id, listing)
	}
	return found
}

// outcomeCellOnAFailedRow is what the outcome column says on a job that failed: it settled on no word,
// and the listing writes a dash where nothing was stated.
const outcomeCellOnAFailedRow = "-"

// The requirement in one reading: the listing says why the job failed.
func TestJobListSaysWhyAFailedJobFailed(t *testing.T) {
	client, srv := aSystemWhereWorkFails(t, map[string]string{"electricity": "no credential"})
	aJobThatFailed(t, client, srv, "read the electricity bill", "open the electricity bill and say when it is due")

	listing := mustRun(t, client, "job", "list")

	if !strings.Contains(listing, "no credential") {
		t.Fatalf("krewe job list says %q, want it to say why the job failed", listing)
	}
}

// On the row, because a note under a listing says one thing about a listing of many failures, and the
// reader of the fourth row cannot tell whether the note is theirs.
func TestTheFailureReasonIsOnTheFailedRowRatherThanUnderTheListing(t *testing.T) {
	client, srv := aSystemWhereWorkFails(t, map[string]string{"electricity": "no credential"})
	id := aJobThatFailed(t, client, srv, "read the electricity bill", "open the electricity bill and say when it is due")

	row := rowOf(t, mustRun(t, client, "job", "list"), id)

	if !strings.Contains(row, "no credential") {
		t.Fatalf("the row of the failed job reads %q, want the reason on it", row)
	}
}

// Beside the outcome, which is where a reader already looks when a row says failed. Nothing stands
// between the outcome cell and the reason, and the title stays behind both: a row that runs long can
// lose the end of a title and lose nothing else.
func TestTheFailureReasonSitsBesideTheOutcome(t *testing.T) {
	client, srv := aSystemWhereWorkFails(t, map[string]string{"electricity": "no credential"})
	id := aJobThatFailed(t, client, srv, "read the electricity bill",
		"open the electricity bill and say when it is due",
		"--product", "a person reads when the electricity bill is due")

	row := rowOf(t, mustRun(t, client, "job", "list"), id)

	reason := strings.Index(row, "no credential")
	if reason < 0 {
		t.Fatalf("the row reads %q, want the reason on it", row)
	}
	before, after := row[:reason], row[reason+len("no credential"):]
	if !strings.HasSuffix(strings.TrimRight(before, " "), outcomeCellOnAFailedRow) &&
		!strings.HasPrefix(strings.TrimLeft(after, " "), outcomeCellOnAFailedRow) {
		t.Fatalf("the row reads %q, want the reason beside the outcome", row)
	}
	if title := strings.Index(row, "read the electricity bill"); title < reason {
		t.Fatalf("the row reads %q, want the title after the reason", row)
	}
}

// Each row its own, because two jobs fail for two different reasons, and a listing carrying one of
// them tells a reader the wrong thing about the other.
func TestEachFailedRowCarriesItsOwnReason(t *testing.T) {
	client, srv := aSystemWhereWorkFails(t, map[string]string{
		"electricity": "no credential",
		"water":       "no sandbox at all",
	})
	electricity := aJobThatFailed(t, client, srv, "read the electricity bill",
		"open the electricity bill and say when it is due")
	water := aJobThatFailed(t, client, srv, "read the water bill",
		"open the water bill and say when it is due")

	listing := mustRun(t, client, "job", "list")

	first := rowOf(t, listing, electricity)
	if !strings.Contains(first, "no credential") {
		t.Errorf("the electricity row reads %q, want its own reason", first)
	}
	if strings.Contains(first, "no sandbox at all") {
		t.Errorf("the electricity row reads %q, and that is the other job's reason", first)
	}
	second := rowOf(t, listing, water)
	if !strings.Contains(second, "no sandbox at all") {
		t.Errorf("the water row reads %q, want its own reason", second)
	}
	if strings.Contains(second, "no credential") {
		t.Errorf("the water row reads %q, and that is the other job's reason", second)
	}
}

// A column rather than text appended to a row: two reasons of different lengths still leave the titles
// under one another. A listing whose last column starts in a different place on every line is one a
// reader has to read word by word.
func TestTheFailureReasonIsAColumnSoTheTitlesStillLineUp(t *testing.T) {
	client, srv := aSystemWhereWorkFails(t, map[string]string{
		"electricity": "no credential",
		"water":       "no sandbox at all",
	})
	electricity := aJobThatFailed(t, client, srv, "read the electricity bill",
		"open the electricity bill and say when it is due")
	water := aJobThatFailed(t, client, srv, "read the water bill",
		"open the water bill and say when it is due")

	listing := mustRun(t, client, "job", "list")

	short, long := rowOf(t, listing, electricity), rowOf(t, listing, water)
	// The reasons first. Two rows carrying no reason at all line up perfectly, and that is the state
	// this test has to fail in.
	if !strings.Contains(short, "no credential") ||
		!strings.Contains(long, "no sandbox at all") {
		t.Fatalf("the rows read %q and %q, want each reason on its row", short, long)
	}
	first, second := strings.Index(short, "read the electricity bill"), strings.Index(long, "read the water bill")
	if first != second {
		t.Fatalf("the titles start at %d and %d, want one column:\n%s", first, second, listing)
	}
}

// The listing that reads every project is the same listing, so it says the same thing. A person
// standing at the system reads the failures of every project in one screen, and a reason that only
// reaches the narrowed listing sends that person back into the jobs one at a time.
func TestTheListingOfEveryProjectAlsoSaysWhyAFailedJobFailed(t *testing.T) {
	client, srv := aSystemWhereWorkFails(t, map[string]string{"electricity": "no credential"})
	id := aJobThatFailed(t, client, srv, "read the electricity bill", "open the electricity bill and say when it is due")

	row := rowOf(t, mustRun(t, client, "job", "list", "system"), id)

	if !strings.Contains(row, "no credential") {
		t.Fatalf("the row of the failed job reads %q, want the reason on it", row)
	}
}
