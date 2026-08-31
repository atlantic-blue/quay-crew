package publish_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/atlantic-blue/krewe/internal/publish"
	"github.com/atlantic-blue/krewe/internal/sandbox"
)

// What the system finds when it goes looking for the work a session finished, and what it does about
// it. The empty cases come first, because those are the ones a reason has to get right: a reason that
// names a branch nobody made sends the operator looking for work that was never done.

// aSessionWhoseGitSays is a container answering the git commands the system runs, and recording them.
// The answers are keyed on the command, because the whole behaviour is the system telling four
// outcomes apart by asking git different questions.
type aSessionWhoseGitSays struct {
	branch string
	// unpublished is what `git log ... --not --remotes` says: a commit identifier means there is work
	// on the branch that no remote has.
	unpublished string
	// onTheRemote says whether refs/remotes/origin/<branch> is there.
	onTheRemote bool
	// pushFails is what git says when the push is refused, and empty when it goes.
	pushFails string
	// broken is a git that cannot answer at all.
	broken string
	ran    []string
}

func (s *aSessionWhoseGitSays) Exec(_ context.Context, spec sandbox.Spec) (sandbox.Process, error) {
	line := strings.Join(spec.Argv, " ")
	s.ran = append(s.ran, line)
	if s.broken != "" {
		return answer{err: errors.New("exit status 128"), stderr: s.broken}, nil
	}
	switch {
	case strings.Contains(line, "rev-parse --abbrev-ref HEAD"):
		return answer{out: s.branch}, nil
	case strings.Contains(line, "--not --remotes"):
		return answer{out: s.unpublished}, nil
	case strings.Contains(line, "refs/remotes/origin/"):
		if s.onTheRemote {
			return answer{out: "a9f1c2d"}, nil
		}
		return answer{err: errors.New("exit status 1")}, nil
	case strings.Contains(line, "push"):
		if s.pushFails != "" {
			return answer{err: errors.New("exit status 128"), stderr: s.pushFails}, nil
		}
		return answer{}, nil
	}
	return answer{}, nil
}

func (s *aSessionWhoseGitSays) pushed() bool {
	for _, line := range s.ran {
		if strings.Contains(line, "push") {
			return true
		}
	}
	return false
}

type answer struct {
	out    string
	stderr string
	err    error
}

func (a answer) Stdout() io.Reader { return strings.NewReader(a.out) }
func (a answer) Wait() error       { return a.err }
func (a answer) Stderr() string    { return a.stderr }

var aPlace = sandbox.Place{
	Dir:     "/data/workspaces/w/projects/p/sessions/s/workspace",
	Host:    "/qdata/workspaces/w/projects/p/sessions/s/workspace",
	Sandbox: "/home/agent/workspace",
}

// The case that matters most. A branch cut from the base and never committed to is not work, and the
// system must not name it: an operator sent to a branch nobody made goes looking for something that
// is not there.
func TestASessionThatCommittedNothingSaysSoAndNamesNoBranch(t *testing.T) {
	git := &aSessionWhoseGitSays{branch: "sort-the-listing"}
	found := publish.Read(context.Background(), git, aPlace)

	if found.State != publish.Nothing {
		t.Fatalf("the system read the work as %q saying %q, want %q",
			found.State, found.Why, publish.Nothing)
	}
	if found.Branch != "" {
		t.Fatalf("it names the branch %q, and there is nothing on it to name", found.Branch)
	}
	if git.pushed() {
		t.Fatalf("it pushed a branch with nothing on it: %v", git.ran)
	}
	if found.Host != aPlace.Host {
		t.Fatalf("it says the work is at %q, want %q", found.Host, aPlace.Host)
	}
}

// A session with no branch checked out. There is nothing for a push to name, so this stops rather
// than inventing one.
func TestAWorkingTreeOnNoBranchIsNotPushed(t *testing.T) {
	git := &aSessionWhoseGitSays{branch: "HEAD", unpublished: "a9f1c2d"}
	found := publish.Read(context.Background(), git, aPlace)

	if found.State != publish.Held {
		t.Fatalf("the system read a detached working tree as %q, want %q", found.State, publish.Held)
	}
	if git.pushed() {
		t.Fatalf("it tried to push with no branch to push to: %v", git.ran)
	}
	if !strings.Contains(found.Why, "not on a branch") {
		t.Fatalf("it says %q, want it to say the session is not on a branch", found.Why)
	}
}

