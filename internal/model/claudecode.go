package model

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// ClaudeCodeRunner runs turns by driving the local Claude Code CLI as a subprocess, under your
// subscription (no API cost). It mirrors the print mode with resume pattern: it streams JSON events,
// captures the session id so the thread can be resumed, and returns the final result text.
type ClaudeCodeRunner struct {
	// Bin is the CLI binary; empty defaults to "claude".
	Bin string
	// DefaultWorkdir is used when a Request does not set Workdir.
	DefaultWorkdir string
}

// NewClaudeCodeRunner returns a runner that invokes the "claude" binary.
func NewClaudeCodeRunner() *ClaudeCodeRunner { return &ClaudeCodeRunner{Bin: "claude"} }

// compile time check.
var _ Runner = (*ClaudeCodeRunner)(nil)

func buildArgs(req Request) []string {
	mode := req.PermissionMode
	if mode == "" {
		mode = "plan"
	}
	args := []string{"-p", req.Text, "--output-format", "stream-json", "--verbose", "--permission-mode", mode}
	if req.ModelSessionID != "" {
		args = append(args, "--resume", req.ModelSessionID)
	}
	return args
}

// Run invokes the CLI for one turn and parses its streamed output.
func (r *ClaudeCodeRunner) Run(ctx context.Context, req Request) (Response, error) {
	bin := r.Bin
	if bin == "" {
		bin = "claude"
	}
	cmd := exec.CommandContext(ctx, bin, buildArgs(req)...)
	if req.Workdir != "" {
		cmd.Dir = req.Workdir
	} else if r.DefaultWorkdir != "" {
		cmd.Dir = r.DefaultWorkdir
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Response{}, fmt.Errorf("model: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return Response{}, fmt.Errorf("model: start %s: %w", bin, err)
	}

	resp, parseErr := parseStream(stdout)
	waitErr := cmd.Wait()
	if waitErr != nil {
		return Response{}, fmt.Errorf("model: %s exited: %w", bin, waitErr)
	}
	if parseErr != nil {
		return Response{}, fmt.Errorf("model: parse stream: %w", parseErr)
	}
	return resp, nil
}

// streamEvent is the subset of the Claude Code stream JSON we read.
type streamEvent struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Result    string `json:"result"`
	Message   struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// parseStream reads streamed JSON events and extracts the session id and the final reply. It prefers
// the "result" event's text and falls back to the concatenated assistant text.
func parseStream(r io.Reader) (Response, error) {
	var resp Response
	var assistant strings.Builder

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event streamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue // ignore non JSON lines
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
		}
	}
	if err := scanner.Err(); err != nil {
		return Response{}, err
	}
	if resp.Reply == "" {
		resp.Reply = assistant.String()
	}
	return resp, nil
}
