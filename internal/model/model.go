// Package model is the adapter between the control plane and whatever runs the model.
//
// The default implementation drives the local Claude Code CLI as a subprocess, so threads run under
// your existing subscription with no API cost. An API backed implementation, or a local model, can
// sit behind the same Runner interface and be selected by configuration. A turn runs inside the
// session's sandbox, which the control plane hands to Run.
package model

import (
	"context"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// Request is one turn to run against the model.
type Request struct {
	// Text is the user input for this turn.
	Text string
	// ModelSessionID resumes an existing model thread when set; empty starts a new thread.
	ModelSessionID string
	// PermissionMode controls autonomy: "plan", "acceptEdits", or "bypassPermissions".
	PermissionMode string
	// Workdir is the directory the turn runs in; empty uses the runner's default.
	Workdir string
}

// Response is the result of a turn.
type Response struct {
	// Reply is the model's final text for the turn.
	Reply string
	// ModelSessionID is the model thread id, used to resume the thread on the next turn.
	ModelSessionID string
}

// Runner runs a single turn against a model inside the session's sandbox, returning the reply and
// the thread id to resume.
type Runner interface {
	Run(ctx context.Context, box sandbox.Sandbox, req Request) (Response, error)
}
