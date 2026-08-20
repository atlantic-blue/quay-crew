package display

import (
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
)

func TestShortID(t *testing.T) {
	for _, testCase := range []struct {
		name string
		id   string
		want string
	}{
		{"a full identifier is cut to eight characters", "5d013d07b9bcc8c05a1f437a", "5d013d07"},
		{"exactly eight characters is left alone", "5d013d07", "5d013d07"},
		{"something shorter is left alone", "abc", "abc"},
		{"empty stays empty", "", ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ShortID(testCase.id); got != testCase.want {
				t.Fatalf("ShortID(%q) = %q, want %q", testCase.id, got, testCase.want)
			}
		})
	}
}

func TestDisplayName(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		given string
		id    string
		want  string
	}{
		{"a name wins", "demo", "5d013d07b9bcc8c05a1f437a", "demo"},
		{"no name falls back to a short identifier", "", "5d013d07b9bcc8c05a1f437a", "5d013d07"},
		{"neither shows a dash rather than a blank cell", "", "", "-"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Name(testCase.given, testCase.id); got != testCase.want {
				t.Fatalf("Name(%q, %q) = %q, want %q", testCase.given, testCase.id, got, testCase.want)
			}
		})
	}
}

// The listing has to print the identifier an address takes. It printed the session's own id, which
// no address would take, and a named session lost its handle from the screen altogether.
func TestTheFirstColumnIsTheIdentifierAnAddressTakes(t *testing.T) {
	session := &quaycrewv1.Session{
		Id: "5d013d07b9bcc8c05a1f437a", Handle: "97a71ccd4b1e2f3a4b5c6d7e", Status: "idle",
	}

	if got := SessionColumns()[0]; got != "session" {
		t.Fatalf("the first column is headed %q, want %q", got, "session")
	}
	cells := SessionCells(session, "me", "website")
	if cells[0] != "97a71ccd" {
		t.Fatalf("the first cell is %q, want the shortened handle", cells[0])
	}
	if strings.Contains(strings.Join(cells, " "), "5d013d07") {
		t.Fatalf("the row prints the session id, which an address does not take: %v", cells)
	}
}

// A name is a column of its own rather than a replacement for the identifier, which is how naming a
// session used to take its handle off the screen.
func TestNamingASessionFillsTheNameColumnAndNothingElse(t *testing.T) {
	const nameColumn = 3
	for _, testCase := range []struct {
		name    string
		session *quaycrewv1.Session
		want    string
	}{
		{
			"the operator's name wins",
			&quaycrewv1.Session{Handle: "97a71ccd4b1e2f3a4b5c6d7e", Label: "the electricity bill", Description: "reading bills"},
			"the electricity bill",
		},
		{
			"then the one the crew wrote",
			&quaycrewv1.Session{Handle: "97a71ccd4b1e2f3a4b5c6d7e", Description: "reading bills"},
			"reading bills",
		},
		{
			"and a session nobody has named leaves the column empty rather than repeating the identifier",
			&quaycrewv1.Session{Handle: "97a71ccd4b1e2f3a4b5c6d7e"},
			"-",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cells := SessionCells(testCase.session, "me", "website")
			if cells[nameColumn] != testCase.want {
				t.Fatalf("the name column says %q, want %q", cells[nameColumn], testCase.want)
			}
			if cells[0] != "97a71ccd" {
				t.Fatalf("the identifier column says %q, want it unchanged by any name", cells[0])
			}
		})
	}
}
