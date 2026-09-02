package publish

import (
	"context"
	"fmt"
	"strings"

	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

// Delivering work onto one branch that several sessions write at the same time.
//
// Read, above, pushes what a session left on the branch the session chose, which is the whole answer
// for a job with one session. A stage that fans out has five, each in a container of its own with a
// clone of its own, and the work of all five has to arrive in one place for the stage after it to
// read. Five branches would need something to merge them, and nothing in this system holds git
// outside a container.
//
// So the branch belongs to the job rather than to the session, and each session's commits are put
// on top of what is already there. The remote decides who was first: a push that is refused as
// behind is answered by fetching that branch, replaying this session's commits onto it, and pushing
// again. Two sessions that wrote different files both survive it, which is the case this exists for.

// Attempts is how many times a delivery replays onto the branch before it gives up.
//
// One for the push, and two more for the sessions that were ahead. It is small on purpose: a
// delivery that keeps losing the race is a branch something else is writing continuously, and
// answering that with a longer loop inside a container hides it rather than reporting it.
const Attempts = 3

// Deliver puts what one session committed onto the branch this job's work belongs on, and says what
// it found.
//
// The states are Read's, so a caller reads one vocabulary: Nothing where the session committed
// nothing of its own, Absent where it holds no repository, Held where the remote would not take it,
// and Pushed where the work is on the branch. A session that wrote no file is Nothing rather than a
// quiet success, which is the difference between a stage that closes on work and one that closes on
// a report.
func Deliver(ctx context.Context, box Runner, place sandbox.Place, branch string) Work {
	found := Work{Host: place.Host, Branch: branch}
	if box == nil {
		found.State, found.Why = Unreadable,
			"the session has no container, so the system could not read or push its work"
		return found
	}
	if branch == "" {
		found.State, found.Why = Held, "this job names no branch for its work to go on"
		return found
	}
	// Whether this session committed anything of its own, asked first. It is asked of git rather than
	// of the model, and it is the one question that tells work that was done from work that was not: a
	// session that wrote no file still holds a branch, and putting that on the remote would read as a
	// delivery. It is asked before the branch is looked at for the same reason. The base is an
	// ancestor of the branch by construction, so a session that committed nothing is on the branch in
	// the sense git means and holds none of the work in the sense anybody cares about.
	made, err := committed(ctx, box, place)
	if err != nil {
		found.State, found.Why = Unreadable,
			oneLine("the system could not read what this session committed: "+err.Error())
		return found
	}
	if made == "" {
		found.State, found.Branch = Nothing, ""
		return found
	}
	// Then whether the branch already carries it, so a second pass over a delivery that already
	// happened says the work is there rather than claiming a push nobody made.
	if onIt(ctx, box, place, branch) {
		found.State = Pushed
		return found
	}
	for attempt := 1; ; attempt++ {
		_, err := git(ctx, box, place, "push", "origin", "HEAD:refs/heads/"+branch)
		if err == nil {
			found.State, found.Pushed = Pushed, true
			return found
		}
		if attempt == Attempts || !behind(err) {
			found.State, found.Why = Held, whyItRefused(err.Error())
			return found
		}
		// Somebody else reached the branch first, so this session's commits are replayed onto theirs
		// and pushed again. The rebase is the system's rather than the model's: a session told to
		// resolve a race it cannot see writes the file twice or takes the other worker's away.
		if err := replay(ctx, box, place, branch); err != nil {
			found.State, found.Why = Held, whyItRefused(err.Error())
			return found
		}
	}
}

// committed is a commit this session made that the work it started from does not have, and empty
// where it made none.
//
// The base is what the clone was cut from, which a clone always knows as origin/HEAD. It is that
// rather than every remote ref, because a session that pushed a branch of its own has its commits on
// a remote already, and reading those as nothing would refuse a worker that did the work. A tree
// that has no origin/HEAD is read against every remote instead, which is the same question with a
// wider net.
func committed(ctx context.Context, box Runner, place sandbox.Place) (string, error) {
	against := "--remotes"
	if base, err := git(ctx, box, place, "rev-parse", "--verify", "--quiet", "origin/HEAD"); err == nil &&
		base != "" {
		against = base
	}
	return git(ctx, box, place, "log", "--format=%H", "--max-count=1", "HEAD", "--not", against)
}

// onIt says whether the branch on the remote already carries what this session is holding.
func onIt(ctx context.Context, box Runner, place sandbox.Place, branch string) bool {
	if _, err := git(ctx, box, place, "fetch", "origin", branch); err != nil {
		return false
	}
	_, err := git(ctx, box, place, "merge-base", "--is-ancestor", "HEAD", "FETCH_HEAD")
	return err == nil
}

// replay fetches the branch and puts this session's commits on top of it.
//
// A rebase that cannot be finished leaves the working tree mid replay, so it is abandoned rather
// than left there: the reason then names the conflict, and the session's own branch is where it was.
func replay(ctx context.Context, box Runner, place sandbox.Place, branch string) error {
	if _, err := git(ctx, box, place, "fetch", "origin", branch); err != nil {
		return fmt.Errorf("the system could not read the branch %s: %w", branch, err)
	}
	if _, err := git(ctx, box, place, "rebase", "FETCH_HEAD"); err != nil {
		_, _ = git(ctx, box, place, "rebase", "--abort")
		return fmt.Errorf("this session's work does not replay onto %s: %w", branch, err)
	}
	return nil
}

// behind says whether a push was refused because the branch moved under it, which is the one refusal
// worth answering with a replay. Every other one is a remote that will not take this work however
// often it is offered.
func behind(err error) bool {
	said := strings.ToLower(err.Error())
	for _, refusal := range []string{"non-fast-forward", "fetch first", "behind", "rejected"} {
		if strings.Contains(said, refusal) {
			return true
		}
	}
	return false
}

// whyItRefused is the line of git's answer that says what happened.
//
// A push writes "To <remote>" first and the refusal underneath it, so the first line of a failed
// push names the remote and nothing else. An operator reading a reason needs the refusal, so the
// line carrying it is the one kept, and the first line is what stands where none of them does.
func whyItRefused(said string) string {
	for _, line := range strings.Split(said, "\n") {
		line = strings.TrimSpace(line)
		for _, marks := range []string{"rejected", "error:", "fatal:", "remote:"} {
			if strings.Contains(strings.ToLower(line), marks) {
				return line
			}
		}
	}
	return oneLine(said)
}
