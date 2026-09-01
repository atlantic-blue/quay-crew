package console

import (
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
)

// TestTheStatusCellCarriesTheStaleMark pins the operator's cue: a session whose live sandbox was
// born before the workspace's current skills says so in the listing, and one that holds the
// current set says nothing extra.
func TestTheStatusCellCarriesTheStaleMark(t *testing.T) {
	current := sessionRow(&quaycrewv1.Session{Id: "a", Status: "idle"}, "acme", "bills")
	if got := strings.Join(current.Cells, " "); strings.Contains(got, "stale") {
		t.Fatalf("a current session's row says stale: %q", got)
	}

	stale := sessionRow(&quaycrewv1.Session{Id: "b", Status: "idle", Stale: true}, "acme", "bills")
	if got := strings.Join(stale.Cells, " "); !strings.Contains(got, "idle stale") {
		t.Fatalf("a stale session's row does not say so beside its status: %q", got)
	}
}
