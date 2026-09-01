package features_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/cucumber/godog"
)

// The system publishing what a job left behind, and the operator reading it back.
//
// The session's directory is real: the repository is on disk and the file the operator reads is the
// file the session wrote, so the read goes the whole road rather than agreeing with a double. What is
// scripted is git, because a scenario has to be able to say a session committed nothing, committed
// and did not push, or was refused by the remote, and those are three answers to one question.

// theWorkOfASession is what the scenario put in the session's directory, so a later step can say the
// operator got it back.
type theWorkOfASession struct {
	branch string
	// wrote is the file the scenario left in the repository, against what is in it.
	wrote map[string]string
	// listed is what the operator's listing came back with.
	listed *quaycrewv1.ReadSessionWorkResponse
}

type workKey struct{}

func workFrom(ctx context.Context) *theWorkOfASession {
	found, _ := ctx.Value(workKey{}).(*theWorkOfASession)
	if found == nil {
		return &theWorkOfASession{wrote: map[string]string{}}
	}
	return found
}

func initializePublishingSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, workKey{}, &theWorkOfASession{wrote: map[string]string{}}), nil
	})

	// A session whose git answers the way a session that committed nothing does: it is on a branch,
	// and the branch carries no commit that a remote does not already have.
	sc.Step(`^the session's git is on the branch "([^"]*)" with nothing committed$`,
		func(ctx context.Context, branch string) error {
			return gitInTheSession(ctx, branch, "")
		})

	sc.Step(`^the session's git is on the branch "([^"]*)" with work committed$`,
		func(ctx context.Context, branch string) error {
			return gitInTheSession(ctx, branch, "a9f1c2d4e6")
		})

	sc.Step(`^the remote refuses the push saying "([^"]*)"$`, func(ctx context.Context, said string) error {
		w := worldFrom(ctx)
		w.provider.Replies = append(w.provider.Replies, sandbox.Reply{
			Match: "push", Stderr: said, Err: errors.New("exit status 128"),
		})
		return nil
	})

	sc.Step(`^the session wrote "([^"]*)" saying "([^"]*)"$`,
		func(ctx context.Context, name, body string) error {
			workFrom(ctx).wrote[name] = body
			return nil
		})

	// The two answers that end the job: the work, the ask, and an answer that still names no pull
	// request. The repository is written into the session's directory between them, because a session
	// only has a directory once the system has made one for it.
	sc.Step(`^the session answers twice without a pull request$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.server.TickJob(ctx)
		// The session exists before the scenario writes into its directory: the system makes one when it
		// starts the job, and there is nowhere to put a repository until it has.
		if err := theSessionCloned(ctx); err != nil {
			return err
		}
		for range 2 {
			if err := taskLands(ctx); err != nil {
				return err
			}
			w.server.TickJob(ctx)
		}
		w.server.TickJob(ctx)
		return nil
	})

	sc.Step(`^the job is stopped, and the reason says the session holds no repository$`,
		theReasonSays("no repository"))
	sc.Step(`^the job is stopped, and the reason says the session committed nothing$`,
		theReasonSays("committed nothing"))
	sc.Step(`^the job is stopped, and the reason names the branch "([^"]*)"$`,
		func(ctx context.Context, branch string) error { return theReasonSays(branch)(ctx) })
	sc.Step(`^the job is stopped, and the reason says the system pushed it and one step is left$`,
		theReasonSays("pushed the branch", "open the pull request"))
	sc.Step(`^the reason carries what the remote said$`, theReasonSays("Permission to"))

	// A reason that names a branch nobody made sends the operator looking for work that was never
	// done, which is worse than saying nothing at all.
	sc.Step(`^the reason names no branch$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if strings.Contains(one.GetReason(), workFrom(ctx).branch) {
			return fmt.Errorf("the reason names a branch with nothing on it:\n%s", one.GetReason())
		}
		return nil
	})

	sc.Step(`^the reason says where the work is on the machine$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		dir, err := theSessionsOwnDirectory(ctx)
		if err != nil {
			return err
		}
		if !strings.Contains(one.GetReason(), dir) {
			return fmt.Errorf("the reason is %q, want it to name the directory %s", one.GetReason(), dir)
		}
		if !strings.Contains(one.GetReason(), "krewe read ") {
			return fmt.Errorf("the reason is %q, want it to say how to read the work", one.GetReason())
		}
		return nil
	})

	// The fault, in one line. Every reason this writes has to be something an operator can act on
	// from where they are standing.
	sc.Step(`^the reason never sends anybody into a container$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		for _, word := range []string{"open it", "open the container", "attach", "and push what is there"} {
			if strings.Contains(strings.ToLower(one.GetReason()), word) {
				return fmt.Errorf("the reason says %q, which makes the operator the transport:\n%s",
					word, one.GetReason())
			}
		}
		return nil
	})

	sc.Step(`^the system pushed the branch "([^"]*)"$`, func(ctx context.Context, branch string) error {
		ran, err := whatTheSystemRanInTheSession(ctx)
		if err != nil {
			return err
		}
		want := "git push --set-upstream origin " + branch
		for _, line := range ran {
			if line == want {
				return nil
			}
		}
		return fmt.Errorf("the system ran %v, want %q among them", ran, want)
	})

	// A push applies nothing, so it needs nobody. A pull request is a decision and a merge runs the
	// pipeline, so the system does neither.
	sc.Step(`^the system opened no pull request and merged nothing$`, func(ctx context.Context) error {
		ran, err := whatTheSystemRanInTheSession(ctx)
		if err != nil {
			return err
		}
		for _, line := range ran {
			if strings.Contains(line, "merge") || strings.Contains(line, "gh ") {
				return fmt.Errorf("the system ran %q, and it may only push", line)
			}
		}
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPullRequest() != "" {
			return fmt.Errorf("the job names the pull request %q, and nobody opened one",
				one.GetPullRequest())
		}
		return nil
	})

	sc.Step(`^the operator lists what that session made$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		listed, err := w.client.ReadSessionWork(ctx, &quaycrewv1.ReadSessionWorkRequest{
			Session: one.GetSession(),
		})
		if err != nil {
			return err
		}
		workFrom(ctx).listed = listed
		return nil
	})

	sc.Step(`^the listing names "([^"]*)" and the directory the reason named$`,
		func(ctx context.Context, name string) error {
			listed := workFrom(ctx).listed
			if listed == nil {
				return fmt.Errorf("the operator read nothing")
			}
			if !listed.GetDirectory() {
				return fmt.Errorf("what came back is a file, want the directory the session worked in")
			}
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			// The same directory the reason sent them to. Two answers to "where is the work" is how
			// somebody ends up looking in the wrong one.
			if !strings.Contains(one.GetReason(), listed.GetHost()) {
				return fmt.Errorf("the listing is of %s and the reason names somewhere else:\n%s",
					listed.GetHost(), one.GetReason())
			}
			for _, entry := range listed.GetEntries() {
				if entry.GetName() == name {
					return nil
				}
			}
			return fmt.Errorf("the listing of %s does not hold %q: %v",
				listed.GetHost(), name, listed.GetEntries())
		})

	sc.Step(`^reading "([^"]*)" out of that session gives back what the session wrote$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			read, err := w.client.ReadSessionWork(ctx, &quaycrewv1.ReadSessionWorkRequest{
				Session: one.GetSession(), Path: name,
			})
			if err != nil {
				return err
			}
			want := workFrom(ctx).wrote[name]
			if strings.TrimSpace(string(read.GetContent())) != want {
				return fmt.Errorf("%s reads %q, want %q", name, read.GetContent(), want)
			}
			return nil
		})
}

// gitInTheSession scripts what git inside the session answers, which is how a scenario says a session
// committed nothing, or committed and did not push.
//
// Set before the first task, because a sandbox is given these when it is made.
func gitInTheSession(ctx context.Context, branch, unpublished string) error {
	w := worldFrom(ctx)
	workFrom(ctx).branch = branch
	w.provider.Replies = append(w.provider.Replies,
		sandbox.Reply{Match: "rev-parse --abbrev-ref HEAD", Out: branch},
		sandbox.Reply{Match: "--not --remotes", Out: unpublished},
		// The branch is on no remote, which is what makes the empty case empty rather than published.
		sandbox.Reply{Match: "refs/remotes/origin/", Err: errors.New("exit status 1")},
	)
	return nil
}

// theSessionCloned puts a repository in the session's own working directory, with whatever the
// scenario said the session wrote in it. It is the real disk, because that is what the system reads.
func theSessionCloned(ctx context.Context) error {
	work := workFrom(ctx)
	if work.branch == "" && len(work.wrote) == 0 {
		return nil
	}
	dir, err := theSessionsOwnDirectory(ctx)
	if err != nil {
		return err
	}
	at := filepath.Join(dir, "krewe")
	if err := os.MkdirAll(filepath.Join(at, ".git"), 0o777); err != nil {
		return err
	}
	for name, body := range work.wrote {
		if err := os.WriteFile(filepath.Join(at, name), []byte(body+"\n"), 0o666); err != nil {
			return err
		}
	}
	return nil
}

// whatTheSystemRanInTheSession is every command the system ran inside the session's container.
func whatTheSystemRanInTheSession(ctx context.Context) ([]string, error) {
	w := worldFrom(ctx)
	ran := []string{}
	for _, box := range w.provider.Boxes {
		for _, spec := range box.Ran {
			ran = append(ran, strings.Join(spec.Argv, " "))
		}
	}
	return ran, nil
}

// theReasonSays holds a stopped job's reason to what it has to say.
func theReasonSays(wants ...string) func(context.Context) error {
	return func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseStopped {
			return fmt.Errorf("the job is %q saying %q, want stopped", one.GetPhase(), one.GetReason())
		}
		for _, want := range wants {
			if !strings.Contains(one.GetReason(), want) {
				return fmt.Errorf("the reason is %q, want it to say %q", one.GetReason(), want)
			}
		}
		return nil
	}
}

// taskLands releases the task the controller sent and waits for it to settle, the way the controller
// scenarios do.
func taskLands(ctx context.Context) error {
	w := worldFrom(ctx)
	if w.release != nil {
		w.release()
		w.release = nil
	}
	return w.settled(ctx)
}

// theSessionsOwnDirectory is where the system keeps the files of the session doing this job.
//
// Read off the job rather than off the scenario's own dispatches, because nobody dispatched this one:
// the controller did, which is the whole shape of the behaviour.
func theSessionsOwnDirectory(ctx context.Context) (string, error) {
	one, err := theJobsSession(ctx)
	if err != nil {
		return "", err
	}
	dir, kept := worldFrom(ctx).storage.WorkingDir(sandbox.Config{
		ID: one.GetSession(), Workspace: one.GetWorkspace(), Project: one.GetProject(),
	})
	if !kept {
		return "", fmt.Errorf("this system keeps no working directory for session %s", one.GetSession())
	}
	return dir, nil
}

// theJobsSession is the job once the system has made it a session, waited for rather than read once:
// a dispatch that lets go answers before the task starts, so reading immediately would pass or fail
// on how fast the machine is.
func theJobsSession(ctx context.Context) (*quaycrewv1.Job, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		one, err := readJob(ctx, 0)
		if err != nil {
			return nil, err
		}
		if one.GetSession() != "" {
			return one, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("the job never got a session")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
