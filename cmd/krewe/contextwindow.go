package main

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/atlantic-blue/krewe/internal/sandbox"
)

// conversationDir is where the conversation directory is mounted inside a sandbox. A variable rather
// than the constant used in place, so a test can point it at a directory of its own: this writes to
// the operator's own home otherwise, on any machine the tests run on.
var conversationDir = sandbox.ConversationPath

// rememberWindowSize writes down how big the model's context window is, inside the conversation
// directory the system mounts into this sandbox.
//
// It is the only way the system can ever learn the size. The size is not in the transcript, and a list
// of models in the system's own code would be right today and quietly wrong at the next one. The
// runtime says it here, to the status line, and nowhere else.
//
// A failure is silent. This runs on every draw of a line under somebody's prompt, and a status line
// that reports a full disk in place of the conversation is worse than a listing without a share in it.
//
// Written only when the number changes, which is almost never: the file is read on every draw and the
// runtime draws several times a task.
func rememberWindowSize(dir string, size int64) {
	if size <= 0 || dir == "" {
		return
	}
	at := filepath.Join(dir, sandbox.ContextWindowFile)
	said := strconv.FormatInt(size, 10) + "\n"
	if held, err := os.ReadFile(at); err == nil && string(held) == said { //nolint:gosec // a constant path inside this sandbox
		return
	}
	_ = os.WriteFile(at, []byte(said), 0o644) //nolint:gosec // a size, not a secret
}
