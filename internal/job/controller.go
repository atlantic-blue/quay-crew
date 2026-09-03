package job

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/capacity"
	"github.com/atlantic-blue/quay-krewe/internal/publish"
	"github.com/atlantic-blue/quay-krewe/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

// DefaultTickEvery is how often the controller looks at the job the system holds.
//
// A tick costs one indexed query when there is nothing to do, which is the property the flow poller
// already has and is worth keeping. Job takes minutes, so a few seconds of delay before it starts
// is not worth a mechanism with its own timers and its own failure modes.
const DefaultTickEvery = 5 * time.Second

// DefaultBatch is how much job one tick will look at. A system with a thousand jobs is not
// a reason for one tick to hold the store open.
const DefaultBatch = 20

// DefaultLease is how long a controller's hold on a job lasts before another may take it.
//
// Measured rather than chosen, and the measurement is of the loop rather than of the job. A holder
// renews its lease on every tick while its task is open, so what the lease has to outlast is a gap
// between renewals, not a task: a task killed at seventeen minutes is on this system's own record, and
// a lease that had to outlast one would leave a dead controller unnoticed for that long.
//
// On this machine, against the real control plane and the real store with a model that answers at
// once, a tick with nothing to do cost 1 to 4 microseconds, a tick that dispatched a hundred
// jobs cost under a millisecond, and a whole job from declared to done cost 2
// milliseconds over twenty runs. So the renewal rate is set by DefaultTickEvery and not by the job,
// and this is twelve of those intervals: a holder has to miss twelve renewals in a row before its
// job is taken. It is the same budget the system already gives the longest healthy operation it has,
// the whole path from a session row to a sandbox ready for its first task.
//
// Provisional. What replaces it is the ninety fifth percentile of the gap between renewals over the
// first fifty completed jobs, which needs the metric that slice 6 adds.
const DefaultLease = 60 * time.Second

// ErrHeld is what a store says when a lease belongs to somebody else. It is not a failure: the job
// is another controller's, and this one leaves it alone.
var ErrHeld = errors.New("job: that job is held by another controller")

// Lease is who holds a job and until when.
type Lease struct {
	Owner string
	Until time.Time
}

// ErrNotPending is what a store says when the claim did not apply, which means another controller
// claimed the same row first. It is not a failure: the row is somebody else's now.
var ErrNotPending = errors.New("job: that job is no longer pending")

// ErrNotRunning is the same answer for a landing: the row moved on before this controller wrote
// what it read. It is also what a session asking a question about a job that has already ended is
// told: a job nothing is running has nobody to answer to.
var ErrNotRunning = errors.New("job: that job is no longer running")

// ErrNotAsking is what a store says when an answer arrived for a job that is not waiting for one.
// A question already answered, or one nobody asked, and in both cases the answer is not applied.
var ErrNotAsking = errors.New("job: that job is not waiting to be told anything")

// ErrNotFailed is what a store says when a job is continued or refused and it did not fail. Both
// verbs are answers to a failure, so both refuse the same rows, and a job already going again is one
// of them: resuming twice must leave one attempt rather than two tasks against one conversation.
var ErrNotFailed = errors.New("job: that job did not fail, so there is nothing to continue")

// ErrNothingHandedOver is what a store says when a job would be moved to a fresh session and the
// session that had it wrote nothing down.
//
// The condition lives in the write rather than in a read before it, for the reason every other one
// here does: the session is still answering while the controller decides, so a handoff written a
// moment later must not be missed and a job with none must not be moved on a stale read. What it
// protects is the only thing a fresh session would have to start from.
var ErrNothingHandedOver = errors.New("job: that job has no handoff, so a fresh session would start from nothing")

// Requeue is a job put back to pending, and why it is waiting again.
type Requeue struct {
	// Owner is the controller giving the job up. The write applies only where that controller still
	// holds the lease, so a controller that lost the row cannot put another controller's job back.
	Owner string
	// Reason is what a reader is told while the job waits. A pending job with nothing said about it
	// reads as one nobody has reached yet, and this one has been reached and turned away.
	Reason string
}

// Landing is what came of a job, written onto the row in one movement.
type Landing struct {
	Phase  string
	Answer string
	// Outcome is the one word the answer stated, read off it rather than reported by the model, the
	// way the pull request address is. Empty on every landing that is not an answer: a job the model
	// never finished has nothing to state.
	Outcome     string
	Reason      string
	SpentTokens int64
	// PullRequest is the address the answer named, where the job named a repository. It is read off
	// the answer rather than reported by the model, the way an expectation is.
	PullRequest string
	// Reviewed and Tested are the gates that passed this work, in sessions that did not do it. They
	// are written here rather than as the gate runs, because until the job lands they can be read off
	// the record of those sessions, and a status a controller has to remember is a status the next
	// controller does not have.
	Reviewed bool
	Tested   bool
	// Attempt is what this attempt at the job produced, with how like the earlier attempts at the same
	// step it was. It is written in the same transaction as the landing, keyed on the task, so a
	// controller reading a task a dead one already read leaves one row rather than two.
	Attempt *Attempt
}

// Store is the rows a controller reads and writes. Every write takes the event that describes it, so
// the row and the record of how it moved land in one transaction or neither does.
type Store interface {
	// RunnableJob is the job this controller may start: pending with nothing it waits for, oldest
	// declared first. Job under a parent is included, because a flow run declares every step under
	// its own job. Job that names a role is included, and the controller runs it as that role.
	//
	// Both this and HeldJob carry the job's handoffs, because the conversation a job runs in is
	// derived from them: each handover moves the name on, and a controller reading a job without them
	// would send the rest of that job straight back into the conversation that was full.
	RunnableJob(ctx context.Context, limit int) ([]*Job, error)
	// HeldJob is the job this controller is holding: running, with a session, under a lease that
	// is this controller's and has not run out. Another controller's job is not this one's to move.
	HeldJob(ctx context.Context, owner string, limit int) ([]*Job, error)
	// ExpiredJob is the job whose holder went away: running, under a lease that has run out or was
	// never written. Reading it is how a controller finds what a dead one left behind.
	ExpiredJob(ctx context.Context, limit int) ([]*Job, error)
	// WorkspaceLimits is what a workspace lets its job do, which the controller reads for the lease
	// length: a workspace whose store is slow is the operator's to give a longer hold.
	WorkspaceLimits(ctx context.Context, workspace string) (Limits, error)
	// HoldJob says on a pending job why it was not started, leaving it pending. A job the machine
	// has no room for is not failed and not moved: it waits, and the reason is what tells an
	// operator that the system is full rather than stalled.
	HoldJob(ctx context.Context, id, reason string, event *Event) (*Job, error)
	// StartJob claims one job and takes a lease on it. It applies only to a job that is
	// still pending, which is what keeps two controllers from both starting it, and it is what makes
	// a tick safe to run twice.
	StartJob(ctx context.Context, id string, lease Lease, events []*Event) (*Job, error)
	// TakeOverJob takes the lease on a job whose holder went away. It applies only where the lease
	// has run out, in the same statement, so two controllers finding the same abandoned row leave
	// one holder.
	TakeOverJob(ctx context.Context, id string, lease Lease, events []*Event) (*Job, error)
	// ReleaseJob puts job back to pending. It applies only to running job with no session under a
	// lease that has run out, which is the one state that says for certain no task was ever sent.
	ReleaseJob(ctx context.Context, id string, events []*Event) (*Job, error)
	// HandOffJob puts a running job back to pending and lets go of its session, so the next start
	// dispatches into a fresh conversation carrying what the last one wrote down. It applies only
	// where this controller still holds the lease, in the same statement, for the reason a requeue
	// does: a controller that lost the row must not take another one's job away from the session
	// doing it. The job does not restart, and nothing it produced is dropped.
	HandOffJob(ctx context.Context, id string, back Requeue, event *Event) (*Job, error)
	// RequeueJob puts a running job back to pending because the system could not start it. It applies
	// only where this controller still holds the lease, in the same statement, so a controller that
	// lost the row cannot put another controller's job back under it.
	//
	// It is not ReleaseJob. That one is for a row nobody holds and no task was ever sent for, found
	// by a later controller; this one is the holder itself giving up an attempt it made.
	RequeueJob(ctx context.Context, id string, back Requeue, events []*Event) (*Job, error)
	// RenewLease moves this controller's hold on further. Only the holder renews: a controller that
	// lost the row must not take it back by renewing.
	RenewLease(ctx context.Context, id string, lease Lease) error
	// RecordJobSession writes the session the system made for a job. It is not a movement,
	// so it carries no record of its own: a reader learns which conversation did the job from the
	// row, and the row is where krewe attach reads it.
	RecordJobSession(ctx context.Context, id, session string) error
	// LandJob writes what came of the job and lets go of the lease. It applies only to a job that
	// is still running.
	LandJob(ctx context.Context, id string, landed Landing, event *Event) (*Job, error)
	// GetJob reads one job whole: what its session finished, and what its attempts said. A listing
	// carries neither, and a loop cannot be read without both.
	GetJob(ctx context.Context, id string) (*Job, error)
	// LoopJob writes that a job went in circles and takes the route the job declared, in one
	// movement. It applies only to a running job this controller still holds.
	LoopJob(ctx context.Context, id string, looped Loop, event *Event) (*Job, error)
	// ProposeJobPlan writes the plan the crew wrote and puts the question about it to a person, in one
	// movement. It applies only to a running job, which is what asking already applies to.
	//
	// One movement because the two halves are one fact. A reader who found a job asking with no plan
	// on it would be looking at a question about nothing, and a plan on a running row would be a plan
	// nobody was ever asked to approve.
	ProposeJobPlan(ctx context.Context, id, plan, question string, event *Event) (*Job, error)
	// ProposeJobIdeation writes what the session said it understood and puts the questions to a
	// person, in one movement, for the reason above: a job asking with no record on it is a question
	// about nothing.
	ProposeJobIdeation(ctx context.Context, id, understood, question string, event *Event) (*Job, error)
	// ProposeJobDesign writes the list of verticals the crew said it would build and puts it to a
	// person, in one movement, so a reader never finds a job asking with no list on it.
	ProposeJobDesign(ctx context.Context, id, design, question string, event *Event) (*Job, error)
	// RecordJobTests writes the record of the requirements this job turned into failing tests, on a
	// pending job that has none yet. The job is pending while its workers run, because the row itself
	// is doing nothing: what is running is one job for each requirement, each in its own session.
	RecordJobTests(ctx context.Context, id, tests string, event *Event) (*Job, error)
	// AskAboutJobTests puts the question about a suite that is not red to a person, from the pending
	// phase. It is the one ask a job makes without a session behind it: what finished is its workers.
	AskAboutJobTests(ctx context.Context, id, question string, event *Event) (*Job, error)
	// HoldJobForAcceptance writes what this job's verticals were built into and stops the job for a
	// person to accept it, in one movement, so a reader never finds a built job carrying on as though
	// somebody had already looked at it.
	HoldJobForAcceptance(ctx context.Context, id, built, question string,
		events ...*Event) (*Job, error)
	// AskAboutJobBuild puts the question about verticals that are not built to a person, from the
	// pending phase, for the reason the ask about the tests is made from there.
	AskAboutJobBuild(ctx context.Context, id, question string, event *Event) (*Job, error)
	// AcceptJob writes that a person looked at a picture of what was built and said the value arrived.
	// The row stays pending under it, because the acceptance is permission rather than an ending: what
	// refuses done to a job that never got it is NotYetAccepted.
	AcceptJob(ctx context.Context, id, answer string, events ...*Event) (*Job, error)
	// SendJobBackToBuild clears what was built and puts the job back to pending, so the build stage
	// fans out again against what the person said was missing. It applies to a job that carries a build
	// record and no acceptance, so an accepted job can never be sent back over its own acceptance.
	SendJobBackToBuild(ctx context.Context, id string, events ...*Event) (*Job, error)
	// CreateJob declares one job and the record of declaring it, in one transaction.
	CreateJob(ctx context.Context, declared *Job, event *Event) error
	// JobsClaiming is the jobs in one workspace claiming any of these pieces of work, whole.
	JobsClaiming(ctx context.Context, workspace string, claims []string) ([]*Job, error)
	// CreateExecution writes one run of one stage of one job, which is how a stage fans out. The
	// claim on the requirement or the vertical is refused here, so two controllers ticking one stage
	// at the same moment run one session and not two. See execution.go.
	CreateExecution(ctx context.Context, run *Execution, event *Event) error
	// ListExecutions is the runs of one stage of one job, oldest first. It is how a stage gathers:
	// what has run for each requirement or vertical, and what the newest of them answered.
	ListExecutions(ctx context.Context, filter ExecutionFilter) ([]*Execution, error)
	// The same four questions the job queries above answer, asked of the runs. A run is dispatched,
	// held, taken over and landed the way a job is, and it passes through no stages and no gates.
	RunnableExecutions(ctx context.Context, limit int) ([]*Execution, error)
	HeldExecutions(ctx context.Context, owner string, limit int) ([]*Execution, error)
	ExpiredExecutions(ctx context.Context, limit int) ([]*Execution, error)
	StartExecution(ctx context.Context, id string, lease Lease, event *Event) (*Execution, error)
	TakeOverExecution(ctx context.Context, id string, lease Lease) (*Execution, error)
	RenewExecutionLease(ctx context.Context, id string, lease Lease) error
	RecordExecutionSession(ctx context.Context, id, session string) error
	RecordExecutionBranch(ctx context.Context, id, branch string) error
	LandExecution(ctx context.Context, id string, landed ExecutionLanding, event *Event) (*Execution, error)
	// AskJob puts a question to a person and stops the job there. It applies only to a running job.
	//
	// The controller reaches for it where a session answered that a person has to decide, so that job
	// and a job whose session called krewe job ask reach one state rather than two. A second state for
	// the same fact would mean every reader of what waits on a person had to know both.
	AskJob(ctx context.Context, id, question string, event *Event) (*Job, error)
	// IdleSandboxes is the fourth query: the sessions that still hold a container and nothing is
	// holding open, oldest touched first. A session is not a second resource with a declaration of its
	// own, so what is wanted of it is derived from the job that names it, and a job still in flight
	// keeps its session alive.
	IdleSandboxes(ctx context.Context, limit int) ([]*quaycrewv1.Session, error)
	// ReclaimedSessions is the same question about the sessions whose container has already gone,
	// longest reclaimed first. It is a second query rather than half of the one above because a
	// reclaimed session never leaves that set where no archive time is set, and a single batch fills
	// with rows nothing can move while a sandbox sits behind them holding a whole machine. See issue 575.
	ReclaimedSessions(ctx context.Context, limit int) ([]*quaycrewv1.Session, error)
	// AnythingMoving and TurnedAwayJob are the fifth comparison, and it is the one that says whether
	// the four above are working. Nothing moving with something held is a state that is always wrong,
	// and it is the state this system sat in for an hour with twelve idle sandboxes holding the
	// machine. The probe runs on every tick; the listing runs only when the probe says nothing moves.
	AnythingMoving(ctx context.Context) (bool, error)
	TurnedAwayJob(ctx context.Context, limit int) ([]*Job, error)
}

