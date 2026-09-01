package display_test

import (
	"fmt"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/statusline"
)

// The column that says whether a conversation is still worth continuing. A share where the system knows
// how big the window is, and the count on its own where nothing told it, because a share an operator
// acts on has to be true and a guessed one is worse than none.
func TestTheContextCellSaysAShareOrACount(t *testing.T) {
	for _, tc := range []struct {
		name    string
		window  *quaycrewv1.ContextWindow
		want    string
		because string
	}{
		{
			name: "a known window", window: &quaycrewv1.ContextWindow{Used: 258_000, Size: 1_000_000},
			want:    "26%",
			because: "the share is what decides anything",
		},
		{
			name: "a window nothing has measured", window: &quaycrewv1.ContextWindow{Used: 258_000},
			want:    "258k",
			because: "the count is true, and a share worked out from a guessed window is not",
		},
		{
			name: "a conversation nobody has spoken in", window: nil,
			want:    "",
			because: "a session with no conversation behind it has filled nothing, and the token columns are blank there too",
		},
		{
			name:    "a conversation the runtime is about to compact",
			window:  &quaycrewv1.ContextWindow{Used: 1_040_000, Size: 1_000_000},
			want:    "100%",
			because: "a hundred and four per cent reads as a defect in the system",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := display.ContextLabel(&quaycrewv1.Session{ContextWindow: tc.window}); got != tc.want {
				t.Errorf("the cell reads %q, want %q\n\n%s", got, tc.want, tc.because)
			}
		})
	}
}

// The column and the line under the prompt answer the same question about the same conversation, and
// the operator reads both. They job the share out in one place so they cannot disagree at the edges.
func TestTheColumnAndTheLineAgreeOnTheShare(t *testing.T) {
	// From a conversation that has been spoken in. At nothing at all the two answer different
	// questions on purpose: the line is drawn inside a session that exists, and the column is a row
	// for a session that may never have been opened.
	for used := int64(7_919); used <= 1_000_000; used += 7_919 {
		cell := display.ContextLabel(&quaycrewv1.Session{
			ContextWindow: &quaycrewv1.ContextWindow{Used: used, Size: 1_000_000},
		})
		line := statusline.Line([]byte(fmt.Sprintf(
			`{"context_window":{"total_input_tokens":%d,"context_window_size":1000000}}`, used)))
		if want := "context " + strings.TrimSuffix(cell, "%") + "% used"; !strings.Contains(line, want) {
			t.Fatalf("the column says %q and the line says %q", cell, line)
		}
	}
}
