package model

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"

	"github.com/atlantic-blue/krewe/internal/sandbox"
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
	got := buildArgs(Request{Text: "do a thing"}, "")
	want := "-p do a thing --output-format stream-json --verbose --permission-mode plan"
	if strings.Join(got, " ") != want {
		t.Fatalf("buildArgs = %q, want %q", strings.Join(got, " "), want)
	}
}

func TestBuildArgsResumeAndMode(t *testing.T) {
	got := strings.Join(buildArgs(Request{
		Text: "go on", ModelSessionID: "sess-1", ConversationStarted: true, PermissionMode: "acceptEdits",
	}, ""), " ")
	if !strings.Contains(got, "--permission-mode acceptEdits") {
		t.Fatalf("missing permission mode: %q", got)
	}
	if !strings.Contains(got, "--resume sess-1") {
		t.Fatalf("missing resume: %q", got)
	}
}

// The pair. A conversation the runtime has never seen is started under the name the system gave it, and
// one it has seen is resumed by that name. Both, because getting either wrong fails the task: resuming
// a name with nothing behind it prints "No conversation found" and exits, and starting a name that is
// already there is refused as one in use.
//
// The first of the two is the defect this pair exists for. A first task carried no name at all, so the
// runtime named its own conversation and told nobody until the task was over, and anybody opening the
// session while it worked opened an empty conversation beside the job.
func TestBuildArgsStartsAConversationTheRuntimeHasNotSeenAndResumesOneItHas(t *testing.T) {
	for _, tc := range []struct {
		name    string
		started bool
		want    string
		absent  string
		because string
	}{
		{
			name: "the system has named it and nothing has opened it", started: false,
			want: "--session-id sess-1", absent: "--resume",
			because: "the task is what makes the name true, and there is no transcript to resume",
		},
		{
			name: "the runtime has opened it already", started: true,
			want: "--resume sess-1", absent: "--session-id",
			because: "it is the same conversation and the task continues it",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(buildArgs(Request{
				Text: "go on", ModelSessionID: "sess-1", ConversationStarted: tc.started,
			}, ""), " ")
			if !strings.Contains(got, tc.want) {
				t.Errorf("the task is %q, want it to carry %q, because %s", got, tc.want, tc.because)
			}
			if strings.Contains(got, tc.absent) {
				t.Errorf("the task is %q, and it must not carry %q, because %s", got, tc.absent, tc.because)
			}
		})
	}
}

// A task with no conversation at all names none, which leaves the runtime to name its own. It is what
// a system that could not name one falls back to, and it must stay a fallback: nothing that runs a task
// through the control plane arrives here, because the system names the conversation first.
func TestBuildArgsNamesNoConversationWhenItHasNone(t *testing.T) {
	got := strings.Join(buildArgs(Request{Text: "go on"}, ""), " ")
	for _, absent := range []string{"--session-id", "--resume"} {
		if strings.Contains(got, absent) {
			t.Fatalf("the task is %q, and it names a conversation nobody chose with %q", got, absent)
		}
	}
}

// What the runtime says it used, checked against what the system asked for. The identifier in the output
// stream was where the name came from, which is why it arrived too late to be any use; it is a check
// now, and a runtime that ignored the flag is worth a sentence naming both, because the session's
// history is under the name the system did not choose.
func TestConversationCheckSpeaksUpOnlyWhenTheRuntimeIgnoredTheName(t *testing.T) {
	for _, tc := range []struct {
		name, asked, reported string
		says                  bool
	}{
		{name: "the runtime used the name it was given", asked: "sess-1", reported: "sess-1"},
		{name: "the runtime said nothing at all", asked: "sess-1", reported: ""},
		{name: "the system named nothing, so there is nothing to check", asked: "", reported: "sess-9"},
		{name: "the runtime used a name of its own", asked: "sess-1", reported: "sess-9", says: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			said := ConversationCheck(tc.asked, tc.reported)
			if tc.says && said == "" {
				t.Fatal("the runtime ignored the name and nothing was said about it")
			}
			if !tc.says && said != "" {
				t.Fatalf("nothing is wrong and it said %q", said)
			}
			if !tc.says {
				return
			}
			for _, want := range []string{tc.asked, tc.reported} {
				if !strings.Contains(said, want) {
					t.Errorf("it said %q, want it to name %q so the transcript can be found", said, want)
				}
			}
		})
	}
}

// A task says which model it wants, and a task that has not been told says nothing rather than
// guessing. The command line tool picks Sonnet when nobody says, which is how every session came to
// run Sonnet while the system was configured for Claude Code and nothing anywhere was wrong.
func TestBuildArgsNamesTheModel(t *testing.T) {
	got := strings.Join(buildArgs(Request{Text: "do a thing"}, "claude-opus-5"), " ")
	if !strings.Contains(got, "--model claude-opus-5") {
		t.Fatalf("the task does not name the model: %q", got)
	}
}

func TestBuildArgsLeavesTheModelToTheToolWhenUnset(t *testing.T) {
	got := strings.Join(buildArgs(Request{Text: "do a thing"}, ""), " ")
	if strings.Contains(got, "--model") {
		t.Fatalf("the task names a model nobody chose: %q", got)
	}
}

