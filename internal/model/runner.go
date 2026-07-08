package model

import (
	"fmt"

	"github.com/atlantic-blue/quay-crew/internal/session"
)

// NewRunner builds a Runner by kind, so the composition root chooses the model backend by config and
// nothing downstream depends on a concrete implementation. The default is the Claude Code adapter,
// which runs inside the given session Runtime.
func NewRunner(kind string, runtime session.Runtime, workdir string) (Runner, error) {
	switch kind {
	case "", "claude-code":
		return &ClaudeCodeRunner{Bin: "claude", Runtime: runtime, DefaultWorkdir: workdir}, nil
	default:
		return nil, fmt.Errorf("model: unknown runner %q", kind)
	}
}