// ControlPlane is what a controller may do on the system. It is the same interface every other caller
// speaks to, deliberately: a controller holds no privileged road into anything, so it can move out
// of this process later without changing a line of its logic.
type ControlPlane interface {
	Dispatch(ctx context.Context, req *quaycrewv1.DispatchRequest) (*quaycrewv1.DispatchResponse, error)
	ListTasks(ctx context.Context, req *quaycrewv1.ListTasksRequest) (*quaycrewv1.ListTasksResponse, error)
	// ListSessions is how a controller finds the conversation a job ran in when the row
	// does not say. The session is named after the job, so it can be found without being told.
	ListSessions(ctx context.Context, req *quaycrewv1.ListSessionsRequest) (*quaycrewv1.ListSessionsResponse, error)
	// ReclaimSession takes a settled session's container back and leaves everything else. Only the
	// control plane can close a sandbox, so the controller asks rather than doing it.
	ReclaimSession(ctx context.Context, req *quaycrewv1.ReclaimSessionRequest) (*quaycrewv1.ReclaimSessionResponse, error)
	// ArchiveSession files a session away. It is the same call an operator makes, with no operator
	// behind it, which is the whole of what the controller does at the end of a session's life.
	ArchiveSession(ctx context.Context, req *quaycrewv1.ArchiveSessionRequest) (*quaycrewv1.ArchiveSessionResponse, error)
}

// Attachment says whether an operator has a session's conversation open.
//
// It exists because the controller must never close a container somebody is typing into, and the
// system could not tell before this: `krewe attach` hands the operator a command to run against the
// container and then records nothing about it. The implementation asks the container itself.
//
// An implementation that cannot answer returns an error, and the controller reads that as attached.
// The two mistakes are not the same size: being wrong one way holds a container a little longer, and
// being wrong the other way closes a conversation under somebody's hands.
type Attachment interface {
	SessionAttached(ctx context.Context, session string) (bool, error)
}

// Room is the machine, asked whether it can host one more sandbox before a job is started.
//
// This is the kubernetes shape and it is deliberate. A scheduler places a pod only where the
// requests already on the node plus this one still fit, and a pod that fits nowhere stays pending
// with a reason naming the resource, for as long as it takes. It is never admitted and then killed,
// which is what the system did: nine jobs against a runtime with room for eight, and the ninth waited
// two minutes for a container, was failed, and took the runtime down with it. See issue 466.
//
// Admit reserves the room in the same movement as the decision, under the caller's key. It has to:
// a dispatch is detached, so the container appears seconds later, and nine jobs admitted against one
// reading of an empty machine all fit. Whoever admits is responsible for releasing what it took on
// every road that does not end in a sandbox.
type Room interface {
	Admit(ctx context.Context, key string, want capacity.Request) capacity.Verdict
	Release(key string)
}

// Spend is what one session's conversation has cost. An implementation that cannot tell answers zero.
type Spend interface {
	SessionTokens(ctx context.Context, session string) int64
}

// Windows is how full one session's context window is, and how big that window is.
//
// A different question from what the conversation cost, and the one that decides whether the session
// should be given anything else to do: cost only grows, while the window empties again when the model
// compacts. See ceiling.go for what the controller does with it.
//
// An implementation that cannot tell answers zero for the size, and a window of no size never refuses
// anything. A controller with no reader wired refuses nothing at all, which is every controller before
// this and is what a system that cannot measure should do.
type Windows interface {
	SessionWindow(ctx context.Context, session string) (used, size int64)
}

// Redactor takes what the system is about to write down and removes anything the workspace keeps
// sealed. Everything recorded here is persisted, and what a model says can carry a value somebody
// pasted into a conversation. A nil redactor writes the text as it is, which is what a test wants
// and what a system must never do.
type Redactor interface {
	RedactFor(ctx context.Context, workspace, text string) string
}

// Exporter offers each record of a movement to the event log, after the transaction that wrote it.
//
// The store is the truth and the log is the copy, so this never fails what it describes and a system
// with no broker configured loses the export and nothing else. A nil exporter is exactly that system.
type Exporter interface {
	ExportJob(ctx context.Context, events ...*Event)
}

// Roles is how a controller reads the role a job names, as the workspace holds it now.
//
// Now rather than at the version the job pinned, because it is the same role the system would build
// the session from: two answers to "which role is this" is how a boundary comes to be checked
// against one role and applied against another. A system that cannot answer refuses the job, because
// running it would run it as nobody.
type Roles interface {
	RoleFor(ctx context.Context, workspace, named string) (Receiver, error)
}

// Revoker takes back the credentials the system minted for a job, once that job has ended.
//
// It is what ends a session's credential in a working system: a session stops being able to call
// because its job is over, and the credential's own expiry is only the backstop behind that. A system
// with no revoker leans on the clock and nothing else.
type Revoker interface {
	RevokeJobCredentials(job, phase string)
}

// Prover answers what a job said would show its task did the job.
//
// An implementation that cannot answer says so, and the job stops. A check that quietly passes when
// it could not be run is the same false green as no check at all.
type Prover interface {
	SessionHolds(ctx context.Context, session, path string) (bool, error)
}

// Publisher puts the work a session finished where somebody else can read it, and says what it found.
//
// It answers with a state rather than with an error, because "the system could not look" is one of
// the answers and must never be reported as one of the others. A controller with no publisher is a
// system that cannot reach a session's files at all, which is a state too and says so.
type Publisher interface {
	PublishSessionWork(ctx context.Context, session string) publish.Work
	// PushSessionWork puts what a session committed onto one named branch, which is how the work of
	// several sessions at once arrives in one place. It replays onto whatever is already there, so two
	// workers that wrote different files both survive it.
	PushSessionWork(ctx context.Context, session, branch string) publish.Work
}

// Controller makes reality match the job the system holds.
//
// It reads the rows, compares them against the world, and closes the gap: it sends a task for a job
// that has not started, and writes what came back onto the row for a job whose task has landed. It
// never waits on a model. A dispatch lets go of its task, and the answer is read off the record on a
// later tick, so a task that runs for an hour costs the loop nothing.
//
// One movement per job per tick. Nothing here loops until a row is finished, because a
// controller that drives one row to the end is a controller that is not watching the others.
type Controller struct {
	store    Store
	plane    ControlPlane
	spend    Spend
	prover   Prover
	roles    Roles
	redactor Redactor
	// attached is how the controller asks whether an operator has a session's conversation open. Nil
	// means the system cannot tell, and a system that cannot tell reclaims nothing.
	attached Attachment
	// room is the machine. Nil admits everything, which is the system this controller had before
	// admission was arithmetic, and it says so in the log rather than quietly.
	room Room
	// windows is how full each session's context window is. Nil refuses nothing, which is every
	// controller before the ceiling existed.
	windows  Windows
	exporter Exporter
	// publisher pushes the branch a finished job left behind, and says where the work is. Nil cannot
	// reach a session's files at all, and the reason says that rather than naming a path this system
	// does not have.
	publisher Publisher
	// revoker takes a job's credentials back when the job ends. Nil takes none back, and then a
	// credential lasts until it runs out.
	revoker Revoker
	logger  *slog.Logger
	every   time.Duration
	batch   int
	// owner is what this controller writes on a lease. Two controllers must not share one, or
	// neither could tell its own job from the other's.
	owner string
	// lease is how long a hold lasts before another controller may take the job.
	lease time.Duration
}

