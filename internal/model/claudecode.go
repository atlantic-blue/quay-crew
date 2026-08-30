package model

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// ClaudeCodeOAuthTokenEnv is the environment variable the Claude Code CLI reads a long lived
// subscription token from (minted by `claude setup-token`). The control plane stores this per workspace
// as a secret and injects it into the sandbox at task time.
const ClaudeCodeOAuthTokenEnv = "CLAUDE_CODE_OAUTH_TOKEN"

// ClaudeCodeRunner runs tasks by driving the Claude Code CLI, under your subscription (no API cost).
// It runs the CLI inside the session's sandbox it is handed, so the run is isolated and the CLI's
// state persists across the session's tasks. It streams JSON events, captures the session id so the
// session can be resumed, and returns the result text.
type ClaudeCodeRunner struct {
	// Bin is the CLI binary; empty defaults to "claude".
	Bin string
	// DefaultWorkdir is used when a Request does not set Workdir.
	DefaultWorkdir string
	// Model is which model a task runs against, as an alias for the newest of a tier ("opus") or a
	// full name ("claude-opus-5"). Empty passes no --model, which leaves the choice to the CLI, and
	// the CLI chooses Sonnet.
	Model string
}

// NewClaudeCodeRunner returns a runner that invokes "claude".
func NewClaudeCodeRunner() *ClaudeCodeRunner { return &ClaudeCodeRunner{Bin: "claude"} }

// compile time check.
var _ Runner = (*ClaudeCodeRunner)(nil)

// envList tasks the request env into the "KEY=value" form the sandbox expects, sorted so the exec is
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

func buildArgs(req Request, model string) []string {
	mode := req.PermissionMode
	if mode == "" {
		mode = "plan"
	}
	args := []string{"-p", req.Text, "--output-format", "stream-json", "--verbose", "--permission-mode", mode}
	// Which model, when the system has been told. Left off when it has not, so a system can always fall
	// back to whatever the CLI picks for itself: a name the CLI stops accepting would otherwise fail
	// every task with no way to configure around it.
	if model != "" {
		args = append(args, "--model", model)
	}
	// Which conversation, and which of the two ways of naming one. A first task used to carry neither,
	// so the runtime named the conversation itself and told nobody until the task was over, and a
	// session opened while it worked opened an empty conversation beside the one doing the work.
	if req.ModelSessionID != "" {
		args = append(args, conversationFlag(req.ConversationStarted), req.ModelSessionID)
	}
	// Additional settings, so the operator's own file inside the sandbox still applies and the system's
	// hooks are added to it rather than replacing it. Left off when there are none, because a path to
	// a file that is not there is a task that fails before it starts.
	if req.Settings != "" {
		args = append(args, "--settings", req.Settings)
	}
	return args
}

// Run runs one task inside the session's sandbox and parses its streamed output.
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

	spec := sandbox.Spec{Argv: append([]string{bin}, buildArgs(req, r.Model)...), Workdir: workdir, Env: envList(req.Env)}
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

// why explains a task that failed, in the order the explanations are worth anything.
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
	if wasKilled(exit) {
		return killed
	}
	return fmt.Sprintf("run exited: %v, and it said nothing about why", exit)
}

// killedStatus is what a shell, and the docker command line, report for a process taken by signal 9.
// It is 128 plus the signal.
const killedStatus = 137

// killed is what the operator is told instead of the exit status on its own.
//
// Nothing is killed with signal 9 by accident, and nothing that is killed gets to say why: no last
// line on either stream, and the kernel log is not readable from inside a container. So "exit status
// 137" with nothing beside it reads as a hang, and the two things that actually cause it are worth
// naming, because what to do differs. Measured: an allocator taking memory in a sandbox with no
// limit was killed at exit 137 with both streams empty.
const killed = "run exited: exit status 137, which is a kill rather than a failure, and nothing " +
	"killed this way gets to say so. Two things do it. The kernel takes a process for memory, which " +
	"quay room reports from inside the sandbox: it says what memory is there and what has already " +
	"been killed in it. Or the container went away underneath the task, which an upgrade or a stop " +
	"does."

