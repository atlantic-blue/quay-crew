package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/controlplane"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/cucumber/godog"
)

// Steps for the session lifecycle: giving a container back, filing a session away, and stopping the
// task one session is running.
//
// The two times are set in seconds and then genuinely waited out, rather than a clock being moved
// under the code. A second is a real second and it costs the suite one, which buys a scenario that
// exercises the same comparison the system makes in production instead of one that exercises a hook
// written for the test.
func initializeLifecycleSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the workspace reclaims a session after (\d+) seconds?$`,
		func(ctx context.Context, seconds int) error {
			return setTimes(ctx, func(limits *quaycrewv1.WorkspaceLimits) {
				limits.ReclaimSeconds = int32(seconds)
			})
		})

	sc.Step(`^the workspace archives a reclaimed session after (\d+) seconds?$`,
		func(ctx context.Context, seconds int) error {
			return setTimes(ctx, func(limits *quaycrewv1.WorkspaceLimits) {
				limits.ArchiveSeconds = int32(seconds)
			})
		})

	sc.Step(`^the workspace's reclaim time is unset$`, func(ctx context.Context) error {
		return timeIsUnset(ctx, "reclaim", func(l *quaycrewv1.WorkspaceLimits) int32 { return l.GetReclaimSeconds() })
	})

	sc.Step(`^the workspace's archive time is unset$`, func(ctx context.Context) error {
		return timeIsUnset(ctx, "archive", func(l *quaycrewv1.WorkspaceLimits) int32 { return l.GetArchiveSeconds() })
	})

	sc.Step(`^the reclaim time passes$`, func(ctx context.Context) error {
		return waitOut(ctx, func(l *quaycrewv1.WorkspaceLimits) int32 { return l.GetReclaimSeconds() })
	})

	sc.Step(`^the archive time passes$`, func(ctx context.Context) error {
		return waitOut(ctx, func(l *quaycrewv1.WorkspaceLimits) int32 { return l.GetArchiveSeconds() })
	})

	// The signal that stops a reclaim closing a container somebody is typing into. The provider is
	// asked, the way the real one asks tmux inside the container.
	sc.Step(`^an operator has that session's conversation open$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		w.provider.Watch(current.sessionID)
		return nil
	})

	sc.Step(`^the system still holds its container$`, func(ctx context.Context) error {
		open, err := containerIsOpen(ctx)
		if err != nil {
			return err
		}
		if !open {
			return fmt.Errorf("the session's container has gone, and nothing should have taken it")
		}
		return nil
	})

	sc.Step(`^its container is gone$`, func(ctx context.Context) error {
		open, err := containerIsOpen(ctx)
		if err != nil {
			return err
		}
		if open {
			return fmt.Errorf("the session reads reclaimed and its container is still running, " +
				"which is the leak the whole slice exists to close")
		}
		return nil
	})

	sc.Step(`^no session was reclaimed$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
		if err != nil {
			return err
		}
		for _, session := range listed.GetSessions() {
			if session.GetStatus() == controlplane.StatusReclaimed {
				return fmt.Errorf("session %s was reclaimed", session.GetHandle())
			}
		}
		return nil
	})

	sc.Step(`^the session is archived$`, func(ctx context.Context) error {
		archived, err := sessionIsArchived(ctx)
		if err != nil {
			return err
		}
		if !archived {
			return fmt.Errorf("the session was not filed away")
		}
		return nil
	})

	sc.Step(`^the session is not archived$`, func(ctx context.Context) error {
		archived, err := sessionIsArchived(ctx)
		if err != nil {
			return err
		}
		if archived {
			return fmt.Errorf("the session was filed away, and only a reclaim time was set")
		}
		return nil
	})

	sc.Step(`^the session answers$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		if current.reply == "" {
			return fmt.Errorf("the session answered nothing")
		}
		return nil
	})

	sc.Step(`^the session still holds both tasks, oldest first$`, func(ctx context.Context) error {
		tasks, err := tasksOfTheSession(ctx)
		if err != nil {
			return err
		}
		if len(tasks) != 2 {
			return fmt.Errorf("the session holds %d tasks, want both of them: the history is the "+
				"whole difference between this and starting again", len(tasks))
		}
		if !tasks[0].GetOccurredAt().AsTime().Before(tasks[1].GetOccurredAt().AsTime()) {
			return fmt.Errorf("the tasks read %v, want the oldest first",
				[]string{tasks[0].GetPrompt(), tasks[1].GetPrompt()})
		}
		return nil
	})

	sc.Step(`^the session still holds (\d+) tasks?$`, func(ctx context.Context, want int) error {
		tasks, err := tasksOfTheSession(ctx)
		if err != nil {
			return err
		}
		if len(tasks) != want {
			return fmt.Errorf("the session holds %d tasks, want %d", len(tasks), want)
		}
		return nil
	})

	// Issue 395: stopping the task one session is running, and keeping the session.
	sc.Step(`^the operator stops the session saying "([^"]*)"$`, func(ctx context.Context, reason string) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		return stopTask(ctx, current.sessionID, reason)
	})

	sc.Step(`^the operator stops the session the job is running in saying "([^"]*)"$`,
		func(ctx context.Context, reason string) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetSession() == "" {
				return fmt.Errorf("the job says no session, so there is nothing to stop")
			}
			return stopTask(ctx, one.GetSession(), reason)
		})

	sc.Step(`^the task reads stopped, with that reason$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		tasks, err := tasksOfTheSession(ctx)
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			return fmt.Errorf("the session holds no task, so the stop recorded nothing")
		}
		last := tasks[len(tasks)-1]
		if last.GetStatus() != controlplane.StatusTaskStopped {
			return fmt.Errorf("the task reads %q, want stopped: an operator asking for a stop is not "+
				"a fault, and a stop that reports as a crash hides the crashes", last.GetStatus())
		}
		if !strings.Contains(last.GetFailure(), w.lastStopReason) {
			return fmt.Errorf("the task record says %q, want the operator's own reason %q",
				last.GetFailure(), w.lastStopReason)
		}
		return nil
	})

	sc.Step(`^the system says there was nothing to stop$`, func(ctx context.Context) error {
		if worldFrom(ctx).lastStop.GetStopped() {
			return fmt.Errorf("the system says it stopped a task, and nothing was running")
		}
		return nil
	})

	sc.Step(`^the model answers again$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.release == nil {
			return fmt.Errorf("no task was being held, so there is nothing to let go of")
		}
		w.release()
		w.release = nil
		return nil
	})

	// Read off the system rather than off what the scenario declared, because what is being specified
	// is where the controller moved the job to after the task was stopped.
	sc.Step(`^the job reads stopped, saying "([^"]*)"$`, func(ctx context.Context, reason string) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseStopped {
			return fmt.Errorf("the job is %q, want stopped: an operator halting a task is not the "+
				"model failing, and the two must never read the same", one.GetPhase())
		}
		if !strings.Contains(one.GetReason(), reason) {
			return fmt.Errorf("the job says %q, want the operator's own reason %q", one.GetReason(), reason)
		}
		return nil
	})

	sc.Step(`^the job carries no answer$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetAnswer() != "" {
			return fmt.Errorf("the stopped job carries the answer %q, and the task ended before it "+
				"had one: reporting an answer nobody gave is worse than reporting none", one.GetAnswer())
		}
		return nil
	})
}

// setTimes reads the workspace's ceiling, changes one number on it and writes the row back, which is
// how one limit is changed: the whole row is written as it arrives.
func setTimes(ctx context.Context, change func(*quaycrewv1.WorkspaceLimits)) error {
	w := worldFrom(ctx)
	held, err := w.client.GetWorkspaceLimits(ctx, &quaycrewv1.GetWorkspaceLimitsRequest{
		Workspace: w.workspaceID,
	})
	if err != nil {
		return err
	}
	asked := held.GetLimits()
	asked.Workspace = w.workspaceID
	change(asked)
	_, err = w.client.SetWorkspaceLimits(ctx, &quaycrewv1.SetWorkspaceLimitsRequest{Limits: asked})
	return err
}

func timeIsUnset(ctx context.Context, name string, read func(*quaycrewv1.WorkspaceLimits) int32) error {
	w := worldFrom(ctx)
	held, err := w.client.GetWorkspaceLimits(ctx, &quaycrewv1.GetWorkspaceLimitsRequest{
		Workspace: w.workspaceID,
	})
	if err != nil {
		return err
	}
	if got := read(held.GetLimits()); got != 0 {
		return fmt.Errorf("the %s time is %d seconds, and it ships unset: three measurements decide "+
			"it and none has been taken", name, got)
	}
	return nil
}

// waitOut waits the workspace's own number out, with a small margin so the comparison the system makes
// is genuinely past rather than exactly on it.
func waitOut(ctx context.Context, read func(*quaycrewv1.WorkspaceLimits) int32) error {
	w := worldFrom(ctx)
	held, err := w.client.GetWorkspaceLimits(ctx, &quaycrewv1.GetWorkspaceLimitsRequest{
		Workspace: w.workspaceID,
	})
	if err != nil {
		return err
	}
	seconds := read(held.GetLimits())
	if seconds == 0 {
		return fmt.Errorf("this scenario waits out a time the workspace never set")
	}
	time.Sleep(time.Duration(seconds)*time.Second + 200*time.Millisecond)
	return nil
}

// containerIsOpen says whether the system still holds a sandbox for the session the scenario is about.
func containerIsOpen(ctx context.Context) (bool, error) {
	w := worldFrom(ctx)
	current, err := w.lastTask()
	if err != nil {
		return false, err
	}
	stranded, err := w.provider.Stranded(ctx)
	if err != nil {
		return false, err
	}
	for _, id := range stranded {
		if id == current.sessionID {
			return true, nil
		}
	}
	return false, nil
}

// sessionIsArchived reads the session the scenario is about out of the archived listing, because an
// archived session is not in the default one.
func sessionIsArchived(ctx context.Context) (bool, error) {
	w := worldFrom(ctx)
	current, err := w.lastTask()
	if err != nil {
		return false, err
	}
	resp, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: current.sessionID})
	if err != nil {
		return false, err
	}
	return resp.GetSession().GetArchivedAt() != nil, nil
}

// tasksOfTheSession is the history of the session the scenario is about, oldest first.
func tasksOfTheSession(ctx context.Context) ([]*quaycrewv1.Task, error) {
	w := worldFrom(ctx)
	current, err := w.lastTask()
	if err != nil {
		return nil, err
	}
	resp, err := w.client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: current.sessionID})
	if err != nil {
		return nil, err
	}
	return resp.GetTasks(), nil
}

// stopTask halts the task a session is running, keeping both the answer and the reason so a Then
// step can assert on either.
func stopTask(ctx context.Context, session, reason string) error {
	w := worldFrom(ctx)
	resp, err := w.client.StopTask(ctx, &quaycrewv1.StopTaskRequest{Id: session, Reason: reason})
	w.lastErr, w.lastStop, w.lastStopReason = err, resp, reason
	return err
}
