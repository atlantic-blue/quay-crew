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

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

// ErrNotFound is returned when a workspace or session does not exist, or has been deleted.
var ErrNotFound = errors.New("store: not found")

// SessionFilter narrows a listing. The zero value is every live session the crew has.
//
// It is a struct rather than a list of arguments because the third one would have been a bare
// boolean at a call site, where nobody reading it could tell what true meant.
type SessionFilter struct {
	// Project wins over Workspace when both are set, because it is the narrower of the two.
	Workspace string
	Project   string
	// Archived asks for the threads that have been put away instead of the live ones. A listing is
	// one or the other and never both: the default view must not quietly grow back the threads
	// somebody deliberately hid.
	Archived bool
}

// Store persists workspaces, channels and sessions.
//
// Workspaces are soft deleted: a deleted workspace is invisible to every read, and its rows stay so the
// sessions that reference it keep their history.
type Store interface {
	CreateWorkspace(ctx context.Context, name string) (*quaycrewv1.Workspace, error)
	GetWorkspace(ctx context.Context, id string) (*quaycrewv1.Workspace, error)
	ListWorkspaces(ctx context.Context) ([]*quaycrewv1.Workspace, error)
	DeleteWorkspace(ctx context.Context, id string) error
	AttachChannel(ctx context.Context, workspace, id, kind string) (*quaycrewv1.Channel, error)

	// CreateProject adds a body of work to a workspace. Threads happen inside a project.
	CreateProject(ctx context.Context, workspace, name string) (*quaycrewv1.Project, error)
	GetProject(ctx context.Context, id string) (*quaycrewv1.Project, error)
	// ListProjects lists every project, or one workspace's when workspace is set.
	ListProjects(ctx context.Context, workspace string) ([]*quaycrewv1.Project, error)
	DeleteProject(ctx context.Context, id string) error

	// FindOrCreateSession returns the session for a project's thread, creating it on first use, so
	// a channel that only knows its own thread id always lands in the same session.
	FindOrCreateSession(ctx context.Context, project, thread string) (*quaycrewv1.Session, error)
	// RecordTurn stores the model conversation handle and the session's status after a turn. An
	// empty modelSessionID leaves the stored handle alone, so a failed turn cannot erase it.
	RecordTurn(ctx context.Context, id, modelSessionID, status string) error
	GetSession(ctx context.Context, id string) (*quaycrewv1.Session, error)
	// ListSessions returns the sessions a filter selects.
	ListSessions(ctx context.Context, filter SessionFilter) ([]*quaycrewv1.Session, error)
	StopSession(ctx context.Context, id string) error
	// ArchiveSession puts a thread away: it disappears from the default listing and nothing else
	// happens to it. The row, the conversation handle and the files on the host all stay.
	ArchiveSession(ctx context.Context, id string) error
	// RestoreSession brings an archived thread back into the default listing.
	RestoreSession(ctx context.Context, id string) error
	// RestartSession marks a stopped session idle again. The conversation is untouched, because it
	// lives on the host rather than in the sandbox that was torn down, which is the whole reason
	// bringing a thread back is possible at all. Whether the session was stopped in the first place
	// is the control plane's question, not the store's.
	RestartSession(ctx context.Context, id string) error

	// Close releases whatever the implementation holds open.
	Close()
}

// NewID returns a random identifier for a workspace or a session.
func NewID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
