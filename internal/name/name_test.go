package name_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/name"
)

func TestIsSlug(t *testing.T) {
	accepted := []string{"me", "house-bills", "q1", "2026-budget", "a", strings.Repeat("a", name.MaxLength)}
	for _, value := range accepted {
		if !name.IsSlug(value) {
			t.Errorf("IsSlug(%q) = false, want true", value)
		}
	}

	refused := map[string]string{
		"empty":             "",
		"a space":           "house bills",
		"a slash":           "me/bills",
		"capitals":          "Bills",
		"a leading hyphen":  "-bills",
		"a trailing hyphen": "bills-",
		"a doubled hyphen":  "house--bills",
		"an underscore":     "house_bills",
		"an accent":         "café",
		"too long":          strings.Repeat("a", name.MaxLength+1),
	}
	for reason, value := range refused {
		if name.IsSlug(value) {
			t.Errorf("IsSlug(%q) = true (%s), want false", value, reason)
		}
	}
}

func TestSlugifySuggestsSomethingUsable(t *testing.T) {
	cases := map[string]string{
		"House Bills":     "house-bills",
		"me/bills":        "me-bills",
		"  spaced  out  ": "spaced-out",
		"Q1 2026 Budget!": "q1-2026-budget",
		"house_bills":     "house-bills",
		"already-fine":    "already-fine",
	}
	for input, want := range cases {
		got := name.Slugify(input)
		if got != want {
			t.Errorf("Slugify(%q) = %q, want %q", input, got, want)
		}
		// Whatever it suggests has to be a name that would actually be accepted, otherwise the
		// refusal sends the operator round the same loop again.
		if !name.IsSlug(got) {
			t.Errorf("Slugify(%q) = %q, which is not itself a valid name", input, got)
		}
	}

	long := name.Slugify(strings.Repeat("word ", 40))
	if !name.IsSlug(long) {
		t.Errorf("Slugify of a long value gave %q, which is not a valid name", long)
	}
}

func TestValidateNamesWhatWouldWork(t *testing.T) {
	if err := name.Validate("workspace", "house-bills"); err != nil {
		t.Fatalf("Validate rejected a slug: %v", err)
	}
	err := name.Validate("project", "House Bills")
	if err == nil {
		t.Fatal("Validate accepted \"House Bills\", want a refusal")
	}
	if !strings.Contains(err.Error(), "house-bills") {
		t.Errorf("the refusal is %q, want it to suggest house-bills", err)
	}
	if !strings.Contains(err.Error(), "project") {
		t.Errorf("the refusal is %q, want it to say which thing was being named", err)
	}
	// A value with nothing usable in it still has to be refused, without suggesting an empty name.
	if err := name.Validate("workspace", "///"); err == nil || strings.Contains(err.Error(), `example ""`) {
		t.Errorf("Validate(\"///\") = %v, want a refusal that suggests something real", err)
	}
}

// "crew" is the word every address takes for the level above a workspace. A workspace called crew
// would take the secrets, skills, hooks and roles meant for every workspace, and nothing else would
// ever read them.
func TestAWorkspaceCannotBeCalledCrew(t *testing.T) {
	err := name.ValidateWorkspace(name.Crew)
	if err == nil {
		t.Fatal("ValidateWorkspace(\"crew\") = nil, want a refusal")
	}
	// The reason, not only the refusal. An operator told no and not why types it again.
	if !strings.Contains(err.Error(), "whole crew") {
		t.Fatalf("the refusal is %q, and it does not say why", err)
	}
	if err := name.ValidateWorkspace("crews"); err != nil {
		t.Fatalf("ValidateWorkspace(\"crews\") = %v, want it accepted", err)
	}
	// A project may still be called crew: an address names a project under a workspace, so there is
	// nothing for it to shadow.
	if err := name.Validate("project", name.Crew); err != nil {
		t.Fatalf("Validate(project, \"crew\") = %v, want it accepted", err)
	}
}
