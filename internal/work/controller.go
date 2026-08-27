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
	"github.com/atlantic-blue/quay-crew/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
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

// DefaultLease is how long a controller's hold on a piece of work lasts before another may take it.
//
// Measured rather than chosen, and the measurement is of the loop rather than of the work. A holder
// renews its lease on every tick while its task is open, so what the lease has to outlast is a gap
// between renewals, not a task: a task killed at seventeen minutes is on this crew's own record, and
// a lease that had to outlast one would leave a dead controller unnoticed for that long.
//
// On this machine, against the real control plane and the real store with a model that answers at
// once, a tick with nothing to do cost 1 to 4 microseconds, a tick that dispatched a hundred pieces
// of work cost under a millisecond, and a whole piece of work from declared to done cost 2
// milliseconds over twenty runs. So the renewal rate is set by DefaultTickEvery and not by the work,
// and this is twelve of those intervals: a holder has to miss twelve renewals in a row before its
// work is taken. It is the same budget the crew already gives the longest healthy operation it has,
// the whole path from a session row to a sandbox ready for its first task.
//
// Provisional. What replaces it is the ninety fifth percentile of the gap between renewals over the
// first fifty completed pieces of work, which needs the metric that slice 6 adds.
const DefaultLease = 60 * time.Second

// ErrHeld is what a store says when a lease belongs to somebody else. It is not a failure: the work
// is another controller's, and this one leaves it alone.
var ErrHeld = errors.New("work: that work is held by another controller")

// Lease is who holds a piece of work and until when.
type Lease struct {
	Owner string
	Until time.Time
}

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
	// HeldWork is the work this controller is holding: running, with a session, under a lease that
	// is this controller's and has not run out. Another controller's work is not this one's to move.
	HeldWork(ctx context.Context, owner string, limit int) ([]*Work, error)
	// ExpiredWork is the work whose holder went away: running, under a lease that has run out or was
	// never written. Reading it is how a controller finds what a dead one left behind.
	ExpiredWork(ctx context.Context, limit int) ([]*Work, error)
	// StartWork claims one piece of work and takes a lease on it. It applies only to work that is
	// still pending, which is what keeps two controllers from both starting it, and it is what makes
	// a tick safe to run twice.
	StartWork(ctx context.Context, id string, lease Lease, events []*Event) (*Work, error)
	// TakeOverWork takes the lease on work whose holder went away. It applies only where the lease
	// has run out, in the same statement, so two controllers finding the same abandoned row leave
	// one holder.
	TakeOverWork(ctx context.Context, id string, lease Lease, events []*Event) (*Work, error)
	// ReleaseWork puts work back to pending. It applies only to running work with no session under a
	// lease that has run out, which is the one state that says for certain no task was ever sent.
	ReleaseWork(ctx context.Context, id string, events []*Event) (*Work, error)
	// RenewLease moves this controller's hold on further. Only the holder renews: a controller that
	// lost the row must not take it back by renewing.
	RenewLease(ctx context.Context, id string, lease Lease) error
	// RecordWorkSession writes the session the crew made for a piece of work. It is not a movement,
	// so it carries no record of its own: a reader learns which conversation did the work from the
	// row, and the row is where quay attach reads it.
	RecordWorkSession(ctx context.Context, id, session string) error
	// LandWork writes what came of the work and lets go of the lease. It applies only to work that
	// is still running.
	LandWork(ctx context.Context, id string, landed Landing, event *Event) (*Work, error)
}

