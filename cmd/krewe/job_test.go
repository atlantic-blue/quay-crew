package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/controlplane"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/secrets"
	"github.com/atlantic-blue/krewe/internal/skill"
	"github.com/atlantic-blue/krewe/internal/store"
)

// aSystemToJobIn is a system with one workspace and one project, with the operator standing in it.
func aSystemToJobIn(t *testing.T) quaycrewv1.ControlPlaneServiceClient {
	t.Helper()
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	return client
}

// declaredHere declares one job and hands back the identifier the system printed.
func declaredHere(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, title string) string {
	t.Helper()
	said := mustRun(t, client, "job", "create", "--title", title, "--brief", "open the bill and say when it is due")
	fields := strings.Fields(said)
	if len(fields) < 2 || fields[0] != "declared" {
		t.Fatalf("krewe job create said %q, want it to name what it declared", said)
	}
	return fields[1]
}

func TestJobIsDeclaredAndReadBack(t *testing.T) {
	client := aSystemToJobIn(t)

	id := declaredHere(t, client, "read the electricity bill")

	shown := mustRun(t, client, "job", "show", id)
	for _, want := range []string{"read the electricity bill", "pending", "open the bill and say when it is due"} {
		if !strings.Contains(shown, want) {
			t.Errorf("krewe job show does not say %q: %q", want, shown)
		}
	}
}

// A declaration says where to read it back, because a caller that declared job and got a bare
// identifier has to work out the next command for itself.
func TestDeclaringJobSaysHowToReadItBack(t *testing.T) {
	client := aSystemToJobIn(t)

	said := mustRun(t, client, "job", "create", "--title", "read the bill", "--brief", "open it")

	if !strings.Contains(said, "krewe job show") {
		t.Fatalf("krewe job create says %q, want it to name how to read the job back", said)
	}
	if !strings.Contains(said, "A controller picks it up") {
		t.Fatalf("krewe job create says %q, want it to say what happens to the job next", said)
	}
}

func TestJobCarriesEverythingTheDeclarationGaveIt(t *testing.T) {
	client := aSystemToJobIn(t)

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
			t.Errorf("krewe job show does not say %q: %q", want, shown)
		}
	}
}

// The tool refuses flags everywhere else, so the ones this command takes have to reach it.
func TestTheFlagsJobTakesReachTheCommand(t *testing.T) {
	client := aSystemToJobIn(t)

	if _, err := runKrewe(t, client, "job", "create", "--title", "read the bill", "--brief", "open it"); err != nil {
		t.Fatalf("a flag krewe job create takes was refused: %v", err)
	}
	if _, err := runKrewe(t, client, "job", "create", "--title", "read the bill", "--brief", "open it", "--sideways", "yes"); err == nil {
		t.Fatal("a flag no command takes was accepted")
	}
}

