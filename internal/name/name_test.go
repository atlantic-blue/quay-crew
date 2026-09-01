package name_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/name"
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

func TestValidateNamesWhatWouldJob(t *testing.T) {
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

// "system" is the word every address takes for the level above a workspace. A workspace called system
// would take the secrets, skills, hooks and roles meant for every workspace, and nothing else would
// ever read them.
func TestAWorkspaceCannotBeCalledSystem(t *testing.T) {
	err := name.ValidateWorkspace(name.System)
	if err == nil {
		t.Fatal("ValidateWorkspace(\"system\") = nil, want a refusal")
	}
	// The reason, not only the refusal. An operator told no and not why types it again.
	if !strings.Contains(err.Error(), "whole system") {
		t.Fatalf("the refusal is %q, and it does not say why", err)
	}
	if err := name.ValidateWorkspace("systems"); err != nil {
		t.Fatalf("ValidateWorkspace(\"systems\") = %v, want it accepted", err)
	}
	// A project may still be called system: an address names a project under a workspace, so there is
	// nothing for it to shadow.
	if err := name.Validate("project", name.System); err != nil {
		t.Fatalf("Validate(project, \"system\") = %v, want it accepted", err)
	}
}

// The word the level took before it was called system is refused by name. A word that stops working
// quietly is the regression this repository has already had once: the operator types what worked
// yesterday, is told there is no such workspace, and goes looking for a workspace.
func TestTheWordTheLevelUsedToTakeIsRefusedByName(t *testing.T) {
	err := name.RefuseRetired(name.Retired)
	if err == nil {
		t.Fatalf("RefuseRetired(%q) = nil, want a refusal", name.Retired)
	}
	// The refusal has one job, which is to say what to type instead.
	if !strings.Contains(err.Error(), name.System) {
		t.Fatalf("the refusal is %q, and it never says to type %q", err, name.System)
	}
	// Spacing is what a shell leaves behind, so it is refused the same way.
	if err := name.RefuseRetired("  " + name.Retired + " "); err == nil {
		t.Fatal("a padded word went through, so the refusal can be walked past with a space")
	}
	// Everything else is somebody's workspace and is none of this function's business.
	for _, typed := range []string{"", "acme", "crews", "crew-cut", name.System} {
		if err := name.RefuseRetired(typed); err != nil {
			t.Fatalf("RefuseRetired(%q) = %v, want it left alone", typed, err)
		}
	}
}

// And it stays reserved as a workspace name. A workspace called crew would be handed everything typed
// out of habit, and the operator would read the acknowledgement as the level having been set.
func TestAWorkspaceCannotBeCalledTheWordTheLevelUsedToTake(t *testing.T) {
	err := name.ValidateWorkspace(name.Retired)
	if err == nil {
		t.Fatalf("ValidateWorkspace(%q) = nil, want a refusal", name.Retired)
	}
	if !strings.Contains(err.Error(), name.System) {
		t.Fatalf("the refusal is %q, and it never says the word is now %q", err, name.System)
	}
	// The word is the whole of it, so a name that merely starts with it is still a name.
	if err := name.ValidateWorkspace("crew-quarters"); err != nil {
		t.Fatalf("ValidateWorkspace(\"crew-quarters\") = %v, want it accepted", err)
	}
}

// A reserved word is reserved however it was typed.
//
// A name is lowercase letters, digits and hyphens, so neither reserved word can ever be capitalised
// and still be a name. The general rule read "System" and answered with the typed name lowercased,
// which is "system", the one name a workspace may not hold. The operator was told to type the very
// thing that is refused, so the rule read as not applying here.
func TestTheReservedWordsAreRefusedHoweverTheyAreTyped(t *testing.T) {
	for _, typed := range []string{"System", "SYSTEM", "sYsTeM", "Crew", "CREW"} {
		err := name.ValidateWorkspace(typed)
		if err == nil {
			t.Fatalf("ValidateWorkspace(%q) = nil, want a refusal", typed)
		}
		// The refusal that names the word, rather than the one that says to lowercase it.
		if strings.Contains(err.Error(), "lowercase letters, digits and hyphens") {
			t.Fatalf("ValidateWorkspace(%q) = %q, which advises typing a name this refuses", typed, err)
		}
		if !strings.Contains(err.Error(), name.System) {
			t.Fatalf("ValidateWorkspace(%q) = %q, and it never says the word", typed, err)
		}
	}
}

// The same for the word where an address goes. "Crew" typed at a command answered that this system
// has no workspace called Crew, which sends the operator looking for a workspace.
func TestTheWordTheLevelUsedToTakeIsRefusedHoweverItIsTyped(t *testing.T) {
	for _, typed := range []string{"Crew", "CREW", "cReW", "  Crew  "} {
		err := name.RefuseRetired(typed)
		if err == nil {
			t.Fatalf("RefuseRetired(%q) = nil, want the refusal that says what to type", typed)
		}
		if !strings.Contains(err.Error(), name.System) {
			t.Fatalf("RefuseRetired(%q) = %q, and it never says to type %q", typed, err, name.System)
		}
	}
	// Only the word itself. A name that contains it is somebody's workspace.
	for _, typed := range []string{"Crews", "Crew-cut", "Acme"} {
		if err := name.RefuseRetired(typed); err != nil {
			t.Fatalf("RefuseRetired(%q) = %v, want it left alone", typed, err)
		}
	}
}
