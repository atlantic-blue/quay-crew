// Package sandbox provides isolated execution environments for agent sessions. Each session runs in
// its own Sandbox, created by a Provider and reused across the session's turns, then closed when the
// session ends.
package sandbox

import (
	"context"
	"io"
)

// Spec describes a command to run inside a Sandbox.
type Spec struct {
	Argv    []string
	Workdir string
	// Env is extra environment, each entry "KEY=value".
	Env []string
}

// Process is a running command with a streaming stdout.
type Process interface {
	Stdout() io.Reader
	Wait() error
	// Stderr is the tail of the error stream, available once Wait has returned. A reader nobody
	// drains stops the command dead as soon as the pipe fills, and nothing reads this until the
	// command has already failed.
	Stderr() string
}

// Sandbox is one session's isolated environment.
type Sandbox interface {
	Exec(ctx context.Context, spec Spec) (Process, error)
	Close(ctx context.Context) error
}

// Config describes the sandbox a session needs.
//
// The provider is told the workspace and the project as well as the session, because a session's
// state does not all sit at one level: the conversation store belongs to the workspace and the
// working files to the project.
type Config struct {
	ID        string
	Workspace string
	Project   string
	// Env is set on the sandbox itself, so anything started inside it later inherits the values. They
	// are readable for the life of the container, through docker inspect among other things, so pass
	// only what the session needs.
	Env []string
	// Mounts are directories this session gets on top of the state the crew keeps for it.
	Mounts []Mount
	// Driver joins the control plane's network and gets the host paths handed to the driver. An
	// ordinary session gets neither.
	Driver bool
}

// Provider mints a Sandbox per session.
type Provider interface {
	Create(ctx context.Context, cfg Config) (Sandbox, error)
}

// Mount is a directory made available inside a sandbox.
type Mount struct {
	// Source is the directory as the host daemon sees it.
	Source string
	Target string
	// ReadOnly stops a session rewriting what it was given. A session that can edit its own skills
	// can give itself a capability nobody approved.
	ReadOnly bool
}

// These are properties of the sandbox image, so they move with deploy/sandbox/claude.Dockerfile.
const (
	ConversationPath = "/home/agent/.claude"
	WorkingPath      = "/home/agent/workspace"
	SkillsPath       = "/home/agent/skills"
	// SharedPath is the workspace's own volume, mounted read write into every session in it. The
	// repositories a workspace works in are cloned here once and shared; anything else a workspace
	// accumulates and wants its sessions to see can live here too.
	SharedPath = "/home/agent/shared"
	// AttachedSessionName is the operator's open conversation inside the sandbox. One per sandbox, so
	// opening a thread twice lands in the one already running.
	AttachedSessionName = "quay"
	// MemoryFile is the model's own convention, not ours.
	MemoryFile = "CLAUDE.md"
	// OpenConversation keeps the terminal alive when a conversation ends, so ending one does not take
	// everything running with it.
	OpenConversation = "open-conversation"
)

const ContainerPrefix = "quaycrew-"

// ContainerName is derived here rather than rebuilt by everything that needs to reach into a session
// from outside the provider.
func ContainerName(sessionID string) string { return ContainerPrefix + sessionID }