// The parent is refused by name, with the sentence that says where a parent comes from.
func TestTheParentFlagIsRefusedWithTheReason(t *testing.T) {
	client := aSystemToJobIn(t)

	_, err := runKrewe(t, client, "job", "create", "--title", "read the bill", "--brief", "open it",
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
	client := aSystemToJobIn(t)

	_, err := runKrewe(t, client, "job", "create", "--title")
	if err == nil {
		t.Fatal("a flag with no value was accepted")
	}
	if !strings.Contains(err.Error(), "--title") {
		t.Fatalf("the refusal says %q, want it to name the flag", err)
	}
}

func TestALabelThatIsNotAPairIsRefused(t *testing.T) {
	client := aSystemToJobIn(t)

	_, err := runKrewe(t, client, "job", "create", "--title", "read the bill", "--brief", "open it", "--label", "owner")
	if err == nil {
		t.Fatal("a label that is not a pair was accepted")
	}
	if !strings.Contains(err.Error(), "key=value") {
		t.Fatalf("the refusal says %q, want it to say how a label is written", err)
	}
}

func TestABudgetThatIsNotANumberIsRefused(t *testing.T) {
	client := aSystemToJobIn(t)

	_, err := runKrewe(t, client, "job", "create", "--title", "read the bill", "--brief", "open it",
		"--budget-tokens", "lots")
	if err == nil {
		t.Fatal("a budget that is not a number was accepted")
	}
	if !strings.Contains(err.Error(), "--budget-tokens") {
		t.Fatalf("the refusal says %q, want it to name the flag", err)
	}
}

func TestADeadlineThatIsNotAMomentIsRefused(t *testing.T) {
	client := aSystemToJobIn(t)

	_, err := runKrewe(t, client, "job", "create", "--title", "read the bill", "--brief", "open it",
		"--deadline", "next tuesday")
	if err == nil {
		t.Fatal("a deadline that is not a moment was accepted")
	}
	if !strings.Contains(err.Error(), "2026-08-27T15:04:05Z") {
		t.Fatalf("the refusal says %q, want it to show the shape of a moment", err)
	}
}

// The system's refusal reaches the operator whole. A tool that swallowed it would leave them with a
// failure and no sentence.
func TestTheSystemsRefusalReachesTheOperator(t *testing.T) {
	client := aSystemToJobIn(t)

	_, err := runKrewe(t, client, "job", "create", "--title", "read the bill", "--brief", "open it",
		"--role", "backlog-clearer")
	if err == nil {
		t.Fatal("job naming a role the workspace does not hold was accepted")
	}
	if !strings.Contains(err.Error(), "backlog-clearer") {
		t.Fatalf("the refusal says %q, want the system's own sentence", err)
	}
}

func TestTheListingIsNewestFirstAndNarrowsByPhase(t *testing.T) {
	client := aSystemToJobIn(t)
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
	client := aSystemToJobIn(t)

	listed := mustRun(t, client, "job", "list")

	if !strings.Contains(listed, "krewe job create") {
		t.Fatalf("an empty listing says %q, want it to say how to declare one", listed)
	}
}

func TestJobIsStoppedByTheShortIdentifierAListingPrints(t *testing.T) {
	client := aSystemToJobIn(t)
	id := declaredHere(t, client, "read the electricity bill")

	said := mustRun(t, client, "job", "stop", id, "the bill is not due yet")

	if !strings.Contains(said, "stopped") || !strings.Contains(said, "the bill is not due yet") {
		t.Fatalf("krewe job stop said %q, want it to say what stopped and why", said)
	}
	shown := mustRun(t, client, "job", "show", id)
	if !strings.Contains(shown, "stopped") || !strings.Contains(shown, "the bill is not due yet") {
		t.Fatalf("the job reads back as %q", shown)
	}
}

func TestStoppingJobTwiceIsRefused(t *testing.T) {
	client := aSystemToJobIn(t)
	id := declaredHere(t, client, "read the electricity bill")
	mustRun(t, client, "job", "stop", id, "the bill is not due yet")

	_, err := runKrewe(t, client, "job", "stop", id, "changed my mind")
	if err == nil {
		t.Fatal("job that already ended was stopped again")
	}
	if !strings.Contains(err.Error(), "already") {
		t.Fatalf("the refusal says %q, want it to say the job already ended", err)
	}
}

func TestJobNobodyHoldsIsRefusedBySayingWhereToLook(t *testing.T) {
	client := aSystemToJobIn(t)

	_, err := runKrewe(t, client, "job", "show", "nosuchwork")
	if err == nil {
		t.Fatal("job nobody holds was shown")
	}
	if !strings.Contains(err.Error(), "krewe job list") {
		t.Fatalf("the refusal says %q, want it to say where to look", err)
	}
}

func TestAWordThatIsNotAJobCommandIsRefused(t *testing.T) {
	client := aSystemToJobIn(t)

	_, err := runKrewe(t, client, "job", "sideways")
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

	_, err := runKrewe(t, client, "job", "create", "--title", "read the bill", "--brief", "open it")
	if err == nil {
		t.Fatal("job was declared with no project")
	}
	if !strings.Contains(err.Error(), "project") {
		t.Fatalf("the refusal says %q, want it to say a job runs in a project", err)
	}
}

// TestASessionDeclaresWithNoAddressAndTheSystemReadsTheProjectFromItsCredential.
//
// A session running a job is standing nowhere and cannot resolve an address: resolving one means
// listing workspaces and projects, and a role grants the four job verbs and nothing else. So the
// tool sends no project and the system reads it from the credential, the same place the parent comes
// from. The tool refusing here would make the first verb a role grants unusable from the only place
// it is ever held.
//
// A system that has no credential to read is the case underneath, and it is the one this can assert
// with no interceptor in front of it: the declaration has to reach the system and be refused there,
// rather than be stopped in the tool for being nowhere.
func TestASessionDeclaresWithNoAddressAndTheSystemReadsTheProjectFromItsCredential(t *testing.T) {
	client := testClient(t)

	_, err := runKrewe(t, client, "job", "create", "--title", "read the bill", "--brief", "open it")

	if err == nil {
		t.Fatal("a job was declared with no project and no credential to read one from")
	}
	if !strings.Contains(err.Error(), "job needs a project to run in") {
		t.Fatalf("the refusal says %q, want the system's own answer: the tool stopped it before the system saw it", err)
	}
}

// What a job requires reaches the system and comes back on the listing, because a flag that
// is quietly dropped looks exactly like one that took effect.
func TestWhatJobRequiresReachesTheSystemAndIsShownBack(t *testing.T) {
	client := aSystemToJobIn(t)

	said := mustRun(t, client, "job", "create",
		"--title", "read the electricity bill",
		"--brief", "open it",
		"--requires", "context",
		"--requires", "skills")

	shown := mustRun(t, client, "job", "show", strings.Fields(said)[1])
	if !strings.Contains(shown, "requires context, skills") {
		t.Errorf("krewe job show says %q, want it to say what the job requires", shown)
	}
}

// A word the system does not hand out is refused by name, with the three that would work.
func TestJobRequiringSomethingTheSystemDoesNotHandOutIsRefusedByTheTool(t *testing.T) {
	client := aSystemToJobIn(t)

	_, err := runKrewe(t, client, "job", "create",
		"--title", "read the electricity bill", "--brief", "open it", "--requires", "the codebase")

	if err == nil {
		t.Fatal("job requiring material the system does not hand out was accepted")
	}
	for _, want := range []string{"the codebase", "context", "skills"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal says %q, want it to name %q", err, want)
		}
	}
}

