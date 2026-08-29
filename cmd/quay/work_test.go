package main

import (
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

// aCrewToWorkIn is a crew with one workspace and one project, with the operator standing in it.
func aCrewToWorkIn(t *testing.T) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	return client
}

// declaredHere declares one piece of work and hands back the identifier the crew printed.
func declaredHere(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, title string) string {
	t.Helper()
	said := mustRun(t, client, "work", "create", "--title", title, "--brief", "open the bill and say when it is due")
	fields := strings.Fields(said)
	if len(fields) < 2 || fields[0] != "declared" {
		t.Fatalf("quay work create said %q, want it to name what it declared", said)
	}
	return fields[1]
}

func TestWorkIsDeclaredAndReadBack(t *testing.T) {
	client := aCrewToWorkIn(t)

	id := declaredHere(t, client, "read the electricity bill")

	shown := mustRun(t, client, "work", "show", id)
	for _, want := range []string{"read the electricity bill", "pending", "open the bill and say when it is due"} {
		if !strings.Contains(shown, want) {
			t.Errorf("quay work show does not say %q: %q", want, shown)
		}
	}
}

// A declaration says where to read it back, because a caller that declared work and got a bare
// identifier has to work out the next command for itself.
func TestDeclaringWorkSaysHowToReadItBack(t *testing.T) {
	client := aCrewToWorkIn(t)

	said := mustRun(t, client, "work", "create", "--title", "read the bill", "--brief", "open it")

	if !strings.Contains(said, "quay work show") {
		t.Fatalf("quay work create says %q, want it to name how to read the work back", said)
	}
	if !strings.Contains(said, "A controller picks it up") {
		t.Fatalf("quay work create says %q, want it to say what happens to the work next", said)
	}
}

func TestWorkCarriesEverythingTheDeclarationGaveIt(t *testing.T) {
	client := aCrewToWorkIn(t)

	said := mustRun(t, client, "work", "create",
		"--title", "pay the electricity bill",
		"--brief", "pay it before the 14th",
		"--mode", "plan",
		"--expect-file", "notes/bill.md",
		"--expect-contains", "paid",
		"--budget-tokens", "5000",
		"--deadline", "2026-12-24T09:00:00Z",
		"--label", "owner=house",
		"--label", "kind=bills")

	shown := mustRun(t, client, "work", "show", strings.Fields(said)[1])
	for _, want := range []string{"budget 5000", "label kind=bills", "label owner=house", "2026-12-24"} {
		if !strings.Contains(shown, want) {
			t.Errorf("quay work show does not say %q: %q", want, shown)
		}
	}
}

// The tool refuses flags everywhere else, so the ones this command takes have to reach it.
func TestTheFlagsWorkTakesReachTheCommand(t *testing.T) {
	client := aCrewToWorkIn(t)

	if _, err := runQuay(t, client, "work", "create", "--title", "read the bill", "--brief", "open it"); err != nil {
		t.Fatalf("a flag quay work create takes was refused: %v", err)
	}
	if _, err := runQuay(t, client, "work", "create", "--title", "read the bill", "--brief", "open it", "--sideways", "yes"); err == nil {
		t.Fatal("a flag no command takes was accepted")
	}
}

// The parent is refused by name, with the sentence that says where a parent comes from.
func TestTheParentFlagIsRefusedWithTheReason(t *testing.T) {
	client := aCrewToWorkIn(t)

	_, err := runQuay(t, client, "work", "create", "--title", "read the bill", "--brief", "open it",
		"--parent", "0123456789abcdef01234567")
	if err == nil {
		t.Fatal("a parent given on the command line was accepted")
	}
	if !strings.Contains(err.Error(), "credential") {
		t.Fatalf("the refusal says %q, want it to say the parent comes from the credential", err)
	}
}

