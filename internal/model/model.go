// Package model is the adapter between the control plane and whatever runs the model. The default
// implementation drives the Claude Code command line tool inside the session's sandbox, so tasks run
// on the operator's subscription; anything else sits behind the same Runner interface.
package model

import (
	"context"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// Request is one task to run against the model.
type Request struct {
	Text string
	// ModelSessionID resumes an existing conversation; empty starts one.
	ModelSessionID string
	// PermissionMode is "plan", "acceptEdits" or "bypassPermissions".
	PermissionMode string
	Workdir        string
	// Env is filled from the workspace's secrets, so a value reaches the sandbox without ever being
	// part of the request text or the event log.
	Env map[string]string
	// Settings is an extra settings file inside the sandbox for the runtime to load, which is how the
	// hooks a session runs under reach it. Empty means the session is under none, and the flag is left
	// off entirely: pointing the runtime at a file that is not there fails the task.
	Settings string
}

// Response is the result of a task.
type Response struct {
	Reply          string
	ModelSessionID string
}

// Runner runs one task inside the session's sandbox.
type Runner interface {
	Run(ctx context.Context, box sandbox.Sandbox, req Request) (Response, error)
}
