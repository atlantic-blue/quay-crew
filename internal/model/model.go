// Package model is the adapter between the control plane and whatever runs the model. The default
// implementation drives the Claude Code command line tool inside the session's sandbox, so tasks run
// on the operator's subscription; anything else sits behind the same Runner interface.
package model

import (
	"context"

	"github.com/atlantic-blue/krewe/internal/sandbox"
)

// Request is one task to run against the model.
type Request struct {
	Text string
	// ModelSessionID is the conversation this task runs in. The system names it before the task starts,
	// so a task always has one. Empty leaves the naming to the runtime, which tells nobody what it
	// chose until the task is over.
	ModelSessionID string
	// ConversationStarted says whether the runtime has opened that conversation already, which decides
	// how it is named on the command line: a name the runtime has never seen is started under, and a
	// name with a transcript behind it is resumed. The two are not interchangeable. Resuming a name
	// that is not there prints "No conversation found" and exits, and starting a name that is there is
	// refused as one already in use.
	ConversationStarted bool
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
	// Usage is what this one task spent, in the same four numbers the system already reads off a
	// conversation's transcript. The transcript carries a conversation's running total, which is the
	// right shape for "what has this session cost"; this is the per task figure, which is the right
	// shape for a counter.
	Usage sandbox.Usage
	// CostUSD is what the model's own tooling says this task would cost at published prices. The
	// system runs under a subscription, so it is not a charge anybody receives: it is the number that
	// says whether a system of agents is affordable, and it is worth having for exactly that.
	CostUSD float64
	// UsageReported distinguishes a task that spent nothing from a task whose backend never said. A
	// zero that means "unknown" read as "free" is how a cost dashboard lies.
	UsageReported bool
}

// Runner runs one task inside the session's sandbox.
type Runner interface {
	Run(ctx context.Context, box sandbox.Sandbox, req Request) (Response, error)
}
