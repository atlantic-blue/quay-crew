package main

import (
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

// aCrewToJobIn is a crew with one workspace and one project, with the operator standing in it.
func aCrewToJobIn(t *testing.T) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	return client
}

// declaredHere declares one job and hands back the identifier the crew printed.
func declaredHere(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, title string) string {
	t.Helper()
	said := mustRun(t, client, "job", "create", "--title", title, "--brief", "open the bill and say when it is due")
	fields := strings.Fields(said)
	if len(fields) < 2 || fields[0] != "declared" {
		t.Fatalf("quay job create said %q, want it to name what it declared", said)
	}
	return fields[1]
}

func TestJobIsDeclaredAndReadBack(t *testing.T) {
	client := aCrewToJobIn(t)

	id := declaredHere(t, client, "read the electricity bill")

	shown := mustRun(t, client, "job", "show", id)
	for _, want := range []string{"read the electricity bill", "pending", "open the bill and say when it is due"} {
		if !strings.Contains(shown, want) {
			t.Errorf("quay job show does not say %q: %q", want, shown)
		}
	}
}

// A declaration says where to read it back, because a caller that declared job and got a bare
// identifier has to work out the next command for itself.
func TestDeclaringJobSaysHowToReadItBack(t *testing.T) {
	client := aCrewToJobIn(t)

	said := mustRun(t, client, "job", "create", "--title", "read the bill", "--brief", "open it")

	if !strings.Contains(said, "quay job show") {
		t.Fatalf("quay job create says %q, want it to name how to read the job back", said)
	}
	if !strings.Contains(said, "A controller picks it up") {
		t.Fatalf("quay job create says %q, want it to say what happens to the job next", said)
	}
}

func TestJobCarriesEverythingTheDeclarationGaveIt(t *testing.T) {
	client := aCrewToJobIn(t)

	said := mustRun(t, client, "job", "create",
		"--title", "pay the electricity bill",
		"--brief", "pay it before the 14th",
		"--mode", "plan",
		"--expect-file", "notes/bill.md",
		"--expect-contains", "paid",
		"--budget-tokens", "5000",
		"--deadline", "2026-12-24T09:00:00Z",
		"--label", "owner=house",
		"--label", "kind=bills")

	shown := mustRun(t, client, "job", "show", strings.Fields(said)[1])
	for _, want := range []string{"budget 5000", "label kind=bills", "label owner=house", "2026-12-24"} {
		if !strings.Contains(shown, want) {
			t.Errorf("quay job show does not say %q: %q", want, shown)
		}
	}
}

// The tool refuses flags everywhere else, so the ones this command takes have to reach it.
func TestTheFlagsJobTakesReachTheCommand(t *testing.T) {
	client := aCrewToJobIn(t)

	if _, err := runQuay(t, client, "job", "create", "--title", "read the bill", "--brief", "open it"); err != nil {
		t.Fatalf("a flag quay job create takes was refused: %v", err)
	}
	if _, err := runQuay(t, client, "job", "create", "--title", "read the bill", "--brief", "open it", "--sideways", "yes"); err == nil {
		t.Fatal("a flag no command takes was accepted")
	}
}

