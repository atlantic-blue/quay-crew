// Package sandbox provides isolated execution environments for agent sessions. Each session runs in
// its own Sandbox, created by a Provider and reused across the session's turns, then closed when the
// session ends. "Sandbox" is the common industry term for an isolated agent execution environment
// (E2B, Modal, and others use it). Docker is the default provider; a local backend is a stopgap.
package sandbox

import (
	"context"
	"io"
)

// Spec describes a command to run inside a Sandbox.
type Spec struct {
	// Argv is the command and its arguments, for example ["claude", "-p", "..."].
	Argv []string
	// Workdir is the directory to run in; empty uses the sandbox default.
	Workdir string
	// Env is extra environment, each entry "KEY=value".
	Env []string
}

// Process is a running command with a streaming stdout.
type Process interface {
	Stdout() io.Reader
	Wait() error
}

// Sandbox is one session's isolated environment. Commands exec inside it; it lives across the
// session's turns and is closed when the session ends.
type Sandbox interface {
	Exec(ctx context.Context, spec Spec) (Process, error)
	Close(ctx context.Context) error
}

// Config describes the sandbox a session needs.
//
// A sandbox belongs to a session, and through it to a project and a workspace. The provider is told
// all three because a session's state does not all sit at the same level: the conversation store
// belongs to the workspace, and the working files belong to the project. How that state is actually
// kept is the provider's business, a host directory on Docker and a volume on Kubernetes.
type Config struct {
	// ID is the session's id. It names the sandbox.
	ID string
	// Workspace owns the conversation store and the workspace's own context.
	Workspace string
	// Project owns the working directory: the project's files and its context.
	Project string
	// Env is set on the sandbox itself, so every process started inside it inherits the values. That
	// is what lets an operator attach to a session's conversation directly, rather than the tool
	// having to carry a credential to each command it runs. The cost is that these values are
	// readable for the life of the container, for example through docker inspect, so pass only what
	// the session needs.
	Env []string
}

// Provider mints a Sandbox per session.
type Provider interface {
	Create(ctx context.Context, cfg Config) (Sandbox, error)
}

// Mount is a directory made available inside a sandbox. Both sides are read write: the model's own
// conversation store is one of them, and the model has to be able to write it.
type Mount struct {
	// Source is the directory as the host daemon sees it.
	Source string
	// Target is where it appears inside the sandbox.
	Target string
}

// These are properties of the sandbox image, so they move with deploy/sandbox/claude.Dockerfile.
const (
	// ConversationPath is where the model's command line tool keeps its own state: its settings, the
	// workspace memory it reads as CLAUDE.md, and the transcripts a turn resumes. It is inside the
	// container, so without a mount it dies with the container.
	ConversationPath = "/home/agent/.claude"
	// WorkingPath is the directory a turn runs in: the project's files, and the project memory the
	// model reads as CLAUDE.md.
	WorkingPath = "/home/agent/workspace"
	// AttachedSessionName is what the operator's open conversation is called inside the sandbox. It
	// is one per sandbox, and a sandbox holds one thread, so opening a thread twice lands in the one
	// that is already running rather than starting a second beside it.
	AttachedSessionName = "quay"
)

// ContainerPrefix is what a session's container name starts with, so ours are recognisable among
// everything else on the daemon.
const ContainerPrefix = "quaycrew-"

// ContainerName is the container a session's sandbox runs in. Anything that needs to reach into a
// session from outside the provider (attaching to its conversation, shelling in, reaping strays)
// derives the name from here rather than rebuilding it.
func ContainerName(sessionID string) string { return ContainerPrefix + sessionID }
