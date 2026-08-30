// Package store is the durable home of workspaces, their channels, and their sessions.
//
// It exists because the control plane must hold no state of its own. A session's handle to the model
// conversation lives here, so a restart resumes the conversation instead of orphaning it: the
// conversation still exists inside the model's own store, and the pointer to it is the only thing
// that can be lost.
//
// Two implementations, one behaviour. Memory is for tests and for running without a database;
// Postgres is what the composed stack and the cloud use. Both are held to the same conformance suite
// in internal/store/storetest, so a behaviour proven against one is proven against the other.
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/deploy"
	"github.com/atlantic-blue/krewe/internal/flow"
	"github.com/atlantic-blue/krewe/internal/hook"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/origin"
	"github.com/atlantic-blue/krewe/internal/role"
	"github.com/atlantic-blue/krewe/internal/session"
	"github.com/atlantic-blue/krewe/internal/skill"
	"github.com/google/uuid"
)

// A conversation is unbounded and a terminal is not, so asking for everything gets the most recent
// slice of it.
const (
	defaultTaskLimit = 50
	maxTaskLimit     = 500
)

// TaskLimit applies the default when nothing was asked for and the ceiling when too much was.
func TaskLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultTaskLimit
	case limit > maxTaskLimit:
		return maxTaskLimit
	default:
		return limit
	}
}

// ErrNotFound covers deleted as well as never existed.
var ErrNotFound = errors.New("store: not found")

// StatusReclaimed is the session status this store writes when the system takes a container back. The
// control plane owns the whole vocabulary; this one is here because two queries below are written in
// terms of it and the store must not depend on the package that calls it.
const StatusReclaimed = "reclaimed"

// settledStatuses are the states a session can be in and still be nothing's to hold open: waiting for
// job, holding a failed last task, or already reclaimed and waiting to be filed.
//
// "running" is absent because a task is in flight. "stopped" is absent because an operator put the
// session down, and a system that archived what somebody halted would be overwriting a decision with
// bookkeeping.
func settledStatuses() []string { return []string{"idle", "failed", StatusReclaimed} }

// terminalPhases are the phases of a job that no longer hold a session open. Read from the job
// package rather than listed here, so a phase added there cannot be forgotten in these two queries.
func terminalPhases() []string {
	var terminal []string
	for _, phase := range job.Phases() {
		if job.Terminal(phase) {
			terminal = append(terminal, phase)
		}
	}
	return terminal
}

// ErrSkillChanged is returned when a version of a skill is imported again carrying a different skill.
//
// It is a refusal rather than an overwrite because a workspace pins the version it holds. Overwriting
// would change a skill under sessions already using it, silently, which is exactly what pinning is
// for. The way forward is to raise the version in the manifest.
var ErrSkillChanged = errors.New("store: that version of the skill is already imported and differs")

// Imported is a skill the system holds: the skill as its author wrote it, and when it came in.
type Imported struct {
	skill.Skill
	ImportedAt time.Time
}

// ErrRoleChanged is returned when a version of a role is imported again carrying a different role.
//
// A refusal rather than an overwrite, for the reason ErrSkillChanged gives: a workspace pins the
// version it holds, and a role that changed underneath it would change how a session already running
// as it is told to work. The way forward is to raise the version in the manifest.
var ErrRoleChanged = errors.New("store: that version of the role is already imported and differs")

// ImportedRole is a role the system holds: the role as its author wrote it, when it came in, and where
// it came from.
type ImportedRole struct {
	role.Role
	ImportedAt time.Time
	// Origin is where the files were read from, as the machine that imported them saw it. It is not
	// part of the fingerprint: the same bytes imported from two checkouts are one role, read in two
	// places, and a fingerprint that disagreed would refuse the second import as a different role.
	Origin origin.Origin
}

// ErrHookChanged is returned when a version of a hook is imported again carrying a different hook.
//
// A refusal rather than an overwrite, and the stakes are higher here than for a skill: overwriting
// would change a constraint under sessions already running under it, which is how a gate quietly
// stops gating. The way forward is to raise the version in the manifest.
var ErrHookChanged = errors.New("store: that version of the hook is already imported and differs")