// ControlPlane is what a controller may do on the crew. It is the same interface every other caller
// speaks to, deliberately: a controller holds no privileged road into anything, so it can move out
// of this process later without changing a line of its logic.
type ControlPlane interface {
	Dispatch(ctx context.Context, req *quaycrewv1.DispatchRequest) (*quaycrewv1.DispatchResponse, error)
	ListTasks(ctx context.Context, req *quaycrewv1.ListTasksRequest) (*quaycrewv1.ListTasksResponse, error)
	// ListSessions is how a controller finds the conversation a piece of work ran in when the row
	// does not say. The session is named after the work, so it can be found without being told.
	ListSessions(ctx context.Context, req *quaycrewv1.ListSessionsRequest) (*quaycrewv1.ListSessionsResponse, error)
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

// Exporter offers each record of a movement to the event log, after the transaction that wrote it.
//
// The store is the truth and the log is the copy, so this never fails what it describes and a crew
// with no broker configured loses the export and nothing else. A nil exporter is exactly that crew.
type Exporter interface {
	ExportWork(ctx context.Context, events ...*Event)
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
	exporter Exporter
	logger   *slog.Logger
	every    time.Duration
	batch    int
	// owner is what this controller writes on a lease. Two controllers must not share one, or
	// neither could tell its own work from the other's.
	owner string
	// lease is how long a hold lasts before another controller may take the work.
	lease time.Duration
}

// NewController builds one. A nil spend reader means what work cost is not written; a nil prover
// means work claiming a file stops rather than being believed.
func NewController(store Store, plane ControlPlane, spend Spend, prover Prover, logger *slog.Logger) *Controller {
	if logger == nil {
		logger = slog.Default()
	}
	return &Controller{
		store: store, plane: plane, spend: spend, prover: prover, logger: logger,
		every: DefaultTickEvery, batch: DefaultBatch, lease: DefaultLease,
		// A name of its own, minted rather than asked for, because two controllers sharing one could
		// each take the other's work by renewing a lease they never held.
		owner: "controller-" + newEventID()[:8],
	}
}

// Owned returns a controller that writes this name on the leases it takes. An investigator reading
// a released record wants the name of the thing that stopped, so a crew names its own.
func (c *Controller) Owned(owner string) *Controller {
	if strings.TrimSpace(owner) != "" {
		c.owner = strings.TrimSpace(owner)
	}
	return c
}

// Owner is the name this controller writes on a lease.
func (c *Controller) Owner() string { return c.owner }

// Leasing sets how long this controller's hold on a piece of work lasts. Zero or less leaves the
// measured default.
func (c *Controller) Leasing(lease time.Duration) *Controller {
	if lease > 0 {
		c.lease = lease
	}
	return c
}

// leaseNow is a hold that starts now and runs for this controller's lease.
func (c *Controller) leaseNow() Lease {
	return Lease{Owner: c.owner, Until: time.Now().UTC().Add(c.lease)}
}

// Exporting returns a controller that offers each record it writes to the event log, after the
// transaction that wrote it. Without one nothing is exported, which is a crew with no broker.
func (c *Controller) Exporting(exporter Exporter) *Controller {
	c.exporter = exporter
	return c
}

// exported hands records to the log after they have landed in the store, and does nothing at all
// where there is no exporter. It is called after every write rather than inside it, because a record
// that is on the log and not in the store is a record nothing can explain.
func (c *Controller) exported(ctx context.Context, events ...*Event) {
	if c.exporter == nil {
		return
	}
	c.exporter.ExportWork(ctx, events...)
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
	// Recovery first, so work a dead controller left behind is in this controller's hands before it
	// reads what it holds, and one tick both takes the work over and writes what came of it.
	c.recoverAbandoned(ctx)
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
	// Everything this controller does for this piece of work happens under the work's own trace. The
	// dispatch below then writes its task with the same identifier, which is what lets a reader join
	// a piece of work to the conversation that ran it. The context comes off the row rather than out
	// of this process, so it is the same trace after a controller has died and another took over.
	ctx = telemetry.Under(ctx, one.TraceID, one.ParentSpanID)

	handle := SessionFor(one.ID)
	lease := c.leaseNow()
	records := []*Event{
		c.event(ctx, one, EventClaimed, c.leaseDetail(lease)),
		c.event(ctx, one, EventStarted, fmt.Sprintf("attempt %d in session %s", one.Attempts+1, handle)),
	}
	claimed, err := c.store.StartWork(ctx, one.ID, lease, records)
	if err != nil {
		if !errors.Is(err, ErrNotPending) {
			// One row that cannot be claimed must not stop the others: the next tick tries it again.
			c.logger.WarnContext(ctx, "could not claim a piece of work", "work", one.ID, "error", err)
		}
		return
	}
	c.exported(ctx, records...)

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

// recoverAbandoned picks up the work a controller left behind when it went away.
//
// A controller is disposable and the work is not. The task it started keeps running, because the
// sandbox belongs to the control plane rather than to the controller, and the model does not know
// anybody stopped watching. So the rule here is: read what the crew actually did before doing
// anything, and never send a second task for work that has already been paid for.
func (c *Controller) recoverAbandoned(ctx context.Context) {
	abandoned, err := c.store.ExpiredWork(ctx, c.batch)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which work has been left behind", "error", err)
		return
	}
	for _, one := range abandoned {
		c.recover(ctx, one)
	}
}

// recover takes one abandoned piece of work back in hand.
//
// The session it ran in is what decides between the two outcomes, and where the row does not say,
// the crew is asked: the session is named after the work, so a task sent by a controller that died
// before it could write the name down is still found. Only work with no task anywhere goes back to
// pending, and that is the one state that says for certain nothing was paid for.
func (c *Controller) recover(ctx context.Context, one *Work) {
	session := one.Session
	if session == "" {
		session = c.sessionNamedAfter(ctx, one)
	}
	if session == "" {
		c.release(ctx, one)
		return
	}
	// Read the task row before writing anything. A task that has already answered must be adopted
	// rather than sent again, and this is the read that tells the two apart.
	if _, err := c.plane.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session}); err != nil {
		c.logger.WarnContext(ctx, "could not read the task of work that was left behind",
			"work", one.ID, "session", session, "error", err)
		return
	}

	lease := c.leaseNow()
	records := []*Event{
		c.event(ctx, one, EventReleased,
			fmt.Sprintf("previous owner %s, phase found %s", ownerOrNobody(one.LeaseOwner), one.Phase)),
		c.event(ctx, one, EventClaimed, c.leaseDetail(lease)),
	}
	if _, err := c.store.TakeOverWork(ctx, one.ID, lease, records); err != nil {
		if !errors.Is(err, ErrHeld) {
			c.logger.WarnContext(ctx, "could not take over work that was left behind", "work", one.ID, "error", err)
		}
		return
	}
	c.exported(ctx, records...)
	// The session goes onto the row now, so the next reader learns it from the record rather than by
	// asking the crew again.
	if one.Session == "" {
		if err := c.store.RecordWorkSession(ctx, one.ID, session); err != nil {
			c.logger.WarnContext(ctx, "could not record which session work that was left behind ran in",
				"work", one.ID, "session", session, "error", err)
		}
	}
}