// givenUp is the job one tick has put back to pending for want of a sandbox.
//
// It is born and dies inside a tick, and it is passed rather than kept, so nothing about it outlives
// the pass that made it. The row is still the truth: a controller that lost this would start the job
// a tick early rather than get anything wrong. What it buys is the one movement per job per tick
// this loop is built on, so a job turned away is left pending for something to read rather than
// given up and started again inside one pass.
type givenUp map[string]bool

// NewController builds one. A nil spend reader means what job cost is not written; a nil prover
// means job claiming a file stops rather than being believed.
func NewController(store Store, plane ControlPlane, spend Spend, prover Prover, logger *slog.Logger) *Controller {
	if logger == nil {
		logger = slog.Default()
	}
	return &Controller{
		store: store, plane: plane, spend: spend, prover: prover, logger: logger,
		every: DefaultTickEvery, batch: DefaultBatch, lease: DefaultLease,
		// A name of its own, minted rather than asked for, because two controllers sharing one could
		// each take the other's job by renewing a lease they never held.
		owner: "controller-" + newRowID()[:8],
	}
}

// Owned returns a controller that writes this name on the leases it takes. An investigator reading
// a released record wants the name of the thing that stopped, so a system names its own.
func (c *Controller) Owned(owner string) *Controller {
	if strings.TrimSpace(owner) != "" {
		c.owner = strings.TrimSpace(owner)
	}
	return c
}

// Owner is the name this controller writes on a lease.
func (c *Controller) Owner() string { return c.owner }

// Leasing sets how long this controller's hold on a job lasts. Zero or less leaves the
// measured default.
func (c *Controller) Leasing(lease time.Duration) *Controller {
	if lease > 0 {
		c.lease = lease
	}
	return c
}

// leaseIn is a hold as long as that workspace says, or as long as this controller's own where the
// workspace says nothing. A workspace whose store is slow is the operator's to give more room.
func (c *Controller) leaseIn(ctx context.Context, workspace string) Lease {
	return Lease{Owner: c.owner, Until: time.Now().UTC().Add(c.limitsIn(ctx, workspace).Lease(c.lease))}
}

// limitsIn is what this workspace says about its jobs, and nothing where it could not be read. A
// start reads them once and takes two answers from them, the length of the hold and the size of the
// sandbox, rather than asking the store the same question twice for one job.
func (c *Controller) limitsIn(ctx context.Context, workspace string) Limits {
	limits, err := c.store.WorkspaceLimits(ctx, workspace)
	if err != nil {
		return Limits{Workspace: workspace}
	}
	return limits
}

// Reading returns a controller that reads the role a job names. Without one, job that
// names a role is stopped rather than run, because a boundary nobody can read is not a boundary.
func (c *Controller) Reading(roles Roles) *Controller {
	c.roles = roles
	return c
}

// Exporting returns a controller that offers each record it writes to the event log, after the
// transaction that wrote it. Without one nothing is exported, which is a system with no broker.
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
	c.exporter.ExportJob(ctx, events...)
}

// Redacting returns a controller that takes every line it writes through the system redactor.
func (c *Controller) Redacting(redactor Redactor) *Controller {
	c.redactor = redactor
	return c
}

// Measuring gives the controller a reader for how full each session's context window is, which is
// what turns the ceiling on. A controller with none gives every session new work, however full it is.
func (c *Controller) Measuring(windows Windows) *Controller {
	c.windows = windows
	return c
}

// Placing returns a controller that asks the machine for room before it starts a job. Without one
// it starts whatever it reads, which is admission by count with the count taken off.
func (c *Controller) Placing(room Room) *Controller {
	c.room = room
	return c
}

// Publishing returns a controller that publishes the work a job leaves behind when it stops without
// a pull request. Without one the system can neither push nor say where the work is, and the reason
// says exactly that.
func (c *Controller) Publishing(publisher Publisher) *Controller {
	c.publisher = publisher
	return c
}

// published is what the system found and did with the work of a job that is about to stop.
//
// Asked once, at the moment of stopping, and never while a job is running: it costs commands inside a
// container, and a job still working has not finished the work this is about.
func (c *Controller) published(ctx context.Context, one *Job) publish.Work {
	if c.publisher == nil {
		return publish.Work{
			State: publish.Unreadable,
			Why:   "this system has no way to reach a session's files",
		}
	}
	return c.publisher.PublishSessionWork(ctx, one.Session)
}

// Revoking returns a controller that takes a job's credentials back as it writes the end of that
// job. Without one a credential outlives its job, until it runs out on its own.
func (c *Controller) Revoking(revoker Revoker) *Controller {
	c.revoker = revoker
	return c
}

// revoked takes back the credentials minted for a job that has ended, after the end is in the store.
// After rather than before, because a credential taken back over a write that then failed would
// leave a session that is still working unable to call.
func (c *Controller) revoked(ended *Job) {
	if c.revoker == nil || ended == nil || !Terminal(ended.Phase) {
		return
	}
	c.revoker.RevokeJobCredentials(ended.ID, ended.Phase)
}

// Watching returns a controller that can ask whether an operator is in a session before taking its
// container back. Without it the controller reclaims nothing, whatever the times say.
func (c *Controller) Watching(attached Attachment) *Controller {
	c.attached = attached
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
	// Once on the way up, so a system restarted onto job that was declared while it was down starts
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
	turnedAway := givenUp{}
	// Recovery first, so job a dead controller left behind is in this controller's hands before it
	// reads what it holds, and one tick both takes the job over and writes what came of it.
	c.recoverAbandoned(ctx)
	c.recoverAbandonedExecutions(ctx)
	c.adoptAnswers(ctx, turnedAway)
	// The runs of the stages, in the same order and for the same reasons. They are a separate pass
	// because a run is not a job: it answers no gate, writes no plan and settles nothing. See
	// executionrun.go.
	c.adoptExecutions(ctx)
	c.startDeclared(ctx, turnedAway)
	c.runExecutions(ctx)
	// Last but one, so a session this tick has just dispatched into is not a candidate on the same
	// pass. The three above decide what is wanted alive; this one acts on what is left.
	c.putAway(ctx)
	// Last, and the only comparison here that is about the loop rather than about a row. The four
	// above are what this system is meant to do; this one asks whether it is doing any of it, and
	// takes a container back when the answer is no. See stall.go and issue 575.
	c.unstick(ctx)
}

// startDeclared sends a task for each job that has not started.
//
// The claim comes first and the dispatch second. A claim that did not apply is another controller
// holding the row, and it is left alone rather than dispatched for twice: job is paid for, so a
// second dispatch is a second bill.
func (c *Controller) startDeclared(ctx context.Context, turnedAway givenUp) {
	runnable, err := c.store.RunnableJob(ctx, c.batch)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which job is ready to run", "error", err)
		return
	}
	// Worked out once for the whole pass, and only if a job is actually turned away. A burst of
	// twenty held jobs must not be twenty probes of the same question.
	moving := &stillMoving{}
	for _, one := range runnable {
		// Turned away a moment ago, in this same pass. Starting it again here would ask the machine
		// that had no room whether it has room yet, without anything having had a chance to change.
		if turnedAway[one.ID] {
			continue
		}
		c.start(ctx, one, moving)
	}
}

// start claims one job and sends its task.
func (c *Controller) start(ctx context.Context, one *Job, moving *stillMoving) {
	// Everything this controller does for this job happens under the job's own trace. The
	// dispatch below then writes its task with the same identifier, which is what lets a reader join
	// a job to the conversation that ran it. The context comes off the row rather than out
	// of this process, so it is the same trace after a controller has died and another took over.
	ctx = telemetry.Under(ctx, one.TraceID, one.ParentSpanID)

	// A job whose accepted requirements have no failing tests yet never gets a session of its own. The
	// work of this stage is done by one job for each requirement, and this row waits for them, so it
	// takes no room on the machine and pays for no container.
	if WaitingForItsTests(one) {
		c.writeTheTests(ctx, one)
		return
	}

	// A job whose plan a person approved and whose suite is red never gets a session of its own either.
	// The work of this stage is done by one job for each vertical, all at once, and this row waits for
	// them under the same arithmetic the stage before it waits under.
	if WaitingForItsBuild(one) {
		c.buildIt(ctx, one)
		return
	}

	// A job a person has answered about what was built never gets a session either, and this is the
	// only stage where that is true of the answer rather than of the work. Their word is the last thing
	// that happens: it lands the job, or it sends the verticals back to be built again. A session
	// started here would be a session carrying on past the acceptance, which is work nobody accepted.
	if WaitingForItsAcceptance(one) {
		c.acceptIt(ctx, one)
		return
	}

	// The conversation this attempt runs in. It is named after the job, so a dispatch made twice lands
	// where the job has been all along, and it moves on once per handoff: a session that reached the
	// ceiling is exactly the conversation this attempt must not land in.
	handle := ConversationFor(one)
	limits := c.limitsIn(ctx, one.Workspace)
	lease := Lease{Owner: c.owner, Until: time.Now().UTC().Add(limits.Lease(c.lease))}

	// The machine is asked before the row is claimed, because a job the machine cannot host must
	// stay pending rather than start and be killed. The room is taken in the same movement as the
	// answer, so the next job in this same pass counts it: nine jobs asking one ten second old
	// reading whether it is empty all get told yes, which is how nine went onto a machine with room
	// for eight. Every road below that does not end in a dispatch gives it back.
	key := capacity.KeyFor(one.Project, handle)
	if verdict := c.admit(ctx, key, limits.Request(capacity.DefaultRequest())); !verdict.OK {
		c.hold(ctx, one, c.whyItWaits(ctx, verdict.Reason, moving))
		return
	}

	// The boundary is read before the claim is described, so job that is refused carries a claim and
	// a refusal on its record and no line saying a task was started that never was.
	refusal := c.refusedMaterial(ctx, one)
	records := []*Event{c.event(ctx, one, EventClaimed, c.leaseDetail(lease))}
	if refusal == "" {
		records = append(records,
			c.event(ctx, one, EventStarted, fmt.Sprintf("attempt %d in session %s", one.Attempts+1, handle)))
	}
	claimed, err := c.store.StartJob(ctx, one.ID, lease, records)
	if err != nil {
		if !errors.Is(err, ErrNotPending) {
			// One row that cannot be claimed must not stop the others: the next tick tries it again.
			c.logger.WarnContext(ctx, "could not claim a job", "job", one.ID, "error", err)
		}
		// The row is somebody else's, so the room this controller took for it is not its to hold.
		c.releaseRoom(key)
		return
	}
	c.exported(ctx, records...)
	// Claimed and then stopped, with nothing dispatched. The claim is what keeps another controller
	// from picking the same row up while this one writes the refusal.
	if refusal != "" {
		c.releaseRoom(key)
		c.land(ctx, claimed, Landing{Phase: PhaseStopped, Reason: refusal}, EventStopped)
		return
	}

	// Detached, because a controller that waits on a model is a controller that stops controlling.
	// The answer is read off the task record on a later tick.
	//
	// The handle is derived from the job, so a dispatch that has to be made again lands in the same
	// conversation rather than starting a second one.
	//
	// What that conversation is asked for is normally the job. Where it has already filled its context
	// window, it is the handoff instead and then nothing else: a job continued after a failure goes
	// back into the session that did the work, and that session can be the full one, which is how the
	// task that ends a job came to be run in the fullest window the job ever had.
	text := Asked(claimed)
	if c.pastTheCeiling(ctx, claimed, limits.ContextCeiling()) {
		text = AskedForAHandoff(claimed, limits.ContextCeiling())
	}
	sent, err := c.plane.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: claimed.Project, Handle: handle, Text: text,
		PermissionMode: claimed.Mode, Detach: true,
		// The name this job was declared with, so a listing says which conversation is doing which job
		// while they are all still running. It reaches the session only when the session is made, so a
		// dispatch made again after a controller died cannot put it over a label the operator has set.
		Title: claimed.Title,
		// The role comes off the row and never from a caller. A caller that could name its own role
		// could name one granting more than the job was declared with, and the credential the system
		// mints for this task carries what that role declared it may call. A job handed on after it went
		// in circles runs as the role it was handed to, which is the whole of what handing it on means.
		Role: RoleNow(claimed),
		// Which job this task runs, so the system mints the credential for it and puts it in
		// the environment of this task alone.
		Job: claimed.ID,
	})
	if err == nil {
		if err := c.store.RecordJobSession(ctx, claimed.ID, sent.GetId()); err != nil {
			c.logger.WarnContext(ctx, "could not record which session a job runs in",
				"job", claimed.ID, "session", sent.GetId(), "error", err)
		}
		return
	}
	// A job whose task could not be started is failed with the reason rather than left running with
	// nothing behind it, which is a row nobody can tell from job in progress. Its room goes back:
	// there is no sandbox and there is not going to be one.
	c.releaseRoom(key)
	c.land(ctx, claimed, Landing{Phase: PhaseFailed, Reason: oneLine(err.Error())}, EventFailed)
}

