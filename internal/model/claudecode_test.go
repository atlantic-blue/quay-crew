package model

import (
	"context"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

func TestBuildArgs(t *testing.T) {
	got := buildArgs(Request{Text: "do a thing"})
	want := "-p do a thing --output-format stream-json --verbose --permission-mode plan"
	if strings.Join(got, " ") != want {
		t.Fatalf("buildArgs = %q, want %q", strings.Join(got, " "), want)
	}
}

func TestBuildArgsResumeAndMode(t *testing.T) {
	got := strings.Join(buildArgs(Request{Text: "go on", ModelSessionID: "sess-1", PermissionMode: "acceptEdits"}), " ")
	if !strings.Contains(got, "--permission-mode acceptEdits") {
		t.Fatalf("missing permission mode: %q", got)
	}
	if !strings.Contains(got, "--resume sess-1") {
		t.Fatalf("missing resume: %q", got)
	}
}

func TestParseStream(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","session_id":"abc-123"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}`,
		`not json, ignored`,
		`{"type":"result","result":"all done","session_id":"abc-123"}`,
	}, "\n")

	resp, err := parseStream(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parseStream: %v", err)
	}
	if resp.ModelSessionID != "abc-123" {
		t.Fatalf("session id = %q, want abc-123", resp.ModelSessionID)
	}
	if resp.Reply != "all done" {
		t.Fatalf("reply = %q, want 'all done'", resp.Reply)
	}
}

func TestParseStreamFallsBackToAssistantText(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","session_id":"s1"}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"part one "}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"part two"}]}}`,
	}, "\n")
	resp, err := parseStream(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parseStream: %v", err)
	}
	if resp.Reply != "part one part two" {
		t.Fatalf("reply = %q, want 'part one part two'", resp.Reply)
	}
}

func TestRunInSandbox(t *testing.T) {
	stream := `{"type":"system","session_id":"s-1"}` + "\n" + `{"type":"result","result":"ok","session_id":"s-1"}`
	box := &sandbox.FakeSandbox{Output: stream}
	runner := NewClaudeCodeRunner()

	resp, err := runner.Run(context.Background(), box, Request{Text: "hi", PermissionMode: "acceptEdits"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Reply != "ok" || resp.ModelSessionID != "s-1" {
		t.Fatalf("bad response: %+v", resp)
	}
	if len(box.LastSpec.Argv) == 0 || box.LastSpec.Argv[0] != "claude" {
		t.Fatalf("sandbox did not receive the claude command: %+v", box.LastSpec.Argv)
	}
	if !strings.Contains(strings.Join(box.LastSpec.Argv, " "), "--permission-mode acceptEdits") {
		t.Fatalf("permission mode not passed through: %+v", box.LastSpec.Argv)
	}
}

func TestRunRequiresSandbox(t *testing.T) {
	runner := NewClaudeCodeRunner()
	if _, err := runner.Run(context.Background(), nil, Request{Text: "hi"}); err == nil {
		t.Fatal("Run with nil sandbox = nil error, want error")
	}
}
