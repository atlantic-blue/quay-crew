package model

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// ClaudeCodeOAuthTokenEnv is the environment variable the Claude Code CLI reads a long lived
// subscription token from (minted by `claude setup-token`). The control plane stores this per workspace
// as a secret and injects it into the sandbox at turn time.
const ClaudeCodeOAuthTokenEnv = "CLAUDE_CODE_OAUTH_TOKEN"

// ClaudeCodeRunner runs turns by driving the Claude Code CLI, under your subscription (no API cost).
// It runs the CLI inside the session's sandbox it is handed, so the run is isolated and the CLI's
// state persists across the session's turns. It streams JSON events, captures the session id so the
// thread can be resumed, and returns the result text.
type ClaudeCodeRunner struct {
	// Bin is the CLI binary; empty defaults to "claude".
	Bin string
	// DefaultWorkdir is used when a Request does not set Workdir.
	DefaultWorkdir string
}

// NewClaudeCodeRunner returns a runner that invokes "claude".
func NewClaudeCodeRunner() *ClaudeCodeRunner { return &ClaudeCodeRunner{Bin: "claude"} }

// compile time check.
var _ Runner = (*ClaudeCodeRunner)(nil)

// envList turns the request env into the "KEY=value" form the sandbox expects, sorted so the exec is
// deterministic.
func envList(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(env))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

func buildArgs(req Request) []string {
	mode := req.PermissionMode
	if mode == "" {
		mode = "plan"
	}
	args := []string{"-p", req.Text, "--output-format", "stream-json", "--verbose", "--permission-mode", mode}
	if req.ModelSessionID != "" {
		args = append(args, "--resume", req.ModelSessionID)
	}
	// Additional settings, so the operator's own file inside the sandbox still applies and the crew's
	// hooks are added to it rather than replacing it. Left off when there are none, because a path to
	// a file that is not there is a turn that fails before it starts.
	if req.Settings != "" {
		args = append(args, "--settings", req.Settings)
	}
	return args
}

// Run runs one turn inside the session's sandbox and parses its streamed output.
func (r *ClaudeCodeRunner) Run(ctx context.Context, box sandbox.Sandbox, req Request) (Response, error) {
	if box == nil {
		return Response{}, fmt.Errorf("model: no sandbox provided")
	}
	bin := r.Bin
	if bin == "" {
		bin = "claude"
	}
	workdir := req.Workdir
	if workdir == "" {
		workdir = r.DefaultWorkdir
	}

	spec := sandbox.Spec{Argv: append([]string{bin}, buildArgs(req)...), Workdir: workdir, Env: envList(req.Env)}
	proc, err := box.Exec(ctx, spec)
	if err != nil {
		return Response{}, fmt.Errorf("model: exec: %w", err)
	}

	resp, refused, unparsed, parseErr := parseStream(proc.Stdout())
	waitErr := proc.Wait()
	if waitErr != nil {
		return Response{}, fmt.Errorf("model: %s", why(refused, proc.Stderr(), unparsed, waitErr, req.Env))
	}
	if parseErr != nil {
		return Response{}, fmt.Errorf("model: parse stream: %w", parseErr)
	}
	return resp, nil
}

// why explains a turn that failed, in the order the explanations are worth anything.
//
// Every model failure used to read "run exited: exit status 1", which is the same sentence for an
// expired token, a network failure, a missing binary and the model refusing the request. The reason
// is nearly always somewhere: the model says so in its own stream, and what is left says so on the
// error stream. Only when both are silent is the exit status all there is.
//
// Everything here goes through redact first. This runs with the subscription token in its
// environment, and an error is a thing people paste.
func why(refused, stderr, unparsed string, exit error, env map[string]string) string {
	if refused != "" {
		return Redact(refused, env)
	}
	if said := firstOf(stderr, unparsed); said != "" {
		return fmt.Sprintf("run exited: %v, saying: %s", exit, Redact(said, env))
	}
	return fmt.Sprintf("run exited: %v, and it said nothing about why", exit)
}

func firstOf(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// streamEvent is the subset of the Claude Code stream JSON we read.
type streamEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Result    string `json:"result"`
	// IsError and APIErrorStatus are how the model says the turn did not do what was asked. They
	// arrive on the result event, on standard output, in the same stream as a reply: the reason a
	// turn failed is usually here rather than on the error stream, and it was being parsed past.
	IsError        bool `json:"is_error"`
	APIErrorStatus int  `json:"api_error_status"`
	Message        struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// parseStream reads streamed JSON events and extracts the session id, the final reply, and the
// model's own account of refusing the turn. It prefers the "result" event's text and falls back to
// the concatenated assistant text.
// It also keeps whatever arrived on that stream and was not a stream event at all. Something running
// in place of the model writes there too, and it is the only account of the failure there is: the
// Docker command line reports "executable file not found" on standard output rather than on the error
// stream, so a turn against an image with no model in it explained itself as an exit status until
// this was kept. Found by running it, not by reading it.
func parseStream(r io.Reader) (Response, string, string, error) {
	var resp Response
	var refused string
	var assistant, unparsed strings.Builder

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event streamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			// Not a stream event. Keep a bounded amount of it, because when the model never ran
			// this is the only thing that says why.
			if unparsed.Len() < unparsedKept {
				if unparsed.Len() > 0 {
					unparsed.WriteString("\n")
				}
				unparsed.WriteString(line)
			}
			continue
		}
		if event.SessionID != "" {
			resp.ModelSessionID = event.SessionID
		}
		switch event.Type {
		case "assistant":
			for _, block := range event.Message.Content {
				if block.Type == "text" && block.Text != "" {
					assistant.WriteString(block.Text)
				}
			}
		case "result":
			if event.Result != "" {
				resp.Reply = event.Result
			}
			if event.IsError {
				refused = event.Result
				if refused == "" {
					refused = "the model refused the turn and gave no reason"
				}
				if event.APIErrorStatus != 0 {
					refused = fmt.Sprintf("%s (status %d)", refused, event.APIErrorStatus)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Response{}, "", "", err
	}
	if resp.Reply == "" {
		resp.Reply = assistant.String()
	}
	return resp, refused, unparsed.String(), nil
}

// unparsedKept bounds what is remembered of output that was not a stream event, so a command that
// prints megabytes of something else cannot be held in memory whole.
const unparsedKept = 4 << 10
