package job_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/hook"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// One requirement, one branch, one pull request, and two workers on it.
//
// The fault these are written against: the worker that wrote a requirement's tests wrote them into a
// sandbox and answered with three lines, and the sandbox went away with the files in it. The worker
// that built the same requirement took a fresh clone and was told to read tests that were not in it.
// Every check was green the whole time, because nothing ever asked where the files went.

// aTestFileTheWorkerWrote is what the worker writing a requirement's tests leaves behind, and what
// the worker building it has to find. The bytes are checked rather than the path, because a file of
// the right name holding nothing would read as work arriving.
const aTestFileTheWorkerWrote = `package transcript

func TestPastingALinkPrintsTheTranscript(t *testing.T) {
	t.Fatal("nothing pastes a link yet")
}
`

// The round trip, driven with git, following the commands the two briefs actually carry.
//
// Nothing here types a git command of its own. Both sets are read back out of the briefs the system
// writes, because the words a worker is given are the mechanism: a test that typed the commands
// itself would prove that this test can check out a branch, which nobody doubted.
func TestTheBuildRunFindsTheTestsTheTestRunLeftOnTheBranch(t *testing.T) {
	remote := aBareRemote(t)
	one := aJobInARepository()
	requirement := job.RequirementsOf(one)[0]
	branch := job.BranchForRequirement(one.ID, requirement)
	where := filepath.Join("internal", "transcript", "paste_test.go")

	// The worker that writes the tests, following its brief. It cuts the branch the brief names,
	// writes its test there and pushes it.
	writing := gitCloneOf(t, remote, "the-test-worker")
	testRun := job.TestExecutions(one, []job.Requirement{requirement})[0]
	if testRun.Branch != branch {
		t.Fatalf("the run writing requirement %d is on %q, and its branch is %q",
			requirement.Number, testRun.Branch, branch)
	}
	runTheGitIn(t, writing, job.WriteFailingTests(one, requirement))
	if on := gitSaid(t, writing, "rev-parse", "--abbrev-ref", "HEAD"); on != branch {
		t.Fatalf("the brief left the worker writing tests on %q, want %q", on, branch)
	}
	writeFileUnder(t, writing, where, aTestFileTheWorkerWrote)
	gitSaid(t, writing, "add", where)
	gitSaid(t, writing, "commit", "-m", "write the failing test for requirement 1")
	gitSaid(t, writing, "push", "--set-upstream", "origin", branch)

	// The worker that builds it, in a sandbox of its own with a fresh clone. This is the state the
	// fault lived in: the tests it is told to read are not in this checkout.
	building := gitCloneOf(t, remote, "the-build-worker")
	if _, err := os.Stat(filepath.Join(building, where)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a fresh clone already holds %s, so this proves nothing about fetching it", where)
	}
	opened := job.Opened{Branch: branch, PullRequest: aPullRequestOn(requirement)}
	runTheGitIn(t, building,
		job.BuildTheVertical(one, requirement, job.FailuresOn(one.Tests)[requirement.Number], opened))

	// The whole of it: the file the first worker wrote is in the second worker's checkout, and it
	// holds what was written rather than only carrying the name.
	found, err := os.ReadFile(filepath.Join(building, where))
	if err != nil {
		t.Fatalf("the tests the first worker wrote are not in the second worker's checkout: %v", err)
	}
	if string(found) != aTestFileTheWorkerWrote {
		t.Fatalf("the test reads back as %q", found)
	}
	if on := gitSaid(t, building, "rev-parse", "--abbrev-ref", "HEAD"); on != branch {
		t.Fatalf("the worker building requirement %d is on %q, and its tests are on %q",
			requirement.Number, on, branch)
	}

	// And the boundary is real for the first time. It guarded files that were not in the checkout,
	// and this is the same write refused against a test that is now there to be read.
	if !refusedByTheTestGate(t, where) {
		t.Fatalf("a building session may write to %s, which is the test its own branch carries", where)
	}
	if refusedByTheTestGate(t, "internal/transcript/paste.go") {
		t.Fatal("a building session may not write the code its tests are about")
	}
}

// aPullRequestOn is the address the pull request of one requirement is open at.
func aPullRequestOn(requirement job.Requirement) string {
	return fmt.Sprintf("https://github.com/atlantic-blue/quay-krewe/pull/%d", requirement.Number)
}

// aJobInARepository is the job both workers belong to: its list is accepted, its suite is red, and it
// works somewhere a branch can be pushed to.
func aJobInARepository() *job.Job {
	one := buildingJob()
	one.Repository = "atlantic-blue/quay-krewe"
	return one
}

// aBareRemote is the repository the workers push to and clone from, holding one commit on its
// default branch. It stands in for the forge, and it is bare so a push to it is never refused for
// being a checked out branch.
func aBareRemote(t *testing.T) string {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "remote.git")
	gitSaid(t, "", "init", "--bare", "--initial-branch=main", remote)
	seed := filepath.Join(t.TempDir(), "seed")
	gitSaid(t, "", "clone", remote, seed)
	writeFileUnder(t, seed, "README.md", "the transcript page\n")
	gitSaid(t, seed, "add", "README.md")
	gitSaid(t, seed, "commit", "-m", "the first commit")
	gitSaid(t, seed, "push", "origin", "main")
	return remote
}

