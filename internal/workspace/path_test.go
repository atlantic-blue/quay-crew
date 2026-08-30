package workspace_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/name"
	"github.com/atlantic-blue/quay-crew/internal/workspace"
)

func TestParsePathReadsTheLevels(t *testing.T) {
	cases := map[string]workspace.Path{
		"me":                      {Workspace: "me"},
		"me/house-bills":          {Workspace: "me", Project: "house-bills"},
		"me/house-bills/3cb04bf5": {Workspace: "me", Project: "house-bills", Session: "3cb04bf5"},
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
		"empty":                 "",
		"only spaces":           "   ",
		"deeper than a session": "me/house-bills/session/deeper",
		"an empty level":        "me//bills",
		"a trailing slash":      "me/",
		"a leading slash":       "/me",
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
	partial := workspace.Path{Workspace: "me", Session: "3cb04bf5"}
	if partial.String() != "me" {
		t.Errorf("a path with no project renders as %q, want %q", partial.String(), "me")
	}
}

// The word the level above every workspace used to take is not an address, and no workspace may be
// called it, so every command that takes an address refuses it here rather than sending the operator
// away to look for a workspace that could never have existed.
func TestParsePathRefusesTheWordTheLevelUsedToTake(t *testing.T) {
	_, err := workspace.ParsePath(name.Retired)
	if err == nil {
		t.Fatalf("ParsePath(%q) = nil, want a refusal", name.Retired)
	}
	if !strings.Contains(err.Error(), name.System) {
		t.Fatalf("the refusal is %q, and it never says to type %q", err, name.System)
	}
	// The word it became is not an address either, and it is refused by the rule that refuses any
	// name a workspace cannot take rather than by this one, so it is only checked here for company.
	if _, err := workspace.ParsePath("crew-quarters"); err != nil {
		t.Fatalf("ParsePath(\"crew-quarters\") = %v, want an ordinary workspace name", err)
	}
}