// What the operator sees, driven all the way through: a job that names a repository, a session that
// answers with the address, and the address on the screen beside the answer.
//
// The whole point of the change is that reading a job says where the work is. A test that stopped at
// the row would prove the field is written and nothing about whether anybody can find it.
func TestJobShowSaysWhereTheWorkWent(t *testing.T) {
	const address = "https://github.com/atlantic-blue/quay-crew/pull/454"
	srv := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "Pushed the branch and opened " + address},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
	})
	client := testClientFor(t, srv)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	mustRun(t, client, "job", "create",
		"--title", "sort the listing",
		"--brief", "make the listing sort by the clock it shows",
		"--repository", "atlantic-blue/quay-crew",
		"--mode", "dangerous")
	// The whole identifier, because a listing prints the short one and only the tool expands it.
	listed, err := client.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{})
	if err != nil || len(listed.GetJobs()) != 1 {
		t.Fatalf("the system holds %v jobs (%v), want the one just declared", len(listed.GetJobs()), err)
	}
	id := listed.GetJobs()[0].GetId()

	// The declaration says where the work goes, before anything has run.
	if shown := mustRun(t, client, "job", "show", id); !strings.Contains(shown, "in atlantic-blue/quay-crew") {
		t.Fatalf("krewe job show says %q, want it to say which repository the job works in", shown)
	}

	ctx := context.Background()
	srv.TickJob(ctx)
	waitForJob(t, client, id, job.PhaseDone)
	srv.TickJob(ctx)

	shown := mustRun(t, client, "job", "show", id)
	if !strings.Contains(shown, "pull request: "+address) {
		t.Fatalf("krewe job show says %q, want it to name the pull request", shown)
	}
	// Above the answer, so somebody reading a job finds it without reading an answer to the end.
	if strings.Index(shown, "pull request: ") > strings.Index(shown, "\nanswer:") {
		t.Fatalf("krewe job show puts the pull request below the answer: %q", shown)
	}
}

// waitForJob waits for the controller's detached task to land, which is a goroutine rather than the
// tick that started it. Waited for rather than slept through: how long a goroutine takes to be
// scheduled is a question about the machine.
func waitForJob(t *testing.T, client quaycrewv1.ControlPlaneServiceClient, id, phase string) {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		found, err := client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: id})
		if err == nil && found.GetJob().GetPhase() == phase {
			return
		}
		if err == nil && found.GetJob().GetSession() != "" {
			tasks, err := client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: found.GetJob().GetSession()})
			if err == nil && len(tasks.GetTasks()) > 0 && tasks.GetTasks()[0].GetStatus() != "running" {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the job never reached %s", phase)
}

