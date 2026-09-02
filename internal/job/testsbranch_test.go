package job_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/publish"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

// The tests one stage writes reaching the stage that builds against them.
//
// These drive the road rather than the call. A bare repository stands in for the forge, each worker
// is a real clone in a directory of its own, and what the assertions read at the end are the files
// in a build worker's checkout. Asserting that the system asked for a push would have checked the
// easy half: the half that decides whether this works is whether the next worker can open the file.

// aFanOut is one job in a repository, its workers, and the remote all of them share.
type aFanOut struct {
	controller *job.Controller
	kept       *rows
	plane      *system
	publisher  *gitPublisher
	job        *job.Job
	remote     string
}

// inTheTestStage is a job whose list a person accepted, in a repository. Its plan is not approved
// yet: a job that holds one is past this stage, so the road to the build stage below goes through
// the plan the way a real job does.
func inTheTestStage(t *testing.T) *aFanOut {
	t.Helper()
	kept, plane := newRows(), newSystem()
	publisher := aGitPublisher(t)
	controller := job.NewController(kept, plane, nil, nil, nil).Publishing(publisher)
	one := testingJob()
	one.Repository = "atlantic-blue/quay-krewe"
	return &aFanOut{
		controller: controller, kept: kept, plane: plane, publisher: publisher,
		job: kept.add(one), remote: publisher.remote,
	}
}

// intoTheBuildStage is the road between the two stages, walked the way a job walks it: the suite is
// red, so the job is asked for its plan, and a person approves it. The build stage opens on the tick
// after that.
func (f *aFanOut) intoTheBuildStage(t *testing.T, ctx context.Context) {
	t.Helper()
	f.controller.Tick(ctx)
	f.plane.lands(aPlan)
	f.controller.Tick(ctx)
	f.kept.approvePlan(f.job.ID)
	f.controller.Tick(ctx)
}

// theTestWorkers is the workers the fan out declared, started, and given a session each.
func (f *aFanOut) theTestWorkers(t *testing.T, ctx context.Context) []*job.Job {
	t.Helper()
	f.controller.Tick(ctx)
	f.controller.Tick(ctx)
	workers := f.kept.children(f.job.ID)
	if len(workers) != 2 {
		t.Fatalf("the fan out declared %d workers for 2 requirements", len(workers))
	}
	return workers
}

// writesAndAnswers is one worker doing what its brief asks: it writes a test file in the clone its
// own session holds, commits it, and answers with its report.
func (f *aFanOut) writesAndAnswers(t *testing.T, worker *job.Job, at int, name, body string) {
	t.Helper()
	f.publisher.aClone(t, f.sessionOf(t, worker)).writes(t, name, body)
	f.plane.landsIn(job.SessionFor(worker.ID), landed(theTestReport(at)))
}

// sessionOf is the session the controller gave one worker, read off the row rather than built from
// the identifier: it is the string the publisher is asked about, and a test that guessed it would
// pass while nothing was ever delivered.
func (f *aFanOut) sessionOf(t *testing.T, worker *job.Job) string {
	t.Helper()
	session := f.kept.get(worker.ID).Session
	if session == "" {
		t.Fatalf("worker %q holds no session, so nothing wrote its tests", worker.Title)
	}
	return session
}

// theTestReport is what a worker answers with, naming a pull request because a job in a repository
// is asked for one and this test is not about that rule.
func theTestReport(requirement int) string {
	return "I wrote the tests and ran the suite.\n\n" +
		"Requirement: " + itoa(requirement) + "\nRan: 12\n" +
		"Failing 1: TestRequirement" + itoa(requirement) + "FailsUntilSomethingBuildsIt\n\n" +
		"https://github.com/atlantic-blue/quay-krewe/pull/700"
}

