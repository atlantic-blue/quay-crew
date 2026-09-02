package main

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/controlplane"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/secrets"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// What an operator reads on the command line when the session had a lot to say.
//
// This is the surface the requirement names. A reading held to a paragraph reaches this page as
// nothing at all: the record is refused before it is written, the session is asked a second time,
// and the job stops. So the test drives the whole thing and reads the page.

// aLongReadingOnTheCommandLine is the 859 byte reading a session answers with here. It is the same
// reading internal/job holds, written again rather than shared, because a double that imported it
// would prove the two agree and not that either is right.
const aLongReadingOnTheCommandLine = "Understood: " + theLongParagraph + "\n" +
	"Not: a shorter reading\n" +
	"Told: the operator asked for the whole reading\n" +
	"Assumed: nothing else truncates it\n" +
	"Unknown: how long the longest reading gets\n" +
	"Confidence: sure of the shape\n" +
	"Question 1: which surface does a person read this on"

const theLongParagraph = "This job takes the ceiling off what a session may write when it says what " +
	"it understood. Today the reading is held to a paragraph, so a session that read the repository " +
	"and has a lot to say is refused, asked again, and then the job stops with nobody having read a " +
	"word of it. The length of a reading is not the system's to decide: a person asked for the " +
	"reading and a person reads it, so it goes on the row whole and it reaches the operator whole, " +
	"however long it is. This paragraph is longer than the ceiling the system holds today, which is " +
	"exactly what makes it the reading the operator never gets to read at all."

// theLastSentence is the end of that paragraph. A page that prints the opening and stops is a page
// that answers this test with half a reading.
const theLastSentence = "exactly what makes it the reading the operator never gets to read at all."

// aJobThatWroteALongReading is a system holding one job whose session answered with the 859 byte
// reading, standing wherever that leaves it.
func aJobThatWroteALongReading(t *testing.T) (quaycrewv1.ControlPlaneServiceClient,
	*controlplane.Server, string) {
	t.Helper()
	srv := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: aLongReadingOnTheCommandLine},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := testClientFor(t, srv)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "job", "create", "--title", "the transcript page",
		"--brief", "build what the design describes",
		"--product", "you paste a link and get the text back")

	held, err := client.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{})
	if err != nil || len(held.GetJobs()) != 1 {
		t.Fatalf("the system holds %d jobs (%v), want the one just declared", len(held.GetJobs()), err)
	}
	id := held.GetJobs()[0].GetId()
	// Asking or stopped, because stopped is what a job does with a long reading today and a test that
	// waited for asking alone would report a timeout rather than the fault.
	waitForPhase(t, srv, client, id, job.PhaseAsking, job.PhaseStopped)
	return client, srv, id
}

// The requirement, on the page a person reads it on: the whole 859 bytes, and a job that is in
// design rather than stopped.
func TestJobShowPrintsAWholeLongReadingAndTheJobIsInDesign(t *testing.T) {
	if len(aLongReadingOnTheCommandLine) != 859 {
		t.Fatalf("the reading under test is %d bytes, want the 859 the requirement names",
			len(aLongReadingOnTheCommandLine))
	}
	client, srv, id := aJobThatWroteALongReading(t)

	shown := mustRun(t, client, "job", "show", id)

	if !strings.Contains(shown, "what it understands") {
		t.Fatalf("krewe job show says nothing about what the job understood:\n%s", shown)
	}
	reading := theReadingOn(t, shown)
	if reading != aLongReadingOnTheCommandLine {
		t.Fatalf("the reading on the page is %d bytes and the session wrote %d:\n%s",
			len(reading), len(aLongReadingOnTheCommandLine), reading)
	}
	if !strings.Contains(reading, theLastSentence) {
		t.Fatalf("the page stops before the end of the reading:\n%s", reading)
	}
	found, err := client.GetJob(context.Background(), &quaycrewv1.GetJobRequest{Id: id})
	if err != nil {
		t.Fatalf("reading the job back: %v", err)
	}
	if phase := found.GetJob().GetPhase(); phase == job.PhaseStopped {
		t.Fatalf("the job is stopped over the length of its reading: %s", found.GetJob().GetReason())
	}

	// Then the person answers, and the job is in the next stage.
	mustRun(t, client, "job", "answer", id, "1: on the command line, the way every other listing is read")
	waitForPhase(t, srv, client, id, job.PhaseAsking)

	shown = mustRun(t, client, "job", "show", id)
	if !strings.Contains(shown, "stage 2 of 4: design") {
		t.Fatalf("krewe job show does not say the job is in design:\n%s", shown)
	}
	// And the reading is still whole on the page, because it is what the answer was written against.
	if answered := theReadingOn(t, shown); !strings.Contains(answered, theLastSentence) {
		t.Fatalf("krewe job show drops the end of the reading once it is answered:\n%s", answered)
	}
}

// theReadingOn is the block of the page that is the reading itself, which is the indented lines
// under the heading. The page carries what the session answered further down as well, so an
// assertion against the whole page passes on a reading nothing ever kept.
func theReadingOn(t *testing.T, shown string) string {
	t.Helper()
	lines := strings.Split(shown, "\n")
	for at, line := range lines {
		if !strings.HasPrefix(line, "what it underst") {
			continue
		}
		block := []string{}
		for _, under := range lines[at+1:] {
			if !strings.HasPrefix(under, "  ") {
				break
			}
			block = append(block, strings.TrimPrefix(under, "  "))
		}
		return strings.Join(block, "\n")
	}
	t.Fatalf("the page carries no reading at all:\n%s", shown)
	return ""
}
