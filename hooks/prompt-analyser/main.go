// Command hook is the UserPromptSubmit hook that reads a message and hands the session a short
// analysis beside it.
//
// Claude Code never lets a hook replace what was typed, so this does not rewrite the message. It
// gathers the facts around it, asks a small model for a short restatement, and prints the message
// and that restatement together as context.
//
// It always exits 0. Every failure inside is already handled as silence, and an exit code the
// runtime reads as a refusal would block the message rather than let it through.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	// The child model call must never analyse its own prompt.
	if os.Getenv(Guard) == "1" {
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}

	fs := OS{}
	config := Default(home)
	// A config file that cannot be read leaves the defaults in place, which is a working hook rather
	// than no hook.
	if body, err := fs.Read(configPath()); err == nil {
		config = LoadConfig(body, home)
	}

	Run(Options{
		Config: config,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		FS:     fs,
		Now:    time.Now,
		Facts:  GatherFacts(home, fs),
		Ask:    AskModel(config, home, trace),
	})
}

// configPath is hook.config.json beside the hook, found from the running binary rather than from the
// working directory, because the runtime runs this from wherever the session happens to be.
func configPath() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(self), "..", "hook.config.json")
}

// trace reports what was sent, what came back and how long it took, with
// CLAUDE_PROMPT_ANALYSER_DEBUG=1 set. Standard error, because standard output is the hook's answer.
func trace(label, detail string) {
	if os.Getenv("CLAUDE_PROMPT_ANALYSER_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "[prompt-analyser] %s: %s\n", label, detail)
	}
}
