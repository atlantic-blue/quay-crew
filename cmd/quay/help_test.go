package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

// asked runs one invocation and hands back what it printed and whether it was refused, because every
// case here is about which of those two an operator gets.
func asked(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(context.Background(), client, args, &out, "")
	return out.String(), err
}

// Asking what the tool does is the first thing anybody types, and until this it was refused four
// different ways: help and -h were unknown commands, and --help and --version were answered with
// advice about addresses, which is not what either was asking.
func TestEveryWayOfAskingForHelpIsAnswered(t *testing.T) {
	client := testClient(t)

	for _, spelling := range []string{"help", "-h", "--help", "-help", "?"} {
		printed, err := asked(t, client, spelling)
		if err != nil {
			t.Errorf("quay %s was refused: %v", spelling, err)
			continue
		}
		if !strings.Contains(printed, "commands:") || !strings.Contains(printed, "dispatch") {
			t.Errorf("quay %s did not print the commands: %q", spelling, printed)
		}
	}
}

// The advice has to be actable. "say where with an address instead" cannot be acted on by somebody
// asking which build they are running.
func TestAskingForTheVersionAsAFlagNamesTheCommand(t *testing.T) {
	client := testClient(t)

	_, err := asked(t, client, "--version")
	if err == nil {
		t.Fatal("--version was accepted, and this tool takes no flags")
	}
	if !strings.Contains(err.Error(), "quay version") {
		t.Errorf("the refusal does not name the command that answers it: %s", err)
	}
	if strings.Contains(err.Error(), "say where with an address") {
		t.Errorf("the refusal still gives address advice to a question about the build: %s", err)
	}
}

// A flag that is genuinely somebody trying to say where they are still gets the address advice, so
// fixing the two above did not flatten every refusal into the same sentence.
func TestAFlagThatIsAskingWhereStillGetsTheAddressAdvice(t *testing.T) {
	client := testClient(t)

	_, err := asked(t, client, "--somewhere")
	if err == nil {
		t.Fatal("an unknown flag was accepted")
	}
	if !strings.Contains(err.Error(), "address") {
		t.Errorf("the refusal lost the address advice: %s", err)
	}
}

// The usage teaches the word session three times over and the command was sessions alone.
func TestSessionIsACommandBecauseTheToolTeachesTheWord(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "ask", "hello")

	listed := mustRun(t, client, "sessions")
	for _, spelling := range []string{"session", "sessions", "session"} {
		also, err := asked(t, client, spelling)
		if err != nil {
			t.Errorf("quay %s was refused: %v", spelling, err)
			continue
		}
		if also != listed {
			t.Errorf("quay %s answered differently from quay sessions:\n%q\nwant\n%q", spelling, also, listed)
		}
	}
}

// The usage is what somebody reads before typing, so it has to name both.
func TestTheUsageNamesHelpAndSession(t *testing.T) {
	client := testClient(t)

	printed, err := asked(t, client, "help")
	if err != nil {
		t.Fatalf("help was refused: %v", err)
	}
	for _, want := range []string{"help", "session"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the usage does not mention %q", want)
		}
	}
}
