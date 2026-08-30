package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/display"
	"github.com/atlantic-blue/krewe/internal/workspace"
)

// allAnswers is the one flag this command takes.
const allAnswers = "--all"

// statusRunning and statusFailed are what a task record says about how it ended. A task that has
// not landed carries no answer, and a failed one carries what went wrong instead of a reply.
const (
	statusRunning = "running"
	statusFailed  = "failed"
)

// runAnswer writes what a session came back with, and nothing else.
//
// This is the way an answer leaves the system as data. Asking waits for its own answer and prints it,
// a dispatch lets go, and the history listing is written for a person: it shortens a reply at 120
// characters and puts a clock and a speaker beside it. A caller piping that reads a listing where
// the value belongs.
//
// So a refusal goes to standard error, by being returned rather than printed, and what a failed task
// failed with goes where an answer goes: it is the answer to what was asked, and the exit status is
// what tells the caller it is reading one.
func runAnswer(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	every := false
	named := make([]string, 0, 1)
	for _, arg := range args {
		if arg == allAnswers {
			every = true
			continue
		}
		named = append(named, arg)
	}
	if len(named) != 1 {
		return fmt.Errorf("usage: krewe answer <session> [%s]\n\na session is its id, its handle, or its address", allAnswers)
	}

	session, err := workspace.Session(ctx, client, named[0])
	if err != nil {
		return err
	}
	resp, err := client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session.GetId()})
	if err != nil {
		return err
	}
	tasks := resp.GetTasks()
	if every {
		return everyAnswer(tasks, session.GetId(), out)
	}
	return lastAnswer(tasks, session.GetId(), out)
}

// lastAnswer writes the answer of the most recent task.
//
// A task still running is refused rather than skipped. The answer of the task before it is an answer
// to a different question, and a caller that dispatched a task and came back for its answer would
// read the older one as the new one and never know.
func lastAnswer(tasks []*quaycrewv1.Task, session string, out io.Writer) error {
	if len(tasks) == 0 {
		return noLandedTask(session)
	}
	last := tasks[len(tasks)-1]
	if last.GetStatus() == statusRunning {
		return fmt.Errorf("the task on %s is still running, so it has no answer yet"+
			"\n\nwatch it with krewe task list %s", display.ShortID(session), display.ShortID(session))
	}
	writeAnswer(out, answerOf(last))
	if last.GetStatus() == statusFailed {
		// Already written where an answer goes, so it is not repeated on the other stream.
		return fmt.Errorf("%w: the task on %s failed", ErrSaid, display.ShortID(session))
	}
	return nil
}

// everyAnswer writes every answer that landed, oldest first, one record per line.
func everyAnswer(tasks []*quaycrewv1.Task, session string, out io.Writer) error {
	written := 0
	for _, task := range tasks {
		if task.GetStatus() == statusRunning {
			continue
		}
		writeAnswer(out, answerOf(task))
		written++
	}
	if written == 0 {
		return noLandedTask(session)
	}
	return nil
}

// answerOf is what a task came back with: what the model said, or what it failed with.
func answerOf(task *quaycrewv1.Task) string {
	if task.GetStatus() == statusFailed {
		return task.GetFailure()
	}
	return task.GetReply()
}

// writeAnswer puts one answer on the stream with a single trailing newline, so a caller reading a
// line at a time gets one record per read and never has to trim.
func writeAnswer(out io.Writer, answer string) {
	fmt.Fprintln(out, strings.TrimRight(answer, "\n"))
}

func noLandedTask(session string) error {
	return fmt.Errorf("%s has no landed task, so there is no answer to give"+
		"\n\nstart one with krewe task %s \"...\"", display.ShortID(session), display.ShortID(session))
}