// The whole road, and the fault this answers. Two workers write their tests in sandboxes of their
// own, and the worker that builds afterwards has both files in front of it.
func TestTheTestsTwoWorkersWroteAreInTheBuildWorkersCheckout(t *testing.T) {
	fan := inTheTestStage(t)
	ctx := context.Background()

	workers := fan.theTestWorkers(t, ctx)
	fan.writesAndAnswers(t, workers[0], 1, "basket_test.go",
		"func TestABasketHoldsWhatWasPutInIt(t *testing.T) { t.Fatal(\"nothing builds this yet\") }")
	fan.writesAndAnswers(t, workers[1], 2, "checkout_test.go",
		"func TestCheckoutRefusesAnEmptyBasket(t *testing.T) { t.Fatal(\"nothing builds this yet\") }")
	fan.controller.Tick(ctx)

	// The stage closed, so the record is on the row and it says where the tests are.
	got := fan.kept.get(fan.job.ID)
	if got.Tests == "" {
		t.Fatalf("the workers answered and the stage did not close: %s", got.Reason)
	}
	branch := job.TestBranch(got)
	if !strings.Contains(got.Tests, "Branch: "+branch) {
		t.Fatalf("the record does not say where the tests are:\n%s", got.Tests)
	}

	// The build stage fans out, and its worker is told the branch.
	fan.intoTheBuildStage(t, ctx)
	builders := theBuildersOf(t, fan)
	if len(builders) != 2 {
		t.Fatalf("the build stage declared %d workers for 2 verticals", len(builders))
	}
	if !strings.Contains(builders[0].Brief, branch) {
		t.Fatalf("the build worker's brief does not name the branch its tests are on:\n%s",
			builders[0].Brief)
	}

	// And the assertion the whole change is for: the files are in the checkout that worker gets, read
	// out of a clone of the branch the way the brief tells it to take one.
	checkout := theCheckoutOf(t, fan.remote, branch)
	for name, body := range map[string]string{
		"basket_test.go":   "func TestABasketHoldsWhatWasPutInIt(t *testing.T) { t.Fatal(\"nothing builds this yet\") }",
		"checkout_test.go": "func TestCheckoutRefusesAnEmptyBasket(t *testing.T) { t.Fatal(\"nothing builds this yet\") }",
	} {
		if checkout[name] != body {
			t.Fatalf("the build worker's checkout holds %q for %s, want the test the worker wrote:\n%v",
				checkout[name], name, checkout)
		}
	}
}

// A worker that wrote no file. Its report reads exactly like the others, and nothing it was asked to
// write can be read by anybody, so the stage does not close on it.
func TestAWorkerWhoseTestsReachedNoBranchStopsTheStageForAPerson(t *testing.T) {
	fan := inTheTestStage(t)
	ctx := context.Background()

	workers := fan.theTestWorkers(t, ctx)
	fan.writesAndAnswers(t, workers[0], 1, "basket_test.go",
		"func TestABasketHoldsWhatWasPutInIt(t *testing.T) { t.Fatal(\"nothing builds this yet\") }")
	// The second answers a report the system can read, out of a clone it committed nothing to.
	fan.publisher.aClone(t, fan.sessionOf(t, workers[1]))
	fan.plane.landsIn(job.SessionFor(workers[1].ID), landed(theTestReport(2)))
	fan.controller.Tick(ctx)

	got := fan.kept.get(fan.job.ID)
	if got.Tests != "" {
		t.Fatalf("the stage closed on a requirement whose tests reached no branch:\n%s", got.Tests)
	}
	if got.Phase != job.PhaseAsking {
		t.Fatalf("the job is %q, want asking: %s", got.Phase, got.Reason)
	}
	for _, want := range []string{"requirement 2", job.TestBranch(got), "committed nothing"} {
		if !strings.Contains(got.Question, want) {
			t.Fatalf("the question does not say %q:\n%s", want, got.Question)
		}
	}
	// And the work of the worker that did write is on the branch either way, so nothing is lost by
	// the stage stopping here.
	if held := theCheckoutOf(t, fan.remote, job.TestBranch(got)); held["basket_test.go"] == "" {
		t.Fatalf("the branch lost the tests the first worker wrote: %v", held)
	}
}

