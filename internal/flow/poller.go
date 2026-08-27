package flow

import (
	"context"
	"log/slog"
	"time"
)

// DefaultPollEvery is how often the crew looks for waits that have come due.
//
// A wait is a coarse instrument by nature: an automation that says "leave it ten minutes" does not
// care about the second it resumes on. Polling this often costs one indexed query and keeps the
// mechanism something a person can hold in their head, which a scheduler with its own timers and
// its own failure modes would not.
const DefaultPollEvery = 5 * time.Second

// Poller resumes waiting runs whose time has come.
//
// It exists because a wait is a row rather than a timer somebody is holding: a process holding
// timers forgets every one of them when it restarts, and a run that was going to resume in ten
// minutes simply never would. This reads the rows instead, so a crew restarted mid wait picks the
// run up on its next tick.
type Poller struct {
	engine *Engine
	every  time.Duration
	logger *slog.Logger
}

// NewPoller builds one over an engine.
func NewPoller(engine *Engine, every time.Duration, logger *slog.Logger) *Poller {
	if every <= 0 {
		every = DefaultPollEvery
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Poller{engine: engine, every: every, logger: logger}
}

// Run polls until ctx is done. It blocks, so a caller that wants it in the background runs it in a
// goroutine: something has to own the lifetime, and hiding a goroutine inside a constructor makes
// that impossible to see.
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.every)
	defer ticker.Stop()
	// Once on the way up, before the first tick, so a crew restarted onto a pile of overdue waits
	// resumes them now rather than in five seconds.
	p.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.Tick(ctx)
		}
	}
}

// Tick resumes every run that is due right now. Exported so a test can drive one tick rather than
// waiting for a ticker, which would be slow when it passed and flaky when it did not.
func (p *Poller) Tick(ctx context.Context) {
	p.carryWorked(ctx)
	p.startScheduled(ctx)
	p.resumeWaiting(ctx)
}

// carryWorked carries on every run whose step has ended.
//
// This is what replaced the engine holding its own dispatch open. A run out with a piece of work is
// a row rather than a goroutine, so a crew restarted while twenty steps were running picks all
// twenty up on its next tick rather than losing them.
func (p *Poller) carryWorked(ctx context.Context) {
	landed, err := p.engine.store.LandedFlowSteps(ctx, pollBatch)
	if err != nil {
		p.logger.Warn("could not read which flow runs have a step that ended", "error", err)
		return
	}
	for _, one := range landed {
		if _, err := p.engine.Worked(ctx, one.Run, one.Step); err != nil {
			// One run that cannot move must not stop the others: a graph edited into something
			// unparseable, or a run whose project is gone, is that run's problem.
			p.logger.Warn("a flow run could not be carried on from the work its step did",
				"run", one.Run.ID, "work", one.Step.ID, "error", err)
		}
	}
}

// startScheduled begins a run of every graph whose schedule has come due.
//
// The schedule is moved on before the run is started, so a start that fails leaves the schedule
// pointing at its next time rather than firing again on every tick for as long as the failure
// lasts, which is the shape that turns one broken graph into a spend loop.
func (p *Poller) startScheduled(ctx context.Context) {
	now := p.engine.now()
	due, err := p.engine.store.DueFlowSchedules(ctx, now)
	if err != nil {
		p.logger.Warn("could not read which flow schedules are due", "error", err)
		return
	}
	for _, schedule := range due {
		if err := p.engine.store.MarkFlowScheduled(ctx, schedule.GraphName, schedule.Project, now.Add(schedule.Every)); err != nil {
			p.logger.Warn("could not move a flow schedule on, so it is left alone this tick",
				"graph", schedule.GraphName, "project", schedule.Project, "error", err)
			continue
		}
		if _, err := p.engine.Begin(ctx, schedule.GraphName, schedule.Workspace, schedule.Project, nil); err != nil {
			// One graph that cannot start must not stop the others, and its schedule has already
			// moved on, so this is reported rather than retried in a tight loop.
			p.logger.Warn("a scheduled flow could not be started",
				"graph", schedule.GraphName, "project", schedule.Project, "error", err)
		}
	}
}

// resumeWaiting carries on every run whose wait has come due.
func (p *Poller) resumeWaiting(ctx context.Context) {
	due, err := p.engine.store.DueFlowRuns(ctx, p.engine.now())
	if err != nil {
		p.logger.Warn("could not read which flow runs are due", "error", err)
		return
	}
	for _, run := range due {
		if _, err := p.engine.Resume(ctx, *run); err != nil {
			// One run that cannot move must not stop the others: a graph that was edited into
			// something unparseable, or a run whose project is gone, is this run's problem.
			p.logger.Warn("a waiting flow run could not be resumed", "run", run.ID, "error", err)
		}
	}
}

// Answer carries an asking run on with what the operator decided, and drives it until it stops
// again. Like Resume it uses the version the run pinned, so a graph edited while somebody was
// deciding does not change what their answer does.
func (e *Engine) Answer(ctx context.Context, run Run, answer string) (Run, error) {
	definition, err := e.store.FlowGraph(ctx, run.GraphName, run.GraphVersion)
	if err != nil {
		return run, err
	}
	graph, err := Parse([]byte(definition))
	if err != nil {
		return run, err
	}
	carrier, err := e.store.FlowRunCarrier(ctx, run.ID)
	if err != nil {
		return run, err
	}
	return e.advance(ctx, graph, run, where{carrier: carrier}, Event{Kind: EventAnswered, Node: run.Node, Answer: answer})
}

// Resume carries a waiting run on from the wait it was sitting on, and drives it until it stops
// again. The due event names the node the run is on, so a poller that fired twice over the same row
// moves it once: the second event arrives for a node the run has already left and is refused.
func (e *Engine) Resume(ctx context.Context, run Run) (Run, error) {
	// The version the run pinned, not the newest: a graph edited while a run was waiting must not
	// change what that run does when it wakes up.
	definition, err := e.store.FlowGraph(ctx, run.GraphName, run.GraphVersion)
	if err != nil {
		return run, err
	}
	graph, err := Parse([]byte(definition))
	if err != nil {
		return run, err
	}
	carrier, err := e.store.FlowRunCarrier(ctx, run.ID)
	if err != nil {
		return run, err
	}
	return e.advance(ctx, graph, run, where{carrier: carrier}, Event{Kind: EventDue, Node: run.Node})
}
