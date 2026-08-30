package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// `quay stop <session> [<reason>]` at the command line.
//
// The interface matters as much as the mechanism here. The way people stopped a session before this
// was killing the dispatch client, and a command that reads as having worked when it has not would be
// the same failure with a nicer name.

func TestStopSaysThereWasNothingToStop(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)

	said := mustRun(t, client, "stop", sessionOf(t, client))

	if !strings.Contains(said, "nothing is running") {
		t.Fatalf("stopping an idle session said %q, and it has to say nothing was running", said)
	}
	// It must not claim to have stopped anything, or an operator reads success and stops watching.
	if strings.Contains(said, "stopped the task") {
		t.Fatalf("stopping an idle session claimed it stopped a task: %q", said)
	}
}

func TestStopNamesTheSessionAndSaysItSurvives(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)

	said := mustRun(t, client, "stop", sessionOf(t, client))

	if !strings.Contains(said, sessionOf(t, client)) {
		t.Fatalf("the answer does not name the session: %q", said)
	}
}

func TestStopWithNoSessionSaysHowToUseIt(t *testing.T) {
	client := testClient(t)

	var out bytes.Buffer
	err := run(context.Background(), client, []string{"stop"}, &out, "")
	if err == nil {
		t.Fatalf("quay stop with no session was accepted: %q", out.String())
	}
	for _, want := range []string{"quay stop <session>", "<reason>"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal is %q, and it does not say %q", err, want)
		}
	}
}

func TestStopTakesAtMostASessionAndAReason(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)

	var out bytes.Buffer
	err := run(context.Background(), client,
		[]string{"stop", sessionOf(t, client), "because", "of", "this"}, &out, "")
	if err == nil {
		t.Fatalf("quay stop took four words and acted on them: %q", out.String())
	}
}

// A word nobody can act on is worth no more than no word at all, so a session the system does not have
// is refused rather than reported as having nothing to stop.
func TestStopRefusesASessionTheSystemDoesNotHave(t *testing.T) {
	client, _ := aSessionWatchingTheModel(t)

	var out bytes.Buffer
	err := run(context.Background(), client, []string{"stop", "deadbeef"}, &out, "")
	if err == nil {
		t.Fatalf("quay stop on a session that does not exist was accepted: %q", out.String())
	}
}

// The command has to be findable, or an operator reaches for killing the client again.
func TestStopIsInTheUsage(t *testing.T) {
	client := testClient(t)

	said := mustRun(t, client, "help")

	if !strings.Contains(said, "stop <session>") {
		t.Fatalf("the usage does not carry quay stop")
	}
}
