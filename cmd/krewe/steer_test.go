package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
)

// A steer is the score of a job, marked in one command while it is happening. What is proved here is
// that the mark takes one word, that the report reads it back with the time and the job it landed
// on, and that a system with no job in flight says which job to name rather than guessing.

func TestASteerIsMarkedInOneCommandAgainstTheJobInFlight(t *testing.T) {
	client := aSystemToJobIn(t)
	id := declaredHere(t, client, "build the transcripts page")

	said := mustRun(t, client, "steer", "the workspace has no secrets")

	if !strings.Contains(said, "1 steer") {
		t.Fatalf("krewe steer says %q, want the count so far", said)
	}
	if !strings.Contains(said, "build the transcripts page") {
		t.Fatalf("krewe steer says %q, want the job it landed on", said)
	}
	shown := mustRun(t, client, "job", "show", id)
	if !strings.Contains(shown, "1 steer") {
		t.Fatalf("krewe job show says %q, want the count on the job", shown)
	}
}

// The report is the half that makes the count worth having: what was said, when, and on which job.
func TestTheReportReadsEverySteerBackInOrder(t *testing.T) {
	client := aSystemToJobIn(t)
	id := declaredHere(t, client, "build the transcripts page")
	mustRun(t, client, "steer", "the workspace has no secrets")
	mustRun(t, client, "steer", "it chose a store that bills while idle")

	report := mustRun(t, client, "steers", id)

	for _, want := range []string{
		"the workspace has no secrets",
		"it chose a store that bills while idle",
		"2 steers",
		"build the transcripts page",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("the report does not say %q: %q", want, report)
		}
	}
	if first, second := strings.Index(report, "no secrets"), strings.Index(report, "bills while idle"); first > second {
		t.Errorf("the report is out of order: %q", report)
	}
	// The definition ships with the tool, because a count whose definition drifted compares with
	// nothing.
	if !strings.Contains(report, "should have known") {
		t.Errorf("the report does not say what a steer is: %q", report)
	}
}

// The question the number exists to answer is whether this job took fewer than the one before it.
func TestTheListingComparesOneJobWithTheJobBeforeIt(t *testing.T) {
	client := aSystemToJobIn(t)
	before := declaredHere(t, client, "the job before")
	for _, said := range []string{
		"the workspace has no secrets",
		"it chose a store that bills while idle",
		"it never opened a pull request",
	} {
		mustRun(t, client, "steer", before, said)
	}
	after := declaredHere(t, client, "the job after")
	mustRun(t, client, "steer", after, "the workspace has no secrets")

	listed := mustRun(t, client, "steers")

	if !strings.Contains(listed, "2 fewer than the job before it") {
		t.Fatalf("the listing does not compare the two jobs: %q", listed)
	}
	for _, want := range []string{"the job before", "the job after", "3 steers", "1 steer"} {
		if !strings.Contains(listed, want) {
			t.Errorf("the listing does not say %q: %q", want, listed)
		}
	}
}

func TestASteerWithNoJobInFlightSaysWhichJobToName(t *testing.T) {
	client := aSystemToJobIn(t)
	id := declaredHere(t, client, "build the transcripts page")
	mustRun(t, client, "job", "stop", id, "that is enough")

	err := failRun(t, client, "steer", "the workspace has no secrets")

	if !strings.Contains(err.Error(), "krewe steer <job>") {
		t.Fatalf("the refusal does not say how to name a job: %v", err)
	}
}

// Two jobs in flight at once is the shape that would otherwise pick one at random and count the
// score against the wrong tree.
func TestASteerWithTwoJobsInFlightRefusesRatherThanGuessing(t *testing.T) {
	client := aSystemToJobIn(t)
	declaredHere(t, client, "build the transcripts page")
	declaredHere(t, client, "write the migration")

	err := failRun(t, client, "steer", "the workspace has no secrets")

	if !strings.Contains(err.Error(), "2 jobs") {
		t.Fatalf("the refusal does not say why it cannot choose: %v", err)
	}
}

func TestASteerWithNoWordsIsRefusedByTheTool(t *testing.T) {
	client := aSystemToJobIn(t)
	declaredHere(t, client, "build the transcripts page")

	err := failRun(t, client, "steer")

	if !strings.Contains(err.Error(), "krewe steer") {
		t.Fatalf("the refusal does not say what to type: %v", err)
	}
}

// failRun runs one invocation that is expected to be refused, and hands back the refusal.
func failRun(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, args ...string) error {
	t.Helper()
	var out bytes.Buffer
	err := run(context.Background(), client, args, &out, "")
	if err == nil {
		t.Fatalf("krewe %s was accepted, saying %q, and it should have been refused",
			strings.Join(args, " "), out.String())
	}
	return err
}

// A sentence typed without quotes arrives as one argument per word. Joining it up quietly would
// record a steer nobody could see had gone wrong, so it is refused with the line to type.
func TestASentenceWithoutQuotesIsRefusedWithTheLineToType(t *testing.T) {
	client := aSystemToJobIn(t)
	declaredHere(t, client, "build the transcripts page")

	err := failRun(t, client, "steer", "the", "workspace", "has", "no", "secrets")

	if !strings.Contains(err.Error(), `krewe steer "the workspace has no secrets"`) {
		t.Fatalf("the refusal does not show the line to type: %v", err)
	}
}

// An identifier on its own is a job somebody meant to steer and then said nothing about. Recording
// it would put the identifier in the report where the sentence belongs.
func TestAnIdentifierWithNothingSaidIsRefused(t *testing.T) {
	client := aSystemToJobIn(t)
	id := declaredHere(t, client, "build the transcripts page")

	err := failRun(t, client, "steer", id[:8])

	if !strings.Contains(err.Error(), "rather than something you said") {
		t.Fatalf("the refusal does not say what is missing: %v", err)
	}
	report := mustRun(t, client, "steers", id)
	if strings.Contains(report, id[:8]+"\n") {
		t.Fatalf("the identifier was recorded as a steer: %q", report)
	}
	if !strings.Contains(report, "0 steers") {
		t.Fatalf("the job counts something after a refused steer: %q", report)
	}
}
