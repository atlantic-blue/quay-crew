package sandbox_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

func TestComposeLeavesOutLevelsWithNothingToSay(t *testing.T) {
	body := sandbox.Compose([]sandbox.Section{
		{Scope: "crew", Body: "no acronyms"},
		{Scope: "workspace", Body: "   "},
		{Scope: "project", Body: "pay the water bill first"},
	})

	if strings.Contains(body, "workspace") {
		t.Fatalf("an empty level was written out, which is noise the model has to read:\n%s", body)
	}
	for _, want := range []string{"no acronyms", "pay the water bill first"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the composed file does not carry %q:\n%s", want, body)
		}
	}
}

// TestComposeAndDecomposeAreTheSameThingBothWays is what makes reading an edit back possible: a level
// has to come out of the file as what went into it, or every turn would quietly rewrite the crew's
// memory.
func TestComposeAndDecomposeAreTheSameThingBothWays(t *testing.T) {
	scopes := []string{"crew", "workspace"}
	sections := []sandbox.Section{
		{Scope: "crew", Body: "no acronyms\nno em dashes"},
		{Scope: "workspace", Body: "this is the house"},
	}

	got := sandbox.Decompose(sandbox.Compose(sections), scopes)
	for _, section := range sections {
		if got[section.Scope] != section.Body {
			t.Fatalf("%s came back as %q, want %q", section.Scope, got[section.Scope], section.Body)
		}
	}
}

// TestSomethingAppendedBelongsToTheInnermostLevel: an agent writing a note about the work it is doing
// writes at the end of the file, and that note is about this piece of work rather than about the crew.
func TestSomethingAppendedBelongsToTheInnermostLevel(t *testing.T) {
	scopes := []string{"project", "session"}
	file := sandbox.Compose([]sandbox.Section{
		{Scope: "project", Body: "pay the water bill first"},
		{Scope: "session", Body: "the account number is 4471"},
	}) + "\nand the meter is under the stairs"

	got := sandbox.Decompose(file, scopes)
	if got["project"] != "pay the water bill first" {
		t.Fatalf("the project level changed under an append: %q", got["project"])
	}
	if want := "the account number is 4471\nand the meter is under the stairs"; got["session"] != want {
		t.Fatalf("the session level is %q, want %q", got["session"], want)
	}
}

// TestATotallyRewrittenFileIsNotThrownAway: somebody who deletes the marks and writes prose still
// meant it, and losing what they wrote is worse than filing it in the wrong place.
func TestATotallyRewrittenFileIsNotThrownAway(t *testing.T) {
	got := sandbox.Decompose("everything I know about this", []string{"project", "session"})
	if got["session"] != "everything I know about this" {
		t.Fatalf("a file with no marks came back as %q", got)
	}
}
