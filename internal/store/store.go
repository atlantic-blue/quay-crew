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
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/atlantic-blue/quay-crew/internal/skill"
	"github.com/google/uuid"
)

// A conversation is unbounded and a terminal is not, so asking for everything gets the most recent
// slice of it.
const (
	defaultTurnLimit = 50
	maxTurnLimit     = 500
)

// TurnLimit applies the default when nothing was asked for and the ceiling when too much was.
func TurnLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultTurnLimit
	case limit > maxTurnLimit:
		return maxTurnLimit
	default:
		return limit
	}
}

// ErrNotFound covers deleted as well as never existed.
var ErrNotFound = errors.New("store: not found")

// ErrSkillChanged is returned when a version of a skill is imported again carrying a different skill.
//
// It is a refusal rather than an overwrite because a workspace pins the version it holds. Overwriting
// would change a skill under sessions already using it, silently, which is exactly what pinning is
// for. The way forward is to raise the version in the manifest.
var ErrSkillChanged = errors.New("store: that version of the skill is already imported and differs")

// Imported is a skill the crew holds: the skill as its author wrote it, and when it came in.
type Imported struct {
	skill.Skill
	ImportedAt time.Time
}

// SessionFilter narrows a listing. The zero value is every live session the crew has.
type SessionFilter struct {
	// Project wins over Workspace when both are set, being the narrower.
	Workspace string
	Project   string
	// Archived asks for the threads put away instead of the live ones, never both: the default view
	// must not quietly grow back the threads somebody hid.
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

	// FindOrCreateSession creates on first use, so a channel that knows only its own thread id always
	// lands in the same session.
	FindOrCreateSession(ctx context.Context, project, thread string) (*quaycrewv1.Thread, error)
	// SetSessionSkills records the skill set a session's live sandbox was born with; empty clears
	// it. SessionSkills reads it back, empty when no live sandbox is known. Stopping or archiving
	// a session clears it, because the sandbox goes with it and the next one is born current.
	SetSessionSkills(ctx context.Context, id, fingerprint string) error
	SessionSkills(ctx context.Context, id string) (string, error)
	// FindOrCreateDriver returns the project's driver, one per project, creating it on first open.
	FindOrCreateDriver(ctx context.Context, project string) (*quaycrewv1.Thread, error)
	// RecordTurn leaves the stored handle alone when modelSessionID is empty, so a failed turn cannot
	// erase it.
	RecordTurn(ctx context.Context, id, modelSessionID, status string) error
	GetSession(ctx context.Context, id string) (*quaycrewv1.Thread, error)
	ListSessions(ctx context.Context, filter SessionFilter) ([]*quaycrewv1.Thread, error)
	StopSession(ctx context.Context, id string) error
	// ArchiveSession only hides a thread from the default listing. The row, the conversation handle
	// and the files on the host all stay.
	ArchiveSession(ctx context.Context, id string) error
	// RestoreSession brings an archived thread back into the default listing.
	RestoreSession(ctx context.Context, id string) error
	// SetPermissionMode records what a thread's turns may do without asking. Whether the mode is one
	// the model understands is the control plane's question, not the store's.
	SetPermissionMode(ctx context.Context, id, mode string) error
	// RestartSession marks a stopped session idle again. The conversation is untouched, because it
	// lives on the host rather than in the sandbox that was torn down, which is the whole reason
	// bringing a thread back is possible at all. Whether the session was stopped in the first place
	// is the control plane's question, not the store's.
	RestartSession(ctx context.Context, id string) error

	// GetContext returns what the model should be told at a scope, and empty when nothing has been
	// written there. Nothing written is the normal state and is not an error.
	GetContext(ctx context.Context, scope ContextScope, owner string) (string, error)
	// SetContext records what the model should be told at a scope.
	SetContext(ctx context.Context, scope ContextScope, owner, body string) error

	// ImportSkill takes a skill into the crew at the version its manifest declares.
	//
	// Importing the same name and version again is fine when it is the same skill and refused when it
	// is not, because a workspace pins a version: a version that changed underneath it would be a
	// skill changing under a session already using it, which is the one thing pinning exists to stop.
	ImportSkill(ctx context.Context, imported Imported) error
	// GetSkill returns one revision of a skill, its files included.
	GetSkill(ctx context.Context, name string, version int) (Imported, error)
	// ListSkills returns the newest revision of every skill the crew holds, without their files. A
	// listing is read to see what exists, and the files are the largest part of a skill.
	ListSkills(ctx context.Context) ([]Imported, error)
	// AttachSkill gives a workspace a skill, pinned to the newest revision the crew holds now.
	// Attaching one it already holds moves it to that revision.
	AttachSkill(ctx context.Context, workspace, name string) (Imported, error)
	// DetachSkill takes a skill away from a workspace. The skill stays imported, because another
	// workspace may hold it and because importing it again should not be the price of a change of mind.
	DetachSkill(ctx context.Context, workspace, name string) error
	// WorkspaceSkills returns the skills a workspace holds, at the versions it pinned, files included,
	// which is what a sandbox needs to be built.
	WorkspaceSkills(ctx context.Context, workspace string) ([]Imported, error)

	// AppendTurn records one turn of a session's history, and is safe to call twice with the same
	// turn: a caller retrying a write it is not sure landed must leave one turn, so a record it has
	// already written must not double it. The turn's Id is what makes that possible.
	AppendTurn(ctx context.Context, turn *quaycrewv1.Turn, workspace, project, thread string) error

	// The flow engine's substrate: a run and its transitions are rows written in one transaction,
	// which is what makes reconstructable a guarantee rather than a sentence. The contract is
	// flow.Store; the reads beside it are for listings and tests.
	ImportFlowGraph(ctx context.Context, name string, version int, definition string) error
	LatestFlowGraph(ctx context.Context, name string) (int, string, error)
	FlowGraph(ctx context.Context, name string, version int) (string, error)
	DueFlowRuns(ctx context.Context, now time.Time) ([]*flow.Run, error)
	ScheduleFlow(ctx context.Context, graph, project string, every time.Duration, next time.Time) error
	UnscheduleFlow(ctx context.Context, graph, project string) error
	DueFlowSchedules(ctx context.Context, now time.Time) ([]flow.Schedule, error)
	MarkFlowScheduled(ctx context.Context, graph, project string, next time.Time) error
	CreateFlowRun(ctx context.Context, run *flow.Run) error
	AdvanceFlowRun(ctx context.Context, run *flow.Run, transition flow.Transition) error
	GetFlowRun(ctx context.Context, id string) (*flow.Run, error)
	ListFlowRuns(ctx context.Context, project string) ([]*flow.Run, error)
	// StopFlowRun halts a run that is still running, keeping the reason. A run that already ended is
	// refused rather than overwritten: the record of how it ended is the useful part.
	StopFlowRun(ctx context.Context, id, reason string) (*flow.Run, error)
	ListFlowTransitions(ctx context.Context, run string) ([]flow.RecordedTransition, error)
	// ListTurns returns a session's history oldest first, capped at limit, so a conversation reads
	// the way it happened. A limit of zero or less means the default.
	ListTurns(ctx context.Context, session string, limit int) ([]*quaycrewv1.Turn, error)

	// Close releases whatever the implementation holds open.
	Close()
}

