package model_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/model"
)

// The words for a mode were written out three times: in the command line tool, in the console's
// wizard, and about to be a third time in the control plane. Three tables drift, and the drift is
// invisible until somebody types a word one surface takes and another refuses.

func TestAModeIsNamedByWhatSomebodyTypes(t *testing.T) {
	for _, tc := range []struct {
		typed string
		want  string
	}{
		{typed: "plan", want: model.PermissionPlan},
		{typed: "edits", want: model.PermissionAcceptEdits},
		{typed: "dangerous", want: model.PermissionBypass},
		// The protocol's own spellings, because they are what the manual prints and what a script
		// that read the protocol would send.
		{typed: "acceptEdits", want: model.PermissionAcceptEdits},
		{typed: "bypassPermissions", want: model.PermissionBypass},
		// Typed by a person, so the shape they typed it in is not the point.
		{typed: "  Dangerous  ", want: model.PermissionBypass},
		{typed: "PLAN", want: model.PermissionPlan},
	} {
		t.Run(tc.typed, func(t *testing.T) {
			got, known := model.PermissionModeNamed(tc.typed)
			if !known {
				t.Fatalf("%q was refused, want %s", tc.typed, tc.want)
			}
			if got != tc.want {
				t.Fatalf("%q became %q, want %q", tc.typed, got, tc.want)
			}
		})
	}
}

func TestAWordThatIsNotAModeIsRefused(t *testing.T) {
	for _, typed := range []string{"", "   ", "yolo", "accept", "bypass", "planning"} {
		if got, known := model.PermissionModeNamed(typed); known {
			t.Errorf("%q was taken as %q, and it is not a mode", typed, got)
		}
	}
}

// A refusal has to name what the modes are, because a person who typed the wrong one has no other
// way to find out and the words are not guessable from the protocol's spellings.
func TestTheModesAreOfferedNarrowestFirst(t *testing.T) {
	offered := model.PermissionModesOffered()

	if want := []string{"plan", "edits", "dangerous"}; strings.Join(offered, ",") != strings.Join(want, ",") {
		t.Fatalf("the modes are offered as %v, want %v, narrowest first so the most permissive is never first", offered, want)
	}
}
