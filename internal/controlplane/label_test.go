package controlplane

import (
	"strings"
	"testing"
)

// A label goes straight into a listing row, so what it is allowed to be is decided by what a row can
// hold rather than by taste.

func TestALabelIsTidiedRatherThanRefused(t *testing.T) {
	for _, tc := range []struct {
		name  string
		typed string
		want  string
	}{
		{name: "an ordinary name", typed: "the electricity bill", want: "the electricity bill"},
		{name: "space nobody can see", typed: "  the electricity bill  ", want: "the electricity bill"},
		{name: "cleared", typed: "", want: ""},
		{name: "cleared with space", typed: "   ", want: ""},
		// A stored newline draws a row two rows tall, which breaks the cursor and every count of what
		// is on screen.
		{name: "a newline", typed: "the electricity\nbill", want: "the electricity bill"},
		{name: "a carriage return", typed: "the electricity\r\nbill", want: "the electricity  bill"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tidyLabel(tc.typed); got != tc.want {
				t.Fatalf("tidyLabel(%q) = %q, want %q", tc.typed, got, tc.want)
			}
		})
	}
}

// A label long enough to push every other column off the screen is capped rather than refused: it is
// a name somebody typed, and there is nothing to correct.
func TestALongLabelIsCappedAtSomethingARowCanHold(t *testing.T) {
	long := strings.Repeat("a", labelLimit*3)

	tidy := tidyLabel(long)

	if len([]rune(tidy)) != labelLimit {
		t.Fatalf("a label of %d characters was kept at %d, want %d", len(long), len([]rune(tidy)), labelLimit)
	}
}

// Cutting on bytes would split a multi byte character and put a broken rune in the listing.
func TestCappingALabelDoesNotBreakACharacter(t *testing.T) {
	long := strings.Repeat("é", labelLimit*2)

	tidy := tidyLabel(long)

	if strings.Contains(tidy, "�") {
		t.Fatalf("capping broke a character: %q", tidy)
	}
	if len([]rune(tidy)) != labelLimit {
		t.Fatalf("kept %d characters, want %d", len([]rune(tidy)), labelLimit)
	}
}