// A flag at the end of the line with nothing after it is a value the caller thinks they gave.
func TestAFlagWithNoValueIsRefusedByName(t *testing.T) {
	client := aCrewToWorkIn(t)

	_, err := runQuay(t, client, "work", "create", "--title")
	if err == nil {
		t.Fatal("a flag with no value was accepted")
	}
	if !strings.Contains(err.Error(), "--title") {
		t.Fatalf("the refusal says %q, want it to name the flag", err)
	}
}

func TestALabelThatIsNotAPairIsRefused(t *testing.T) {
	client := aCrewToWorkIn(t)

	_, err := runQuay(t, client, "work", "create", "--title", "read the bill", "--brief", "open it", "--label", "owner")
	if err == nil {
		t.Fatal("a label that is not a pair was accepted")
	}
	if !strings.Contains(err.Error(), "key=value") {
		t.Fatalf("the refusal says %q, want it to say how a label is written", err)
	}
}

func TestABudgetThatIsNotANumberIsRefused(t *testing.T) {
	client := aCrewToWorkIn(t)

	_, err := runQuay(t, client, "work", "create", "--title", "read the bill", "--brief", "open it",
		"--budget-tokens", "lots")
	if err == nil {
		t.Fatal("a budget that is not a number was accepted")
	}
	if !strings.Contains(err.Error(), "--budget-tokens") {
		t.Fatalf("the refusal says %q, want it to name the flag", err)
	}
}

func TestADeadlineThatIsNotAMomentIsRefused(t *testing.T) {
	client := aCrewToWorkIn(t)

	_, err := runQuay(t, client, "work", "create", "--title", "read the bill", "--brief", "open it",
		"--deadline", "next tuesday")
	if err == nil {
		t.Fatal("a deadline that is not a moment was accepted")
	}
	if !strings.Contains(err.Error(), "2026-08-27T15:04:05Z") {
		t.Fatalf("the refusal says %q, want it to show the shape of a moment", err)
	}
}

// The crew's refusal reaches the operator whole. A tool that swallowed it would leave them with a
// failure and no sentence.
func TestTheCrewsRefusalReachesTheOperator(t *testing.T) {
	client := aCrewToWorkIn(t)

	_, err := runQuay(t, client, "work", "create", "--title", "read the bill", "--brief", "open it",
		"--role", "backlog-clearer")
	if err == nil {
		t.Fatal("work naming a role the workspace does not hold was accepted")
	}
	if !strings.Contains(err.Error(), "backlog-clearer") {
		t.Fatalf("the refusal says %q, want the crew's own sentence", err)
	}
}

func TestTheListingIsNewestFirstAndNarrowsByPhase(t *testing.T) {
	client := aCrewToWorkIn(t)
	first := declaredHere(t, client, "read the electricity bill")
	declaredHere(t, client, "pay the electricity bill")

	listed := mustRun(t, client, "work", "list")
	lines := strings.Split(strings.TrimSpace(listed), "\n")
	if len(lines) != 2 {
		t.Fatalf("the listing has %d lines, want 2: %q", len(lines), listed)
	}
	if !strings.Contains(lines[0], "pay the electricity bill") {
		t.Fatalf("the listing opens with %q, want the newest first", lines[0])
	}

	mustRun(t, client, "work", "stop", first, "the bill is not due yet")
	pending := mustRun(t, client, "work", "list", "--phase", "pending")
	if strings.Contains(pending, "read the electricity bill") {
		t.Fatalf("the pending listing carries the stopped work: %q", pending)
	}
	if !strings.Contains(pending, "pay the electricity bill") {
		t.Fatalf("the pending listing lost the work that is still pending: %q", pending)
	}
}

func TestAnEmptyListingSaysHowToDeclareWork(t *testing.T) {
	client := aCrewToWorkIn(t)

	listed := mustRun(t, client, "work", "list")

	if !strings.Contains(listed, "quay work create") {
		t.Fatalf("an empty listing says %q, want it to say how to declare work", listed)
	}
}

