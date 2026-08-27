package flow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/work"
)

// A trigger is something that happened, written down as a row so that a run starts from it.
//
// The row is the mechanism, and it is the same one the crew already uses for waits, for dispatch
// idempotency and for work events: write it in the transaction that holds whatever happened, poll an
// indexed query, and claim the row with a conditional write where it must be acted on once. Nothing
// is held in a process, so a crew restarted between the thing happening and the run starting still
// starts the run.
//
// A trigger is claimed, and a work event never is. That is why they are two tables: marking an audit
// record consumed rewrites the history, and a queue entry that is not claimed starts two runs from
// one event.

// Trigger statuses. Pending is a row nothing has started yet, and the only one the poller reads.
// Started and failed are both ends of the road: a trigger is acted on once.
const (
	TriggerPending = "pending"
	TriggerStarted = "started"
	// TriggerFailed is a trigger the crew could not start a run from, carrying the sentence that
	// says why. It is not retried, because a trigger that silently did nothing is the failure this
	// status exists to make visible.
	TriggerFailed = "failed"
)

// TriggerLease is how long a poller's claim on a trigger row lasts before another poller may take
// it.
//
// The same discipline the work controller holds a piece of work under, and the same budget, for a
// much shorter piece of work: what this has to outlast is one transaction, the one that writes the
// run and marks the trigger started. A poller that dies inside that window leaves a row nothing has
// started, and this is how long the crew waits before another one picks it up.
const TriggerLease = work.DefaultLease

// ErrTriggerTaken is what a store says when a claim did not apply: another poller holds the row, or
// it has already started its run. It is not a failure, and the poller that lost leaves it alone.
var ErrTriggerTaken = errors.New("flow: that trigger is held by another poller, or has already been acted on")

// Trigger is one thing that happened, and the run it asks for.
type Trigger struct {
	ID string
	// GraphName is the flow to run. The row names the graph rather than a node, because whoever
	// raises a trigger knows what should happen and not where in a graph it happens.
	GraphName string
	Workspace string
	Project   string
	// Payload is what the trigger carried, and it becomes the run's opening state, which is what a
	// prompt template reads with {{key}}.
	Payload map[string]string
	// Source says what raised this, for a reader: an in process caller, or the ingress that slice 3
	// of quay-crew#399 adds. The crew does not act on it.
	Source string
	// Cause is the piece of work that caused this trigger, empty where nothing did. The run's own
	// work hangs under it, so a flow started by work that finished is counted by the same depth
	// limit and the same tree budget as everything else in that tree.
	Cause string
	// Status is pending, started or failed.
	Status string
	// Run is the run this trigger started, empty until it starts one.
	Run string
	// Reason says why a failed trigger started nothing, in words that name what to do about it. It
	// is the only place that failure is ever read.
	Reason string
	// Attempts counts the claims taken on this row, so a trigger picked up again after a poller died
	// says so.
	Attempts int
	// Lease is who holds the row and until when, empty on a row nobody has claimed.
	Lease work.Lease
	// RaisedAt is when the thing that caused this happened, as the store recorded it.
	RaisedAt time.Time
}

// Raise writes down that something happened, so the crew starts a run of the graph it names on its
// next tick. This is the whole of the in process source: a caller inside this process writes a row,
// and the row is what starts the run. The latency is the poll interval.
//
// It does not check that the graph exists. The caller writing this row is writing it beside whatever
// happened, and a source made to read another table first is a source that fails for reasons that
// have nothing to do with the thing it is recording. The check happens where the run starts, and its
// refusal lands on the row, so there is one place to read what became of a trigger rather than two.
func (e *Engine) Raise(ctx context.Context, trigger Trigger) (Trigger, error) {
	trigger.GraphName = strings.TrimSpace(trigger.GraphName)
	trigger.Workspace = strings.TrimSpace(trigger.Workspace)
	trigger.Project = strings.TrimSpace(trigger.Project)
	if trigger.GraphName == "" {
		return Trigger{}, fmt.Errorf("flow: a trigger has to name the flow it starts")
	}
	if trigger.Workspace == "" || trigger.Project == "" {
		return Trigger{}, fmt.Errorf("flow: a trigger of %s has to say which project the run happens in", trigger.GraphName)
	}
	trigger.ID = newTriggerID()
	trigger.Status = TriggerPending
	trigger.Run, trigger.Reason, trigger.Attempts = "", "", 0
	trigger.Lease = work.Lease{}
	if trigger.Payload == nil {
		trigger.Payload = map[string]string{}
	}
	if err := e.store.RaiseTrigger(ctx, &trigger); err != nil {
		return Trigger{}, fmt.Errorf("flow: raise a trigger of %s: %w", trigger.GraphName, err)
	}
	return trigger, nil
}

// Triggered starts the run a claimed trigger asks for, and takes its first movement.
//
// The run and the trigger being marked started land in one transaction, which is what makes a
// trigger start exactly one run: a poller that dies after writing the run leaves a row that says
// started, so the poller that takes the expired claim finds nothing left to do.
func (e *Engine) Triggered(ctx context.Context, trigger Trigger) (Run, error) {
	// Read for the sentence rather than for the graph, which create reads again to run it. What
	// became of a trigger is only ever read off its own row, so a trigger naming a flow nobody
	// imported says what to do about it rather than "not found".
	if _, _, err := e.store.LatestFlowGraph(ctx, trigger.GraphName); err != nil {
		return Run{}, fmt.Errorf("the flow %s could not be read, so nothing was started: %v. Import the graph with quay flow import <file>",
			trigger.GraphName, err)
	}
	run, carrier, graph, err := e.create(ctx, starting{
		graph: trigger.GraphName, workspace: trigger.Workspace, project: trigger.Project,
		// What the trigger carried is what the run opens knowing, so a prompt reads it with {{key}}.
		state: trigger.Payload,
		// Under the work that caused it, where something did. A flow started by a piece of work that
		// finished hangs under that work, so the depth limit and the tree budget bound the loop.
		under: trigger.Cause, trigger: trigger.ID,
	})
	if err != nil {
		return Run{}, err
	}
	return e.advance(ctx, graph, run, where{carrier: carrier}, Event{Kind: EventStarted})
}

// newTriggerID mints an identifier for a trigger, the same shape everything else here uses.
func newTriggerID() string { return newRunID() }
