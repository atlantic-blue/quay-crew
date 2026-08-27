package work

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

// DefaultTickEvery is how often the controller looks at the work the crew holds.
//
// A tick costs one indexed query when there is nothing to do, which is the property the flow poller
// already has and is worth keeping. Work takes minutes, so a few seconds of delay before it starts
// is not worth a mechanism with its own timers and its own failure modes.
const DefaultTickEvery = 5 * time.Second

// DefaultBatch is how much work one tick will look at. A crew with a thousand pieces of work is not
// a reason for one tick to hold the store open.
const DefaultBatch = 20

// ErrNotPending is what a store says when the claim did not apply, which means another controller
// claimed the same row first. It is not a failure: the row is somebody else's now.
var ErrNotPending = errors.New("work: that work is no longer pending")

// ErrNotRunning is the same answer for a landing: the row moved on before this controller wrote
// what it read.
var ErrNotRunning = errors.New("work: that work is no longer running")

// Landing is what came of a piece of work, written onto the row in one movement.
type Landing struct {
	Phase       string
	Answer      string
	Reason      string
	SpentTokens int64
}

// Store is the rows a controller reads and writes. Every write takes the event that describes it, so
// the row and the record of how it moved land in one transaction or neither does.
type Store interface {
	// RunnableWork is the work this controller may start: pending, with no parent, no role and
	// nothing it waits for, oldest declared first.
	RunnableWork(ctx context.Context, limit int) ([]*Work, error)
	// StartedWork is the work a controller started and has not written an answer onto.
	StartedWork(ctx context.Context, limit int) ([]*Work, error)
	// StartWork claims one piece of work. It applies only to work that is still pending, which is
	// what keeps two controllers from both starting it, and it is what makes a tick safe to run
	// twice.
	StartWork(ctx context.Context, id string, event *Event) (*Work, error)
	// RecordWorkSession writes the session the crew made for a piece of work. It is not a movement,
	// so it carries no record of its own: a reader learns which conversation did the work from the
	// row, and the row is where quay attach reads it.
	RecordWorkSession(ctx context.Context, id, session string) error
	// LandWork writes what came of the work. It applies only to work that is still running.
	LandWork(ctx context.Context, id string, landed Landing, event *Event) (*Work, error)
}

// ControlPlane is what a controller may do on the crew. It is the same interface every other caller
// speaks to, deliberately: a controller holds no privileged road into anything, so it can move out
// of this process later without changing a line of its logic.
type ControlPlane interface {
	Dispatch(ctx context.Context, req *quaycrewv1.DispatchRequest) (*quaycrewv1.DispatchResponse, error)
	ListTasks(ctx context.Context, req *quaycrewv1.ListTasksRequest) (*quaycrewv1.ListTasksResponse, error)
}

// Spend is what one session's conversation has cost. An implementation that cannot tell answers zero.
type Spend interface {
	SessionTokens(ctx context.Context, session string) int64
}

// Redactor takes what the crew is about to write down and removes anything the workspace keeps
// sealed. Everything recorded here is persisted, and what a model says can carry a value somebody
// pasted into a conversation. A nil redactor writes the text as it is, which is what a test wants
// and what a crew must never do.
type Redactor interface {
	RedactFor(ctx context.Context, workspace, text string) string
}

// Prover answers what a piece of work said would show its task did the work.
//
// An implementation that cannot answer says so, and the work stops. A check that quietly passes when
// it could not be run is the same false green as no check at all.
type Prover interface {
	SessionHolds(ctx context.Context, session, path string) (bool, error)
}

// Controller makes reality match the work the crew holds.
//
// It reads the rows, compares them against the world, and closes the gap: it sends a task for work
// that has not started, and writes what came back onto the row for work whose task has landed. It
// never waits on a model. A dispatch lets go of its task, and the answer is read off the record on a
// later tick, so a task that runs for an hour costs the loop nothing.
//
// One movement per piece of work per tick. Nothing here loops until a row is finished, because a
// controller that drives one row to the end is a controller that is not watching the others.
type Controller struct {
	store    Store
	plane    ControlPlane
	spend    Spend
	prover   Prover
	redactor Redactor
	logger   *slog.Logger
	every    time.Duration
	batch    int
}

