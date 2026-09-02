package job

import (
	"context"
	"errors"
	"fmt"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/capacity"
	"github.com/atlantic-blue/quay-krewe/internal/telemetry"
)

// A controller runs an execution the way it runs a job, and it is a much shorter road.
//
// A job passes through four stages, answers a person's gates, writes a plan somebody approves, keeps
// a record of its steps, hands itself over at the context ceiling and settles behind two independent
// sessions. A run of a stage does none of that. It is dispatched, it answers, and the stage that
// wrote it reads the answer. Everything a person decides belongs to the job.
//
// What it keeps of the job's road is what a run genuinely needs: a lease, so two controllers cannot
// both send it; a place on the machine, so a run cannot start where no container fits; and a landing
// that says what came back.

// The records a run writes. They hang off the job it belongs to and name the run, because a run is
// not a job and the history of one job is where a person reads what happened to it.
const (
	// EventRan is written when the system writes a run of a stage. It is not a movement of the job:
	// the job is pending before it and pending after it, and what it adds is the record of what the
	// stage sent out.
	EventRan = "job.ran"
	// EventRunEnded is written when a run of a stage lands, however it landed.
	EventRunEnded = "job.run_ended"
)

// executionEvent is one record about a run, written against the job it belongs to.
func (c *Controller) executionEvent(ctx context.Context, one *Job, run *Execution,
	kind, detail string) *Event {
	if c.redactor != nil {
		detail = c.redactor.RedactFor(ctx, one.Workspace, detail)
	}
	return &Event{
		ID: newRowID(), Kind: kind, Job: one.ID, Workspace: one.Workspace, Project: one.Project,
		Parent: one.Parent, Depth: one.Depth, Execution: run.ID, Detail: detail,
		TraceID: one.TraceID, OccurredAt: time.Now().UTC(),
	}
}

// runExecutions sends a task for each run that has not started.
func (c *Controller) runExecutions(ctx context.Context) {
	runnable, err := c.store.RunnableExecutions(ctx, c.batch)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which runs are ready to start", "error", err)
		return
	}
	for _, run := range runnable {
		c.startExecution(ctx, run)
	}
}

// startExecution claims one run and sends its task.
//
// Everything the session is asked comes off the job at this moment: the requirement or the vertical,
// the words, the mode, the repository and the role. Nothing of it is stored on the run, so a run
// cannot carry a copy of the job that disagrees with the job.
func (c *Controller) startExecution(ctx context.Context, run *Execution) {
	one, wanted, why := c.whatTheRunIsFor(ctx, run)
	if why != "" {
		// The run cannot be sent and never will be: the job it belongs to is gone, or the list it was
		// written against changed under it. It lands stopped, so the stage reads a run that ended
		// rather than waiting on one that will never move.
		c.stopExecution(ctx, run, why)
		return
	}
	ctx = telemetry.Under(ctx, run.TraceID, run.ParentSpanID)

	handle := SessionForExecution(run)
	limits := c.limitsIn(ctx, one.Workspace)
	lease := Lease{Owner: c.owner, Until: time.Now().UTC().Add(limits.Lease(c.lease))}

	// The machine is asked before the row is claimed, for the reason a job asks first: a run the
	// machine cannot host must stay pending rather than start and be killed. Every road below that
	// does not end in a dispatch gives the room back.
	key := capacity.KeyFor(one.Project, handle)
	if verdict := c.admit(ctx, key, limits.Request(capacity.DefaultRequest())); !verdict.OK {
		// Nothing is written on the run. The job it belongs to already says the stage is waiting, and a
		// reason written here would be a second answer to the same question.
		return
	}

	record := c.executionEvent(ctx, one, run, EventRan,
		fmt.Sprintf("attempt %d of the %s stage of job %s, number %d, in session %s",
			run.Attempts+1, run.Stage, one.ID, run.Number, handle))
	claimed, err := c.store.StartExecution(ctx, run.ID, lease, record)
	if err != nil {
		if !errors.Is(err, ErrNotPending) {
			c.logger.WarnContext(ctx, "could not claim a run", "execution", run.ID, "error", err)
		}
		c.releaseRoom(key)
		return
	}
	c.exported(ctx, record)

	sent, err := c.plane.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: one.Project, Handle: handle, Text: c.askedOfTheRun(one, claimed, wanted),
		PermissionMode: one.Mode, Detach: true,
		Title: titleOfTheRun(claimed, wanted),
		// The job this run belongs to, so the system mints the credential against it, and the run, so
		// the system knows which stage the session is under. A build run works behind a boundary that
		// refuses it a write to a test, and the stage is what says so.
		Job: one.ID, Execution: claimed.ID,
	})
	if err == nil {
		if err := c.store.RecordExecutionSession(ctx, claimed.ID, sent.GetId()); err != nil {
			c.logger.WarnContext(ctx, "could not record which session a run happens in",
				"execution", claimed.ID, "session", sent.GetId(), "error", err)
		}
		return
	}
	// A run whose task could not be started is failed with the reason rather than left running with
	// nothing behind it. Its room goes back: there is no sandbox and there is not going to be one.
	c.releaseRoom(key)
	c.landExecution(ctx, one, claimed, ExecutionLanding{
		Phase: PhaseFailed, Reason: oneLine(err.Error()),
	})
}

