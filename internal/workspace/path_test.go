package workspace_test

import (
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/workspace"
)

func TestParsePathReadsTheLevels(t *testing.T) {
	cases := map[string]workspace.Path{
		"me":                      {Workspace: "me"},
		"me/house-bills":          {Workspace: "me", Project: "house-bills"},
		"me/house-bills/3cb04bf5": {Workspace: "me", Project: "house-bills", Thread: "3cb04bf5"},
		"  me/house-bills  ":      {Workspace: "me", Project: "house-bills"},
		// A name from before names were slugs still has to be reachable, quoted.
		"me/house bills": {Workspace: "me", Project: "house bills"},
	}
	for input, want := range cases {
		got, err := workspace.ParsePath(input)
		if err != nil {
			t.Errorf("ParsePath(%q): %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParsePath(%q) = %+v, want %+v", input, got, want)
		}
	}
}

func TestParsePathRefusesWhatItCannotRead(t *testing.T) {
	refused := map[string]string{
		"empty":                "",
		"only spaces":          "   ",
		"deeper than a thread": "me/house-bills/thread/deeper",
		"an empty level":       "me//bills",
		"a trailing slash":     "me/",
		"a leading slash":      "/me",
	}
	for reason, input := range refused {
		if _, err := workspace.ParsePath(input); err == nil {
			t.Errorf("ParsePath(%q) was accepted (%s), want a refusal", input, reason)
		}
	}
}

func TestPathRoundTripsThroughItsString(t *testing.T) {
	for _, address := range []string{"me", "me/house-bills", "me/house-bills/3cb04bf5"} {
		parsed, err := workspace.ParsePath(address)
		if err != nil {
			t.Fatalf("ParsePath(%q): %v", address, err)
		}
		if parsed.String() != address {
			t.Errorf("%q came back as %q", address, parsed.String())
		}
	}
	if (workspace.Path{}).String() != "" {
		t.Errorf("an empty path renders as %q, want nothing", (workspace.Path{}).String())
	}
	// A path with a hole in it renders the part that is real, rather than an address with an empty
	// level that could never be parsed back.
	partial := workspace.Path{Workspace: "me", Thread: "3cb04bf5"}
	if partial.String() != "me" {
		t.Errorf("a path with no project renders as %q, want %q", partial.String(), "me")
	}
}
