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

// Provider mints a Sandbox per session, keyed by the session id.
type Provider interface {
	Create(ctx context.Context, id string) (Sandbox, error)
}
