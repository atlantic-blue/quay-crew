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
	// Settings is an extra settings file inside the sandbox for the runtime to load, which is how the
	// hooks a session runs under reach it. Empty means the session is under none, and the flag is left
	// off entirely: pointing the runtime at a file that is not there fails the turn.
	Settings string
}

// Response is the result of a turn.
type Response struct {
	Reply          string
	ModelSessionID string
	// Usage is what this one turn spent, in the same four numbers the crew already reads off a
	// conversation's transcript. The transcript carries a conversation's running total, which is the
	// right shape for "what has this thread cost"; this is the per turn figure, which is the right
	// shape for a counter.
	Usage sandbox.Usage
	// CostUSD is what the model's own tooling says this turn would cost at published prices. The
	// crew runs under a subscription, so it is not a charge anybody receives: it is the number that
	// says whether a crew of agents is affordable, and it is worth having for exactly that.
	CostUSD float64
	// UsageReported distinguishes a turn that spent nothing from a turn whose backend never said. A
	// zero that means "unknown" read as "free" is how a cost dashboard lies.
	UsageReported bool
}

// Runner runs one turn inside the session's sandbox.
type Runner interface {
	Run(ctx context.Context, box sandbox.Sandbox, req Request) (Response, error)
}
