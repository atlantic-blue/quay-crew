package model

import "fmt"

// Kinds of model backend. The default is the Claude Code adapter, which drives the operator's own
// subscription rather than an API key.
const (
	// KindClaudeCode runs tasks through the Claude Code command line tool inside the sandbox.
	KindClaudeCode = "claude-code"
	// KindEcho runs `echo` in the sandbox instead of a model. It is how the smoke test drives a real
	// task end to end without a subscription.
	KindEcho = "echo"
)

// ResolveKind names the backend a kind selects, filling in the default for an empty one. Anything
// that reports the configuration reads it from here, so what is reported cannot drift from what is
// built.
func ResolveKind(kind string) (string, error) {
	switch kind {
	case "", KindClaudeCode:
		return KindClaudeCode, nil
	case KindEcho:
		return KindEcho, nil
	default:
		return "", fmt.Errorf("model: unknown runner %q", kind)
	}
}

// NewRunner builds a Runner by kind, so the composition root chooses the model backend by config and
// nothing downstream depends on a concrete implementation. The runner is handed a session sandbox per
// task, so it holds no sandbox itself.
//
// model is which model a task runs against, and it means nothing to the echo backend, which runs no
// model at all.
func NewRunner(kind, workdir, model string) (Runner, error) {
	resolved, err := ResolveKind(kind)
	if err != nil {
		return nil, err
	}
	if resolved == KindEcho {
		return EchoRunner{}, nil
	}
	return &ClaudeCodeRunner{Bin: "claude", DefaultWorkdir: workdir, Model: model}, nil
}

// The permission modes a task can run in, which are the model's own, not ours.
const (
	// PermissionPlan reads and proposes and changes nothing.
	PermissionPlan = "plan"
	// PermissionAcceptEdits lets the model edit the files in its working directory without asking.
	// It is what every task has run as, hardcoded, since the control plane was written.
	PermissionAcceptEdits = "acceptEdits"
	// PermissionBypass lets the model do anything without asking. In a sandbox that means anything to
	// one container holding one project's files; on the local backend it means anything to the host.
	PermissionBypass = "bypassPermissions"
)

// KnownPermissionMode says whether a mode is one the model understands. An unknown one is refused
// where it is set rather than handed to the model, which would take it as far as its own argument
// parser and no further.
func KnownPermissionMode(mode string) bool {
	switch mode {
	case PermissionPlan, PermissionAcceptEdits, PermissionBypass:
		return true
	default:
		return false
	}
}
