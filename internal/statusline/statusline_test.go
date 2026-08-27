package statusline_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/statusline"
)

// payload is the shape the model runtime hands its status line command: the whole session, of which
// this build reads one object. It is written out here rather than reduced to the two numbers so the
// test proves those numbers are still found in a payload carrying everything else.
func payload(used, size int64) []byte {
	document := map[string]any{
		"session_id":      "0b4f2f7c-2f0e-4a1e-8a2a-9a6f1b3c5d7e",
		"transcript_path": "/home/agent/.claude/projects/-home-agent-workspace/0b4f2f7c.jsonl",
		"cwd":             "/home/agent/workspace",
		"model":           map[string]any{"id": "claude-opus-5", "display_name": "Opus 5"},
		"workspace":       map[string]any{"current_dir": "/home/agent/workspace"},
		"version":         "2.1.233",
		"cost":            map[string]any{"total_cost_usd": 0.42},
		"context_window": map[string]any{
			"total_input_tokens":   used,
			"total_output_tokens":  476,
			"context_window_size":  size,
			"used_percentage":      used * 100 / size,
			"remaining_percentage": 100 - used*100/size,
		},
		"exceeds_200k_tokens": used > 200_000,
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		panic(err)
	}
	return encoded
}

const million = 1_000_000

// warned is the line as the runtime is handed it once the share is at or over the mark: the same
// words, in bold yellow.
func warned(line string) string {
	return "\033[1;33m" + line + "\033[0m"
}

func TestTheLineSaysHowMuchOfTheContextWindowIsUsed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		used    int64
		size    int64
		want    string
		because string
	}{
		{
			name: "a conversation nobody has spoken in", used: 0, size: million,
			want:    "context 0% used (0 of 1M)",
			because: "a session that has just opened is the one where the number is least interesting and most reassuring, and a blank count reads as a defect",
		},
		{
			name: "a conversation with some history behind it", used: 124_000, size: million,
			want:    "context 12% used (124k of 1M)",
			because: "the share is what decides anything, and the count is what makes the share checkable",
		},
		{
			name: "a smaller window", used: 40_000, size: 200_000,
			want:    "context 20% used (40k of 200k)",
			because: "the window is whatever the runtime says it is, so the same count is a different share on a different model",
		},
		{
			name: "a window reported as more than full", used: 1_040_000, size: million,
			want:    warned("context 100% used (1M of 1M), over the 30% mark"),
			because: "a hundred and four per cent reads as a defect in the crew, and this is a conversation about to be compacted rather than a broken one",
		},
		{
			name: "a count that makes no sense at all", used: -12, size: million,
			want:    "context 0% used (0 of 1M)",
			because: "nothing the runtime can say should come out as a negative share",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := statusline.Line(payload(tc.used, tc.size))
			if got != tc.want {
				t.Errorf("the session says %q, want %q\n\n%s", got, tc.want, tc.because)
			}
		})
	}
}

// The threshold is the whole point of the line: a number nobody is told to act on is a number nobody
// reads.
func TestTheLineWarnsFromThirtyPerCent(t *testing.T) {
	for _, tc := range []struct {
		name string
		used int64
		warn bool
	}{
		{name: "just under the mark", used: 294_000, warn: false},
		{name: "rounding onto the mark", used: 296_000, warn: true},
		{name: "on the mark", used: 300_000, warn: true},
		{name: "well over it", used: 700_000, warn: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := statusline.Line(payload(tc.used, million))
			warned := strings.Contains(line, fmt.Sprintf("over the %d%% mark", statusline.Warn))
			if warned != tc.warn {
				t.Errorf("at %d of %d the session says %q, warning=%v, want warning=%v",
					tc.used, int64(million), line, warned, tc.warn)
			}
			if warned && !strings.Contains(line, "\033[1;33m") {
				t.Errorf("the warning is not coloured, so it reads as the same line as every other draw: %q", line)
			}
			if !warned && strings.Contains(line, "\033[") {
				t.Errorf("an ordinary draw is coloured, which leaves nothing for the warning to stand out against: %q", line)
			}
		})
	}
}

// The percentage printed and the decision to warn are read off the same number, so the line can never
// say thirty per cent without warning about it.
func TestThePrintedShareAndTheWarningNeverDisagree(t *testing.T) {
	for used := int64(0); used <= million; used += 1_000 {
		line := statusline.Line(payload(used, million))
		var percent int
		if _, err := fmt.Sscanf(strings.TrimPrefix(line, "\033[1;33m"), "context %d%%", &percent); err != nil {
			t.Fatalf("the line no longer opens with the share, so nothing here is checking anything: %q", line)
		}
		if warned := strings.Contains(line, "mark"); warned != (percent >= statusline.Warn) {
			t.Fatalf("the line says %d%% and warning=%v: %q", percent, warned, line)
		}
	}
}

// A runtime older than this build, or one that is not the model's own tool at all, hands over a
// payload with no context window in it. The line then says that, rather than saying nothing or
// guessing a window size, because a status line that goes blank reads as the crew being broken and a
// guessed window is a confident wrong number.
func TestTheLineSaysWhenTheRuntimeDoesNotReportIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		said string
	}{
		{name: "no context window at all", said: `{"session_id":"abc","model":{"id":"claude-opus-5"}}`},
		{name: "a window of no size", said: `{"context_window":{"total_input_tokens":12,"context_window_size":0}}`},
		{name: "nothing on standard input", said: ``},
		{name: "something that is not a session", said: `not json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := statusline.Line([]byte(tc.said))
			if !strings.HasPrefix(line, "context") {
				t.Errorf("the session says %q, which does not say what it is about", line)
			}
			if strings.Contains(line, "%") {
				t.Errorf("the session made a share up out of a payload that carries none: %q", line)
			}
		})
	}
}
