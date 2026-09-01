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

// The column says what the share means, not only what it is.
//
// A share on its own left an operator holding the workspace's ceiling in their head to read a listing.
// The word beside the number says what the system is about to do: over means the session is given no
// new work on its job and hands the rest over, and near means it is inside the band below that.
func TestTheContextCellSaysWhereTheShareSitsAgainstTheCeiling(t *testing.T) {
	for _, tc := range []struct {
		name    string
		used    int64
		ceiling int32
		want    string
		because string
	}{
		{
			name: "no ceiling stated", used: 900_000, ceiling: 0, want: "90%",
			because: "a mark against a number nobody answered with would be the console inventing a limit",
		},
		{
			name: "well under it", used: 260_000, ceiling: 70, want: "26%",
			because: "a listing that marked every row would say nothing",
		},
		{
			name: "one point below the band", used: 490_000, ceiling: 70, want: "49%",
			because: "the band opens at the ceiling less twenty, which is 50 here",
		},
		{
			name: "inside the band", used: 550_000, ceiling: 70, want: "55% near",
			because: "what an operator does about it is much cheaper decided early",
		},
		{
			name: "exactly at the ceiling", used: 700_000, ceiling: 70, want: "70% over",
			because: "the gate refuses at the ceiling, so the cell says over at the same number",
		},
		{
			name: "past it", used: 820_000, ceiling: 70, want: "82% over",
			because: "this session takes no new work on its job",
		},
		{
			name: "a workspace that raised the ceiling", used: 820_000, ceiling: 95, want: "82% near",
			because: "the band moves with the ceiling rather than being a second setting",
		},
		{
			name: "a workspace that turned the gate off", used: 820_000, ceiling: 100, want: "82% near",
			because: "turning the gate off does not make 82 per cent of a window a comfortable place to work, and the band still says so",
		},
		{
			name: "a full window where the gate is off", used: 1_000_000, ceiling: 100, want: "100% over",
			because: "at a hundred there is nothing left, whatever the workspace set",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cell := display.ContextLabel(&quaycrewv1.Session{
				ContextWindow: &quaycrewv1.ContextWindow{Used: tc.used, Size: 1_000_000, Ceiling: tc.ceiling},
			})
			if cell != tc.want {
				t.Errorf("the cell reads %q, want %q\n\n%s", cell, tc.want, tc.because)
			}
		})
	}
}

// A window nothing has measured carries no share, so it carries no word either. A count marked over
// would be a limit applied to a number the system never worked out.
func TestACountIsNeverMarkedAgainstACeiling(t *testing.T) {
	cell := display.ContextLabel(&quaycrewv1.Session{
		ContextWindow: &quaycrewv1.ContextWindow{Used: 900_000, Ceiling: 70},
	})
	if cell != "900k" {
		t.Fatalf("the cell reads %q, want the count on its own", cell)
	}
}