// whatTheRunIsFor is the job this run belongs to and the requirement or vertical it is for, and the
// sentence saying why it can never be sent.
func (c *Controller) whatTheRunIsFor(ctx context.Context, run *Execution) (*Job, Requirement, string) {
	one, err := c.store.GetJob(ctx, run.Job)
	if err != nil {
		return nil, Requirement{}, fmt.Sprintf("the job this run belongs to could not be read: %s",
			oneLine(err.Error()))
	}
	for _, wanted := range RequirementsOf(one) {
		if wanted.Number == run.Number {
			return one, wanted, ""
		}
	}
	return nil, Requirement{}, fmt.Sprintf(
		"job %s has no number %d on the list a person accepted, so there is nothing to run",
		run.Job, run.Number)
}

// askedOfTheRun is what the session is asked, built from the job at the moment of the dispatch.
func (c *Controller) askedOfTheRun(one *Job, run *Execution, wanted Requirement) string {
	if run.Stage == StageBuild {
		return BuildTheVertical(one, wanted, FailuresOn(one.Tests)[wanted.Number])
	}
	return WriteFailingTests(one, wanted)
}

// titleOfTheRun is the line a listing of sessions shows for this run.
func titleOfTheRun(run *Execution, wanted Requirement) string {
	if run.Stage == StageBuild {
		return BuildingVertical(wanted)
	}
	return TestsFor(wanted)
}

// adoptExecutions reads the task row for each run this controller holds and lands the ones that
// finished.
func (c *Controller) adoptExecutions(ctx context.Context) {
	held, err := c.store.HeldExecutions(ctx, c.owner, c.batch)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which runs this controller is holding", "error", err)
		return
	}
	for _, run := range held {
		c.adoptExecution(ctx, run)
	}
}

