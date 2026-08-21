package features_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/statusline"
	"github.com/cucumber/godog"
)

// statusLineWorld is the session as the model runtime would describe it, and the line drawn from it.
type statusLineWorld struct {
	said []byte
	line string
}

type statusLineKey struct{}

func statusLineFrom(ctx context.Context) *statusLineWorld {
	s, _ := ctx.Value(statusLineKey{}).(*statusLineWorld)
	return s
}

// What an operator sees under the prompt while they are in a conversation.
func initializeStatusLineSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, statusLineKey{}, &statusLineWorld{}), nil
	})

	// The payload carries the whole session, of which the line reads one object, so a scenario hands
	// over the whole thing rather than the two numbers it happens to need today.
	sc.Step(`^a conversation that has used (\d+) of a (\d+) token context window$`,
		func(ctx context.Context, used, size int64) error {
			said, err := json.Marshal(map[string]any{
				"session_id":      "0b4f2f7c-2f0e-4a1e-8a2a-9a6f1b3c5d7e",
				"transcript_path": "/home/agent/.claude/projects/-home-agent-workspace/0b4f2f7c.jsonl",
				"model":           map[string]any{"id": "claude-opus-5", "display_name": "Opus 5"},
				"context_window": map[string]any{
					"total_input_tokens":  used,
					"total_output_tokens": 476,
					"context_window_size": size,
				},
			})
			if err != nil {
				return err
			}
			statusLineFrom(ctx).said = said
			return nil
		})

	sc.Step(`^a model runtime that says nothing about the context window$`, func(ctx context.Context) error {
		statusLineFrom(ctx).said = []byte(`{"session_id":"0b4f2f7c","model":{"id":"claude-opus-5"}}`)
		return nil
	})

	sc.Step(`^the runtime draws its status line$`, func(ctx context.Context) error {
		s := statusLineFrom(ctx)
		s.line = statusline.Line(s.said)
		return nil
	})

	sc.Step(`^the line says "([^"]*)"$`, func(ctx context.Context, want string) error {
		if got := drawnLine(ctx); got != want {
			return fmt.Errorf("the line says %q, want %q", got, want)
		}
		return nil
	})

	sc.Step(`^the line warns that it is over the (\d+)% mark$`, func(ctx context.Context, mark int) error {
		line := drawnLine(ctx)
		if !strings.Contains(line, fmt.Sprintf("over the %d%% mark", mark)) {
			return fmt.Errorf("the line says %q, which does not warn", line)
		}
		return nil
	})

	sc.Step(`^the line does not warn$`, func(ctx context.Context) error {
		if line := drawnLine(ctx); strings.Contains(line, "mark") {
			return fmt.Errorf("the line warns, at a share the operator asked not to be warned about: %q", line)
		}
		return nil
	})

	sc.Step(`^the line says the runtime does not report it$`, func(ctx context.Context) error {
		if line := drawnLine(ctx); !strings.Contains(line, "does not say") {
			return fmt.Errorf("the line says %q, which does not say why there is no share on it", line)
		}
		return nil
	})

	sc.Step(`^the line claims no share$`, func(ctx context.Context) error {
		if line := drawnLine(ctx); strings.Contains(line, "%") {
			return fmt.Errorf("the line made a share up out of a payload that carries none: %q", line)
		}
		return nil
	})

	// Read from the file the image ships rather than from a constant: the settings are what join the
	// two halves, and a runtime pointed at a command that is not there draws nothing at all.
	sc.Step(`^the settings the sandbox image ships$`, func(ctx context.Context) error {
		contents, err := os.ReadFile(filepath.Join("..", "deploy", "sandbox", "claude-settings.json"))
		if err != nil {
			return fmt.Errorf("read the settings the sandbox image ships: %w", err)
		}
		statusLineFrom(ctx).said = contents
		return nil
	})

	sc.Step(`^they tell the runtime to draw its status line by running quay$`, func(ctx context.Context) error {
		var settings struct {
			StatusLine struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"statusLine"`
		}
		if err := json.Unmarshal(statusLineFrom(ctx).said, &settings); err != nil {
			return fmt.Errorf("the settings the image ships are not readable: %w", err)
		}
		if settings.StatusLine.Type != "command" {
			return fmt.Errorf("the image asks for a status line of type %q, and the runtime only runs a command",
				settings.StatusLine.Type)
		}
		if words := strings.Fields(settings.StatusLine.Command); len(words) < 2 || words[0] != "quay" {
			return fmt.Errorf("the image's status line runs %q, which is not this tool", settings.StatusLine.Command)
		}
		return nil
	})
}

// drawnLine is the line as a terminal shows it, with the colouring taken off, so a scenario reads
// what the operator reads rather than the escape sequences around it.
func drawnLine(ctx context.Context) string {
	line := statusLineFrom(ctx).line
	line = strings.ReplaceAll(line, "\033[1;33m", "")
	return strings.TrimSpace(strings.ReplaceAll(line, "\033[0m", ""))
}