// NewController builds one. A nil spend reader means what work cost is not written; a nil prover
// means work claiming a file stops rather than being believed.
func NewController(store Store, plane ControlPlane, spend Spend, prover Prover, logger *slog.Logger) *Controller {
	if logger == nil {
		logger = slog.Default()
	}
	return &Controller{
		store: store, plane: plane, spend: spend, prover: prover, logger: logger,
		every: DefaultTickEvery, batch: DefaultBatch,
	}
}

// Redacting returns a controller that takes every line it writes through the crew redactor.
func (c *Controller) Redacting(redactor Redactor) *Controller {
	c.redactor = redactor
	return c
}

// Every sets how often the loop ticks. Zero or less leaves the default.
func (c *Controller) Every(every time.Duration) *Controller {
	if every > 0 {
		c.every = every
	}
	return c
}

// Run ticks until ctx is done. It blocks, so a caller that wants it in the background runs it in a
// goroutine: something has to own the lifetime, and hiding a goroutine inside a constructor makes
// that impossible to see.
func (c *Controller) Run(ctx context.Context) {
	ticker := time.NewTicker(c.every)
	defer ticker.Stop()
	// Once on the way up, so a crew restarted onto work that was declared while it was down starts
	// it now rather than one interval later.
	c.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.Tick(ctx)
		}
	}
}

// Tick reads the world once and closes what gaps it can. Exported so a test drives one tick rather
// than waiting for a ticker, which would be slow when it passed and flaky when it did not.
func (c *Controller) Tick(ctx context.Context) {
	c.adoptAnswers(ctx)
	c.startDeclared(ctx)
}

// startDeclared sends a task for each piece of work that has not started.
//
// The claim comes first and the dispatch second. A claim that did not apply is another controller
// holding the row, and it is left alone rather than dispatched for twice: work is paid for, so a
// second dispatch is a second bill.
func (c *Controller) startDeclared(ctx context.Context) {
	runnable, err := c.store.RunnableWork(ctx, c.batch)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which work is ready to run", "error", err)
		return
	}
	for _, one := range runnable {
		c.start(ctx, one)
	}
}

// start claims one piece of work and sends its task.
func (c *Controller) start(ctx context.Context, one *Work) {
	handle := SessionFor(one.ID)
	claimed, err := c.store.StartWork(ctx, one.ID, c.event(ctx, one, EventStarted,
		fmt.Sprintf("attempt %d in session %s", one.Attempts+1, handle)))
	if err != nil {
		if !errors.Is(err, ErrNotPending) {
			// One row that cannot be claimed must not stop the others: the next tick tries it again.
			c.logger.WarnContext(ctx, "could not claim a piece of work", "work", one.ID, "error", err)
		}
		return
	}

	// Detached, because a controller that waits on a model is a controller that stops controlling.
	// The answer is read off the task record on a later tick.
	//
	// The handle is derived from the work, so a dispatch that has to be made again lands in the same
	// conversation rather than starting a second one.
	sent, err := c.plane.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: claimed.Project, Handle: handle, Text: claimed.Brief,
		PermissionMode: claimed.Mode, Detach: true,
	})
	if err == nil {
		if err := c.store.RecordWorkSession(ctx, claimed.ID, sent.GetId()); err != nil {
			c.logger.WarnContext(ctx, "could not record which session a piece of work runs in",
				"work", claimed.ID, "session", sent.GetId(), "error", err)
		}
		return
	}
	// Work whose task could not be started is failed with the reason rather than left running with
	// nothing behind it, which is a row nobody can tell from work in progress.
	c.land(ctx, claimed, Landing{Phase: PhaseFailed, Reason: oneLine(err.Error())}, EventFailed)
}

// adoptAnswers writes what came back onto every piece of work whose task has landed.
func (c *Controller) adoptAnswers(ctx context.Context) {
	started, err := c.store.StartedWork(ctx, c.batch)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which work is running", "error", err)
		return
	}
	for _, one := range started {
		c.adopt(ctx, one)
	}
}