// admit asks the machine for room for this job's sandbox. A controller with no machine to ask admits
// everything, which is what every controller did before this, and it says so once rather than
// quietly: a system running blind should read as running blind.
func (c *Controller) admit(ctx context.Context, key string, want capacity.Request) capacity.Verdict {
	if c.room == nil {
		return capacity.Verdict{OK: true, Unmeasured: true}
	}
	verdict := c.room.Admit(ctx, key, want)
	if verdict.Unmeasured {
		c.logger.WarnContext(ctx, "the system cannot read its runtime, so this job was admitted without arithmetic",
			"job", key, "wants", want.String())
	}
	return verdict
}

// releaseRoom gives back what this controller reserved for a sandbox that will not be made.
func (c *Controller) releaseRoom(key string) {
	if c.room != nil {
		c.room.Release(key)
	}
}

// hold says on a pending job why the system is not starting it.
//
// The phase does not move. The job is still the next thing this system will run and nothing about it
// has failed, which is the whole difference between this and stopping it: a machine that is full
// now has room in ten minutes, and the job is still there when it does. What an operator gets is the
// sentence, because "pending" alone reads the same on a busy system and a stalled one.
//
// Written only when the sentence changes. A controller ticks every few seconds and a machine stays
// full for minutes, so writing every tick would be a row update and a record a second saying the
// same thing.
func (c *Controller) hold(ctx context.Context, one *Job, reason string) {
	if one.Reason == reason {
		return
	}
	record := c.event(ctx, one, EventHeld, reason)
	if _, err := c.store.HoldJob(ctx, one.ID, reason, record); err != nil {
		if !errors.Is(err, ErrNotPending) {
			c.logger.WarnContext(ctx, "could not say why a job is being held", "job", one.ID, "error", err)
		}
		return
	}
	c.exported(ctx, record)
	c.logger.InfoContext(ctx, "a job is waiting for room on the machine", "job", one.ID, "reason", reason)
}

// refusedMaterial is why a job cannot be given to the role it names, and empty where the
// boundary holds or there is no role.
//
// Read here rather than only at the write, because a role can be detached, imported again at a new
// version and attached again while a job sits pending: what the system would put in front of a session
// is settled at the moment it hands it over. A system that cannot read the role refuses the job, for
// the reason an unprovable expectation stops it: a check that quietly passes when it could not be
// run is the same false green as no check at all.
func (c *Controller) refusedMaterial(ctx context.Context, one *Job) string {
	// The role doing the job now, which is the one it was handed to where it went in circles. The
	// boundary is checked against whoever the session will actually run as, or a handoff would put a
	// job in front of a role nobody held it against.
	named := RoleNow(one)
	if named == "" {
		return ""
	}
	if c.roles == nil {
		return fmt.Sprintf("this job runs as the %s role and this system cannot read its roles, "+
			"so what the role receives could not be checked", named)
	}
	held, err := c.roles.RoleFor(ctx, one.Workspace, named)
	if err != nil {
		return oneLine(err.Error())
	}
	if material := Unreceived(one.Requires, held); material != "" {
		return RefusedMaterial(named, material)
	}
	return ""
}

// recoverAbandoned picks up the job a controller left behind when it went away.
//
// A controller is disposable and the job is not. The task it started keeps running, because the
// sandbox belongs to the control plane rather than to the controller, and the model does not know
// anybody stopped watching. So the rule here is: read what the system actually did before doing
// anything, and never send a second task for a job that has already been paid for.
func (c *Controller) recoverAbandoned(ctx context.Context) {
	abandoned, err := c.store.ExpiredJob(ctx, c.batch)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which job has been left behind", "error", err)
		return
	}
	for _, one := range abandoned {
		c.recover(ctx, one)
	}
}

// recover takes one abandoned job back in hand.
//
// The session it ran in is what decides between the two outcomes, and where the row does not say,
// the system is asked: the session is named after the job, so a task sent by a controller that died
// before it could write the name down is still found. Only job with no task anywhere goes back to
// pending, and that is the one state that says for certain nothing was paid for.
func (c *Controller) recover(ctx context.Context, one *Job) {
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
		c.logger.WarnContext(ctx, "could not read the task of a job that was left behind",
			"job", one.ID, "session", session, "error", err)
		return
	}

	lease := c.leaseIn(ctx, one.Workspace)
	records := []*Event{
		c.event(ctx, one, EventReleased,
			fmt.Sprintf("previous owner %s, phase found %s", ownerOrNobody(one.LeaseOwner), one.Phase)),
		c.event(ctx, one, EventClaimed, c.leaseDetail(lease)),
	}
	if _, err := c.store.TakeOverJob(ctx, one.ID, lease, records); err != nil {
		if !errors.Is(err, ErrHeld) {
			c.logger.WarnContext(ctx, "could not take over job that was left behind", "job", one.ID, "error", err)
		}
		return
	}
	c.exported(ctx, records...)
	// The session goes onto the row now, so the next reader learns it from the record rather than by
	// asking the system again.
	if one.Session == "" {
		if err := c.store.RecordJobSession(ctx, one.ID, session); err != nil {
			c.logger.WarnContext(ctx, "could not record which session job that was left behind ran in",
				"job", one.ID, "session", session, "error", err)
		}
	}
}

// release puts job back to pending. Only where no task exists anywhere, so nothing that has been
// paid for is ever sent again.
func (c *Controller) release(ctx context.Context, one *Job) {
	records := []*Event{
		c.event(ctx, one, EventReleased,
			fmt.Sprintf("previous owner %s, phase found %s, no task was sent",
				ownerOrNobody(one.LeaseOwner), one.Phase)),
	}
	if _, err := c.store.ReleaseJob(ctx, one.ID, records); err != nil {
		if !errors.Is(err, ErrHeld) {
			c.logger.WarnContext(ctx, "could not put job that was left behind back", "job", one.ID, "error", err)
		}
		return
	}
	c.exported(ctx, records...)
}

// sessionNamedAfter is the conversation this job would have run in, if the system holds one.
//
// This closes the gap between a dispatch and the row that records its session. A controller that
// died in between left a task running with nothing on the row to say so, and putting that job back
// to pending would send a second task and pay for it twice.
func (c *Controller) sessionNamedAfter(ctx context.Context, one *Job) string {
	sessions, err := c.sessionsIn(ctx, one.Project)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read the system's sessions", "job", one.ID, "error", err)
		// Nothing is released on a read that failed: releasing here is what would pay twice.
		return unknownSession
	}
	return sessions[ConversationFor(one)]
}

// unknownSession is what a failed read answers with. It is not a session and it is not "no session":
// it stops this tick from doing anything, which is the safe direction.
const unknownSession = "?"

// leaseDetail is what a claim writes on the record.
func (c *Controller) leaseDetail(lease Lease) string {
	return fmt.Sprintf("lease_owner %s, lease_until %s", lease.Owner, lease.Until.Format(time.RFC3339))
}

// ownerOrNobody names the controller that held a job, for a record about it being taken.
func ownerOrNobody(owner string) string {
	if owner == "" {
		return "nobody"
	}
	return owner
}

// adoptAnswers writes what came back onto every job whose task has landed.
func (c *Controller) adoptAnswers(ctx context.Context, turnedAway givenUp) {
	started, err := c.store.HeldJob(ctx, c.owner, c.batch)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read which job this controller is holding", "error", err)
		return
	}
	for _, one := range started {
		c.adopt(ctx, one, turnedAway)
	}
}