// ImportedHook is a hook the system holds: the hook as its author wrote it, and when it came in.
type ImportedHook struct {
	hook.Hook
	ImportedAt time.Time
}

// Birth is what is true of a session only at the moment it is made.
//
// Both fields are read once, when the row is written, and ignored for a session that already exists.
// A session's sandbox is born with its capabilities and never drifts, and these are the same
// statement one level up: changing the system's configuration must not widen a conversation already
// running, and a role's boundary must hold for every task of the session rather than for the first.
type Birth struct {
	// Mode is what the session's tasks may do without asking, from the system's configuration. An
	// unknown or empty one is the mode every session had before this was configurable, so a system
	// that says nothing does not change under an upgrade.
	Mode string
	// Role is the role the session works as, empty for a session that works as nobody in particular.
	Role string
	// Title is what to call the session, from whoever dispatched it. A job puts its declared title
	// here, so the name is on the row before the first task runs. Empty for a caller that has no name
	// for the conversation yet.
	Title string
}

// SessionFilter narrows a listing. The zero value is every live session the system has.
type SessionFilter struct {
	// Project wins over Workspace when both are set, being the narrower.
	Workspace string
	Project   string
	// Archived asks for the sessions put away instead of the live ones, never both: the default view
	// must not quietly grow back the sessions somebody hid.
	Archived bool
}