// adopt reads the task row for the work's session and moves the work on if it has landed.
//
// Reading rather than remembering is what makes this safe to run twice: the task row is the record
// of what the crew actually did, so a controller that has just started the work and one that has
// come back to it read the same thing.
func (c *Controller) adopt(ctx context.Context, one *Work) {
	resp, err := c.plane.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: one.Session})
	if err != nil {
		c.logger.WarnContext(ctx, "could not read the task of a piece of work",
			"work", one.ID, "session", one.Session, "error", err)
		return
	}
	tasks := resp.GetTasks()
	if len(tasks) == 0 {
		return
	}
	last := tasks[len(tasks)-1]
	// Still working. Nothing to do, and nothing to write: a controller that wrote on every tick
	// would fill the record with lines saying nothing happened.
	if last.GetStatus() == StatusRunning {
		return
	}

	landing := Landing{
		Phase: PhaseDone, Answer: last.GetReply(),
		SpentTokens: c.spentBy(ctx, one.Session),
	}
	kind := EventAnswered
	switch {
	case last.GetStatus() == StatusFailed:
		landing.Phase, landing.Reason, kind = PhaseFailed, oneLine(last.GetFailure()), EventFailed
	default:
		if unmet := c.unmet(ctx, one, last.GetReply()); unmet != "" {
			landing.Phase, landing.Reason, kind = PhaseStopped, unmet, EventStopped
		}
	}
	c.land(ctx, one, landing, kind)
}

// land writes the end of a piece of work, with the record of it.
func (c *Controller) land(ctx context.Context, one *Work, landed Landing, kind string) {
	detail := landed.Reason
	if detail == "" {
		detail = fmt.Sprintf("%d tokens in session %s", landed.SpentTokens, one.Session)
	}
	if _, err := c.store.LandWork(ctx, one.ID, landed, c.event(ctx, one, kind, detail)); err != nil {
		if !errors.Is(err, ErrNotRunning) {
			c.logger.WarnContext(ctx, "could not write what came of a piece of work",
				"work", one.ID, "error", err)
		}
	}
}

// unmet is what the work said would show its task did it, where that is not there. Empty means the
// work claimed nothing, or the claim held.
//
// Read after the task rather than described by it: the model reporting on its own work is the thing
// this exists to stop.
func (c *Controller) unmet(ctx context.Context, one *Work, reply string) string {
	if carries := one.ExpectContains; carries != "" && !strings.Contains(reply, carries) {
		return fmt.Sprintf("the answer does not carry %q, which this work said would show it was done", carries)
	}
	path := one.ExpectFile
	if path == "" {
		return ""
	}
	if c.prover == nil {
		return fmt.Sprintf("%s could not be checked: this crew cannot read a session's files", path)
	}
	held, err := c.prover.SessionHolds(ctx, one.Session, path)
	if err != nil {
		return fmt.Sprintf("%s could not be checked: %v", path, err)
	}
	if !held {
		return fmt.Sprintf("%s is not in the session that did the work", path)
	}
	return ""
}

// spentBy is what the work's own conversation has cost. A crew with no reader wired answers zero,
// because a cost nobody can read is not a number worth inventing.
func (c *Controller) spentBy(ctx context.Context, session string) int64 {
	if c.spend == nil || session == "" {
		return 0
	}
	return c.spend.SessionTokens(ctx, session)
}

// event describes one movement of a piece of work.
func (c *Controller) event(ctx context.Context, one *Work, kind, detail string) *Event {
	if c.redactor != nil {
		detail = c.redactor.RedactFor(ctx, one.Workspace, detail)
	}
	return &Event{
		ID: newEventID(), Kind: kind, Work: one.ID, Workspace: one.Workspace, Project: one.Project,
		Parent: one.Parent, Depth: one.Depth, Detail: detail, OccurredAt: time.Now().UTC(),
	}
}

// newEventID mints an identifier for a record, the same shape the store mints everything else in.
// Minted here rather than by the store, because writing the same record twice has to leave one row
// and the identifier is what makes that possible.
func newEventID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// SessionFor is the conversation a piece of work runs in.
//
// Named after the work rather than minted, so a controller that comes back to a row finds the same
// session without being told which one it was. A dispatch to a handle that exists continues that
// conversation, so an attempt after the first lands where the work has been all along.
func SessionFor(id string) string { return "work-" + id }

// The status a task row carries. They are the session's words, kept here so the controller reads a
// task without depending on the package that writes one.
const (
	StatusRunning = "running"
	StatusFailed  = "failed"
)

// oneLine keeps a reason readable on a listing: a record is read on one line beside others.
func oneLine(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	const most = 200
	if len(flat) <= most {
		return flat
	}
	return flat[:most] + "..."
}
