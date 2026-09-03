package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/capacity"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/headroom"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// A stopped row must say why it stopped. Most rows are not stopped rows. A column that is on every
// listing is a column of blanks on most of them, and a column of blanks is the thing a person stops
// reading by the second week. So the column comes with a stopped or a failed row, and it goes away
// again with them.
//
// A held pending row is the case that looks like a stopped row and is not one. The system writes a
// reason on it when the machine has no room. That reason belongs under the listing, where it is
// said once, because it is one fact about the machine and not one fact per row.
//
// Each test here also stops a job, because a column that is absent means nothing until the same
// listing can show it. The absent half is the requirement; the present half is what makes the
// absent half a measurement.

// theRowItAlwaysHad is a row of the listing as the command printed it before the reason column
// existed: the identifier, the phase, the stage, the outcome, then the title. The widths are
// captured from a run of `krewe job list`, and a row that gains a column no longer matches them.
func theRowItAlwaysHad(id, phase, stage, outcome, title string) string {
	return fmt.Sprintf("%-10s %-8s %-9s %-9s %s", display.ShortID(id), phase, stage, outcome, title)
}

// theWideRowItAlwaysHad is the same row as `krewe job list system` prints it, which carries the
// address of the project between the identifier and the phase.
func theWideRowItAlwaysHad(id, address, phase, stage, outcome, title string) string {
	return fmt.Sprintf("%-10s %-24s %-8s %-9s %-9s %s",
		display.ShortID(id), address, phase, stage, outcome, title)
}

// rowSaying is the line of a listing that ends in this title.
func rowSaying(t *testing.T, listing, title string) string {
	t.Helper()
	for _, line := range strings.Split(listing, "\n") {
		if strings.HasSuffix(line, title) {
			return line
		}
	}
	t.Fatalf("no row of the listing says %q:\n%s", title, listing)
	return ""
}

// jobCalled is the identifier of the job with this title.
func jobCalled(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, title string) string {
	t.Helper()
	listed, err := client.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	for _, one := range listed.GetJobs() {
		if one.GetTitle() == title {
			return one.GetId()
		}
	}
	t.Fatalf("the system holds no job called %q", title)
	return ""
}

// twoJobsThatDidNotStop is a project holding two pending jobs. Nothing stopped and nothing failed.
func twoJobsThatDidNotStop(t *testing.T) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "job", "create", "--title", "read the electricity bill",
		"--brief", "open it and say when it is due")
	mustRun(t, client, "job", "create", "--title", "read the water bill",
		"--brief", "open it and say when it is due")
	return client
}

// A listing where nothing stopped and nothing failed prints the row it always printed. No cell is
// added to it, and no run of spaces is added between the outcome and the title.
func TestNothingStoppedLeavesTheRowTheListingAlwaysHad(t *testing.T) {
	client := twoJobsThatDidNotStop(t)

	listing := mustRun(t, client, "job", "list")
	for _, title := range []string{"read the electricity bill", "read the water bill"} {
		id := jobCalled(t, client, title)
		want := theRowItAlwaysHad(id, "pending", "-", "-", title)
		if got := rowSaying(t, listing, title); got != want {
			t.Errorf("the listing prints\n%q\nand nothing stopped, so it should print\n%q", got, want)
		}
	}
	// A column that never appears is not a column that is held back. One stopped job puts the reason
	// on the listing, and that is what makes the rows above a measurement rather than a habit.
	stopped := jobCalled(t, client, "read the water bill")
	mustRun(t, client, "job", "stop", stopped, "the credential ran out")
	after := mustRun(t, client, "job", "list")
	if !strings.Contains(after, "the credential ran out") {
		t.Fatalf("a job stopped and the listing says\n%s\nwant the reason on the row", after)
	}
}

// The same rule on the listing that reads every project, where the row carries an address as well.
func TestNothingStoppedLeavesTheWideRowTheListingAlwaysHad(t *testing.T) {
	client := twoJobsThatDidNotStop(t)

	listing := mustRun(t, client, "job", "list", "system")
	for _, title := range []string{"read the electricity bill", "read the water bill"} {
		id := jobCalled(t, client, title)
		want := theWideRowItAlwaysHad(id, "me/house-bills", "pending", "-", "-", title)
		if got := rowSaying(t, listing, title); got != want {
			t.Errorf("the wide listing prints\n%q\nand nothing stopped, so it should print\n%q", got, want)
		}
	}
	stopped := jobCalled(t, client, "read the water bill")
	mustRun(t, client, "job", "stop", stopped, "the credential ran out")
	after := mustRun(t, client, "job", "list", "system")
	if !strings.Contains(after, "the credential ran out") {
		t.Fatalf("a job stopped and the wide listing says\n%s\nwant the reason on the row", after)
	}
}

