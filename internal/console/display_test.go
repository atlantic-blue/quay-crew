package console

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
			if got := shortID(testCase.id); got != testCase.want {
				t.Fatalf("shortID(%q) = %q, want %q", testCase.id, got, testCase.want)
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
			if got := displayName(testCase.given, testCase.id); got != testCase.want {
				t.Fatalf("displayName(%q, %q) = %q, want %q", testCase.given, testCase.id, got, testCase.want)
			}
		})
	}
}