// adoptExecution reads the task row for a run's session and lands the run if it has finished.
//
// The same reading a job's adoption makes, and none of the stages after it. A run answers no gate,
// approves no plan, records no step and hands nothing over, so what the task says is the whole of
// what came back.
func (c *Controller) adoptExecution(ctx context.Context, run *Execution) {
	one, err := c.store.GetJob(ctx, run.Job)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read the job a run belongs to",
			"execution", run.ID, "job", run.Job, "error", err)
		return
	}
	ctx = telemetry.Under(ctx, run.TraceID, run.ParentSpanID)

	resp, err := c.plane.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: run.Session})
	if err != nil {
		c.logger.WarnContext(ctx, "could not read the task of a run",
			"execution", run.ID, "session", run.Session, "error", err)
		return
	}
	tasks := resp.GetTasks()
	if len(tasks) == 0 {
		c.renewExecution(ctx, run)
		return
	}
	last := tasks[len(tasks)-1]
	if last.GetStatus() == StatusRunning {
		c.renewExecution(ctx, run)
		return
	}

	landed := ExecutionLanding{
		Phase: PhaseDone, Answer: last.GetReply(), SpentTokens: c.spentBy(ctx, run.Session),
	}
	switch {
	case last.GetStatus() == StatusFailed:
		landed.Phase, landed.Answer, landed.Reason =
			PhaseFailed, "", oneLine(last.GetFailure())
	// An operator stopped the task, so the run stops too, carrying their reason. A run that went
	// quiet and a run that was halted must never read the same.
	case last.GetStatus() == StatusTaskStopped:
		landed.Phase, landed.Answer, landed.Reason =
			PhaseStopped, "", oneLine(last.GetFailure())
	default:
		landed.Outcome = OutcomeIn(landed.Answer)
		landed.PullRequest = PullRequestIn(one.Repository, landed.Answer)
	}
	c.landExecution(ctx, one, run, landed)
}

// landExecution writes what came of a run and lets go of the machine.
func (c *Controller) landExecution(ctx context.Context, one *Job, run *Execution,
	landed ExecutionLanding) {
	detail := fmt.Sprintf("the %s stage of job %s, number %d, %s", run.Stage, run.Job, run.Number,
		landed.Phase)
	if landed.Reason != "" {
		detail += ": " + landed.Reason
	}
	record := c.executionEvent(ctx, one, run, EventRunEnded, detail)
	ended, err := c.store.LandExecution(ctx, run.ID, landed, record)
	if err != nil {
		if !errors.Is(err, ErrNotRunning) {
			c.logger.WarnContext(ctx, "could not write what came of a run",
				"execution", run.ID, "error", err)
		}
		return
	}
	c.exported(ctx, record)
	c.releaseRoom(capacity.KeyFor(one.Project, SessionForExecution(ended)))
}

// stopExecution ends a run that can never be sent, so the stage reads an ending rather than waiting
// on a row nothing will move.
func (c *Controller) stopExecution(ctx context.Context, run *Execution, why string) {
	lease := Lease{Owner: c.owner, Until: time.Now().UTC().Add(c.lease)}
	if _, err := c.store.StartExecution(ctx, run.ID, lease, nil); err != nil {
		if !errors.Is(err, ErrNotPending) {
			c.logger.WarnContext(ctx, "could not claim a run that cannot be sent",
				"execution", run.ID, "error", err)
		}
		return
	}
	if _, err := c.store.LandExecution(ctx, run.ID,
		ExecutionLanding{Phase: PhaseStopped, Reason: why}, nil); err != nil {
		c.logger.WarnContext(ctx, "could not stop a run that cannot be sent",
			"execution", run.ID, "error", err)
	}
}

// renewExecution moves this controller's hold on a run further on.
func (c *Controller) renewExecution(ctx context.Context, run *Execution) {
	lease := Lease{Owner: c.owner, Until: time.Now().UTC().Add(c.lease)}
	if err := c.store.RenewExecutionLease(ctx, run.ID, lease); err != nil {
		c.logger.WarnContext(ctx, "could not renew the hold on a run", "execution", run.ID, "error", err)
	}
}

// recoverAbandonedExecutions takes over the runs a controller that went away left behind, so the
// next pass of this one reads their tasks.
func (c *Controller) recoverAbandonedExecutions(ctx context.Context) {
	abandoned, err := c.store.ExpiredExecutions(ctx, c.batch)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which runs have been left behind", "error", err)
		return
	}
	lease := Lease{Owner: c.owner, Until: time.Now().UTC().Add(c.lease)}
	for _, run := range abandoned {
		if _, err := c.store.TakeOverExecution(ctx, run.ID, lease); err != nil &&
			!errors.Is(err, ErrNotRunning) {
			c.logger.WarnContext(ctx, "could not take over a run that was left behind",
				"execution", run.ID, "error", err)
		}
	}
}
