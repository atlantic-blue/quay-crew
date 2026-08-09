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
	return e.advance(ctx, graph, run, Event{Kind: EventDue, Node: run.Node})
}