// Store persists workspaces, channels and sessions. Workspaces are soft deleted, so the sessions
// that reference one keep their history.
type Store interface {
	CreateWorkspace(ctx context.Context, name string) (*quaycrewv1.Workspace, error)
	GetWorkspace(ctx context.Context, id string) (*quaycrewv1.Workspace, error)
	ListWorkspaces(ctx context.Context) ([]*quaycrewv1.Workspace, error)
	DeleteWorkspace(ctx context.Context, id string) error
	AttachChannel(ctx context.Context, workspace, id, kind string) (*quaycrewv1.Channel, error)

	CreateProject(ctx context.Context, workspace, name string) (*quaycrewv1.Project, error)
	GetProject(ctx context.Context, id string) (*quaycrewv1.Project, error)
	// ListProjects lists every project, or one workspace's when workspace is set.
	ListProjects(ctx context.Context, workspace string) ([]*quaycrewv1.Project, error)
	DeleteProject(ctx context.Context, id string) error
	// SetDeployTarget records where a project ships, and a zero target clears it. The store keeps
	// what it is given: whether a target is whole, and whether its identity belongs to its account,
	// is the control plane's question.
	SetDeployTarget(ctx context.Context, project string, target deploy.Target) error
	// SetProjectRepository records where a project's work lands, and what kind of repository that is.
	// Writing it again replaces what is held, so a project that moved repository is corrected rather
	// than growing a second answer.
	SetProjectRepository(ctx context.Context, project, repository, visibility string) (*quaycrewv1.Project, error)

	// FindOrCreateSession creates on first use, so a channel that knows only its own session id always
	// lands in the same session.
	//
	// born is what is true of the session only when it is made. It is ignored for a session that
	// already exists: what a session may do and who it works as are its own, and neither may change
	// under a conversation already running.
	//
	// It says whether it made one. Only the store knows, and the caller has to: a session coming into
	// existence is an event, and finding out afterwards means comparing timestamps and guessing.
	FindOrCreateSession(ctx context.Context, project, session string, born Birth) (found *quaycrewv1.Session, created bool, err error)
	// SetSessionSkills records the skill set a session's live sandbox was born with; empty clears
	// it. SessionSkills reads it back, empty when no live sandbox is known. Stopping or archiving
	// a session clears it, because the sandbox goes with it and the next one is born current.
	SetSessionSkills(ctx context.Context, id, fingerprint string) error
	SessionSkills(ctx context.Context, id string) (string, error)
	// FindOrCreateDriver returns the project's driver, one per project, creating it on first open.
	FindOrCreateDriver(ctx context.Context, project string) (*quaycrewv1.Session, error)
	// RecordTask leaves the stored handle alone when modelSessionID is empty, so a failed task cannot
	// erase it.
	RecordTask(ctx context.Context, id, modelSessionID, status string) error
	GetSession(ctx context.Context, id string) (*quaycrewv1.Session, error)
	// ListSessions returns sessions last moved first: put away if they were, last touched otherwise,
	// which is the clock a listing's age column shows. The order is decided here rather than by each
	// surface, so the console, the command line and the web page cannot drift apart.
	ListSessions(ctx context.Context, filter SessionFilter) ([]*quaycrewv1.Session, error)
	StopSession(ctx context.Context, id string) error
	// ReclaimSession records that the system took a session's container back: the status becomes
	// reclaimed and the moment is stamped. Nothing else moves. The conversation handle, the workspace's
	// conversation store and the project's files are all untouched, so the next task builds a fresh
	// container over the same state and the conversation carries on.
	//
	// It is not a stop. A stop is somebody's decision, and a session that went quiet must never read
	// the same as one that was halted. Whether a session is in a state that may be reclaimed is the
	// control plane's question, not the store's.
	ReclaimSession(ctx context.Context, id string) error
	// SettledSessions is the sessions nothing is holding open, oldest touched first: live, not
	// running, and named by no job in a non terminal phase. It is the fourth query a
	// controller runs each tick, and the one the session lifecycle is derived from.
	//
	// A session is not a second resource with a declaration of its own. What is wanted of it is read
	// from the job that names it, so job still in flight keeps its session alive and nothing here
	// has to be told.
	SettledSessions(ctx context.Context, limit int) ([]*quaycrewv1.Session, error)
	// ArchiveSession only hides a session from the default listing. The row, the conversation handle
	// and the files on the host all stay.
	ArchiveSession(ctx context.Context, id string) error
	// RestoreSession brings an archived session back into the default listing.
	RestoreSession(ctx context.Context, id string) error
	// SetPermissionMode records what a session's tasks may do without asking. Whether the mode is one
	// the model understands is the control plane's question, not the store's.
	SetPermissionMode(ctx context.Context, id, mode string) error
	// SetDescription records what the system observed a session to be, and how many tasks it had when
	// that was written, so the two can never disagree about how current the description is.
	SetDescription(ctx context.Context, id, description string, atTask int) error
	// CountTasks is how many tasks a session has had, which is what decides whether a description has
	// fallen behind the conversation.
	CountTasks(ctx context.Context, session string) (int, error)
	// SetLabel records what the operator calls a session. Empty clears it, which is the only way back
	// to the identifier, so it is a value rather than an absence.
	SetLabel(ctx context.Context, id, label string) error
	// RestartSession marks a stopped session idle again. The conversation is untouched, because it
	// lives on the host rather than in the sandbox that was torn down, which is the whole reason
	// bringing a session back is possible at all. Whether the session was stopped in the first place
	// is the control plane's question, not the store's.
	RestartSession(ctx context.Context, id string) error

	// GetContext returns what the model should be told at a scope, and empty when nothing has been
	// written there. Nothing written is the normal state and is not an error.
	GetContext(ctx context.Context, scope ContextScope, owner string) (string, error)
	// SetContext records what the model should be told at a scope.
	SetContext(ctx context.Context, scope ContextScope, owner, body string) error

	// ImportSkill takes a skill into the system at the version its manifest declares.
	//
	// Importing the same name and version again is fine when it is the same skill and refused when it
	// is not, because a workspace pins a version: a version that changed underneath it would be a
	// skill changing under a session already using it, which is the one thing pinning exists to stop.
	ImportSkill(ctx context.Context, imported Imported) error
	// GetSkill returns one revision of a skill, its files included.
	GetSkill(ctx context.Context, name string, version int) (Imported, error)
	// ListSkills returns the newest revision of every skill the system holds, without their files. A
	// listing is read to see what exists, and the files are the largest part of a skill.
	ListSkills(ctx context.Context) ([]Imported, error)
	// AttachSkill gives a workspace a skill, pinned to the newest revision the system holds now.
	// Attaching one it already holds moves it to that revision.
	AttachSkill(ctx context.Context, workspace, name string) (Imported, error)
	// DetachSkill takes a skill away from a workspace. The skill stays imported, because another
	// workspace may hold it and because importing it again should not be the price of a change of mind.
	DetachSkill(ctx context.Context, workspace, name string) error
	// WorkspaceSkills returns the skills a workspace holds, at the versions it pinned, files included,
	// which is what a sandbox needs to be built.
	WorkspaceSkills(ctx context.Context, workspace string) ([]Imported, error)
	// AttachSystemSkill gives the skill to the whole system, pinned to the newest revision the system holds
	// now, so every workspace has it and a workspace made tomorrow has it too. Attaching again is how
	// the system moves to a newer revision.
	AttachSystemSkill(ctx context.Context, name string) (Imported, error)
	// DetachSystemSkill takes a skill away from the system. A workspace that attached it for itself keeps
	// it: the two are separate statements, and the narrower one is not undone by the wider one.
	DetachSystemSkill(ctx context.Context, name string) error
	// SystemSkills returns what the system holds, at the versions it pinned, files included.
	SystemSkills(ctx context.Context) ([]Imported, error)

	// The same six questions again for hooks. A hook is the same kind of thing as a skill, authored
	// as files and attached at a level, and it is a separate set of calls rather than one generic
	// set because the two entities are separate: a session refused a skill still runs, and a session
	// refused a hook is a session running without the constraint it was supposed to have.
	//
	// ImportHook takes a hook into the system at the version its manifest declares. The same name and
	// version again is fine when it is the same hook and refused when it is not, for the reason
	// ImportSkill gives.
	ImportHook(ctx context.Context, imported ImportedHook) error
	// GetHook returns one revision of a hook, its files included.
	GetHook(ctx context.Context, name string, version int) (ImportedHook, error)
	// ListHooks returns the newest revision of every hook the system holds, without their files.
	ListHooks(ctx context.Context) ([]ImportedHook, error)
	// AttachHook gives a workspace a hook, pinned to the newest revision the system holds now.
	AttachHook(ctx context.Context, workspace, name string) (ImportedHook, error)
	// DetachHook takes a hook away from a workspace. The hook stays imported.
	DetachHook(ctx context.Context, workspace, name string) error
	// WorkspaceHooks returns the hooks a workspace holds, at the versions it pinned, files included,
	// which is what a sandbox needs to be built.
	WorkspaceHooks(ctx context.Context, workspace string) ([]ImportedHook, error)
	// AttachSystemHook gives the hook to the whole system, so every workspace has it and one made
	// tomorrow has it too. This is the level most hooks want: a constraint the system agreed on is not
	// usually a per workspace opinion.
	AttachSystemHook(ctx context.Context, name string) (ImportedHook, error)
	// DetachSystemHook takes a hook away from the system. A workspace that attached it for itself keeps
	// it: the two are separate statements, and the narrower one is not undone by the wider one.
	DetachSystemHook(ctx context.Context, name string) error
	// SystemHooks returns what the system holds, at the versions it pinned, files included.
	SystemHooks(ctx context.Context) ([]ImportedHook, error)

	// The same questions again for roles, and a separate set of calls for the same reason hooks are
	// separate: a role is its own entity, and a workspace that holds a skill has said nothing about
	// which roles its job may be split into.
	//
	// ImportRole takes a role into the system at the version its manifest declares. The same name and
	// version again is fine when it is the same role and refused when it is not.
	ImportRole(ctx context.Context, imported ImportedRole) error
	// GetRole returns one revision of a role.
	GetRole(ctx context.Context, name string, version int) (ImportedRole, error)
	// ListRoles returns the newest revision of every role the system holds.
	ListRoles(ctx context.Context) ([]ImportedRole, error)
	// AttachRole gives a workspace a role, pinned to the newest revision the system holds now.
	AttachRole(ctx context.Context, workspace, name string) (ImportedRole, error)
	// DetachRole takes a role away from a workspace. The role stays imported.
	DetachRole(ctx context.Context, workspace, name string) error
	// WorkspaceRoles returns the roles a workspace holds, at the versions it pinned.
	WorkspaceRoles(ctx context.Context, workspace string) ([]ImportedRole, error)
	// AttachSystemRole gives the role to the whole system, so every workspace has it and one made
	// tomorrow has it too.
	AttachSystemRole(ctx context.Context, name string) (ImportedRole, error)
	// DetachSystemRole takes a role away from the system. A workspace that attached it for itself keeps
	// it: the two are separate statements, and the narrower one is not undone by the wider one.
	DetachSystemRole(ctx context.Context, name string) error
	// SystemRoles returns what the system holds, at the versions it pinned.
	SystemRoles(ctx context.Context) ([]ImportedRole, error)

	// AppendTask records one task of a session's history, and is safe to call twice with the same
	// task: a caller retrying a write it is not sure landed must leave one task, so a record it has
	// already written must not double it. The task's Id is what makes that possible.
	AppendTask(ctx context.Context, task *quaycrewv1.Task, workspace, project, session string) error
	// FinishTask writes what came of a task into the record its start opened, and leaves the rest
	// of that record alone. A task is written when it starts, so what a session was asked is
	// visible while it works; this is the other half of that, the same row closed.
	//
	// A task the store does not hold is not an error. The task itself already happened, and the
	// operator has its result, so a missing row must not come back as a failure of the task.
	FinishTask(ctx context.Context, id, status, reply, failure string) error

	// AppendSessionEvent records one thing that happened to a session, and is safe to call twice with
	// the same event for the same reason AppendTask is: the event's Id is what makes a repeat harmless.
	AppendSessionEvent(ctx context.Context, event *quaycrewv1.SessionEvent) error
	// ListSessionEvents returns a session's lifecycle oldest first, capped at limit, so it reads the
	// way it happened. An empty session asks for the whole system's, which is what a view of what is
	// going on right now reads. A limit of zero or less means the default.
	ListSessionEvents(ctx context.Context, session string, limit int) ([]*quaycrewv1.SessionEvent, error)

	// The flow engine's substrate: a run and its transitions are rows written in one transaction,
	// which is what makes reconstructable a guarantee rather than a sentence. The contract is
	// flow.Store; the reads beside it are for listings and tests.
	ImportFlowGraph(ctx context.Context, name string, version int, definition string) error
	LatestFlowGraph(ctx context.Context, name string) (int, string, error)
	FlowGraph(ctx context.Context, name string, version int) (string, error)
	DueFlowRuns(ctx context.Context, now time.Time) ([]*flow.Run, error)
	LandedFlowSteps(ctx context.Context, limit int) ([]flow.Landed, error)
	FlowRunCarrier(ctx context.Context, run string) (string, error)
	ScheduleFlow(ctx context.Context, graph, project string, every time.Duration, next time.Time) error
	UnscheduleFlow(ctx context.Context, graph, project string) error
	DueFlowSchedules(ctx context.Context, now time.Time) ([]flow.Schedule, error)
	MarkFlowScheduled(ctx context.Context, graph, project string, next time.Time) error
	CreateFlowRun(ctx context.Context, run *flow.Run, carrier *job.Job, records []*job.Event, trigger string) error
	// The pending trigger queue: something that happened, written down so a run starts from it. A
	// row is raised in the transaction of whatever caused it, read off an indexed query, and claimed
	// with a conditional write, so two pollers reading one trigger start one run. The contract is
	// flow.Store.
	RaiseTrigger(ctx context.Context, trigger *flow.Trigger) error
	PendingTriggers(ctx context.Context, limit int) ([]*flow.Trigger, error)
	ClaimTrigger(ctx context.Context, id string, lease job.Lease) (*flow.Trigger, error)
	FailTrigger(ctx context.Context, id, reason string) error
	GetTrigger(ctx context.Context, id string) (*flow.Trigger, error)
	AdvanceFlowRun(ctx context.Context, run *flow.Run, transition flow.Transition) error
	GetFlowRun(ctx context.Context, id string) (*flow.Run, error)
	ListFlowRuns(ctx context.Context, project string) ([]*flow.Run, error)
	// StopFlowRun halts a run that is still running, keeping the reason. A run that already ended is
	// refused rather than overwritten: the record of how it ended is the useful part.
	StopFlowRun(ctx context.Context, id, reason string) (*flow.Run, error)
	ListFlowTransitions(ctx context.Context, run string) ([]flow.RecordedTransition, error)
	// Job is declared intent, kept as a row so it outlives the caller that asked for it. A caller
	// writes a job and a controller makes reality match it.
	//
	// CreateJob writes the job and the record of its declaration in one transaction. A row with no
	// record of how it came to exist, and a record of a declaration that is not there, are both
	// states nothing can explain afterwards.
	CreateJob(ctx context.Context, declared *job.Job, event *job.Event) error
	// GetJob reads one job back, whole, its answer included.
	GetJob(ctx context.Context, id string) (*job.Job, error)
	// ListJob returns what matches, newest first and without answers, because a listing of a hundred
	// answers is a listing nobody can read. A caller that wants an answer asks for one job.
	ListJobs(ctx context.Context, filter job.Filter) ([]*job.Job, error)
	// StopJob halts job that has not ended, keeping the reason, and writes the record of the stop
	// beside it. Job that already ended is refused rather than overwritten: how it ended is the
	// useful part.
	StopJob(ctx context.Context, id, reason string, event *job.Event) (*job.Job, error)
	// AskJob puts a running job's question on the record and stops it there. AnswerJob is the other
	// half: it writes what a person decided and puts the job back to pending, so the controller
	// starts it again and hands the answer to the session that asked. Each applies only from the one
	// phase it moves out of, in the same statement, so a question cannot be answered twice.
	AskJob(ctx context.Context, id, question string, event *job.Event) (*job.Job, error)
	AnswerJob(ctx context.Context, id, answer string, event *job.Event) (*job.Job, error)
	// What a controller needs of the store. RunnableJob is the job it may start, HeldJob is what
	// it holds and has to come back to, and ExpiredJob is what a controller that went away left
	// behind. Every write is conditional in the same statement as its condition, which is what keeps
	// two controllers from both starting one job, both taking over one abandoned row, or
	// both writing what came of it. The contract is job.Store.
	RunnableJob(ctx context.Context, limit int) ([]*job.Job, error)
	HeldJob(ctx context.Context, owner string, limit int) ([]*job.Job, error)
	ExpiredJob(ctx context.Context, limit int) ([]*job.Job, error)
	StartJob(ctx context.Context, id string, lease job.Lease, events []*job.Event) (*job.Job, error)
	HoldJob(ctx context.Context, id, reason string, event *job.Event) (*job.Job, error)
	TakeOverJob(ctx context.Context, id string, lease job.Lease, events []*job.Event) (*job.Job, error)
	ReleaseJob(ctx context.Context, id string, events []*job.Event) (*job.Job, error)
	RequeueJob(ctx context.Context, id string, back job.Requeue, events []*job.Event) (*job.Job, error)
	RenewLease(ctx context.Context, id string, lease job.Lease) error
	RecordJobSession(ctx context.Context, id, session string) error
	LandJob(ctx context.Context, id string, landed job.Landing, event *job.Event) (*job.Job, error)
	// ListJobEvents returns one job's own history, oldest first.
	ListJobEvents(ctx context.Context, id string) ([]*job.Event, error)
	// RecordSteer writes one steer and adds it to the count on each job in counted, in one
	// transaction. Counted is the job it landed on and every job above it, so the count on the job at
	// the top is the score of the whole tree. The row and the counts are written together because a
	// score that disagrees with the marks under it is a score nobody can defend.
	RecordSteer(ctx context.Context, steer *job.Steer, counted []string) error
	// ListSteers returns every steer under one job at the top of a tree, oldest first, which is the
	// order they were made in and the order the report reads.
	ListSteers(ctx context.Context, root string) ([]*job.Steer, error)
	// WorkspaceLimits is what a workspace lets its sessions declare, and SetWorkspaceLimits writes
	// it. A workspace with no row takes the defaults, which grant nothing: default deny, so a system
	// nobody configured refuses rather than allows.
	WorkspaceLimits(ctx context.Context, workspace string) (job.Limits, error)
	SetWorkspaceLimits(ctx context.Context, limits job.Limits) (job.Limits, error)

	// ListTasks returns a session's history oldest first, capped at limit, so a conversation reads
	// the way it happened. A limit of zero or less means the default.
	ListTasks(ctx context.Context, session string, limit int) ([]*quaycrewv1.Task, error)

	// Probe writes, so a caller can prove the store still takes one. A system whose reads all answer
	// and whose writes never land looks healthy from every listing, which is how a control plane that
	// dispatched nothing went unnoticed for an hour. It writes one row and keeps writing over it, so
	// asking often costs one row and never grows.
	Probe(ctx context.Context) error

	// Close releases whatever the implementation holds open.
	Close()
}