// A system that cannot reach a session's files closes nothing either. A stage that read that as a
// pass would be a stage with no evidence at all, which is the state this replaced.
func TestAStageThatCannotReachTheFilesDoesNotClose(t *testing.T) {
	kept, plane := newRows(), newSystem()
	controller := job.NewController(kept, plane, nil, nil, nil)
	one := testingJob()
	one.Repository = "atlantic-blue/quay-krewe"
	kept.add(one)
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	for at, worker := range kept.children(one.ID) {
		plane.landsIn(job.SessionFor(worker.ID), landed(theTestReport(at+1)))
	}
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Tests != "" {
		t.Fatalf("the stage closed with nothing able to say the tests reached a branch:\n%s", got.Tests)
	}
	if !strings.Contains(got.Question, "no way to reach a session's files") {
		t.Fatalf("the question is %q, want it to say the system could not put them there", got.Question)
	}
}

// A job with no repository has no remote, so its tests go nowhere and never did. The stage is left
// as it was rather than refusing every requirement of it.
func TestAJobWithNoRepositoryIsNotHeldToABranch(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(testingJob())
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)
	for at, worker := range kept.children(one.ID) {
		plane.landsIn(job.SessionFor(worker.ID), landed(theTestReport(at+1)))
	}
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Tests == "" {
		t.Fatalf("a job with no repository was refused for a branch it could never have: %s", got.Reason)
	}
	if strings.Contains(got.Tests, "Branch:") {
		t.Fatalf("the record names a branch for a job with no remote:\n%s", got.Tests)
	}
}

// The boundary the build stage works under still holds over the tests it now has in front of it. The
// files arrived, and reading them is the point of them arriving, so what must not change is that the
// worker cannot write to one.
func TestTheBuildWorkerHoldsTheTestsItMayNotChange(t *testing.T) {
	fan := inTheTestStage(t)
	ctx := context.Background()

	workers := fan.theTestWorkers(t, ctx)
	fan.writesAndAnswers(t, workers[0], 1, "basket_test.go", "func TestABasketHolds(t *testing.T) {}")
	fan.writesAndAnswers(t, workers[1], 2, "checkout_test.go", "func TestCheckoutRefuses(t *testing.T) {}")
	fan.intoTheBuildStage(t, ctx)

	branch := job.TestBranch(fan.kept.get(fan.job.ID))
	for name := range theCheckoutOf(t, fan.remote, branch) {
		if name == "README.md" {
			continue
		}
		// The same rule the gate refuses a write by, over the files that actually arrived rather than
		// over a name a test made up.
		if !job.ATest(name) {
			t.Fatalf("%s came off the branch and does not read as a test, so nothing stops a build "+
				"worker writing to it", name)
		}
	}
	for _, builder := range theBuildersOf(t, fan) {
		if !builder.Building {
			t.Fatalf("the worker building %q is outside the boundary, so it may change a test",
				builder.Title)
		}
		if !strings.Contains(builder.Brief, "You may not change one") {
			t.Fatalf("the build brief does not say the tests may not be changed:\n%s", builder.Brief)
		}
	}
}

// theBuildersOf is the workers the build stage declared, by the claim each holds. The test stage's
// workers are under the same parent and answered a different question.
func theBuildersOf(t *testing.T, fan *aFanOut) []*job.Job {
	t.Helper()
	one := fan.kept.get(fan.job.ID)
	building := map[string]bool{}
	for _, vertical := range job.RequirementsOf(one) {
		building[job.ClaimOnBuild(one.ID, vertical)] = true
	}
	var builders []*job.Job
	for _, worker := range fan.kept.children(one.ID) {
		if building[worker.Claim] {
			builders = append(builders, worker)
		}
	}
	return builders
}

// gitPublisher is the control plane's publisher over real git: a bare repository standing in for the
// forge, and a clone for each session that asks for one.
type gitPublisher struct {
	remote string
	dir    string
	clones map[string]*aClone
}