func TestWorkIsStoppedByTheShortIdentifierAListingPrints(t *testing.T) {
	client := aCrewToWorkIn(t)
	id := declaredHere(t, client, "read the electricity bill")

	said := mustRun(t, client, "work", "stop", id, "the bill is not due yet")

	if !strings.Contains(said, "stopped") || !strings.Contains(said, "the bill is not due yet") {
		t.Fatalf("quay work stop said %q, want it to say what stopped and why", said)
	}
	shown := mustRun(t, client, "work", "show", id)
	if !strings.Contains(shown, "stopped") || !strings.Contains(shown, "the bill is not due yet") {
		t.Fatalf("the work reads back as %q", shown)
	}
}

func TestStoppingWorkTwiceIsRefused(t *testing.T) {
	client := aCrewToWorkIn(t)
	id := declaredHere(t, client, "read the electricity bill")
	mustRun(t, client, "work", "stop", id, "the bill is not due yet")

	_, err := runQuay(t, client, "work", "stop", id, "changed my mind")
	if err == nil {
		t.Fatal("work that already ended was stopped again")
	}
	if !strings.Contains(err.Error(), "already") {
		t.Fatalf("the refusal says %q, want it to say the work already ended", err)
	}
}

func TestWorkNobodyHoldsIsRefusedBySayingWhereToLook(t *testing.T) {
	client := aCrewToWorkIn(t)

	_, err := runQuay(t, client, "work", "show", "nosuchwork")
	if err == nil {
		t.Fatal("work nobody holds was shown")
	}
	if !strings.Contains(err.Error(), "quay work list") {
		t.Fatalf("the refusal says %q, want it to say where to look", err)
	}
}

func TestAWordThatIsNotAWorkCommandIsRefused(t *testing.T) {
	client := aCrewToWorkIn(t)

	_, err := runQuay(t, client, "work", "sideways")
	if err == nil {
		t.Fatal("a word that is not a work command was accepted")
	}
	if !strings.Contains(err.Error(), "create") {
		t.Fatalf("the refusal says %q, want it to name the commands", err)
	}
}

// Work runs in a project, so an operator standing in a workspace is told what is missing rather
// than having the declaration land somewhere unexpected.
func TestDeclaringWorkFromAWorkspaceSaysItNeedsAProject(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")

	_, err := runQuay(t, client, "work", "create", "--title", "read the bill", "--brief", "open it")
	if err == nil {
		t.Fatal("work was declared with no project")
	}
	if !strings.Contains(err.Error(), "project") {
		t.Fatalf("the refusal says %q, want it to say work runs in a project", err)
	}
}

// What a piece of work requires reaches the crew and comes back on the listing, because a flag that
// is quietly dropped looks exactly like one that took effect.
func TestWhatWorkRequiresReachesTheCrewAndIsShownBack(t *testing.T) {
	client := aCrewToWorkIn(t)

	said := mustRun(t, client, "work", "create",
		"--title", "read the electricity bill",
		"--brief", "open it",
		"--requires", "context",
		"--requires", "skills")

	shown := mustRun(t, client, "work", "show", strings.Fields(said)[1])
	if !strings.Contains(shown, "requires context, skills") {
		t.Errorf("quay work show says %q, want it to say what the work requires", shown)
	}
}

// A word the crew does not hand out is refused by name, with the three that would work.
func TestWorkRequiringSomethingTheCrewDoesNotHandOutIsRefusedByTheTool(t *testing.T) {
	client := aCrewToWorkIn(t)

	_, err := runQuay(t, client, "work", "create",
		"--title", "read the electricity bill", "--brief", "open it", "--requires", "the codebase")

	if err == nil {
		t.Fatal("work requiring material the crew does not hand out was accepted")
	}
	for _, want := range []string{"the codebase", "context", "skills"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal says %q, want it to name %q", err, want)
		}
	}
}
