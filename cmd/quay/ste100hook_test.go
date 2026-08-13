package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// transcriptWith writes a minimal transcript file holding one assistant message with the given text,
// in the shape Claude Code's command line tool writes, and returns its path.
func transcriptWith(t *testing.T, text string) string {
	t.Helper()
	return transcriptWithLines(t, []string{assistantLine(text)})
}

func assistantLine(text string) string {
	line, _ := json.Marshal(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
		},
	})
	return string(line)
}

func toolUseLine() string {
	line, _ := json.Marshal(map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{"type": "tool_use", "id": "toolu_1", "name": "Bash", "input": map[string]any{"command": "ls"}},
			},
		},
	})
	return string(line)
}

func transcriptWithLines(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "conversation.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return path
}

func stopInput(t *testing.T, transcriptPath string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"session_id":       "abc123",
		"transcript_path":  transcriptPath,
		"hook_event_name":  "Stop",
		"stop_hook_active": false,
	})
	if err != nil {
		t.Fatalf("marshal stop input: %v", err)
	}
	return body
}

func TestSTE100HookPassesCleanMessage(t *testing.T) {
	path := transcriptWith(t, "The build is clean. The tests are green, all fifteen of them.")
	var out bytes.Buffer
	if err := runSTE100Hook(nil, bytes.NewReader(stopInput(t, path)), &out); err != nil {
		t.Fatalf("hook on clean text: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("clean text produced output: %q", out.String())
	}
}

func TestSTE100HookBlocksOnViolation(t *testing.T) {
	path := transcriptWith(t, "This is a robust and comprehensive solution — it will unlock huge value at scale.")
	var out bytes.Buffer
	if err := runSTE100Hook(nil, bytes.NewReader(stopInput(t, path)), &out); err != nil {
		t.Fatalf("hook on a violating message: %v", err)
	}

	var decision struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(out.Bytes(), &decision); err != nil {
		t.Fatalf("hook output is not JSON: %v\n%s", err, out.String())
	}
	if decision.Decision != "block" {
		t.Fatalf("decision = %q, want block", decision.Decision)
	}
	if !strings.Contains(decision.Reason, "banned word") {
		t.Fatalf("reason does not name the banned word violation: %q", decision.Reason)
	}
	if !strings.Contains(decision.Reason, "dash") {
		t.Fatalf("reason does not name the dash violation: %q", decision.Reason)
	}
}

func TestSTE100HookSkipsToAnEarlierMessageWithText(t *testing.T) {
	// The most recent assistant line carries only a tool call, which is not something to check, so
	// the hook has to look further back for the message that actually has words in it.
	path := transcriptWithLines(t, []string{
		assistantLine("This is a robust and comprehensive solution."),
		toolUseLine(),
	})
	var out bytes.Buffer
	if err := runSTE100Hook(nil, bytes.NewReader(stopInput(t, path)), &out); err != nil {
		t.Fatalf("hook: %v", err)
	}
	if !strings.Contains(out.String(), "block") {
		t.Fatalf("the earlier message with a real violation was not found: %q", out.String())
	}
}

// TestSTE100HookChecksTheMostRecentTextNotTheFirst is what actually proves this reads backward from
// the end of the transcript rather than forward from the start: two messages carry text, and only the
// later one violates anything. Reading in either direction finds *a* message when only one has text,
// which is why TestSTE100HookSkipsToAnEarlierMessageWithText alone cannot catch reading the wrong way.
func TestSTE100HookChecksTheMostRecentTextNotTheFirst(t *testing.T) {
	path := transcriptWithLines(t, []string{
		assistantLine("The build is clean, and the tests are green."),
		assistantLine("This is a robust and comprehensive solution — it will unlock value."),
	})
	var out bytes.Buffer
	if err := runSTE100Hook(nil, bytes.NewReader(stopInput(t, path)), &out); err != nil {
		t.Fatalf("hook: %v", err)
	}
	if !strings.Contains(out.String(), "block") {
		t.Fatalf("the later, violating message was not the one checked: %q", out.String())
	}
}

func TestSTE100HookIgnoresMalformedInput(t *testing.T) {
	var out bytes.Buffer
	if err := runSTE100Hook(nil, strings.NewReader("not json"), &out); err != nil {
		t.Fatalf("malformed input returned an error rather than passing: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("malformed input produced output: %q", out.String())
	}
}

func TestSTE100HookIgnoresAMissingTranscript(t *testing.T) {
	var out bytes.Buffer
	body := stopInput(t, "/does/not/exist.jsonl")
	if err := runSTE100Hook(nil, bytes.NewReader(body), &out); err != nil {
		t.Fatalf("a missing transcript returned an error rather than passing: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("a missing transcript produced output: %q", out.String())
	}
}

func TestSTE100HookRefusesArguments(t *testing.T) {
	var out bytes.Buffer
	if err := runSTE100Hook([]string{"anything"}, strings.NewReader("{}"), &out); err == nil {
		t.Fatal("hook-ste100 with an argument = nil error, want error")
	}
}

func TestSTE100HookCapsHowManyViolationsItNames(t *testing.T) {
	// Every sentence here is its own paragraph over the word cap and carries a banned word, so this
	// produces more violations than the cap, and the report has to say so rather than list them all.
	var messages []string
	for i := 0; i < 12; i++ {
		messages = append(messages, "This unlock is a powerful, comprehensive, seamless and robust "+
			"leverage of the whole landscape and realm, going well past the twenty five word cap on its own.")
	}
	path := transcriptWith(t, strings.Join(messages, "\n\n"))
	var out bytes.Buffer
	if err := runSTE100Hook(nil, bytes.NewReader(stopInput(t, path)), &out); err != nil {
		t.Fatalf("hook: %v", err)
	}
	if !strings.Contains(out.String(), "more") {
		t.Fatalf("a report with more than the cap did not say how many were left out: %q", out.String())
	}
}
