// Package session runs an agent session's commands in isolation. A session's work (the model CLI,
// later tools) executes inside a Runtime rather than directly on the host, so the run is contained.
// Docker is the default Runtime; a local backend is a short term stopgap; other backends (ssh, a
// remote runner) can implement the same interface.
package session

import (
	"context"
	"io"
)

// Spec describes a command to run in a session Runtime.
type Spec struct {
	// Argv is the command and its arguments, for example ["claude", "-p", "..."].
	Argv []string
	// Workdir is the directory to run in; empty uses the backend default.
	Workdir string
	// Env is extra environment, each entry "KEY=value".
	Env []string
}

// Process is a running command with a streaming stdout.
type Process interface {
	Stdout() io.Reader
	Wait() error
}

// Runtime is the isolated environment an agent session runs in. It starts a command and returns the
// running process.
type Runtime interface {
	Start(ctx context.Context, spec Spec) (Process, error)
}
