package model

import "fmt"

// Kinds of model backend. The default is the Claude Code adapter, which drives the operator's own
// subscription rather than an API key.
const (
	// KindClaudeCode runs turns through the Claude Code command line tool inside the sandbox.
	KindClaudeCode = "claude-code"
	// KindEcho runs `echo` in the sandbox instead of a model. It is how the smoke test drives a real
	// turn end to end without a subscription.
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
// turn, so it holds no sandbox itself.
func NewRunner(kind, workdir string) (Runner, error) {
	resolved, err := ResolveKind(kind)
	if err != nil {
		return nil, err
	}
	if resolved == KindEcho {
		return EchoRunner{}, nil
	}
	return &ClaudeCodeRunner{Bin: "claude", DefaultWorkdir: workdir}, nil
}