// adopt reads the task row for the job's session and moves the job on if it has landed.
//
// Reading rather than remembering is what makes this safe to run twice: the task row is the record
// of what the system actually did, so a controller that has just started the job and one that has
// come back to it read the same thing.
func (c *Controller) adopt(ctx context.Context, one *Job, turnedAway givenUp) {
	resp, err := c.plane.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: one.Session})
	if err != nil {
		c.logger.WarnContext(ctx, "could not read the task of a job",
			"job", one.ID, "session", one.Session, "error", err)
		return
	}
	tasks := resp.GetTasks()
	if len(tasks) == 0 {
		c.renew(ctx, one)
		return
	}
	last := tasks[len(tasks)-1]
	// Still working. Nothing to do but keep hold of it: a controller that stopped renewing while its
	// task ran would have its job taken from under it, and a controller that wrote a record on every
	// tick would fill the history with lines saying nothing happened.
	if last.GetStatus() == StatusRunning {
		c.renew(ctx, one)
		return
	}
	// The system never started this one, so there is nothing to write down about how it went. The
	// machine was busy, which is a moment and not a verdict, so the job goes back to pending and a
	// later tick tries it again. Failing it here is how declared work was lost with nothing raised.
	if last.GetStatus() == StatusFailed && NeverStarted(last.GetFailure()) {
		c.requeue(ctx, one, waitingForRoom(last.GetFailure()), turnedAway)
		return
	}
	// The last task was the system asking this session for a handoff, because its context window had
	// reached the workspace's ceiling. What came back is not an answer to the job, so it is read
	// before the landing below rather than written onto the row as what the job produced.
	if AskingForAHandoff(last.GetPrompt()) {
		c.handOn(ctx, one)
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
	// An operator stopped the task, so the job stops too, carrying their reason. Job that went
	// quiet and a job that was halted must never read the same, and a stop that came back as done
	// would have the system report an answer nobody gave: the task ended before it had one.
	case last.GetStatus() == StatusTaskStopped:
		landing.Phase, landing.Answer, landing.Reason, kind =
			PhaseStopped, "", oneLine(last.GetFailure()), EventStopped
	default:
		if unmet := c.unmet(ctx, one, last.GetReply()); unmet != "" {
			landing.Phase, landing.Reason, kind = PhaseStopped, unmet, EventStopped
		}
	}

	// Read whole once, and used twice below: the plan a person approved is held against the steps the
	// session recorded, and the attempt is held against the attempts before it. Both need what a
	// listing does not carry.
	whole := c.wholeJob(ctx, one)

	// A job that owes a person a plan answered with the plan rather than with work, so nothing is
	// landed here: the plan goes on the row and the question goes to a person. This is the moment the
	// gate exists for, and it costs one task. The same answer after everything is built costs the job.
	// Before that, a job that owes a person what it understood answered with the reading rather than
	// with a plan or with work. The same shape as the plan below it, one stage earlier: nothing is
	// landed, the record goes on the row, and the questions go to a person.
	if kind == EventAnswered && WaitingForItsIdeation(whole) {
		if put, why := c.proposeWhatItUnderstood(ctx, whole, tasks, landing.Answer); put {
			return
		} else if why != "" {
			landing.Phase, landing.Reason, kind =
				PhaseStopped, NoUnderstandingToAnswer(why), EventStopped
		}
	}
	// Then the list of what it would build, between the reading and the plan, and the same shape as
	// both: nothing is landed, the list goes on the row, and it goes to a person to accept.
	if kind == EventAnswered && WaitingForItsDesign(whole) {
		if put, why := c.proposeWhatItWouldBuild(ctx, whole, tasks, landing.Answer); put {
			return
		} else if why != "" {
			landing.Phase, landing.Reason, kind =
				PhaseStopped, NoListToAccept(why), EventStopped
		}
	}
	if kind == EventAnswered && WaitingForItsPlan(whole) {
		if put, why := c.proposeThePlan(ctx, whole, tasks, landing.Answer); put {
			return
		} else if why != "" {
			landing.Phase, landing.Reason, kind = PhaseStopped, NoPlanToApprove(why), EventStopped
		}
	}

	// The session stopped for a person rather than for the work, so the job stops with them and the
	// record says which. It is read off the answer, the way the pull request address and the outcome
	// are, because a session that stopped for a person and a session that finished must never read
	// the same: that is the whole of this. Nothing else is asked of it below, and nothing lands: work
	// that stopped at a decision has no pull request to be asked for and no plan to be held against.
	if kind == EventAnswered && OutcomeIn(landing.Answer) == OutcomeDecide {
		if c.putItToAPerson(ctx, whole, landing.Answer) {
			return
		}
	}

	// Where the job names a repository, the answer has to say where the work went. Read off the answer
	// rather than reported by the model, the way an expectation is, and read on every path so a job
	// that stopped for some other reason still records the pull request it did open.
	landing.PullRequest = PullRequestIn(one.Repository, landing.Answer)
	// A continued attempt has one thing to say that an attempt from nothing does not: what moved under
	// the base its work stands on. Read first, because it decides whether the work is worth publishing
	// at all: a branch opened against a base nobody looked at is the second failure a resume can cause,
	// and it costs more than a branch nobody pushed.
	if kind == EventAnswered && one.Resuming != "" && one.Repository != "" &&
		MovedUnderIt(landing.Answer) == "" {
		// The ceiling comes first, because the session that would answer this ask is the one whose
		// window is full, and asking it again is the thing this gate exists to stop.
		if c.handsOffInstead(ctx, one) {
			return
		}
		if c.askWhatMovedUnderIt(ctx, one, tasks) {
			return
		}
		landing.Phase, landing.Reason, kind =
			PhaseStopped, NothingSaidAboutTheBase(one.Repository), EventStopped
	}
	if kind == EventAnswered && one.Repository != "" && landing.PullRequest == "" {
		// Nothing is landed here. The job stays running while the session is asked, so a controller
		// that dies between the ask and the answer finds a running job with two tasks and reads them
		// the way this one would have.
		//
		// The ceiling comes first. This is the ask the issue behind the gate is about: the last task of
		// a long job is the one that opens the pull request, and it was being run in the fullest window
		// the job ever had.
		if c.handsOffInstead(ctx, one) {
			return
		}
		if c.askForThePullRequest(ctx, one, len(tasks)) {
			return
		}
		landing.Phase, landing.Reason, kind = PhaseStopped,
			WhyNoPullRequest(one.Repository, one.Mode, one.Session, c.published(ctx, one)), EventStopped
	}
	// A plan a person approved and the work then walked away from is the same failure as no plan at
	// all, one step further along, so the record is held against the plan before the job is called
	// done. It is arithmetic over the numbers the session recorded: no model call, and no judgement
	// about prose.
	// A job whose verticals are built reaches done one way, which is a person looking at a picture of
	// them running and saying the value arrived. Every other road into done on such a row is the
	// system calling its own work finished, so this one closes: the answer stays on the record, the
	// job stops, and the reason names the command that ends it.
	//
	// In front of the plan, because a job nobody has looked at and a job whose steps do not add up are
	// two different problems and only one of them has somebody waiting on it. Told that its plan is
	// unaccounted for, a person goes and reads the session; told that nobody looked at the pictures,
	// they go and look.
	if kind == EventAnswered && NotYetAccepted(whole) {
		landing.Phase, landing.Reason, kind = PhaseStopped, NotAccepted(whole), EventStopped
	}
	if kind == EventAnswered && whole.PlanApproved {
		if missing := NotAccountedFor(whole.Plan, whole.Steps); len(missing) > 0 {
			landing.Phase, landing.Reason, kind = PhaseStopped, PlanNotFollowed(missing), EventStopped
		}
	}
	// What this attempt produced goes on the record whichever way it went, and with it how like the
	// earlier attempts at this step it was. The attempt that finished the job is recorded too: it is
	// the other half of the measurement that replaces the threshold, and without it the record holds
	// only the attempts that went nowhere.
	attempt := TheAttempt(whole, last.GetId(), saidBy(last, landing))
	landing.Attempt = &attempt
	// A loop is three attempts at one step the system cannot tell apart, and an attempt that finished
	// the job is never one of them, however like the last it reads: work that got there is not work
	// going in circles. Neither is a task an operator halted, which is a person's decision rather than
	// the session repeating itself.
	if kind != EventAnswered && last.GetStatus() != StatusTaskStopped &&
		c.wentInCircles(ctx, whole, attempt, turnedAway) {
		return
	}
	// Last, because the gate reads the change and the change is the pull request: a gate asked before
	// the address is in hand would be asked to read work it cannot find. Nothing settles here on the
	// session's own account, which is the whole of this: two sessions that did not do the work have to
	// agree first, and while they are being asked the job stays running.
	if kind == EventAnswered && Gated(one) {
		switch decided := c.gate(ctx, one, landing.PullRequest, tasks); {
		case decided.held:
			return
		case decided.passed:
			landing.Reviewed, landing.Tested = true, true
		default:
			landing.Phase, landing.Reason, kind = PhaseStopped, decided.reason, EventStopped
		}
	}
	// The outcome last, because it is the word that settles the job and the asks above may have moved
	// the answer this reads. It is read on the answering path alone: a task that failed, or that an
	// operator stopped, has no answer, and a word invented for one would be the system reporting an
	// outcome nobody stated.
	if kind == EventAnswered {
		if landing.Outcome = OutcomeIn(landing.Answer); landing.Outcome == "" {
			landing.Phase, landing.Reason, kind =
				PhaseStopped, NoOutcomeStated(landing.Answer), EventStopped
		}
	}
	c.land(ctx, one, landing, kind)
}

// requeue puts a job the system could not start back to pending, with the record of it.
//
// There is no ceiling on how often this happens, and that is deliberate. A machine that is busy now
// has room later, and how many tries are enough is a number nobody has measured: the job stays
// declared until it runs, until its deadline passes, or until a person stops it. Each attempt costs
// one start, and the system starts one sandbox at a time, so a job that keeps being turned away asks
// again about as often as a start takes rather than on every tick.
func (c *Controller) requeue(ctx context.Context, one *Job, reason string, turnedAway givenUp) {
	ctx = telemetry.Under(ctx, one.TraceID, one.ParentSpanID)
	record := c.event(ctx, one, EventReleased, reason)
	back := Requeue{Owner: c.owner, Reason: reason}
	if _, err := c.store.RequeueJob(ctx, one.ID, back, []*Event{record}); err != nil {
		if !errors.Is(err, ErrHeld) {
			c.logger.WarnContext(ctx, "could not put a job the system could not start back",
				"job", one.ID, "error", err)
		}
		return
	}
	turnedAway[one.ID] = true
	c.exported(ctx, record)
	// Said out loud, because this is how the work was lost quietly the first time: the job went to one
	// word in a listing that has to be read on purpose, and nothing anywhere raised it.
	c.logger.InfoContext(ctx, "a job is waiting for a machine with room",
		"job", one.ID, "session", one.Session, "attempts", one.Attempts, "reason", reason)
}

// waitingForRoom is what a job waiting for a machine with room says on the listing, carrying what
// the system said underneath it.
func waitingForRoom(failure string) string {
	return oneLine("the system could not give this job a sandbox, so it waits for room: " + failure)
}

// askForThePullRequest sends the session back for the address its answer did not carry, and says
// whether it went. `asked` is how many tasks the session has already run.
//
// Asked once and no more. The bound is the count of tasks rather than a counter of the system's own,
// because the session is named after the job and holds this job's tasks alone: one is the work, two
// is the work and this ask. A controller that took the row over after another died reads the same
// number and does not ask a third time, which is the property every other decision in this loop has.
//
// No record of its own is written. The second task is the record, in the session `krewe job show`
// already names, and a record needs a store write that this does not otherwise need.
//
// This is the one expectation the system asks again about rather than stopping on, and the difference
// is what is missing. An answer that does not carry what it claimed is work that was not done, so
// asking again is asking a model to do it twice. A pull request is work that was done and not
// published: the branch is in the session, the session is open, and opening it is one command. That
// is worth one task, and a job that ends "done, and nowhere anybody can read it" is the silence this
// was built to end.
func (c *Controller) askForThePullRequest(ctx context.Context, one *Job, asked int) bool {
	if asked > 1 {
		return false
	}
	// A mode that cannot reach the network cannot push, so the ask is a task nobody can answer: it asks
	// for the one command the mode stops, and it ends where the first task ended. The job is landed
	// below with the mode named as the reason instead, because a whole task is what this costs.
	//
	// A job that names no mode is asked. The mode it runs in is the system's, and this loop does not
	// hold it, so an unnamed mode is not evidence of anything.
	if ModeCannotPush(one.Mode) {
		return false
	}
	if _, err := c.plane.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: one.Project, Handle: ConversationFor(one), Text: AskedForThePullRequest(one, nil),
		PermissionMode: one.Mode, Detach: true, Role: RoleNow(one), Job: one.ID,
	}); err != nil {
		// A system that cannot ask again lands the job below with the reason, rather than holding a row
		// open waiting for a task nobody sent.
		c.logger.WarnContext(ctx, "could not ask a session again for the pull request",
			"job", one.ID, "session", one.Session, "error", err)
		return false
	}
	// The hold moves on, because the job is still this controller's and a task is in flight again.
	c.renew(ctx, one)
	return true
}

