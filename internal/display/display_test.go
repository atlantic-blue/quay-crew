package display

import "testing"

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

// LooksLikeIdentifier decides whether one word of a command line is a session or the first word of a
// message, so it has to be narrow at both ends: an identifier the operator copied has to be caught,
// and an ordinary word has to be left alone.
func TestLooksLikeIdentifier(t *testing.T) {
	cases := []struct {
		word string
		want bool
	}{
		{"615d48dc", true},
		{"615d48dc7702302ef7a98613", true},
		{"FFFFFFFF", true},
		// Shorter than the column is wide, so it cannot have come off a listing.
		{"615d48d", false},
		{"", false},
		// Ordinary words, which are what the message starts with.
		{"hello", false},
		{"deadbeefs", false},
		{"remember", false},
		{"the-bills", false},
		{"me/house-bills/615d48dc", false},
	}
	for _, one := range cases {
		if got := LooksLikeIdentifier(one.word); got != one.want {
			t.Errorf("LooksLikeIdentifier(%q) = %v, want %v", one.word, got, one.want)
		}
	}
}