// The parent is refused by name, with the sentence that says where a parent comes from.
func TestTheParentFlagIsRefusedWithTheReason(t *testing.T) {
	client := aCrewToJobIn(t)

	_, err := runQuay(t, client, "job", "create", "--title", "read the bill", "--brief", "open it",
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
	client := aCrewToJobIn(t)

	_, err := runQuay(t, client, "job", "create", "--title")
	if err == nil {
		t.Fatal("a flag with no value was accepted")
	}
	if !strings.Contains(err.Error(), "--title") {
		t.Fatalf("the refusal says %q, want it to name the flag", err)
	}
}

func TestALabelThatIsNotAPairIsRefused(t *testing.T) {
	client := aCrewToJobIn(t)

	_, err := runQuay(t, client, "job", "create", "--title", "read the bill", "--brief", "open it", "--label", "owner")
	if err == nil {
		t.Fatal("a label that is not a pair was accepted")
	}
	if !strings.Contains(err.Error(), "key=value") {
		t.Fatalf("the refusal says %q, want it to say how a label is written", err)
	}
}

func TestABudgetThatIsNotANumberIsRefused(t *testing.T) {
	client := aCrewToJobIn(t)

	_, err := runQuay(t, client, "job", "create", "--title", "read the bill", "--brief", "open it",
		"--budget-tokens", "lots")
	if err == nil {
		t.Fatal("a budget that is not a number was accepted")
	}
	if !strings.Contains(err.Error(), "--budget-tokens") {
		t.Fatalf("the refusal says %q, want it to name the flag", err)
	}
}

func TestADeadlineThatIsNotAMomentIsRefused(t *testing.T) {
	client := aCrewToJobIn(t)

	_, err := runQuay(t, client, "job", "create", "--title", "read the bill", "--brief", "open it",
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
	client := aCrewToJobIn(t)

	_, err := runQuay(t, client, "job", "create", "--title", "read the bill", "--brief", "open it",
		"--role", "backlog-clearer")
	if err == nil {
		t.Fatal("job naming a role the workspace does not hold was accepted")
	}
	if !strings.Contains(err.Error(), "backlog-clearer") {
		t.Fatalf("the refusal says %q, want the crew's own sentence", err)
	}
}

func TestTheListingIsNewestFirstAndNarrowsByPhase(t *testing.T) {
	client := aCrewToJobIn(t)
	first := declaredHere(t, client, "read the electricity bill")
	declaredHere(t, client, "pay the electricity bill")

	listed := mustRun(t, client, "job", "list")
	// The rows, which are everything above the line that says where the listing looked.
	rows, _, _ := strings.Cut(strings.TrimSpace(listed), "\n\n")
	lines := strings.Split(rows, "\n")
	if len(lines) != 2 {
		t.Fatalf("the listing has %d rows, want 2: %q", len(lines), listed)
	}
	if !strings.Contains(lines[0], "pay the electricity bill") {
		t.Fatalf("the listing opens with %q, want the newest first", lines[0])
	}

	mustRun(t, client, "job", "stop", first, "the bill is not due yet")
	pending := mustRun(t, client, "job", "list", "--phase", "pending")
	if strings.Contains(pending, "read the electricity bill") {
		t.Fatalf("the pending listing carries the stopped job: %q", pending)
	}
	if !strings.Contains(pending, "pay the electricity bill") {
		t.Fatalf("the pending listing lost the job that is still pending: %q", pending)
	}
}

func TestAnEmptyListingSaysHowToDeclareJob(t *testing.T) {
	client := aCrewToJobIn(t)

	listed := mustRun(t, client, "job", "list")

	if !strings.Contains(listed, "quay job create") {
		t.Fatalf("an empty listing says %q, want it to say how to declare one", listed)
	}
}

func TestJobIsStoppedByTheShortIdentifierAListingPrints(t *testing.T) {
	client := aCrewToJobIn(t)
	id := declaredHere(t, client, "read the electricity bill")

	said := mustRun(t, client, "job", "stop", id, "the bill is not due yet")

	if !strings.Contains(said, "stopped") || !strings.Contains(said, "the bill is not due yet") {
		t.Fatalf("quay job stop said %q, want it to say what stopped and why", said)
	}
	shown := mustRun(t, client, "job", "show", id)
	if !strings.Contains(shown, "stopped") || !strings.Contains(shown, "the bill is not due yet") {
		t.Fatalf("the job reads back as %q", shown)
	}
}

func TestStoppingJobTwiceIsRefused(t *testing.T) {
	client := aCrewToJobIn(t)
	id := declaredHere(t, client, "read the electricity bill")
	mustRun(t, client, "job", "stop", id, "the bill is not due yet")

	_, err := runQuay(t, client, "job", "stop", id, "changed my mind")
	if err == nil {
		t.Fatal("job that already ended was stopped again")
	}
	if !strings.Contains(err.Error(), "already") {
		t.Fatalf("the refusal says %q, want it to say the job already ended", err)
	}
}

func TestJobNobodyHoldsIsRefusedBySayingWhereToLook(t *testing.T) {
	client := aCrewToJobIn(t)

	_, err := runQuay(t, client, "job", "show", "nosuchwork")
	if err == nil {
		t.Fatal("job nobody holds was shown")
	}
	if !strings.Contains(err.Error(), "quay job list") {
		t.Fatalf("the refusal says %q, want it to say where to look", err)
	}
}

func TestAWordThatIsNotAJobCommandIsRefused(t *testing.T) {
	client := aCrewToJobIn(t)

	_, err := runQuay(t, client, "job", "sideways")
	if err == nil {
		t.Fatal("a word that is not a job command was accepted")
	}
	if !strings.Contains(err.Error(), "create") {
		t.Fatalf("the refusal says %q, want it to name the commands", err)
	}
}

// Job runs in a project, so an operator standing in a workspace is told what is missing rather
// than having the declaration land somewhere unexpected.
func TestDeclaringJobFromAWorkspaceSaysItNeedsAProject(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")

	_, err := runQuay(t, client, "job", "create", "--title", "read the bill", "--brief", "open it")
	if err == nil {
		t.Fatal("job was declared with no project")
	}
	if !strings.Contains(err.Error(), "project") {
		t.Fatalf("the refusal says %q, want it to say a job runs in a project", err)
	}
}

// TestASessionDeclaresWithNoAddressAndTheCrewReadsTheProjectFromItsCredential.
//
// A session running a job is standing nowhere and cannot resolve an address: resolving one means
// listing workspaces and projects, and a role grants the four job verbs and nothing else. So the
// tool sends no project and the crew reads it from the credential, the same place the parent comes
// from. The tool refusing here would make the first verb a role grants unusable from the only place
// it is ever held.
//
// A crew that has no credential to read is the case underneath, and it is the one this can assert
// with no interceptor in front of it: the declaration has to reach the crew and be refused there,
// rather than be stopped in the tool for being nowhere.
func TestASessionDeclaresWithNoAddressAndTheCrewReadsTheProjectFromItsCredential(t *testing.T) {
	client := testClient(t)

	_, err := runQuay(t, client, "job", "create", "--title", "read the bill", "--brief", "open it")

	if err == nil {
		t.Fatal("a job was declared with no project and no credential to read one from")
	}
	if !strings.Contains(err.Error(), "job needs a project to run in") {
		t.Fatalf("the refusal says %q, want the crew's own answer: the tool stopped it before the crew saw it", err)
	}
}

// What a job requires reaches the crew and comes back on the listing, because a flag that
// is quietly dropped looks exactly like one that took effect.
func TestWhatJobRequiresReachesTheCrewAndIsShownBack(t *testing.T) {
	client := aCrewToJobIn(t)

	said := mustRun(t, client, "job", "create",
		"--title", "read the electricity bill",
		"--brief", "open it",
		"--requires", "context",
		"--requires", "skills")

	shown := mustRun(t, client, "job", "show", strings.Fields(said)[1])
	if !strings.Contains(shown, "requires context, skills") {
		t.Errorf("quay job show says %q, want it to say what the job requires", shown)
	}
}

// A word the crew does not hand out is refused by name, with the three that would work.
func TestJobRequiringSomethingTheCrewDoesNotHandOutIsRefusedByTheTool(t *testing.T) {
	client := aCrewToJobIn(t)

	_, err := runQuay(t, client, "job", "create",
		"--title", "read the electricity bill", "--brief", "open it", "--requires", "the codebase")

	if err == nil {
		t.Fatal("job requiring material the crew does not hand out was accepted")
	}
	for _, want := range []string{"the codebase", "context", "skills"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal says %q, want it to name %q", err, want)
		}
	}
}
