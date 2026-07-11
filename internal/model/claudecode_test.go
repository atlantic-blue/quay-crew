package model

import (
	"context"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

func TestEnvListIsSortedKeyValues(t *testing.T) {
	got := envList(map[string]string{"B": "2", "A": "1"})
	want := []string{"A=1", "B=2"}
	if len(got) != len(want) {
		t.Fatalf("envList = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("envList = %v, want %v", got, want)
		}
	}
	if envList(nil) != nil {
		t.Fatalf("envList(nil) = %v, want nil", envList(nil))
	}
}

func TestRunForwardsEnvToTheSandbox(t *testing.T) {
	box := &sandbox.FakeSandbox{Output: `{"type":"result","result":"ok","session_id":"s1"}`}
	runner := &ClaudeCodeRunner{Bin: "claude"}

	_, err := runner.Run(context.Background(), box, Request{
		Text: "hello",
		Env:  map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "tok-abc"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := false
	for _, e := range box.LastSpec.Env {
		if e == "CLAUDE_CODE_OAUTH_TOKEN=tok-abc" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sandbox env = %v, want it to carry CLAUDE_CODE_OAUTH_TOKEN", box.LastSpec.Env)
	}
}

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
