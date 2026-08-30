package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/hook"
)

// shipped reads the command the system asks the runtime to run for its status line, out of the settings
// the system renders for every session.
func shipped(t *testing.T) string {
	t.Helper()
	rendered, err := hook.Settings("/home/agent/hooks", nil)
	if err != nil {
		t.Fatalf("render the settings the system mounts: %v", err)
	}
	var settings struct {
		StatusLine struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if err := json.Unmarshal(rendered, &settings); err != nil {
		t.Fatalf("the settings the system renders are not readable as JSON: %v", err)
	}
	if settings.StatusLine.Type != "command" {
		t.Fatalf("the system asks for a status line of type %q, and the runtime only runs %q",
			settings.StatusLine.Type, "command")
	}
	if settings.StatusLine.Command == "" {
		t.Fatal("the system asks for no status line, so an attached operator sees nothing")
	}
	return settings.StatusLine.Command
}

// The words the system renders have to be words this binary answers to. Both halves passed their own
// tests while disagreeing: a settings file naming a subcommand that does not exist leaves the runtime
// running something that fails on every draw, and the operator sees a blank line rather than an
// error, which reads as the system having no answer.
func TestTheStatusLineTheSystemConfiguresIsOneThisBinaryDraws(t *testing.T) {
	words := strings.Fields(shipped(t))
	if len(words) < 2 || words[0] != "quay" {
		t.Fatalf("the system's status line runs %q, which is not this tool", strings.Join(words, " "))
	}

	printed := drawn(t, words[1:], payload(296_000, 1_000_000))
	if !strings.Contains(printed, "context 30% used (296k of 1M)") {
		t.Errorf("running the command the system configures printed %q, which does not say the context", printed)
	}
	if !strings.Contains(printed, "over the 30% mark") {
		t.Errorf("running the command the system configures printed %q, which does not warn", printed)
	}
}

// The runtime hands the session over on standard input, so the line is drawn from what is piped in
// rather than from anything this process knows.
func TestTheLineIsDrawnFromWhatTheRuntimePipesIn(t *testing.T) {
	printed := drawn(t, []string{"statusline"}, payload(124_000, 1_000_000))
	if strings.TrimSpace(printed) != "context 12% used (124k of 1M)" {
		t.Errorf("the session printed %q", printed)
	}
	if !strings.HasSuffix(printed, "\n") {
		t.Errorf("the line is not ended, so the runtime reads it joined to whatever comes next: %q", printed)
	}
}

// A status line takes an argument from nobody: the runtime runs it with none. Saying so beats
// drawing a line that ignores what was typed, because the person who typed it is debugging.
func TestTheStatusLineTakesNoArguments(t *testing.T) {
	var out bytes.Buffer
	err := runStatusLine([]string{"anything"}, strings.NewReader(""), &out)
	if err == nil {
		t.Fatalf("quay statusline anything was answered rather than refused: %q", out.String())
	}
	if !strings.Contains(err.Error(), "standard input") {
		t.Errorf("the refusal does not say how this is driven: %v", err)
	}
}

// drawn runs one invocation through the tool's own dispatch, with the payload on standard input the
// way the runtime hands it over.
func drawn(t *testing.T, args []string, session []byte) string {
	t.Helper()
	// Somewhere of its own to write the window size down, because the real path is the operator's own
	// home on any machine that is not a sandbox.
	heldDir := conversationDir
	conversationDir = t.TempDir()
	defer func() { conversationDir = heldDir }()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("make a pipe: %v", err)
	}
	go func() {
		defer func() { _ = write.Close() }()
		_, _ = write.Write(session)
	}()

	was := os.Stdin
	os.Stdin = read
	defer func() {
		os.Stdin = was
		_ = read.Close()
	}()

	var out bytes.Buffer
	if err := run(context.Background(), nil, args, &out, ""); err != nil {
		t.Fatalf("quay %s was refused: %v", strings.Join(args, " "), err)
	}
	return out.String()
}

// payload is the session as the model runtime describes it, cut down to what this reads.
func payload(used, size int64) []byte {
	document := map[string]any{
		"session_id":      "0b4f2f7c-2f0e-4a1e-8a2a-9a6f1b3c5d7e",
		"transcript_path": "/home/agent/.claude/projects/-home-agent-workspace/0b4f2f7c.jsonl",
		"model":           map[string]any{"id": "claude-opus-5", "display_name": "Opus 5"},
		"context_window": map[string]any{
			"total_input_tokens":  used,
			"total_output_tokens": 476,
			"context_window_size": size,
			"used_percentage":     used * 100 / size,
		},
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		panic(err)
	}
	return encoded
}