// ContextScope is which level a piece of context belongs to. They layer: the system's is true
// everywhere, a workspace's inside it, a project's inside that.
type ContextScope string

const (
	// ContextSystem is true of everything this system does. Its owner is empty.
	ContextSystem ContextScope = "system"
	// ContextWorkspace is true of one workspace, owned by its id.
	ContextWorkspace ContextScope = "workspace"
	// ContextProject is true of one project, owned by its id.
	ContextProject ContextScope = "project"
	// ContextSession is true of one conversation, owned by its session id. It is the innermost level,
	// and where a note written from inside a sandbox lands.
	ContextSession ContextScope = "session"
)

// ContextLevels are the scopes in order, outermost first. They layer: everything the system knows, then
// the workspace, then the project, then this one conversation.
func ContextLevels() []ContextScope {
	return []ContextScope{ContextSystem, ContextWorkspace, ContextProject, ContextSession}
}

// KnownContextScope says whether a scope is one of the three.
func KnownContextScope(scope ContextScope) bool {
	switch scope {
	case ContextSystem, ContextWorkspace, ContextProject, ContextSession:
		return true
	default:
		return false
	}
}

// NewID returns a random identifier for a workspace or a session.
func NewID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// NewConversationID returns an identifier for a conversation with the model.
//
// It is a version 4 identifier rather than one of ours, because the model's command line tool is
// given it with `--session-id` and rejects anything that is not one. The system chooses it rather than
// reading it back afterwards: a conversation started interactively never tells anybody what it picked,
// so every conversation opened from the panel was one the system could not name, could not show a
// history for, and could not count the tokens of.
func NewConversationID() string {
	return uuid.NewString()
}

// sortByLastMoved orders a session listing newest movement first, which is the clock its last column
// shows: when a session was put away if it was, and when it was last touched otherwise.
//
// It sorts on the same stamp the listing renders because the two used to disagree. The order was the
// created stamp and the column was this one, so a session made a week ago and used an hour ago sat
// below one made yesterday and untouched since, and a real listing of forty five sessions read
// 1d, 1d, 1d, 7d, 7d, 7d, 1d, 7d. A column in no order is a column nobody can read.
//
// The identifier breaks a tie, so two sessions that share a moment keep one order between reads.
//
// Postgres writes the same rule as `coalesce(archived_at, updated_at) desc, id`, and storetest holds
// the two to it. The stamp comes from internal/session, which is also where the age column reads it,
// so the order and the cell can never be computed from different fields.
func sortByLastMoved(sessions []*quaycrewv1.Session) {
	sort.Slice(sessions, func(i, j int) bool {
		left, right := session.LastMoved(sessions[i]).AsTime(), session.LastMoved(sessions[j]).AsTime()
		if left.Equal(right) {
			return sessions[i].GetId() < sessions[j].GetId()
		}
		return left.After(right)
	})
}