// exitStatus is anything that can say what status a command exited with. *exec.ExitError is one,
// and it is what both sandbox backends return today. The interface is what is matched so a backend
// that reports a status some other way is understood as well.
type exitStatus interface{ ExitCode() int }

// wasKilled says whether a command was taken by signal 9 rather than exiting on its own.
//
// Read from the status rather than from the process, because the process the system waits on is the
// docker command line and the one that died is inside the container. Docker reports the container
// process status as its own, so the number is the same either way.
func wasKilled(exit error) bool {
	var status exitStatus
	if errors.As(exit, &status) && status.ExitCode() == killedStatus {
		return true
	}
	// A process the system started itself, rather than through the docker command line, carries no
	// exit status at all. Go reports the signal instead, and the status reads -1. Measured rather
	// than assumed: a shell told to kill itself with signal 9 arrives here as "signal: killed".
	return exit != nil && strings.Contains(exit.Error(), signalKilled)
}

// signalKilled is how Go states a process taken by signal 9, which is what os.ProcessState prints
// when there is no exit status to print.
const signalKilled = "signal: killed"

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
	// IsError and APIErrorStatus are how the model says the task did not do what was asked. They
	// arrive on the result event, on standard output, in the same stream as a reply: the reason a
	// task failed is usually here rather than on the error stream, and it was being parsed past.
	IsError        bool `json:"is_error"`
	APIErrorStatus int  `json:"api_error_status"`
	// TotalCostUSD and Usage arrive on the result event and were being read past. The stream has
	// carried them the whole time, so the system was throwing away the only number that says what a
	// task cost.
	TotalCostUSD *float64 `json:"total_cost_usd"`
	Usage        *struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// parseStream reads streamed JSON events and extracts the session id, the final reply, and the
// model's own account of refusing the task. It prefers the "result" event's text and falls back to
// the concatenated assistant text.
// It also keeps whatever arrived on that stream and was not a stream event at all. Something running
// in place of the model writes there too, and it is the only account of the failure there is: the
// Docker command line reports "executable file not found" on standard output rather than on the error
// stream, so a task against an image with no model in it explained itself as an exit status until
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
			if event.Usage != nil {
				resp.Usage = sandbox.Usage{
					Input:        event.Usage.InputTokens,
					Output:       event.Usage.OutputTokens,
					CacheRead:    event.Usage.CacheReadInputTokens,
					CacheWritten: event.Usage.CacheCreationInputTokens,
				}
				resp.UsageReported = true
			}
			if event.TotalCostUSD != nil {
				resp.CostUSD = *event.TotalCostUSD
				resp.UsageReported = true
			}
			if event.IsError {
				refused = event.Result
				if refused == "" {
					refused = "the model refused the task and gave no reason"
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

// conversationFlag is how a conversation is named on the command line: started under a name the
// runtime has not seen, resumed when it has. The sandbox script that opens a conversation for an
// operator chooses between the same two, on the same question, so a conversation reached by typing
// and a conversation reached by dispatching a task are one conversation.
func conversationFlag(started bool) string {
	if started {
		return "--resume"
	}
	return "--session-id"
}

// ConversationCheck compares the conversation the system named with the one the runtime reported in its
// output stream, and returns what to say when they differ. Empty means nothing to say.
//
// The identifier in the stream used to be where the name came from, which is why it arrived too late
// to be any use. It is a check now: the system hands the name down before the task starts, so a stream
// carrying a different one means the runtime ignored the flag, and everything the system reports about
// that session afterwards, its history and what it cost, is being read from a transcript nobody wrote.
// Both names are in the sentence, because the job is under the second one.
func ConversationCheck(asked, reported string) string {
	if asked == "" || reported == "" || asked == reported {
		return ""
	}
	return fmt.Sprintf("the system asked the model runtime for conversation %s and it used %s instead, "+
		"so the flag was ignored and this session's history is under %s", asked, reported, reported)
}