// gitCloneOf is one worker's sandbox: a clone of its own, on the default branch, holding nothing
// anybody else wrote.
func gitCloneOf(t *testing.T, remote, who string) string {
	t.Helper()
	at := filepath.Join(t.TempDir(), who)
	gitSaid(t, "", "clone", remote, at)
	return at
}

// runTheGitIn runs every git command one brief carries, in order, in that worker's checkout.
func runTheGitIn(t *testing.T, at, brief string) {
	t.Helper()
	commands := job.TheGitCommandsIn(brief)
	if len(commands) == 0 {
		t.Fatalf("this brief carries no git command, so the worker is told nothing about where the "+
			"work goes: %s", brief)
	}
	for _, command := range commands {
		gitSaid(t, at, command[1:]...)
	}
}

// gitSaid runs one git command in a checkout and answers what it printed. It is the reading half of
// the harness in testsbranch_test.go, which runs a command and checks nothing back.
func gitSaid(t *testing.T, at string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = at
	command.Env = aClosedGit(at)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v: %s", strings.Join(args, " "), at, err, out)
	}
	return strings.TrimSpace(string(out))
}

// writeFileUnder writes one file at a path inside a checkout, making the directories it needs.
func writeFileUnder(t *testing.T, at, where, body string) {
	t.Helper()
	full := filepath.Join(at, where)
	if err := os.MkdirAll(filepath.Dir(full), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o666); err != nil {
		t.Fatal(err)
	}
}

// refusedByTheTestGate fires the gate this build ships, the way the model runtime fires it, on a
// session the system is building with.
func refusedByTheTestGate(t *testing.T, where string) bool {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"tool_name":  "Write",
		"tool_input": map[string]string{"file_path": where},
	})
	if err != nil {
		t.Fatal(err)
	}
	hooks, err := hook.Load("../../hooks")
	if err != nil {
		t.Fatalf("loading the hooks this build ships (run `make hooks` first): %v", err)
	}
	entry := ""
	for _, one := range hooks {
		if one.Name == "test-gate" && len(one.Events) > 0 {
			entry = filepath.Join("../../hooks", one.Name, one.Events[0].Entry)
		}
	}
	if entry == "" {
		t.Fatal("this build ships no test gate, so the boundary is a sentence in a brief")
	}
	run := exec.Command(entry)
	run.Stdin = strings.NewReader(string(payload))
	run.Env = append(os.Environ(), "KREWE_BUILDING=1")
	var said strings.Builder
	run.Stderr = &said
	switch err := run.Run(); {
	case err == nil:
		return false
	case isRefusal(err):
		return true
	default:
		t.Fatalf("running the test gate: %v: %s", err, said.String())
		return false
	}
}

// isRefusal is the exit status the runtime reads as a refused write.
func isRefusal(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 2
}

// One branch belongs to one requirement, and the two workers that touch that requirement are on it.
func TestOneBranchBelongsToOneRequirementAndBothItsRunsAreOnIt(t *testing.T) {
	one := aJobInARepository()
	wanted := job.RequirementsOf(one)
	opened := map[int]job.Opened{}
	for _, requirement := range wanted {
		opened[requirement.Number] = job.Opened{
			Branch:      job.BranchForRequirement(one.ID, requirement),
			PullRequest: aPullRequestOn(requirement),
		}
	}

	writers := job.TestExecutions(one, wanted)
	builders := job.BuildExecutions(one, wanted, opened)
	branches := map[string]int{}
	for at, requirement := range wanted {
		want := job.BranchForRequirement(one.ID, requirement)
		if writers[at].Branch != want || builders[at].Branch != want {
			t.Fatalf("requirement %d writes on %q and builds on %q, and its branch is %q",
				requirement.Number, writers[at].Branch, builders[at].Branch, want)
		}
		branches[want]++
	}
	// And no two requirements share one, or the second worker home would push over the first.
	if len(branches) != len(wanted) {
		t.Fatalf("%d requirements share %d branches", len(wanted), len(branches))
	}
	// The branch carries the job as well as the number, so two jobs that accepted a list of the same
	// length never write to one branch.
	other := aJobInARepository()
	other.ID = "another-job"
	if job.BranchForRequirement(other.ID, wanted[0]) == job.BranchForRequirement(one.ID, wanted[0]) {
		t.Fatal("two jobs put requirement 1 on the same branch")
	}
}

