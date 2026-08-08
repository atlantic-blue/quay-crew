// Package model is the adapter between the control plane and whatever runs the model. The default
// implementation drives the Claude Code command line tool inside the session's sandbox, so turns run
// on the operator's subscription; anything else sits behind the same Runner interface.
package model

import (
	"context"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// Request is one turn to run against the model.
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
}

// Response is the result of a turn.
type Response struct {
	Reply          string
	ModelSessionID string
}

// Runner runs one turn inside the session's sandbox.
type Runner interface {
	Run(ctx context.Context, box sandbox.Sandbox, req Request) (Response, error)
}