// proposeWhatItUnderstood reads what the session says it understood out of its answer and puts it to
// a person, and says what it did.
//
// It answers the same two things proposeThePlan does, one stage earlier, and it is deliberately the
// same shape: one ask, one second ask carrying the refusal, then a stop. A second mechanism here
// would be a second way for a job to wait for a person, and the two would drift.
func (c *Controller) proposeWhatItUnderstood(ctx context.Context, one *Job, tasks []*quaycrewv1.Task,
	answer string) (put bool, why string) {
	understood, err := ReadIdeation(answer)
	if err != nil {
		if AskedWhatItUnderstoodAgain(tasks[len(tasks)-1].GetPrompt()) {
			return false, oneLine(err.Error())
		}
		if _, sent := c.plane.Dispatch(ctx, &quaycrewv1.DispatchRequest{
			Project: one.Project, Handle: ConversationFor(one),
			Text:           AskedForAnUnderstandingTheSystemCanRead(err.Error()),
			PermissionMode: one.Mode, Detach: true, Role: RoleNow(one), Job: one.ID,
		}); sent != nil {
			// A system that cannot ask again stops the job with the reason, rather than holding a row
			// open waiting for a task nobody sent.
			c.logger.WarnContext(ctx, "could not ask a session again what it understood",
				"job", one.ID, "session", one.Session, "error", sent)
			return false, oneLine(err.Error())
		}
		// The hold moves on, because the job is still this controller's and a task is in flight again.
		c.renew(ctx, one)
		return true, ""
	}

	ctx = telemetry.Under(ctx, one.TraceID, one.ParentSpanID)
	kept := IdeationText(understood)
	question := AskingWhetherThisIsRight(one.Product, kept)
	record := c.event(ctx, one, EventAsked, question)
	if _, err := c.store.ProposeJobIdeation(ctx, one.ID, kept, question, record); err != nil {
		if !errors.Is(err, ErrNotRunning) {
			c.logger.WarnContext(ctx, "could not put what a job understood to a person",
				"job", one.ID, "error", err)
		}
		// Nothing is landed either way. The row moved under this controller, or the write did not
		// apply, and a later tick reads the record again and does the same arithmetic.
		return true, ""
	}
	c.exported(ctx, record)
	return true, ""
}

// proposeWhatItWouldBuild reads the list of verticals out of the session's answer and puts it to a
// person, and says what it did.
//
// The same shape as the two gates around it, on purpose: one ask, one second ask carrying the
// refusal, then a stop. A third mechanism here would be a third way for a job to wait for a person,
// and the three would drift.
//
// A list the system refuses is a list it never puts to a person. The refusal names the line and the
// word it was refused for, so the second ask is worth the task it costs.
func (c *Controller) proposeWhatItWouldBuild(ctx context.Context, one *Job, tasks []*quaycrewv1.Task,
	answer string) (put bool, why string) {
	design, err := ReadDesign(answer)
	if err != nil {
		if AskedForTheListAgain(tasks[len(tasks)-1].GetPrompt()) {
			return false, oneLine(err.Error())
		}
		if _, sent := c.plane.Dispatch(ctx, &quaycrewv1.DispatchRequest{
			Project: one.Project, Handle: ConversationFor(one),
			Text:           AskedForAListTheSystemCanRead(err.Error()),
			PermissionMode: one.Mode, Detach: true, Role: RoleNow(one), Job: one.ID,
		}); sent != nil {
			// A system that cannot ask again stops the job with the reason, rather than holding a row
			// open waiting for a task nobody sent.
			c.logger.WarnContext(ctx, "could not ask a session again what it would build",
				"job", one.ID, "session", one.Session, "error", sent)
			return false, oneLine(err.Error())
		}
		// The hold moves on, because the job is still this controller's and a task is in flight again.
		c.renew(ctx, one)
		return true, ""
	}

	ctx = telemetry.Under(ctx, one.TraceID, one.ParentSpanID)
	kept := DesignText(design)
	question := AskingWhetherThisIsTheList(one.Product, kept)
	record := c.event(ctx, one, EventAsked, question)
	if _, err := c.store.ProposeJobDesign(ctx, one.ID, kept, question, record); err != nil {
		if !errors.Is(err, ErrNotRunning) {
			c.logger.WarnContext(ctx, "could not put what a job would build to a person",
				"job", one.ID, "error", err)
		}
		// Nothing is landed either way. The row moved under this controller, or the write did not
		// apply, and a later tick reads the record again and does the same arithmetic.
		return true, ""
	}
	c.exported(ctx, record)
	return true, ""
}

// proposeThePlan reads the plan out of what the session answered and puts it to a person, and says
// what it did.
//
// It answers two things. `put` is true when the job is now asking, or when the session has been sent
// back for a plan the system can read, and in both cases nothing is landed. `why` is the refusal
// where the session was asked twice and answered with no plan either time, which is what stops the
// job: a job whose plan nobody could read is a job nobody approved.
//
// Asked once and no more, bounded off the record rather than off a counter of the system's own. The
// second ask carries a sentence this reads back, the way the ask about a moved base does, so a
// controller that took the row over after another died reads the same history and does not ask a
// third time.
func (c *Controller) proposeThePlan(ctx context.Context, one *Job, tasks []*quaycrewv1.Task,
	answer string) (put bool, why string) {
	steps, err := ReadPlan(answer)
	if err != nil {
		if AskedForThePlanAgain(tasks[len(tasks)-1].GetPrompt()) {
			return false, oneLine(err.Error())
		}
		if _, sent := c.plane.Dispatch(ctx, &quaycrewv1.DispatchRequest{
			Project: one.Project, Handle: ConversationFor(one),
			Text:           AskedForAPlanTheSystemCanRead(err.Error()),
			PermissionMode: one.Mode, Detach: true, Role: RoleNow(one), Job: one.ID,
		}); sent != nil {
			// A system that cannot ask again stops the job with the reason, rather than holding a row
			// open waiting for a task nobody sent.
			c.logger.WarnContext(ctx, "could not ask a session again for a plan the system can read",
				"job", one.ID, "session", one.Session, "error", sent)
			return false, oneLine(err.Error())
		}
		// The hold moves on, because the job is still this controller's and a task is in flight again.
		c.renew(ctx, one)
		return true, ""
	}

	ctx = telemetry.Under(ctx, one.TraceID, one.ParentSpanID)
	plan := PlanText(steps)
	question := AskingWhetherThisIsThePlan(one.Product, plan)
	record := c.event(ctx, one, EventAsked, question)
	if _, err := c.store.ProposeJobPlan(ctx, one.ID, plan, question, record); err != nil {
		if !errors.Is(err, ErrNotRunning) {
			c.logger.WarnContext(ctx, "could not put a job's plan to a person",
				"job", one.ID, "error", err)
		}
		// Nothing is landed either way. The row moved under this controller, or the write did not
		// apply, and a later tick reads the record again and does the same arithmetic.
		return true, ""
	}
	c.exported(ctx, record)
	return true, ""
}

// putItToAPerson stops the job with a person, where its session answered that a person has to
// decide, and says whether the record took it.
//
// It writes what krewe job ask writes, through the same store call, so a job stopped for a person by
// a measurement and a job stopped for a person by the session itself are one state and not two.
// Anything that reads what waits on you finds both, and nothing has to learn a second field.
//
// The session is left alone. The job holds the question, the lease is let go, and an answer starts it
// again in the same conversation, which is what a session waiting on a person needs to happen.
//
// False on any refusal, and the caller lands the job the ordinary way. A job that could not be put to
// a person must not be left running: that is the state this whole change exists to remove, and
// reaching it through the fix for it would be the worst of both.
func (c *Controller) putItToAPerson(ctx context.Context, one *Job, answer string) bool {
	ctx = telemetry.Under(ctx, one.TraceID, one.ParentSpanID)
	question := TheDecisionPutToAPerson(answer, one.Session)
	record := c.event(ctx, one, EventAsked, question)
	if _, err := c.store.AskJob(ctx, one.ID, question, record); err != nil {
		if !errors.Is(err, ErrNotRunning) {
			c.logger.WarnContext(ctx, "could not stop a job with the person its session asked for",
				"job", one.ID, "session", one.Session, "error", err)
			return false
		}
		// The row moved under this controller, so somebody else already wrote what came of it.
		// Landing it here would write a second ending over the first.
		return true
	}
	c.exported(ctx, record)
	return true
}

// askWhatMovedUnderIt sends a continued session back for the one thing its answer did not carry, and
// says whether it went.
//
// Asked once. The bound is the task being read, off the record: the answer in hand is the answer to
// the task above it, so a session answering the ask is holding the ask in its own history. A
// controller that took this row over after another died reads the same history and does not ask a
// third time. Counting tasks the way the pull request ask does would not work here, because a
// continued job's session already holds the tasks of the attempt that failed.
//
// No record of its own is written, for the same reason the pull request ask writes none: the task is
// the record, in the session krewe job show already names.
func (c *Controller) askWhatMovedUnderIt(ctx context.Context, one *Job, tasks []*quaycrewv1.Task) bool {
	if AskingWhatMoved(tasks[len(tasks)-1].GetPrompt()) {
		return false
	}
	if _, err := c.plane.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: one.Project, Handle: ConversationFor(one), Text: AskedWhatMoved(one.Repository),
		PermissionMode: one.Mode, Detach: true, Role: RoleNow(one), Job: one.ID,
	}); err != nil {
		// A system that cannot ask again lands the job below with the reason, rather than holding a row
		// open waiting for a task nobody sent.
		c.logger.WarnContext(ctx, "could not ask a continued session what moved under its base",
			"job", one.ID, "session", one.Session, "error", err)
		return false
	}
	// The hold moves on, because the job is still this controller's and a task is in flight again.
	c.renew(ctx, one)
	return true
}