// aMachineWithNoRoom reads as a machine whose memory the system's own containers already hold. A
// test cannot fill a real machine, so the reading is the one thing stood in for here.
type aMachineWithNoRoom struct{ mu sync.Mutex }

func (m *aMachineWithNoRoom) Sample(context.Context) (headroom.Sample, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return headroom.Sample{
		TakenAt: time.Now(),
		Limit:   headroom.Measured(7653 << 20), Processors: headroom.MeasuredShare(1400),
		Used: headroom.Measured(6500 << 20), Held: headroom.MeasuredShare(200),
	}, nil
}

// aJobHeldForRoom is a system that would not start the job it holds, so the job waits as pending
// with the machine's words on it. The listing says "held" for that row.
func aJobHeldForRoom(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, *controlplane.Server) {
	t.Helper()
	srv := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Headroom: &aMachineWithNoRoom{}, HeadroomEvery: time.Hour,
		SystemReserve: capacity.Request{Memory: 2048 << 20, Processor: 200},
	})
	srv.SampleHeadroom(context.Background())
	client := testClientFor(t, srv)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "job", "create", "--title", "read the electricity bill",
		"--brief", "open it and say when it is due")
	srv.TickJob(context.Background())

	held := mustRun(t, client, "job", "list")
	if !strings.Contains(held, "held: there is not enough memory") {
		t.Fatalf("the system did not hold the job back:\n%s", held)
	}
	return client, srv
}

// A held pending row carries a reason and it is not a stopped row. Nothing else stopped here, so
// the column is absent, and the machine's words stay on the line under the listing.
func TestAHeldRowAloneAddsNoColumnToTheListing(t *testing.T) {
	client, _ := aJobHeldForRoom(t)

	listing := mustRun(t, client, "job", "list")
	id := jobCalled(t, client, "read the electricity bill")
	want := theRowItAlwaysHad(id, "held", "-", "-", "read the electricity bill")
	if got := rowSaying(t, listing, "read the electricity bill"); got != want {
		t.Errorf("the listing prints\n%q\nand nothing stopped, so it should print\n%q", got, want)
	}
	if !strings.Contains(listing, "held: there is not enough memory") {
		t.Errorf("the listing says\n%s\nwant the machine's words under it", listing)
	}
	// Again the other half: the same listing shows a reason the moment something stops.
	mustRun(t, client, "job", "create", "--title", "read the water bill",
		"--brief", "open it and say when it is due")
	mustRun(t, client, "job", "stop", jobCalled(t, client, "read the water bill"), "the credential ran out")
	after := mustRun(t, client, "job", "list")
	if !strings.Contains(after, "the credential ran out") {
		t.Fatalf("a job stopped and the listing says\n%s\nwant the reason on the row", after)
	}
}

// With a stopped row beside it the column is there, and the held row's cell in it stays empty. The
// machine's words are one fact about the machine, said once under the listing, and a person who
// reads them on every held row stops reading them.
func TestAHeldRowKeepsAnEmptyCellAndItsWordsUnderTheListing(t *testing.T) {
	client, _ := aJobHeldForRoom(t)
	mustRun(t, client, "job", "create", "--title", "read the water bill",
		"--brief", "open it and say when it is due")
	mustRun(t, client, "job", "stop", jobCalled(t, client, "read the water bill"), "the credential ran out")

	listing := mustRun(t, client, "job", "list")
	if !strings.Contains(listing, "the credential ran out") {
		t.Fatalf("a job stopped and the listing says\n%s\nwant the reason on the row", listing)
	}
	held := rowSaying(t, listing, "read the electricity bill")
	if strings.Contains(held, "not enough memory") {
		t.Errorf("the held row says %q, want the machine's words under the listing and not in the column", held)
	}
	if !strings.Contains(listing, "held: there is not enough memory") {
		t.Errorf("the listing says\n%s\nwant the machine's words under it", listing)
	}
}
