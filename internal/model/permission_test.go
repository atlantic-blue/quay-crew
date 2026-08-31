package model_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/krewe/internal/model"
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

// A surface that asks before it widens and not before it narrows has to know which way a change goes.
func TestWideningIsMovingTowardsDoingMore(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		widens   bool
	}{
		{from: model.PermissionPlan, to: model.PermissionAcceptEdits, widens: true},
		{from: model.PermissionPlan, to: model.PermissionBypass, widens: true},
		{from: model.PermissionAcceptEdits, to: model.PermissionBypass, widens: true},
		{from: model.PermissionBypass, to: model.PermissionAcceptEdits, widens: false},
		{from: model.PermissionBypass, to: model.PermissionPlan, widens: false},
		{from: model.PermissionAcceptEdits, to: model.PermissionPlan, widens: false},
		// Standing still is not widening, so re-picking the mode a session is already in asks nothing.
		{from: model.PermissionAcceptEdits, to: model.PermissionAcceptEdits, widens: false},
		// A session from before the mode was written down has none, and runs acceptEdits, so it
		// compares as one rather than as the narrowest thing there is.
		{from: "", to: model.PermissionBypass, widens: true},
		{from: "", to: model.PermissionPlan, widens: false},
	} {
		t.Run(tc.from+" to "+tc.to, func(t *testing.T) {
			if got := model.PermissionModeWidens(tc.from, tc.to); got != tc.widens {
				t.Fatalf("widening from %q to %q reported %v, want %v", tc.from, tc.to, got, tc.widens)
			}
		})
	}
}

// One place answers whether a mode runs a network command without asking, because two surfaces read
// it: the declaration of a job that must clone and push, and the controller that would otherwise ask
// a session to push in the mode that already stopped it.
//
// The refusal first. A predicate that answered true for everything would satisfy every test that
// admits, and the whole point of it is what it refuses.
func TestOnlyTheWidestModeReachesTheNetwork(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		reaches bool
	}{
		// Plan proposes and runs nothing.
		{mode: model.PermissionPlan, reaches: false},
		{mode: "plan", reaches: false},
		// Edits writes the files in the working directory and asks a person before it runs a command.
		// Nobody stands beside a dispatched job, so the approval never arrives.
		{mode: model.PermissionAcceptEdits, reaches: false},
		{mode: "edits", reaches: false},
		{mode: model.PermissionBypass, reaches: true},
		{mode: "dangerous", reaches: true},
		{mode: "bypassPermissions", reaches: true},
		// A mode nobody set counts as the mode a session with nothing set actually runs in, which is
		// edits, so the empty case answers what the session will do rather than nothing at all.
		{mode: "", reaches: false},
		{mode: "yolo", reaches: false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			if got := model.PermissionModeReachesTheNetwork(tc.mode); got != tc.reaches {
				t.Fatalf("mode %q reaches the network: %v, want %v", tc.mode, got, tc.reaches)
			}
		})
	}
}

// A refusal says the mode in the word a person types back, whichever spelling it was written in.
func TestAModeIsSaidInTheWordSomebodyTypes(t *testing.T) {
	for _, tc := range []struct{ mode, spoken string }{
		{mode: model.PermissionAcceptEdits, spoken: "edits"},
		{mode: model.PermissionBypass, spoken: "dangerous"},
		{mode: model.PermissionPlan, spoken: "plan"},
		{mode: "dangerous", spoken: "dangerous"},
		{mode: "", spoken: "edits"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			if got := model.PermissionModeSpoken(tc.mode); got != tc.spoken {
				t.Fatalf("mode %q is said as %q, want %q", tc.mode, got, tc.spoken)
			}
		})
	}
	if got := model.PermissionModeOnTheNetwork(); got != "dangerous" {
		t.Fatalf("the mode that reaches the network is offered as %q, want dangerous", got)
	}
}