// pastTheCeiling says whether the conversation this job is in has filled enough of its context window
// that this workspace gives it no new task.
//
// Three ways of answering no, and each is deliberate. A controller with no reader wired refuses
// nothing, which is every controller before this. A job with no session has no window to be full. And
// a window nothing has told the system the size of is not measured, so it is not full either: the size
// comes from the model runtime by way of a session writing it down, and a gate that read silence as
// full would stop every job on a system nobody has told yet.
func (c *Controller) pastTheCeiling(ctx context.Context, one *Job, ceiling int) bool {
	if c.windows == nil || one.Session == "" {
		return false
	}
	used, size := c.windows.SessionWindow(ctx, one.Session)
	return PastTheCeiling(used, size, ceiling)
}

// handsOffInstead sends the session at the ceiling the one task it has left, in place of the ask the
// controller was about to make, and says whether it went.
//
// The handoff ask is not a task on top of the work. It replaces it, and it is the last task that
// conversation gets for this job: a gate that asked for a handoff and then sent the work anyway would
// have spent a task to change nothing.
//
// No record of its own is written, for the reason the two asks write none: the task is the record, in
// the session krewe job show already names, and the movement that follows it writes one.
func (c *Controller) handsOffInstead(ctx context.Context, one *Job) bool {
	ceiling := c.limitsIn(ctx, one.Workspace).ContextCeiling()
	if !c.pastTheCeiling(ctx, one, ceiling) {
		return false
	}
	if _, err := c.plane.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: one.Project, Handle: ConversationFor(one),
		Text: AskedForAHandoff(one, ceiling), PermissionMode: one.Mode, Detach: true,
		Role: one.Role, Job: one.ID,
	}); err != nil {
		// A system that cannot ask lands the job the way it was going to, rather than holding a row
		// open waiting for a task nobody sent.
		c.logger.WarnContext(ctx, "could not ask a session at the context ceiling for a handoff",
			"job", one.ID, "session", one.Session, "error", err)
		return false
	}
	c.logger.InfoContext(ctx, "a session reached its workspace's context ceiling, so it was asked to hand over",
		"job", one.ID, "session", one.Session, "ceiling", ceiling)
	// The hold moves on, because the job is still this controller's and a task is in flight again.
	c.renew(ctx, one)
	return true
}

// handOn gives the rest of a job to a fresh session, once the one at the ceiling has written what it
// leaves behind.
//
// The job does not restart. It goes back to pending with its steps, its pull request and its identity
// intact, and the next start dispatches into a conversation named one further on, carrying the
// handoff. What changes is which session is doing it.
//
// A session that was asked and wrote nothing stops the job instead. Handing an empty record to a fresh
// session would have it pay for every discovery the last one made, which costs more than leaving the
// work where a person can find it, and a job carried on from nothing reads afterwards exactly like one
// that handed over well.
func (c *Controller) handOn(ctx context.Context, one *Job) {
	ctx = telemetry.Under(ctx, one.TraceID, one.ParentSpanID)
	ceiling := c.limitsIn(ctx, one.Workspace).ContextCeiling()
	used, size := int64(0), int64(0)
	if c.windows != nil {
		used, size = c.windows.SessionWindow(ctx, one.Session)
	}
	// Whether anything was written down is the store's to answer, in the same statement as the
	// movement, rather than read off the row this controller is holding. That row came from a listing,
	// which carries what a listing carries, and the session is still answering while this decides.
	record := c.event(ctx, one, EventHandedOn, HandedOverAt(ShareOf(used, size), ceiling))
	if _, err := c.store.HandOffJob(ctx, one.ID, Requeue{Owner: c.owner}, record); err != nil {
		if errors.Is(err, ErrNothingHandedOver) {
			c.land(ctx, one, Landing{Phase: PhaseStopped, Reason: NothingHandedOver(one, ceiling),
				SpentTokens: c.spentBy(ctx, one.Session)}, EventStopped)
			return
		}
		if !errors.Is(err, ErrHeld) {
			c.logger.WarnContext(ctx, "could not give the rest of a job to a fresh session",
				"job", one.ID, "error", err)
		}
		return
	}
	c.exported(ctx, record)
	c.logger.InfoContext(ctx, "the rest of a job goes to a fresh session",
		"job", one.ID, "session", one.Session, "at", ShareOf(used, size), "ceiling", ceiling)
}

// gateOutcome is what the gate decided about a job whose work has landed.
//
// Held means a task is in flight and the row is not this tick's to move. Passed means every gate
// agreed, so the job may settle. A reason stops the job, because a gate that could not be run, or
// whose answer could not be read, has passed nothing.
//
// The zero value is one gate having passed, which is what carries the loop on to the next.
type gateOutcome struct {
	held   bool
	passed bool
	reason string
}

// gate holds a job back until two sessions that did not do the work have passed it.
//
// Everything it needs is read rather than remembered, which is what makes it safe to run twice: the
// tasks of the two gate sessions are the record of what the system asked and what came back, and a
// controller that took this row over after another died reads the same tasks and reaches the same
// answer. The one thing that lands on the row is which gates passed, written when the job lands.
//
// The two run one after the other rather than at once. A change the reviewer failed does not need
// testing, so the order saves a container and a bill, and it puts the cheaper judgement first.
func (c *Controller) gate(ctx context.Context, one *Job, address string, tasks []*quaycrewv1.Task) gateOutcome {
	// How many times this work has already been round. It is counted off the tasks the working session
	// has run, by the sentence the system writes when it sends work back, for the reason the continued
	// ask is recognised that way: a counter in a field is a thing the next controller does not have.
	sentBack := timesTheGateSentItBack(tasks)
	sessions, err := c.sessionsIn(ctx, one.Project)
	if err != nil {
		// A read that failed is not a verdict. The job stays running and a later tick asks again, which
		// is the safe direction: landing it here would settle work on nothing having read it.
		c.logger.WarnContext(ctx, "could not read the sessions a job's gate would run in",
			"job", one.ID, "error", err)
		return gateOutcome{held: true}
	}
	for _, gate := range []struct {
		named  string
		handle string
		asked  func(*Job, string) string
	}{
		{GateReviewer, ReviewerFor(one.ID), AskedToReview},
		{GateTester, TesterFor(one.ID), AskedToTest},
	} {
		decided := c.passes(ctx, one, gate.named, gate.handle, gate.asked(one, address), sessions[gate.handle], sentBack)
		if decided.held || decided.reason != "" {
			return decided
		}
	}
	return gateOutcome{passed: true}
}

// passes asks one gate about this work, reads what it said, and says what that means for the job.
//
// session is the conversation this gate has run in before, and empty where it has never run. asked is
// what the gate is sent when it has not been asked about this round of the work yet.
func (c *Controller) passes(ctx context.Context, one *Job, gate, handle, asked, session string,
	sentBack int) gateOutcome {
	var judged []*quaycrewv1.Task
	if session != "" {
		resp, err := c.plane.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: session})
		if err != nil {
			c.logger.WarnContext(ctx, "could not read what a job's gate said",
				"job", one.ID, "gate", gate, "session", session, "error", err)
			return gateOutcome{held: true}
		}
		judged = resp.GetTasks()
	}
	// One task per round. The work has been round sentBack+1 times, so a gate holding no more tasks
	// than the number of send backs has not read what is in front of it now, and is asked.
	if len(judged) <= sentBack {
		return c.askTheGate(ctx, one, gate, handle, asked)
	}
	last := judged[len(judged)-1]
	if last.GetStatus() == StatusRunning {
		c.renew(ctx, one)
		return gateOutcome{held: true}
	}
	// A gate whose own task failed or was halted judged nothing. The job does not settle on it, for
	// the reason the prover already gives: a check that quietly passes when it could not be run is the
	// same false green as no check at all.
	if last.GetStatus() == StatusFailed || last.GetStatus() == StatusTaskStopped {
		return gateOutcome{reason: TheGateCouldNotRun(gate, last.GetFailure())}
	}
	verdict := Verdict(last.GetReply())
	switch {
	case !verdict.Said:
		return gateOutcome{reason: TheGateSaidNothing(gate)}
	case verdict.Passed:
		return gateOutcome{}
	case sentBack > 0:
		// Round two, and it still does not pass. Every ask is a task somebody pays for, so this is
		// where it ends, with what the gate said on the row rather than in a conversation.
		return gateOutcome{reason: FailedTheGate(gate, verdict.Reason, one)}
	default:
		return c.sendItBack(ctx, one, gate, verdict.Reason)
	}
}

// askTheGate sends one gate the work to read, and says what that means for the job.
func (c *Controller) askTheGate(ctx context.Context, one *Job, gate, handle, asked string) gateOutcome {
	// No role and no job. The role is what would give this session material it has no business
	// holding, and the job is what mints its credential: a session running no job holds none, so what
	// it may call on the system is nothing. That is the boundary, and it is the credential rather than
	// a sentence in the text above.
	if _, err := c.plane.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: one.Project, Handle: handle, Text: asked, Detach: true,
		Title: gate + ": " + one.Title,
	}); err != nil {
		// The job stays running and a later tick asks again. A machine with no room for the gate's
		// container is a moment rather than a verdict, which is the reasoning a job turned away for
		// want of a sandbox already gets, and landing the job here would settle it unread.
		c.logger.WarnContext(ctx, "a job is waiting for a session to pass it",
			"job", one.ID, "gate", gate, "error", err)
		c.renew(ctx, one)
		return gateOutcome{held: true}
	}
	c.renew(ctx, one)
	return gateOutcome{held: true}
}

// sendItBack gives the work back to the session that did it, carrying what the gate said.
//
// The job stays running, so a controller that dies between the ask and the answer finds a running job
// and reads the tasks the way this one would have. No record of its own is written, for the reason
// the pull request ask writes none: the task is the record, in the session `krewe job show` already
// names, and it is what the count of rounds is read from.
func (c *Controller) sendItBack(ctx context.Context, one *Job, gate, reason string) gateOutcome {
	if _, err := c.plane.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: one.Project, Handle: ConversationFor(one), Text: SentBack(gate, reason, one),
		PermissionMode: one.Mode, Detach: true, Role: one.Role, Job: one.ID,
	}); err != nil {
		c.logger.WarnContext(ctx, "could not send a job back to the session that did the work",
			"job", one.ID, "gate", gate, "session", one.Session, "error", err)
		// Nothing passed this work and it cannot be handed back, so the job stops with what the gate
		// said rather than settling on an answer nothing agreed with.
		return gateOutcome{reason: FailedTheGate(gate, reason, one)}
	}
	c.renew(ctx, one)
	return gateOutcome{held: true}
}

// timesTheGateSentItBack is how many of a session's tasks were the gate handing the work back.
func timesTheGateSentItBack(tasks []*quaycrewv1.Task) int {
	sent := 0
	for _, one := range tasks {
		if SentBackByTheGate(one.GetPrompt()) {
			sent++
		}
	}
	return sent
}

// sessionsIn is every conversation this project holds, by handle, which is how a controller finds the
// sessions its gates run in. A gate's conversation is named after the job, so it is found rather than
// recorded: a handle on the row is a second thing that can be out of date.
func (c *Controller) sessionsIn(ctx context.Context, project string) (map[string]string, error) {
	listed, err := c.plane.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: project})
	if err != nil {
		return nil, err
	}
	by := make(map[string]string, len(listed.GetSessions()))
	for _, session := range listed.GetSessions() {
		by[session.GetHandle()] = session.GetId()
	}
	return by, nil
}