func aGitPublisher(t *testing.T) *gitPublisher {
	t.Helper()
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	gitIn(t, dir, "init", "--bare", "--initial-branch=main", remote)
	first := filepath.Join(dir, "first")
	gitIn(t, dir, "clone", remote, first)
	writeFile(t, filepath.Join(first, "README.md"), "the product")
	gitIn(t, first, "add", "README.md")
	gitIn(t, first, "commit", "-m", "the first commit")
	gitIn(t, first, "push", "origin", "main")
	return &gitPublisher{remote: remote, dir: dir, clones: map[string]*aClone{}}
}

// aClone is one session's container: a clone of the remote on a branch of its own.
func (p *gitPublisher) aClone(t *testing.T, session string) *aClone {
	t.Helper()
	if held, made := p.clones[session]; made {
		return held
	}
	at := filepath.Join(p.dir, session)
	gitIn(t, p.dir, "clone", p.remote, at)
	gitIn(t, at, "switch", "-c", session)
	made := &aClone{dir: at}
	p.clones[session] = made
	return made
}

func (p *gitPublisher) PublishSessionWork(ctx context.Context, session string) publish.Work {
	return p.PushSessionWork(ctx, session, "")
}

func (p *gitPublisher) PushSessionWork(ctx context.Context, session, branch string) publish.Work {
	held, made := p.clones[session]
	if !made {
		return publish.Work{State: publish.Absent}
	}
	place := sandbox.Place{Dir: held.dir, Sandbox: held.dir, Host: held.dir}
	return publish.Deliver(ctx, sessionGit{}, place, branch)
}

type aClone struct{ dir string }

func (c *aClone) writes(t *testing.T, name, body string) {
	t.Helper()
	writeFile(t, filepath.Join(c.dir, name), body)
	gitIn(t, c.dir, "add", name)
	gitIn(t, c.dir, "commit", "-m", "write "+name)
}

// theCheckoutOf is every file a worker holds once it has taken the branch, read out of a clone the
// way the build brief tells it to take one.
func theCheckoutOf(t *testing.T, remote, branch string) map[string]string {
	t.Helper()
	at := filepath.Join(t.TempDir(), "builder")
	gitIn(t, filepath.Dir(at), "clone", remote, at)
	gitIn(t, at, "fetch", "origin", branch)
	gitIn(t, at, "switch", "-c", "building-vertical-1", "origin/"+branch)
	held := map[string]string{}
	entries, err := os.ReadDir(at)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := os.ReadFile(filepath.Join(at, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		held[entry.Name()] = strings.TrimSpace(string(body))
	}
	return held
}

// sessionGit runs a command where the place says the work is, which is what a bind mount makes true
// inside a container.
type sessionGit struct{}

func (sessionGit) Exec(ctx context.Context, spec sandbox.Spec) (sandbox.Process, error) {
	command := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	command.Dir = spec.Workdir
	command.Env = aClosedGit(spec.Workdir)
	out, err := command.Output()
	said := ""
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		said = string(exited.Stderr)
	}
	return sessionRan{out: string(out), stderr: said, err: err}, nil
}

func (sessionGit) Close(context.Context) error { return nil }

type sessionRan struct {
	out    string
	stderr string
	err    error
}

func (r sessionRan) Stdout() io.Reader { return strings.NewReader(r.out) }
func (r sessionRan) Wait() error       { return r.err }
func (r sessionRan) Stderr() string    { return r.stderr }

func aClosedGit(dir string) []string {
	return []string{
		"HOME=" + dir, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=the operator", "GIT_AUTHOR_EMAIL=operator@example.com",
		"GIT_COMMITTER_NAME=the operator", "GIT_COMMITTER_EMAIL=operator@example.com",
		"PATH=" + os.Getenv("PATH"),
	}
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = aClosedGit(dir)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func writeFile(t *testing.T, at, body string) {
	t.Helper()
	if err := os.WriteFile(at, []byte(body+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}
}
