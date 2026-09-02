package publish_test

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/publish"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

// Two sessions delivering onto one branch, against real git and a real remote.
//
// The stage that writes the failing tests fans out. Each worker holds a container of its own with a
// clone of its own, and what all of them wrote has to arrive in one place for the stage after them
// to read. So these drive the whole road rather than asserting that push was called: a bare
// repository stands in for the forge, each session is a clone in a directory of its own, and what
// the test reads at the end is the files on the branch.

// aRemote is a bare repository with one commit on its default branch, which is what every session
// below clones.
func aRemote(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	remote := filepath.Join(dir, "remote.git")
	run(t, dir, "git", "init", "--bare", "--initial-branch=main", remote)
	first := filepath.Join(dir, "first")
	run(t, dir, "git", "clone", remote, first)
	write(t, filepath.Join(first, "README.md"), "the product")
	run(t, first, "git", "add", "README.md")
	run(t, first, "git", "commit", "-m", "the first commit")
	run(t, first, "git", "push", "origin", "main")
	return remote
}

// aSession is one worker's container: a clone of the remote, on a branch of its own, in a directory
// nothing else writes to.
type aSession struct {
	dir   string
	place sandbox.Place
}

func aWorker(t *testing.T, remote, named string) *aSession {
	t.Helper()
	dir := filepath.Join(t.TempDir(), named)
	run(t, filepath.Dir(dir), "git", "clone", remote, dir)
	run(t, dir, "git", "switch", "-c", named)
	return &aSession{dir: dir, place: sandbox.Place{Dir: dir, Sandbox: dir, Host: dir}}
}

// writes is the worker writing a test file and committing it, which is the whole of what this stage
// asks one to do.
func (s *aSession) writes(t *testing.T, name, body string) {
	t.Helper()
	write(t, filepath.Join(s.dir, name), body)
	run(t, s.dir, "git", "add", name)
	run(t, s.dir, "git", "commit", "-m", "write "+name)
}

func (s *aSession) delivers(t *testing.T, branch string) publish.Work {
	t.Helper()
	return publish.Deliver(context.Background(), realGit{}, s.place, branch)
}

// The whole road, and the case the stage exists for. Two workers write different files at the same
// time, both deliver onto one branch, and a reader of that branch afterwards holds both files.
func TestTwoSessionsWritingDifferentFilesBothReachTheBranch(t *testing.T) {
	remote := aRemote(t)
	branch := "krewe/tests/9f2a"
	one := aWorker(t, remote, "requirement-1")
	one.writes(t, "basket_test.go", "func TestABasketHoldsWhatWasPutInIt(t *testing.T) {}")
	two := aWorker(t, remote, "requirement-2")
	two.writes(t, "checkout_test.go", "func TestCheckoutRefusesAnEmptyBasket(t *testing.T) {}")

	first := one.delivers(t, branch)
	second := two.delivers(t, branch)

	for at, found := range []publish.Work{first, second} {
		if found.State != publish.Pushed || !found.Pushed {
			t.Fatalf("delivery %d is %q saying %q, want the work pushed onto the branch",
				at+1, found.State, found.Why)
		}
		if found.Branch != branch {
			t.Fatalf("delivery %d names the branch %q, want %q", at+1, found.Branch, branch)
		}
	}
	// The branch as somebody reading it later holds it, which is the assertion that matters: the
	// second delivery replayed onto the first rather than taking it away.
	held := onTheBranch(t, remote, branch)
	for name, body := range map[string]string{
		"basket_test.go":   "func TestABasketHoldsWhatWasPutInIt(t *testing.T) {}",
		"checkout_test.go": "func TestCheckoutRefusesAnEmptyBasket(t *testing.T) {}",
	} {
		if held[name] != body {
			t.Fatalf("the branch holds %q for %s, want what the worker wrote", held[name], name)
		}
	}
	if held["README.md"] != "the product" {
		t.Fatalf("the branch lost the commit it was cut from: %v", held)
	}
}

// A worker that wrote no file. This is the false pass the stage has to refuse: its branch exists, a
// push of it would succeed, and the branch would be the base under another name.
func TestASessionThatCommittedNothingDeliversNothingAndNamesNoBranch(t *testing.T) {
	remote := aRemote(t)
	quiet := aWorker(t, remote, "requirement-1")

	found := quiet.delivers(t, "krewe/tests/9f2a")

	if found.State != publish.Nothing {
		t.Fatalf("a session that committed nothing delivered %q, want nothing", found.State)
	}
	if found.Branch != "" {
		t.Fatalf("the delivery names the branch %q for work nobody did", found.Branch)
	}
	if onTheRemote(t, remote, "krewe/tests/9f2a") {
		t.Fatalf("an empty branch reached the remote, which reads as a delivery")
	}
}