// The whole way from configuration to what the container is asked to run. The two halves each pass
// their own test while the wire between them is cut: a runner built from a kind and a model name is
// where the name is either carried or dropped.
func TestARunnerBuiltFromConfigurationCarriesTheModelIntoTheSandbox(t *testing.T) {
	runner, err := NewRunner(KindClaudeCode, "", "claude-opus-5")
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	box := &sandbox.FakeSandbox{Output: `{"type":"result","result":"ok","session_id":"s-1"}`}
	if _, err := runner.Run(context.Background(), box, Request{Text: "hi"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(strings.Join(box.LastSpec.Argv, " "), "--model claude-opus-5") {
		t.Fatalf("the sandbox was not told which model to run: %v", box.LastSpec.Argv)
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

// The stream has carried these numbers the whole time and the system read past them, so a turn's cost
// was thrown away at the point it was known.
func TestParseStreamKeepsWhatTheTurnSpent(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","session_id":"abc-123"}`,
		`{"type":"result","result":"all done","session_id":"abc-123","total_cost_usd":0.0241,` +
			`"usage":{"input_tokens":1200,"output_tokens":340,"cache_read_input_tokens":9000,` +
			`"cache_creation_input_tokens":500}}`,
	}, "\n")

	resp, _, _, err := parseStream(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parseStream: %v", err)
	}
	if !resp.UsageReported {
		t.Fatal("usage was not reported, so the turn reads as having spent nothing")
	}
	if resp.Usage.Input != 1200 || resp.Usage.Output != 340 {
		t.Errorf("input and output are %d and %d, want 1200 and 340",
			resp.Usage.Input, resp.Usage.Output)
	}
	if resp.Usage.CacheRead != 9000 || resp.Usage.CacheWritten != 500 {
		t.Errorf("cache read and cache creation are %d and %d, want 9000 and 500",
			resp.Usage.CacheRead, resp.Usage.CacheWritten)
	}
	if resp.CostUSD != 0.0241 {
		t.Errorf("cost is %v, want 0.0241", resp.CostUSD)
	}
}

// A stream that says nothing about usage must leave Reported false, so a caller can tell "spent
// nothing" from "was never told".
func TestParseStreamSaysWhenTheTurnNeverReportedWhatItSpent(t *testing.T) {
	stream := `{"type":"result","result":"all done","session_id":"abc-123"}`

	resp, _, _, err := parseStream(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parseStream: %v", err)
	}
	if resp.UsageReported {
		t.Error("usage reads as reported on a stream that carried none, so an unknown becomes a zero")
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

// realRefusal is the result event a task actually produced against a rejected subscription token,
// captured from a sandbox on 5 August 2026 and trimmed to the fields this reads. It is here rather
// than invented because the whole defect was that nobody had looked at what the model says.
const realRefusal = `{"type":"result","is_error":true,"api_error_status":401,` +
	`"result":"Failed to authenticate. API Error: 401 Invalid bearer token","session_id":"b47db557"}`

// TestAFailedTaskSaysWhy: every model failure read "run exited: exit status 1", which is the same
// sentence for an expired token, a network failure, a missing binary and the model refusing.
func TestAFailedTaskSaysWhy(t *testing.T) {
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
			// Captured on 6 August 2026 by dispatching a task at a sandbox with no model in its
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
				t.Fatal("the task reported success")
			}
			for _, want := range test.wants {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("the failure is %q, want it to say %q", err, want)
				}
			}
		})
	}
}

// TestAFailedTaskNeverCarriesTheToken. The task runs with the subscription token in its environment,
// so every place a failure can quote is a place the token turns up. An error is a thing people paste.
func TestAFailedTaskNeverCarriesTheToken(t *testing.T) {
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
				t.Fatal("the task reported success")
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
	got := Redact("claude-code failed in /home/agent with sk-ant-oat01-hVnQ2mXk9pLrT4wYzB7cD1fG5jH8sN0aE3iU6oP", env)

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
	got := Redact("config has sk-ant-oat01-neverPassedThroughHere1234 in it", nil)
	if strings.Contains(got, "neverPassedThroughHere") {
		t.Fatalf("a token this process never held survived: %q", got)
	}
}

// exited is an error carrying an exit status, the way *exec.ExitError does. Both sandbox backends
// return that type; this stands in for it so the test does not have to start a process to make one.
type exited struct{ status int }

func (e exited) Error() string { return fmt.Sprintf("exit status %d", e.status) }
func (e exited) ExitCode() int { return e.status }

// TestATaskKilledForMemorySaysSoRatherThanShowingAnExitStatus.
//
// Nothing killed with signal 9 gets to say why: no last line on either stream, and the kernel log is
// not readable from inside a container. So the operator read "run exited: exit status 137, and it
// said nothing about why" and could not tell a session that ran out of memory from a session whose
// container was taken away by an upgrade. Both are named now, and the command that answers which one
// it was is named with them.
func TestATaskKilledForMemorySaysSoRatherThanShowingAnExitStatus(t *testing.T) {
	runner := NewClaudeCodeRunner()
	_, err := runner.Run(context.Background(),
		failingSandbox{proc: failingProcess{exit: exited{status: 137}}}, Request{Text: "hello"})
	if err == nil {
		t.Fatal("a killed task reported success")
	}
	for _, want := range []string{"a kill rather than a failure", "krewe room", "container went away"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("a task killed for memory says %q, want it to say %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "said nothing about why") {
		t.Fatalf("a killed task is still reported as saying nothing: %q", err)
	}
}

// TestARealKilledProcessIsRecognised, because the interface above is only worth anything if the type
// the sandbox actually returns satisfies it. This runs a process and has it killed with signal 9.
func TestARealKilledProcessIsRecognised(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("no shell to kill: %v", err)
	}
	waitErr := exec.Command(shell, "-c", "kill -9 $$").Run()
	if waitErr == nil {
		t.Fatal("a process killed with signal 9 exited well")
	}
	if !wasKilled(waitErr) {
		t.Fatalf("a real killed process is not recognised as killed: %v", waitErr)
	}
	if wasKilled(exited{status: 1}) {
		t.Fatal("an ordinary failure is reported as a kill")
	}
}
