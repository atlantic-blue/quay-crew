package main

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// Why a stopped job stopped, on the row of krewe job list.
//
// A person reading the listing sees rows that say "stopped" and nothing about what happened to any
// of them. The reason is on the job, so reading it costs one krewe job show for every stopped row,
// and the question the person came with, which of these needs me, is answered one command at a
// time. Nothing here opens a job, because opening one is the cost these tests exist to remove.
//
// The words each test stops a job with are short on purpose. A column has a width, this stage does
// not choose it, and a column too narrow to carry a few plain words says nothing a person can act
// on. How a long reason is drawn, and what happens to a line break in one, belong to requirement 3
// of https://github.com/atlantic-blue/quay-krewe/issues/675.

// theRowStartingWith is the line of a listing that one job is on.
//
// The row rather than the listing: a test that searched the whole output would pass on a reason
// printed under the rows, and on a reason printed against a different job.
func theRowStartingWith(t *testing.T, listing, id string) string {
	t.Helper()
	for _, line := range strings.Split(listing, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), id) {
			return line
		}
	}
	t.Fatalf("the listing holds no row for %s:\n%s", id, listing)
	return ""
}

// The requirement in one reading: two jobs stopped for two different things, and the listing says
// which is which without either being opened.
func TestJobListSaysWhyEachStoppedJobStopped(t *testing.T) {
	client := aSystemToJobIn(t)
	electricity := declaredHere(t, client, "read the electricity bill")
	water := declaredHere(t, client, "pay the water bill")
	mustRun(t, client, "job", "stop", electricity, "not due yet")
	mustRun(t, client, "job", "stop", water, "paid twice")

	listing := mustRun(t, client, "job", "list")

	if row := theRowStartingWith(t, listing, electricity); !strings.Contains(row, "not due yet") {
		t.Errorf("the row reads %q, want it to say why that job stopped", row)
	}
	if row := theRowStartingWith(t, listing, water); !strings.Contains(row, "paid twice") {
		t.Errorf("the row reads %q, want it to say why that job stopped", row)
	}
}

// A column, which means the same place on every row. Words put after a title start at a different
// place on every line, so a person reads the listing one row at a time instead of down the screen.
func TestTheStoppedReasonIsAColumnRatherThanWordsAfterTheTitle(t *testing.T) {
	client := aSystemToJobIn(t)
	electricity := declaredHere(t, client, "read the electricity bill")
	water := declaredHere(t, client, "pay the water bill")
	mustRun(t, client, "job", "stop", electricity, "not due yet")

	listing := mustRun(t, client, "job", "list")
	stopped := theRowStartingWith(t, listing, electricity)

	why := strings.Index(stopped, "not due yet")
	if why < 0 {
		t.Fatalf("the row reads %q, want it to say why the job stopped", stopped)
	}
	if why < strings.Index(stopped, "stopped") {
		t.Errorf("the row reads %q, want the reason beside the outcome rather than before the phase", stopped)
	}
	if why > strings.Index(stopped, "read the electricity bill") {
		t.Errorf("the row reads %q, want the reason in a column before the title, which has no width", stopped)
	}
	// The row of a job that has not stopped keeps its title in the same place, or the listing no
	// longer reads down the screen.
	pending := theRowStartingWith(t, listing, water)
	if strings.Index(stopped, "read the electricity bill") != strings.Index(pending, "pay the water bill") {
		t.Errorf("the titles start at different columns:\n%q\n%q", stopped, pending)
	}
}

// The listing that reads every project is the one a person runs when they do not know where the work
// is, and it is the listing with the most stopped rows on it.
func TestTheSystemWideListingSaysWhyAStoppedJobStopped(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "atlantic-blue")
	mustRun(t, client, "project", "create", "quay-krewe")
	id := declaredHere(t, client, "read the electricity bill")
	mustRun(t, client, "job", "stop", id, "not due yet")
	mustRun(t, client, "project", "create", "transcript")

	whole := mustRun(t, client, "job", "list", "system")

	if row := theRowStartingWith(t, whole, id); !strings.Contains(row, "not due yet") {
		t.Errorf("the row reads %q, want it to say why that job stopped", row)
	}
}

// The stopped jobs nobody stopped are the ones this is for. A session answered, the answer stated no
// outcome, the system ended the job, and the row says "stopped" and nothing else.
func TestJobListSaysWhyTheSystemStoppedAJob(t *testing.T) {
	client, srv := aSystemAnswering(t, "I read the bill and it is due on the 14th", true)
	id := declared(t, client, srv, "read the electricity bill", job.PhaseStopped)

	row := theRowStartingWith(t, mustRun(t, client, "job", "list"), display.ShortID(id))

	if !strings.Contains(row, "this job's answer") {
		t.Errorf("the row reads %q, want the opening of the reason the system wrote", row)
	}
}

// Nobody types a reason for most stops. The system writes its own words for those, and the row has
// to carry them too: a stopped row with an empty cell sends a person to krewe job show, which is the
// command this requirement removes.
func TestAStoppedJobNobodyGaveAReasonForStillSaysWhyOnTheRow(t *testing.T) {
	client := aSystemToJobIn(t)
	id := declaredHere(t, client, "read the electricity bill")
	mustRun(t, client, "job", "stop", id)

	row := theRowStartingWith(t, mustRun(t, client, "job", "list"), id)

	// What krewe job show gives for this job, read first, so the row is held to the words a person
	// would have opened the job to read rather than to a phrase this test invented.
	shown := mustRun(t, client, "job", "show", id)
	if !strings.Contains(shown, "stopped by the operator") {
		t.Fatalf("krewe job show says %q, want the words the system wrote when nobody typed any", shown)
	}
	if !strings.Contains(row, "stopped by the operator") {
		t.Errorf("the row reads %q, want the words krewe job show gives", row)
	}
}

// The listing a person runs when they want the stopped work. It is the same rows narrowed, so the
// reason is on each of them, and a person who narrowed to stopped jobs to find out what happened
// still opens nothing.
func TestTheListingNarrowedToStoppedJobsSaysWhyEachOneStopped(t *testing.T) {
	client := aSystemToJobIn(t)
	electricity := declaredHere(t, client, "read the electricity bill")
	water := declaredHere(t, client, "pay the water bill")
	mustRun(t, client, "job", "stop", electricity, "not due yet")
	mustRun(t, client, "job", "stop", water, "paid twice")

	narrowed := mustRun(t, client, "job", "list", "--phase", job.PhaseStopped)

	if row := theRowStartingWith(t, narrowed, electricity); !strings.Contains(row, "not due yet") {
		t.Errorf("the narrowed row reads %q, want it to say why that job stopped", row)
	}
	if row := theRowStartingWith(t, narrowed, water); !strings.Contains(row, "paid twice") {
		t.Errorf("the narrowed row reads %q, want it to say why that job stopped", row)
	}
}