// renew moves this controller's hold on, which is what says it is still alive. A hold that stops
// moving is the only signal another controller has that this one went away.
func (c *Controller) renew(ctx context.Context, one *Job) {
	if err := c.store.RenewLease(ctx, one.ID, c.leaseIn(ctx, one.Workspace)); err != nil && !errors.Is(err, ErrHeld) {
		c.logger.WarnContext(ctx, "could not keep hold of a job", "job", one.ID, "error", err)
	}
}

// land writes the end of a job, with the record of it.
func (c *Controller) land(ctx context.Context, one *Job, landed Landing, kind string) {
	ctx = telemetry.Under(ctx, one.TraceID, one.ParentSpanID)
	detail := landed.Reason
	if detail == "" {
		detail = fmt.Sprintf("%d tokens in session %s", landed.SpentTokens, one.Session)
	}
	// The word this job ended on, so a reader counting a tree reads the records rather than the rows.
	if landed.Outcome != "" {
		detail = landed.Outcome + ", " + detail
	}
	// The address first, because it is the one line in this record somebody opens.
	if landed.PullRequest != "" {
		detail = landed.PullRequest + ", " + detail
	}
	record := c.event(ctx, one, kind, detail)
	ended, err := c.store.LandJob(ctx, one.ID, landed, record)
	if err != nil {
		if !errors.Is(err, ErrNotRunning) {
			c.logger.WarnContext(ctx, "could not write what came of a job",
				"job", one.ID, "error", err)
		}
		return
	}
	c.exported(ctx, record)
	c.revoked(ended)
	c.drawSpans(ctx, ended)
}

// drawSpans puts the life of a job on the trace, once the system knows both ends of it.
//
// One span for the attempt that just landed and one for the whole job. They are recorded
// here rather than opened when the job started, because a job outlives the process that
// declared it: a span held in memory across a controller that died would be lost with it, and the
// timestamps are on the row either way.
func (c *Controller) drawSpans(ctx context.Context, ended *Job) {
	if ended == nil || ended.TraceID == "" {
		return
	}
	finished := time.Now().UTC()
	if ended.FinishedAt != nil {
		finished = *ended.FinishedAt
	}
	shared := []attribute.KeyValue{
		attribute.String("quaycrew.job", ended.ID),
		attribute.String("quaycrew.workspace", ended.Workspace),
		attribute.String("quaycrew.project", ended.Project),
		attribute.String("quaycrew.phase", ended.Phase),
	}
	if ended.StartedAt != nil {
		telemetry.Record(ctx, "job.attempt", *ended.StartedAt, finished, append(shared,
			attribute.Int("quaycrew.attempt", ended.Attempts),
			attribute.String("quaycrew.session", ended.Session))...)
	}
	// The whole life of the job, and only once it has ended: a span covering job that is still
	// running would report a duration that is not the answer to anything.
	if Terminal(ended.Phase) {
		telemetry.Record(ctx, "job", ended.CreatedAt, finished, shared...)
	}
}

// unmet is what the job said would show its task did it, where that is not there. Empty means the
// job claimed nothing, or the claim held.
//
// Read after the task rather than described by it: the model reporting on its own job is the thing
// this exists to stop.
func (c *Controller) unmet(ctx context.Context, one *Job, reply string) string {
	if carries := one.ExpectContains; carries != "" && !strings.Contains(reply, carries) {
		return fmt.Sprintf("the answer does not carry %q, which this job said would show it was done", carries)
	}
	path := one.ExpectFile
	if path == "" {
		return ""
	}
	if c.prover == nil {
		return fmt.Sprintf("%s could not be checked: this system cannot read a session's files", path)
	}
	held, err := c.prover.SessionHolds(ctx, one.Session, path)
	if err != nil {
		return fmt.Sprintf("%s could not be checked: %v", path, err)
	}
	if !held {
		return fmt.Sprintf("%s is not in the session that did the job", path)
	}
	return ""
}

// spentBy is what the job's own conversation has cost. A system with no reader wired answers zero,
// because a cost nobody can read is not a number worth inventing.
func (c *Controller) spentBy(ctx context.Context, session string) int64 {
	if c.spend == nil || session == "" {
		return 0
	}
	return c.spend.SessionTokens(ctx, session)
}

// event describes one movement of a job.
func (c *Controller) event(ctx context.Context, one *Job, kind, detail string) *Event {
	if c.redactor != nil {
		detail = c.redactor.RedactFor(ctx, one.Workspace, detail)
	}
	return &Event{
		ID: newRowID(), Kind: kind, Job: one.ID, Workspace: one.Workspace, Project: one.Project,
		Parent: one.Parent, Depth: one.Depth, Detail: detail, TraceID: one.TraceID,
		OccurredAt: time.Now().UTC(),
	}
}

// newRowID mints an identifier for a record or for a job the system declares itself, in the same
// shape the store mints everything else in.
//
// Minted here rather than by the store, because writing the same record twice has to leave one row and
// the identifier is what makes that possible, and because the store imports this package and cannot
// be imported back.
func newRowID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// SessionFor is the conversation a job runs in.
//
// Named after the job rather than minted, so a controller that comes back to a row finds the same
// session without being told which one it was. A dispatch to a handle that exists continues that
// conversation, so an attempt after the first lands where the job has been all along.
func SessionFor(id string) string { return "job-" + id }

// The status a task row carries. They are the session's words, kept here so the controller reads a
// task without depending on the package that writes one.
const (
	StatusRunning = "running"
	StatusFailed  = "failed"
	// StatusTaskStopped is a task an operator halted. It is not a failure, and the controller must
	// not read it as one: the job is stopped with their reason rather than failed with a crash.
	StatusTaskStopped = "stopped"
)

// NoSandbox is what the system writes on a task it could not give a container.
//
// It is a constant rather than the same sentence written in two places, because two ends depend on
// it: the control plane writes it onto the task, and the controller reads it to tell a job that
// never started from a job that ran and did not work. The two must not drift.
const NoSandbox = "the session's sandbox could not be created"

// NeverStarted says whether a task failed for want of a sandbox rather than for anything the job
// asked for. A job the system could not start is not a job that was wrong.
func NeverStarted(failure string) bool { return strings.Contains(failure, NoSandbox) }

// oneLine keeps a reason readable on a listing: a record is read on one line beside others.
func oneLine(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	const most = 200
	if len(flat) <= most {
		return flat
	}
	return flat[:most] + "..."
}

// wholeJob is one job with what its session finished and what its attempts said, which a listing
// leaves out and a loop cannot be read without.
//
// A read per landed task rather than per tick: a job whose task is still running never reaches this,
// so a system holding a hundred running jobs does no more work here than it did before.
func (c *Controller) wholeJob(ctx context.Context, one *Job) *Job {
	whole, err := c.store.GetJob(ctx, one.ID)
	if err != nil {
		c.logger.WarnContext(ctx, "could not read a job whole, so this attempt is not held against the ones before it",
			"job", one.ID, "error", err)
		return one
	}
	return whole
}

// saidBy is what an attempt had to show for itself: the answer where the task answered, and the
// failure where it did not.
//
// Both are compared, because a session going in circles produces both shapes. Three tasks dying on
// the same error is the same loop as three answers saying the same thing, and the second is the one
// that spends a budget.
func saidBy(task *quaycrewv1.Task, landed Landing) string {
	if reply := strings.TrimSpace(task.GetReply()); reply != "" {
		return reply
	}
	if failure := strings.TrimSpace(task.GetFailure()); failure != "" {
		return failure
	}
	return landed.Reason
}

// wentInCircles stops the step where this job is going in circles, takes the route the job declared,
// and says whether it did.
//
// Nothing is landed where it did. The attempt goes onto the record with the loop, in one movement, so
// a reader never finds a loop with no attempt behind it.
func (c *Controller) wentInCircles(ctx context.Context, one *Job, attempt Attempt, turnedAway givenUp) bool {
	at := append(Before(one.Attempted, attempt.Step, attempt.Task), attempt)
	if !Circling(at) {
		return false
	}
	ctx = telemetry.Under(ctx, one.TraceID, one.ParentSpanID)
	looped := Loop{Owner: c.owner, Step: attempt.Step, Similarity: attempt.Similarity, Attempt: &attempt}
	route, err := ReadRoute(one.Escalation)
	if err != nil {
		// A route the system cannot read is a route it cannot take, and the job is still going in
		// circles, so the operator gets it. Refused at the write, so this is the row somebody wrote
		// before the routes existed rather than anything a caller can reach.
		c.logger.WarnContext(ctx, "a job declares a route this build cannot read, so its loop goes to the operator",
			"job", one.ID, "escalation", one.Escalation, "error", err)
		route = Route{Word: RouteAsk}
	}
	switch {
	// Escalated once already, and the escalation is what went in circles this time. Escalating again
	// would be the system going round the same loop with more steps in it, so it stops and a person
	// reads what two different attempts at the work produced.
	case one.EscalatedTo != "":
		looped.Phase, looped.Reason = PhaseStopped, LoopedAgain(attempt.Step, one.EscalatedTo)
	case route.Word == RouteRole:
		// Back to pending, in a conversation of its own, running as the role it was handed to. No
		// reason is written: a pending job carrying one reads as a job the machine is holding back for
		// want of room, and this one is going again.
		looped.Phase, looped.To, looped.Handed = PhasePending, route.String(), true
	default:
		looped.Phase, looped.To = PhaseAsking, route.String()
		looped.Question = LoopQuestion(one, attempt.Step, at)
	}
	record := c.event(ctx, one, EventLooped,
		fmt.Sprintf("%s, %s", Looped(attempt.Step, attempt.Similarity), whereItWent(looped, route)))
	ended, err := c.store.LoopJob(ctx, one.ID, looped, record)
	if err != nil {
		if !errors.Is(err, ErrNotRunning) && !errors.Is(err, ErrHeld) {
			c.logger.WarnContext(ctx, "could not write that a job went in circles", "job", one.ID, "error", err)
		}
		// Left to land the way it was going to. A loop the system could not write down must not swallow
		// the attempt underneath it.
		return false
	}
	c.exported(ctx, record)
	c.revoked(ended)
	// Not started again inside the pass that escalated it. One movement per job per tick is what the
	// rest of this loop does, and a job handed to another role that started again in the same tick
	// would be pending for no time at all: nothing reading the record would ever see it waiting.
	turnedAway[one.ID] = true
	c.logger.InfoContext(ctx, "a job went in circles",
		"job", one.ID, "session", one.Session, "step", attempt.Step,
		"similarity", attempt.Similarity, "escalated", whereItWent(looped, route))
	return true
}

// whereItWent is what the record says happened to a looping job, which is the route it took or the
// stop where it had taken one already.
func whereItWent(looped Loop, route Route) string {
	if looped.Phase == PhaseStopped {
		return "stopped, having escalated already"
	}
	return Escalating(route)
}
