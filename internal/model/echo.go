package model

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// echoModelSessionID is the thread id EchoRunner reports, so a resumed turn is distinguishable from
// a new one in tests and smoke runs.
const echoModelSessionID = "echo-session"

// EchoRunner runs `echo` inside the session's sandbox and returns what it printed.
//
// It exists so the whole dispatch path can be exercised without a model or a subscription: the turn
// really does exec a command inside the session's sandbox and stream its output back, exactly as the
// Claude Code adapter does. A runner that returned a canned string without execing would not prove
// the sandbox works, which is the part that has broken before.
type EchoRunner struct{}

var _ Runner = EchoRunner{}

// Run echoes the request text inside the sandbox and returns its output as the reply.
func (EchoRunner) Run(ctx context.Context, box sandbox.Sandbox, req Request) (Response, error) {
	if box == nil {
		return Response{}, fmt.Errorf("model: no sandbox provided")
	}

	spec := sandbox.Spec{Argv: []string{"echo", req.Text}, Workdir: req.Workdir}
	proc, err := box.Exec(ctx, spec)
	if err != nil {
		return Response{}, fmt.Errorf("model: exec: %w", err)
	}

	out, readErr := io.ReadAll(proc.Stdout())
	if waitErr := proc.Wait(); waitErr != nil {
		return Response{}, fmt.Errorf("model: run exited: %w", waitErr)
	}
	if readErr != nil {
		return Response{}, fmt.Errorf("model: read output: %w", readErr)
	}

	return Response{Reply: strings.TrimRight(string(out), "\n"), ModelSessionID: echoModelSessionID}, nil
}