// The worker that writes the tests is told where they go and that the pull request stays open.
func TestTheRunWritingTestsIsToldToOpenItsPullRequestAndLeaveItRed(t *testing.T) {
	one := aJobInARepository()
	requirement := job.RequirementsOf(one)[0]
	run := job.TestExecutions(one, []job.Requirement{requirement})[0]
	branch := job.BranchForRequirement(one.ID, requirement)

	asked := job.WriteFailingTests(one, requirement)
	for _, phrase := range []string{
		branch, "git switch --create " + branch, "leave it open", "Do not merge it",
	} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("what the run is asked does not say %q: %s", phrase, asked)
		}
	}
	// And the line the system adds beside it names the branch too, because the line on every other
	// job says only to push a branch, which leaves the session to choose the name.
	ending := job.EndsOnItsBranch(one, run)
	if !strings.Contains(ending, "from the branch "+branch) {
		t.Fatalf("the task does not tell the run which branch to open from: %s", ending)
	}

	// A job that names no repository has nowhere to push, so it is told about no branch rather than
	// sent looking for a remote that is not there.
	nowhere := buildingJob()
	if on := job.TestExecutions(nowhere, []job.Requirement{requirement})[0]; on.Branch != "" {
		t.Fatalf("a job that works in no repository put its tests on %q", on.Branch)
	}
}

// The build stage opens no pull request. It continues the one its tests are already open in, and it
// is told so in the brief, in the line beside it, and in the ask it gets if it answers without one.
func TestTheBuildStageOpensNoSecondPullRequest(t *testing.T) {
	one := aJobInARepository()
	requirement := job.RequirementsOf(one)[0]
	opened := job.Opened{
		Branch: job.BranchForRequirement(one.ID, requirement), PullRequest: aPullRequestOn(requirement),
	}
	run := job.BuildExecutions(one, []job.Requirement{requirement},
		map[int]job.Opened{requirement.Number: opened})[0]

	told := job.BuildTheVertical(one, requirement, job.FailuresOn(one.Tests)[requirement.Number],
		opened)
	for _, phrase := range []string{
		opened.Branch, opened.PullRequest, "Do not open a second pull request",
	} {
		if !strings.Contains(told, phrase) {
			t.Fatalf("what the run is asked does not say %q: %s", phrase, told)
		}
	}
	ending := job.EndsOnItsBranch(one, run)
	if !strings.Contains(ending, "already open on that branch") ||
		!strings.Contains(ending, "Do not open a second pull request") {
		t.Fatalf("the task tells a build run to open a pull request of its own: %s", ending)
	}
	// The second ask is the moment this matters most: a session that answered without an address is
	// being told what to do about it, and it does as it is told.
	again := job.AskedForThePullRequest(one, run)
	if !strings.Contains(again, "already open") || !strings.Contains(again, "Do not open a second one") {
		t.Fatalf("the second ask sends a build run to open another pull request: %s", again)
	}

	// And the way off it. A run with no branch is one whose tests were written before a requirement
	// had one, and it is asked for what every job was asked for before this.
	old := job.BuildExecutions(one, []job.Requirement{requirement}, nil)[0]
	if !strings.Contains(job.EndsOnItsBranch(one, old), "ends in a pull request against it. Push your branch") {
		t.Fatalf("a run with no branch is not asked for a pull request: %s", job.EndsOnItsBranch(one, old))
	}
	if !strings.Contains(job.AskedForThePullRequest(one, old), "open the pull request") {
		t.Fatalf("a run with no branch is not asked to open one: %s",
			job.AskedForThePullRequest(one, old))
	}
}

// A worker that pushed nothing is a failed worker, not a quiet pass. Its report can be perfect and
// the tests it describes are gone with the sandbox.
func TestARunWhoseTestsReachedNoBranchIsRefused(t *testing.T) {
	one := aJobInARepository()
	requirement := job.RequirementsOf(one)[0]
	run := job.TestExecutions(one, []job.Requirement{requirement})[0]
	run.Phase, run.Answer = job.PhaseDone, aTestReport(requirement.Number)

	_, why := job.ReportFrom(one, []*job.Execution{run}, requirement)
	if why == "" {
		t.Fatal("a run that opened no pull request closed the stage on a report about files nobody has")
	}
	for _, phrase := range []string{"opened no pull request", "reached no branch"} {
		if !strings.Contains(why, phrase) {
			t.Fatalf("the refusal is %q, want it to say %q", why, phrase)
		}
	}

	// The same run, having pushed. Nothing else about it changed.
	run.PullRequest = aPullRequestOn(requirement)
	if report, why := job.ReportFrom(one, []*job.Execution{run}, requirement); why != "" {
		t.Fatalf("a run that opened its pull request is refused: %s", why)
	} else if report.Requirement != requirement.Number {
		t.Fatalf("the report is filed under requirement %d", report.Requirement)
	}
}

// aTestReport is what a worker answers with: the requirement it wrote for, the run it made, and the
// test that fails now.
func aTestReport(requirement int) string {
	return fmt.Sprintf("I wrote the tests for requirement %d.\n\nRequirement: %d\nRan: 12\n"+
		"Failing 1: TestRequirement%dFailsUntilSomethingBuildsIt", requirement, requirement, requirement)
}