// release puts work back to pending. Only where no task exists anywhere, so nothing that has been
// paid for is ever sent again.
func (c *Controller) release(ctx context.Context, one *Work) {
	records := []*Event{
		c.event(ctx, one, EventReleased,
			fmt.Sprintf("previous owner %s, phase found %s, no task was sent",
				ownerOrNobody(one.LeaseOwner), one.Phase)),
	}
	if _, err := c.store.ReleaseWork(ctx, one.ID, records); err != nil {
		if !errors.Is(err, ErrHeld) {
			c.logger.WarnContext(ctx, "could not put work that was left behind back", "work", one.ID, "error", err)
		}
		return
	}
	c.exported(ctx, records...)
}

// sessionNamedAfter is the conversation this work would have run in, if the crew holds one.
//
// This closes the gap between a dispatch and the row that records its session. A controller that
// died in between left a task running with nothing on the row to say so, and putting that work back
// to pending would send a second task and pay for it twice.
func (c *Controller) sessionNamedAfter(ctx context.Context, one *Work) string {
	listed, err := c.plane.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: one.Project})
	if err != nil {
		c.logger.WarnContext(ctx, "could not read the crew's sessions", "work", one.ID, "error", err)
		// Nothing is released on a read that failed: releasing here is what would pay twice.
		return unknownSession
	}
	handle := SessionFor(one.ID)
	for _, session := range listed.GetSessions() {
		if session.GetHandle() == handle {
			return session.GetId()
		}
	}
	return ""
}

