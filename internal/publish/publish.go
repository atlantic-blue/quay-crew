// Package publish is the system putting the work a session finished where somebody else can read it.
//
// The fault it answers: a job whose session did the work and did not open the pull request stopped
// with a sentence telling a person to open the container and push what was inside it. The system was
// holding the work the whole time. It knew the repository, it knew the session, and the bytes were on
// a bind mount it had made itself, so the one thing it did not do was the only thing that would have
// helped.
//
// So the system pushes. A push applies nothing, which is why it needs nobody's approval: it makes the
// branch readable and changes nothing about what is deployed. The pull request and the merge stay
// decisions, and nothing here opens either.
//
// Git runs where git is. The control plane is a static binary with no shell and no git, and the
// credential that reaches the remote is in the session's container rather than in this process, so
// every command below runs inside the sandbox. Where the session has no container, the system cannot
// read the work or push it, and says so with the path, which is the answer an operator can act on.
package publish

import (
	"context"
	"io"
	"strings"

	"github.com/atlantic-blue/krewe/internal/sandbox"
)

// What the system found, and what it did about it. Five, because a reason that cannot tell them apart
// sends somebody looking for something that is not there.
const (
	// Pushed is the branch is on the remote. The system put it there, or the session had already.
	Pushed = "pushed"
	// Held is there are commits and they are on no remote, and the system could not push them.
	Held = "held"
	// Nothing is a repository is there and the session committed nothing of its own to it. This is the
	// case that matters most: a reason naming a branch nobody made sends the operator looking for
	// work that was never done.
	Nothing = "nothing"
	// Absent is the session holds no repository at all.
	Absent = "absent"
	// Unreadable is the system could not look. It is not the same answer as any of the four above and
	// must never be reported as one of them.
	Unreadable = "unreadable"
)

// Work is what a session left behind and what became of it.
type Work struct {
	State string
	// Branch is what the work is on, and empty where there is no branch worth naming.
	Branch string
	// Host is the directory on the machine running the sandboxes. It is the one thing an operator can
	// always act on, so it is filled in on every state that has one, including the states where
	// nothing could be read.
	Host string
	// Pushed says the system did the pushing on this pass, rather than finding the branch already
	// there. Only ever true beside the Pushed state.
	Pushed bool
	// Why is what stopped the push, or what stopped the system reading anything. One line.
	Why string
}

// Runner is a sandbox, narrowed to the one thing this needs. It takes the sandbox rather than a
// command runner of its own because the commands have to run where the session's git and the
// session's credential are, which is inside its container.
type Runner interface {
	Exec(ctx context.Context, spec sandbox.Spec) (sandbox.Process, error)
}

// Read is what a session's git says about its work, and pushes the branch where there is something to
// push and the remote takes it.
//
// The order is what makes the empty case honest. What is on the branch is read first, so a session
// that committed nothing is told apart from one whose branch is already on the remote and from one
// whose push was refused. Pushing before reading would name a branch in all three.
func Read(ctx context.Context, box Runner, place sandbox.Place) Work {
	found := Work{Host: place.Host}
	if box == nil {
		found.State, found.Why = Unreadable, "the session has no container, so the system could not read or push its work"
		return found
	}
	branch, err := git(ctx, box, place, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		found.State, found.Why = Unreadable, oneLine("the system could not read which branch the work is on: "+err.Error())
		return found
	}
	// A working tree with no branch checked out. There is nothing to push to, because a push names a
	// branch, so this stops here rather than inventing one.
	if branch == "" || branch == "HEAD" {
		found.State, found.Why = Held, "the session is not on a branch, so there is no branch to push"
		return found
	}
	found.Branch = branch

	// Commits on this branch that are on no remote. This is the question, and it is asked of git
	// rather than of the model: a branch cut and never committed to answers nothing here, which is
	// what tells work that was done from work that was not.
	unpublished, err := git(ctx, box, place, "log", "--format=%H", "--max-count=1", branch, "--not", "--remotes")
	if err != nil {
		found.State, found.Why = Unreadable, oneLine("the system could not read what is on the branch: "+err.Error())
		return found
	}
	if unpublished == "" {
		// Nothing to push, and two very different reasons for it. A branch that is on the remote is
		// work anybody can read; a branch that is nowhere is work that was never committed.
		if _, err := git(ctx, box, place, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch); err == nil {
			found.State = Pushed
			return found
		}
		found.State, found.Branch = Nothing, ""
		return found
	}

	if _, err := git(ctx, box, place, "push", "--set-upstream", "origin", branch); err != nil {
		found.State, found.Why = Held, oneLine(err.Error())
		return found
	}
	found.State, found.Pushed = Pushed, true
	return found
}

// git runs one command in the session's own container, in the directory the work is in, and hands
// back what it said.
//
// The error carries what git wrote to its error stream, because that is where the reason lives: "no
// such remote", "authentication failed" and "protected branch" are all the same exit status and the
// operator needs to know which of them happened.
func git(ctx context.Context, box Runner, place sandbox.Place, args ...string) (string, error) {
	proc, err := box.Exec(ctx, sandbox.Spec{
		Argv: append([]string{"git"}, args...), Workdir: place.Sandbox,
	})
	if err != nil {
		return "", err
	}
	// Drained before it is waited on. A command whose output nobody reads stops dead as soon as the
	// pipe fills, and git writes more than a pipe holds on the first push of a repository.
	out, _ := io.ReadAll(proc.Stdout())
	if err := proc.Wait(); err != nil {
		said := strings.TrimSpace(proc.Stderr())
		if said == "" {
			said = err.Error()
		}
		return "", &Refusal{Said: said}
	}
	return strings.TrimSpace(string(out)), nil
}

// Refusal is a git command that failed, carrying what git said about it rather than an exit status.
type Refusal struct{ Said string }

func (r *Refusal) Error() string { return r.Said }

// oneLine keeps a reason to one line. A reason is read in a listing, and git answers in paragraphs.
func oneLine(said string) string {
	said = strings.TrimSpace(said)
	if cut := strings.IndexAny(said, "\r\n"); cut >= 0 {
		said = strings.TrimSpace(said[:cut])
	}
	return said
}
