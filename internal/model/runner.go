package model

import "fmt"

// NewRunner builds a Runner by kind, so the composition root chooses the model backend by config and
// nothing downstream depends on a concrete implementation. The default is the Claude Code adapter.
// The runner is handed a session sandbox per turn, so it holds no sandbox itself.
//
// The "echo" kind runs `echo` in the sandbox instead of a model. It is how the smoke test drives a
// real turn end to end without a subscription.
func NewRunner(kind, workdir string) (Runner, error) {
	switch kind {
	case "", "claude-code":
		return &ClaudeCodeRunner{Bin: "claude", DefaultWorkdir: workdir}, nil
	case "echo":
		return EchoRunner{}, nil
	default:
		return nil, fmt.Errorf("model: unknown runner %q", kind)
	}
}