// unknownSession is what a failed read answers with. It is not a session and it is not "no session":
// it stops this tick from doing anything, which is the safe direction.
const unknownSession = "?"

// leaseDetail is what a claim writes on the record.
func (c *Controller) leaseDetail(lease Lease) string {
	return fmt.Sprintf("lease_owner %s, lease_until %s", lease.Owner, lease.Until.Format(time.RFC3339))
}

// ownerOrNobody names the controller that held a piece of work, for a record about it being taken.
func ownerOrNobody(owner string) string {
	if owner == "" {
		return "nobody"
	}
	return owner
}

// adoptAnswers writes what came back onto every piece of work whose task has landed.
func (c *Controller) adoptAnswers(ctx context.Context) {
	started, err := c.store.HeldWork(ctx, c.owner, c.batch)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which work this controller is holding", "error", err)
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
		c.renew(ctx, one)
		return
	}
	last := tasks[len(tasks)-1]
	// Still working. Nothing to do but keep hold of it: a controller that stopped renewing while its
	// task ran would have its work taken from under it, and a controller that wrote a record on every
	// tick would fill the history with lines saying nothing happened.
	if last.GetStatus() == StatusRunning {
		c.renew(ctx, one)
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

// renew moves this controller's hold on, which is what says it is still alive. A hold that stops
// moving is the only signal another controller has that this one went away.
func (c *Controller) renew(ctx context.Context, one *Work) {
	if err := c.store.RenewLease(ctx, one.ID, c.leaseNow()); err != nil && !errors.Is(err, ErrHeld) {
		c.logger.WarnContext(ctx, "could not keep hold of a piece of work", "work", one.ID, "error", err)
	}
}

// land writes the end of a piece of work, with the record of it.
func (c *Controller) land(ctx context.Context, one *Work, landed Landing, kind string) {
	ctx = telemetry.Under(ctx, one.TraceID, one.ParentSpanID)
	detail := landed.Reason
	if detail == "" {
		detail = fmt.Sprintf("%d tokens in session %s", landed.SpentTokens, one.Session)
	}
	record := c.event(ctx, one, kind, detail)
	ended, err := c.store.LandWork(ctx, one.ID, landed, record)
	if err != nil {
		if !errors.Is(err, ErrNotRunning) {
			c.logger.WarnContext(ctx, "could not write what came of a piece of work",
				"work", one.ID, "error", err)
		}
		return
	}
	c.exported(ctx, record)
	c.drawSpans(ctx, ended)
}

// drawSpans puts the life of a piece of work on the trace, once the crew knows both ends of it.
//
// One span for the attempt that just landed and one for the whole piece of work. They are recorded
// here rather than opened when the work started, because a piece of work outlives the process that
// declared it: a span held in memory across a controller that died would be lost with it, and the
// timestamps are on the row either way.
func (c *Controller) drawSpans(ctx context.Context, ended *Work) {
	if ended == nil || ended.TraceID == "" {
		return
	}
	finished := time.Now().UTC()
	if ended.FinishedAt != nil {
		finished = *ended.FinishedAt
	}
	shared := []attribute.KeyValue{
		attribute.String("quaycrew.work", ended.ID),
		attribute.String("quaycrew.workspace", ended.Workspace),
		attribute.String("quaycrew.project", ended.Project),
		attribute.String("quaycrew.phase", ended.Phase),
	}
	if ended.StartedAt != nil {
		telemetry.Record(ctx, "work.attempt", *ended.StartedAt, finished, append(shared,
			attribute.Int("quaycrew.attempt", ended.Attempts),
			attribute.String("quaycrew.session", ended.Session))...)
	}
	// The whole life of the work, and only once it has ended: a span covering work that is still
	// running would report a duration that is not the answer to anything.
	if Terminal(ended.Phase) {
		telemetry.Record(ctx, "work", ended.CreatedAt, finished, shared...)
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
		Parent: one.Parent, Depth: one.Depth, Detail: detail, TraceID: one.TraceID,
		OccurredAt: time.Now().UTC(),
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
