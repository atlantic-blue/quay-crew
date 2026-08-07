package model

import (
	"context"
	"fmt"
	"io"
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

	resp, _, _, err := parseStream(strings.NewReader(stream))
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
	resp, _, _, err := parseStream(strings.NewReader(stream))
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

// failingProcess is a run that ended badly, with whatever it had to say about it.
type failingProcess struct {
	stdout string
	stderr string
	exit   error
}

func (p failingProcess) Stdout() io.Reader { return strings.NewReader(p.stdout) }
func (p failingProcess) Wait() error       { return p.exit }
func (p failingProcess) Stderr() string    { return p.stderr }

type failingSandbox struct{ proc failingProcess }

func (s failingSandbox) Exec(context.Context, sandbox.Spec) (sandbox.Process, error) {
	return s.proc, nil
}
func (failingSandbox) Close(context.Context) error { return nil }

// realRefusal is the result event a turn actually produced against a rejected subscription token,
// captured from a sandbox on 5 August 2026 and trimmed to the fields this reads. It is here rather
// than invented because the whole defect was that nobody had looked at what the model says.
const realRefusal = `{"type":"result","is_error":true,"api_error_status":401,` +
	`"result":"Failed to authenticate. API Error: 401 Invalid bearer token","session_id":"b47db557"}`

// TestAFailedTurnSaysWhy: every model failure read "run exited: exit status 1", which is the same
// sentence for an expired token, a network failure, a missing binary and the model refusing.
func TestAFailedTurnSaysWhy(t *testing.T) {
	for _, test := range []struct {
		name   string
		proc   failingProcess
		wants  []string
		avoids string
	}{
		{
			name:  "the model's own reason, which arrives on standard output",
			proc:  failingProcess{stdout: realRefusal, exit: fmt.Errorf("exit status 1")},
			wants: []string{"401 Invalid bearer token", "status 401"},
		},
		{
			name:  "the error stream, when the model never got far enough to say anything",
			proc:  failingProcess{stderr: "claude: command not found", exit: fmt.Errorf("exit status 127")},
			wants: []string{"claude: command not found", "exit status 127"},
		},
		{
			// Captured on 6 August 2026 by dispatching a turn at a sandbox with no model in its
			// image. The Docker command line puts this on standard output, not on the error stream,
			// so it arrived where a stream event was expected and was discarded as noise.
			name: "what the daemon said on standard output, when the model never ran at all",
			proc: failingProcess{
				stdout: `OCI runtime exec failed: exec failed: unable to start container process: ` +
					`exec: "claude": executable file not found in $PATH: unknown`,
				exit: fmt.Errorf("exit status 127"),
			},
			wants: []string{"executable file not found", "exit status 127"},
		},
		{
			name:  "and it admits when there is nothing to say, rather than implying there was",
			proc:  failingProcess{exit: fmt.Errorf("exit status 1")},
			wants: []string{"exit status 1", "said nothing about why"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := NewClaudeCodeRunner()
			_, err := runner.Run(context.Background(), failingSandbox{proc: test.proc}, Request{Text: "hello"})
			if err == nil {
				t.Fatal("the turn reported success")
			}
			for _, want := range test.wants {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("the failure is %q, want it to say %q", err, want)
				}
			}
		})
	}
}

// TestAFailedTurnNeverCarriesTheToken. The turn runs with the subscription token in its environment,
// so every place a failure can quote is a place the token turns up. An error is a thing people paste.
func TestAFailedTurnNeverCarriesTheToken(t *testing.T) {
	const token = "sk-ant-oat01-hVnQ2mXk9pLrT4wYzB7cD1fG5jH8sN0aE3iU6oP"
	for _, test := range []struct {
		name string
		proc failingProcess
	}{
		{"quoted by the model", failingProcess{
			stdout: fmt.Sprintf(`{"type":"result","is_error":true,"result":"bad token %s"}`, token),
			exit:   fmt.Errorf("exit status 1"),
		}},
		{"echoed on the error stream", failingProcess{
			stderr: "env: CLAUDE_CODE_OAUTH_TOKEN=" + token,
			exit:   fmt.Errorf("exit status 1"),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := NewClaudeCodeRunner()
			_, err := runner.Run(context.Background(), failingSandbox{proc: test.proc}, Request{
				Text: "hello",
				Env:  map[string]string{ClaudeCodeOAuthTokenEnv: token},
			})
			if err == nil {
				t.Fatal("the turn reported success")
			}
			if strings.Contains(err.Error(), token) {
				t.Fatalf("the failure carries the token: %q", err)
			}
			if !strings.Contains(err.Error(), "redacted") {
				t.Fatalf("the failure is %q, want it to say something was taken out", err)
			}
		})
	}
}

// TestRedactionLeavesTheExplanationReadable: a redaction that eats the sentence is no better than no
// explanation, so a short setting is never mistaken for a secret.
func TestRedactionLeavesTheExplanationReadable(t *testing.T) {
	env := map[string]string{
		ClaudeCodeOAuthTokenEnv: "sk-ant-oat01-hVnQ2mXk9pLrT4wYzB7cD1fG5jH8sN0aE3iU6oP",
		"QC_MODEL":              "claude-code",
		"HOME":                  "/home/agent",
	}
	got := redact("claude-code failed in /home/agent with sk-ant-oat01-hVnQ2mXk9pLrT4wYzB7cD1fG5jH8sN0aE3iU6oP", env)

	if !strings.Contains(got, "claude-code failed in /home/agent") {
		t.Fatalf("redaction ate the explanation: %q", got)
	}
	if strings.Contains(got, "oat01") {
		t.Fatalf("the token survived: %q", got)
	}
}

// TestATokenFromSomewhereElseIsStillRedacted: the model's own tooling can print a token this process
// never passed in, so the published shape is matched as well as the values we know about.
func TestATokenFromSomewhereElseIsStillRedacted(t *testing.T) {
	got := redact("config has sk-ant-oat01-neverPassedThroughHere1234 in it", nil)
	if strings.Contains(got, "neverPassedThroughHere") {
		t.Fatalf("a token this process never held survived: %q", got)
	}
}