// A push the remote refused. What git said is the whole of the answer: no such remote,
// authentication failed and protected branch are one exit status and three different things to do.
func TestAPushTheRemoteRefusedIsHeldAndCarriesWhatGitSaid(t *testing.T) {
	git := &aSessionWhoseGitSays{
		branch:      "sort-the-listing",
		unpublished: "a9f1c2d",
		pushFails:   "remote: Permission to atlantic-blue/krewe.git denied\nfatal: unable to access",
	}
	found := publish.Read(context.Background(), git, aPlace)

	if found.State != publish.Held {
		t.Fatalf("a refused push read as %q, want %q", found.State, publish.Held)
	}
	if found.Pushed {
		t.Fatalf("it says the system pushed, and the remote refused it")
	}
	if found.Branch != "sort-the-listing" {
		t.Fatalf("it names the branch %q, want the one the work is on", found.Branch)
	}
	// One line, because a reason is read in a listing and git answers in paragraphs.
	if strings.Contains(found.Why, "\n") {
		t.Fatalf("the reason runs to more than one line:\n%s", found.Why)
	}
	if !strings.Contains(found.Why, "Permission to") {
		t.Fatalf("the reason is %q, want it to carry what git said", found.Why)
	}
}

// A git that cannot answer at all is its own state. Reading it as any of the other four would report
// a session as having no work when nobody looked.
func TestAGitThatCannotAnswerIsUnreadableRatherThanEmpty(t *testing.T) {
	git := &aSessionWhoseGitSays{broken: "fatal: detected dubious ownership in repository"}
	found := publish.Read(context.Background(), git, aPlace)

	if found.State != publish.Unreadable {
		t.Fatalf("a git that failed read as %q, want %q", found.State, publish.Unreadable)
	}
	if !strings.Contains(found.Why, "dubious ownership") {
		t.Fatalf("the reason is %q, want it to carry what git said", found.Why)
	}
}

// A session with no container. The system cannot read the work and cannot push it, and that is not
// the same answer as a session that did nothing.
func TestASessionWithNoContainerIsUnreadableAndStillNamesThePath(t *testing.T) {
	found := publish.Read(context.Background(), nil, aPlace)

	if found.State != publish.Unreadable {
		t.Fatalf("a session with no container read as %q, want %q", found.State, publish.Unreadable)
	}
	if found.Host != aPlace.Host {
		t.Fatalf("it says the work is at %q, want %q", found.Host, aPlace.Host)
	}
	if !strings.Contains(found.Why, "no container") {
		t.Fatalf("it says %q, want it to say the session has no container", found.Why)
	}
}

// The happy path, last. Work that was committed and never pushed is pushed by the system, which is
// the whole point: a push applies nothing, so it needs nobody's approval.
func TestWorkTheSessionDidNotPushIsPushedByTheSystem(t *testing.T) {
	git := &aSessionWhoseGitSays{branch: "sort-the-listing", unpublished: "a9f1c2d"}
	found := publish.Read(context.Background(), git, aPlace)

	if found.State != publish.Pushed || !found.Pushed {
		t.Fatalf("the system read the work as %q, pushed=%v, want it pushed", found.State, found.Pushed)
	}
	if found.Branch != "sort-the-listing" {
		t.Fatalf("it names the branch %q, want the one the work is on", found.Branch)
	}
	// The command, exactly, because an upstream is what makes the branch readable by name afterwards.
	var push string
	for _, line := range git.ran {
		if strings.Contains(line, "push") {
			push = line
		}
	}
	if push != "git push --set-upstream origin sort-the-listing" {
		t.Fatalf("the system ran %q", push)
	}
	// And nothing else. A pull request is a decision and a merge spends money, so neither is here.
	for _, line := range git.ran {
		if strings.Contains(line, "merge") || strings.Contains(line, "gh ") {
			t.Fatalf("the system ran %q, and it may only push", line)
		}
	}
}

// A branch the session pushed itself. The work is readable, so the system says so rather than
// reporting that nothing was committed, and it does not claim the push as its own.
func TestABranchTheSessionAlreadyPushedIsReadAsPublishedByIt(t *testing.T) {
	git := &aSessionWhoseGitSays{branch: "sort-the-listing", onTheRemote: true}
	found := publish.Read(context.Background(), git, aPlace)

	if found.State != publish.Pushed {
		t.Fatalf("a branch already on the remote read as %q, want %q", found.State, publish.Pushed)
	}
	if found.Pushed {
		t.Fatalf("the system claims it pushed a branch that was already there")
	}
	if git.pushed() {
		t.Fatalf("it pushed a branch that was already on the remote: %v", git.ran)
	}
}

// Every command runs in the directory the work is in, as the container sees it. A command that ran
// anywhere else would read whatever repository the container happened to open in.
func TestEveryCommandRunsWhereTheWorkIs(t *testing.T) {
	git := &whereItRan{}
	publish.Read(context.Background(), git, aPlace)
	if len(git.dirs) == 0 {
		t.Fatalf("the system ran nothing")
	}
	for _, dir := range git.dirs {
		if dir != aPlace.Sandbox {
			t.Fatalf("a command ran in %q, want %q", dir, aPlace.Sandbox)
		}
	}
}

type whereItRan struct{ dirs []string }

func (w *whereItRan) Exec(_ context.Context, spec sandbox.Spec) (sandbox.Process, error) {
	w.dirs = append(w.dirs, spec.Workdir)
	return answer{out: "sort-the-listing"}, nil
}
