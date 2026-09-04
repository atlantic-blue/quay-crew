package display

import (
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
)

// TestTheListingSaysWhichKindOfIdleItIs. Four states used to share one word, and the operator acts on
// the word: a restart, a drain and a reclaim all start from a listing that said idle.
func TestTheListingSaysWhichKindOfIdleItIs(t *testing.T) {
	for name, testCase := range map[string]struct {
		status   string
		presence quaycrewv1.SessionPresence
		want     string
	}{
		"a runtime answering with nobody watching it": {
			status:   StatusIdle,
			presence: quaycrewv1.SessionPresence_SESSION_PRESENCE_AWAKE,
			want:     StatusAwake,
		},
		"somebody typing into the conversation": {
			status:   StatusIdle,
			presence: quaycrewv1.SessionPresence_SESSION_PRESENCE_ATTACHED,
			want:     StatusAttached,
		},
		"an empty container, which is the only real idle": {
			status:   StatusIdle,
			presence: quaycrewv1.SessionPresence_SESSION_PRESENCE_EMPTY,
			want:     StatusIdle,
		},
		"a sandbox that would not say": {
			status:   StatusIdle,
			presence: quaycrewv1.SessionPresence_SESSION_PRESENCE_UNTOLD,
			want:     StatusUnknown,
		},
		"a listing that did not ask reads as it always read": {
			status:   StatusIdle,
			presence: quaycrewv1.SessionPresence_SESSION_PRESENCE_UNSPECIFIED,
			want:     StatusIdle,
		},
	} {
		t.Run(name, func(t *testing.T) {
			session := &quaycrewv1.Session{Status: testCase.status, Presence: testCase.presence}
			if got := SessionStatus(session); got != testCase.want {
				t.Fatalf("the listing says %q, want %q", got, testCase.want)
			}
		})
	}
}

// TestAWordThatIsNotIdleKeepsItself. Each of the others carries something presence cannot say, and
// overwriting failed with awake would lose that the last exec did not land. An exec under way is the
// clearest of them: running is what a drain refuses on.
func TestAWordThatIsNotIdleKeepsItself(t *testing.T) {
	for _, status := range []string{"running", "failed", "stopped", "reclaimed"} {
		t.Run(status, func(t *testing.T) {
			session := &quaycrewv1.Session{
				Status:   status,
				Presence: quaycrewv1.SessionPresence_SESSION_PRESENCE_AWAKE,
			}
			if got := SessionStatus(session); got != status {
				t.Fatalf("a %s session reads %q", status, got)
			}
		})
	}
}

// TestTheStaleMarkRidesOnTheDerivedWord. The two marks share one cell, so a session running a
// conversation in a sandbox born before the workspace's current skills has to say both.
func TestTheStaleMarkRidesOnTheDerivedWord(t *testing.T) {
	session := &quaycrewv1.Session{
		Status:   StatusIdle,
		Stale:    true,
		Presence: quaycrewv1.SessionPresence_SESSION_PRESENCE_AWAKE,
	}
	if got := StatusLabel(session); got != StatusAwake+" stale" {
		t.Fatalf("the status cell says %q, want %q", got, StatusAwake+" stale")
	}
}

// TestTheStatusCellIsWhereTheListingSaysIt. The cell an operator reads is the one the columns name,
// so the derived word has to reach it rather than stopping at a function nothing calls.
func TestTheStatusCellIsWhereTheListingSaysIt(t *testing.T) {
	session := &quaycrewv1.Session{
		Id:       "5d013d07b9bcc8c05a1f437a",
		Status:   StatusIdle,
		Presence: quaycrewv1.SessionPresence_SESSION_PRESENCE_AWAKE,
	}
	cells := SessionCells(session, "acme", "house-bills")
	columns := SessionColumns()

	at := -1
	for index, column := range columns {
		if column == "status" {
			at = index
		}
	}
	if at < 0 {
		t.Fatal("a listing of sessions has no status column")
	}
	if cells[at] != StatusAwake {
		t.Fatalf("the status cell says %q, and the sandbox is running a conversation", cells[at])
	}
}