// A workspace with no credential took a whole tree of job and said nothing, and every session in it
// would have died on its first clone. The system already knew: krewe skill list printed the reason,
// unprompted, in a listing nobody is required to read. So the declaration says it too, where somebody
// is looking.
func aSystemWhoseGitSkillNeedsAToken(t *testing.T) (quaycrewv1.ControlPlaneServiceClient, *controlplane.Server) {
	t.Helper()
	srv := controlplane.NewServer(controlplane.Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Skills: []skill.Skill{{
			Name: "git", Version: 1, Summary: "Branch first.", Brief: "Branch first.",
			Secrets: map[string]string{"GH_TOKEN": "a token with repository scope"},
		}},
	})
	client := testClientFor(t, srv)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	return client, srv
}

func TestDeclaringJobInAWorkspaceWithNoCredentialSaysWhatTheSessionStartsWithout(t *testing.T) {
	client, _ := aSystemWhoseGitSkillNeedsAToken(t)

	said := mustRun(t, client, "job", "create", "--title", "fix the defect", "--brief", "clone it and push a branch")

	for _, want := range []string{
		"starts without",
		"git",
		"GH_TOKEN",
		"krewe secret set",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("krewe job create says %q, want it to say %q", said, want)
		}
	}
	// Said rather than refused: the system cannot know which skill a brief reaches for, and a job that
	// reads an electricity bill runs perfectly well with no forge token.
	if !strings.Contains(said, "declared ") {
		t.Errorf("krewe job create says %q, want the job declared anyway", said)
	}
}

// The note is about a real gap, so a workspace that has its credentials must not carry it: a warning
// printed every time is a warning nobody reads.
func TestDeclaringJobInAWorkspaceThatHasItsCredentialsSaysNothingExtra(t *testing.T) {
	client, _ := aSystemWhoseGitSkillNeedsAToken(t)
	mustRun(t, client, "secret", "set", "me", "GH_TOKEN", "a token")

	said := mustRun(t, client, "job", "create", "--title", "fix the defect", "--brief", "clone it and push a branch")

	if strings.Contains(said, "starts without") {
		t.Fatalf("krewe job create says %q, want nothing about a skill the workspace can supply", said)
	}
}

// A role that does not receive skills is given none of them by design, so there is no gap to report
// and nothing to say. Reporting it anyway would teach the operator to skip the line.
func TestJobInARoleThatDoesNotReceiveSkillsSaysNothingAboutThem(t *testing.T) {
	client, _ := aSystemWhoseGitSkillNeedsAToken(t)
	mustRun(t, client, "role", "import", aRoleDir(t, "test-writer", testWriterManifest))
	mustRun(t, client, "role", "attach", "test-writer")

	said := mustRun(t, client, "job", "create",
		"--title", "read the bill", "--brief", "open it", "--role", "test-writer")

	if strings.Contains(said, "starts without") {
		t.Fatalf("krewe job create says %q, want nothing about skills a role never receives", said)
	}
}

// The refusal reaches the person typing, which is where it has to arrive: a rule that only the
// interface knows is a rule the operator meets after the session is spent.
func TestJobCreateRefusesARepositoryInAModeThatCannotReachIt(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	var out bytes.Buffer
	err := run(context.Background(), client, []string{"job", "create",
		"--title", "sort the listing",
		"--brief", "make the listing sort by the clock it shows",
		"--repository", "atlantic-blue/quay-crew",
		"--mode", "edits"}, &out, "")
	if err == nil {
		t.Fatalf("the tool declared the job and said %q", out.String())
	}
	for _, phrase := range []string{"atlantic-blue/quay-crew", "mode edits", "--mode dangerous"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Errorf("the refusal says %q, want it to say %q", err, phrase)
		}
	}
	// And no row was written, so there is no job for the operator to go and stop.
	listed, err := client.ListJobs(context.Background(), &quaycrewv1.ListJobsRequest{})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(listed.GetJobs()) != 0 {
		t.Fatalf("the system holds %d jobs, and a refusal writes no row", len(listed.GetJobs()))
	}
}