// A worker that pushed a branch of its own before it was delivered. Its commits are on a remote, so
// reading them against every remote ref would say it did nothing, and the requirement it holds would
// be refused for work that is sitting there.
func TestASessionThatPushedItsOwnBranchStillDelivers(t *testing.T) {
	remote := aRemote(t)
	branch := "krewe/tests/9f2a"
	worker := aWorker(t, remote, "requirement-1")
	worker.writes(t, "basket_test.go", "func TestABasketHoldsWhatWasPutInIt(t *testing.T) {}")
	run(t, worker.dir, "git", "push", "origin", "requirement-1")

	found := worker.delivers(t, branch)

	if found.State != publish.Pushed {
		t.Fatalf("the delivery is %q saying %q, want the work on the branch", found.State, found.Why)
	}
	if held := onTheBranch(t, remote, branch); held["basket_test.go"] == "" {
		t.Fatalf("the branch does not hold the file the worker wrote: %v", held)
	}
}

// Delivering twice is the second controller ticking, and it must not read as a push that did not
// happen: the work is already there, and saying the system put it there sends somebody looking for a
// moment that is not in the record.
func TestASecondDeliveryOfTheSameWorkSaysTheWorkIsAlreadyThere(t *testing.T) {
	remote := aRemote(t)
	branch := "krewe/tests/9f2a"
	worker := aWorker(t, remote, "requirement-1")
	worker.writes(t, "basket_test.go", "func TestABasketHoldsWhatWasPutInIt(t *testing.T) {}")
	worker.delivers(t, branch)

	again := worker.delivers(t, branch)

	if again.State != publish.Pushed {
		t.Fatalf("the second delivery is %q saying %q, want the work on the branch", again.State, again.Why)
	}
	if again.Pushed {
		t.Fatalf("the second delivery claims a push the system did not make")
	}
}

// A remote that will not take it. The reason carries what git said, because every refusal exits the
// same way and the operator needs to know which of them happened.
func TestWorkARemoteWouldNotTakeIsHeldWithWhatGitSaid(t *testing.T) {
	remote := aRemote(t)
	worker := aWorker(t, remote, "requirement-1")
	worker.writes(t, "basket_test.go", "func TestABasketHoldsWhatWasPutInIt(t *testing.T) {}")
	if err := os.RemoveAll(remote); err != nil {
		t.Fatal(err)
	}

	found := worker.delivers(t, "krewe/tests/9f2a")

	if found.State != publish.Held {
		t.Fatalf("the delivery is %q, want held", found.State)
	}
	if !strings.Contains(strings.ToLower(found.Why), "does not appear to be a git repository") {
		t.Fatalf("the reason is %q, want it to carry what git said", found.Why)
	}
}

// A session with no container is read as exactly that, never as a session with no work.
func TestASessionWithNoContainerIsUnreadableRatherThanEmpty(t *testing.T) {
	found := publish.Deliver(context.Background(), nil, sandbox.Place{Host: "/qdata/x"}, "krewe/tests/9f2a")

	if found.State != publish.Unreadable {
		t.Fatalf("a session with no container reads as %q, want unreadable", found.State)
	}
}

// realGit runs the commands in the directory the place names, which is what a bind mount makes true
// inside a container: the path the system asks for is the path the work is on.
type realGit struct{}

func (realGit) Exec(ctx context.Context, spec sandbox.Spec) (sandbox.Process, error) {
	command := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	command.Dir = spec.Workdir
	command.Env = closedGit(spec.Workdir)
	out, err := command.Output()
	said := ""
	var exited *exec.ExitError
	if errors.As(err, &exited) {
		said = string(exited.Stderr)
	}
	return ran{out: string(out), stderr: said, err: err}, nil
}

func (realGit) Close(context.Context) error { return nil }

type ran struct {
	out    string
	stderr string
	err    error
}

func (r ran) Stdout() io.Reader { return strings.NewReader(r.out) }
func (r ran) Wait() error       { return r.err }
func (r ran) Stderr() string    { return r.stderr }

// closedGit is an environment where git reads this test's configuration and never the operator's.
func closedGit(dir string) []string {
	return []string{
		"HOME=" + dir, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=the operator", "GIT_AUTHOR_EMAIL=operator@example.com",
		"GIT_COMMITTER_NAME=the operator", "GIT_COMMITTER_EMAIL=operator@example.com",
		"PATH=" + os.Getenv("PATH"),
	}
}

// onTheBranch is every file that branch holds on the remote, read out of a fresh clone the way the
// next stage's worker would.
func onTheBranch(t *testing.T, remote, branch string) map[string]string {
	t.Helper()
	at := filepath.Join(t.TempDir(), "reader")
	run(t, filepath.Dir(at), "git", "clone", remote, at)
	run(t, at, "git", "switch", branch)
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

func onTheRemote(t *testing.T, remote, branch string) bool {
	t.Helper()
	command := exec.Command("git", "--git-dir", remote, "rev-parse", "--verify", "--quiet",
		"refs/heads/"+branch)
	command.Env = closedGit(remote)
	return command.Run() == nil
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = dir
	command.Env = closedGit(dir)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v: %s", name, strings.Join(args, " "), err, out)
	}
}

func write(t *testing.T, at, body string) {
	t.Helper()
	if err := os.WriteFile(at, []byte(body+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}
}