// ContextScope is which level a piece of context belongs to. They layer: the crew's is true
// everywhere, a workspace's inside it, a project's inside that.
type ContextScope string

const (
	// ContextCrew is true of everything this crew does. Its owner is empty.
	ContextCrew ContextScope = "crew"
	// ContextWorkspace is true of one workspace, owned by its id.
	ContextWorkspace ContextScope = "workspace"
	// ContextProject is true of one project, owned by its id.
	ContextProject ContextScope = "project"
	// ContextSession is true of one conversation, owned by its session id. It is the innermost level,
	// and where a note written from inside a sandbox lands.
	ContextSession ContextScope = "session"
)

// ContextLevels are the scopes in order, outermost first. They layer: everything the crew knows, then
// the workspace, then the project, then this one conversation.
func ContextLevels() []ContextScope {
	return []ContextScope{ContextCrew, ContextWorkspace, ContextProject, ContextSession}
}

// KnownContextScope says whether a scope is one of the three.
func KnownContextScope(scope ContextScope) bool {
	switch scope {
	case ContextCrew, ContextWorkspace, ContextProject, ContextSession:
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
// given it with `--session-id` and rejects anything that is not one. The crew chooses it rather than
// reading it back afterwards: a conversation started interactively never tells anybody what it picked,
// so every conversation opened from the panel was one the crew could not name, could not show a
// history for, and could not count the tokens of.
func NewConversationID() string {
	return uuid.NewString()
}
