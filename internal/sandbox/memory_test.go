package sandbox_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

func TestComposeLeavesOutLevelsWithNothingToSay(t *testing.T) {
	body := sandbox.Compose([]sandbox.Section{
		{Scope: "system", Body: "no acronyms"},
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
// has to come out of the file as what went into it, or every exec would quietly rewrite the system's
// memory.
func TestComposeAndDecomposeAreTheSameThingBothWays(t *testing.T) {
	scopes := []string{"system", "workspace"}
	sections := []sandbox.Section{
		{Scope: "system", Body: "no acronyms\nno em dashes"},
		{Scope: "workspace", Body: "this is the house"},
	}

	got := sandbox.Decompose(sandbox.Compose(sections), scopes)
	for _, section := range sections {
		if got[section.Scope] != section.Body {
			t.Fatalf("%s came back as %q, want %q", section.Scope, got[section.Scope], section.Body)
		}
	}
}

// TestSomethingAppendedBelongsToTheInnermostLevel: an agent writing a note about the job it is doing
// writes at the end of the file, and that note is about this job rather than about the system.
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

// TestWithoutSectionDropsOneMarkAndItsBody: a swept skills index is the mark line and everything
// under it up to the next mark, and nothing else moves.
func TestWithoutSectionDropsOneMarkAndItsBody(t *testing.T) {
	cases := []struct {
		name  string
		body  string
		want  string
		found bool
	}{
		{
			name:  "no section leaves the body exactly as it was",
			body:  "remember the invoice\nno trailing newline",
			want:  "remember the invoice\nno trailing newline",
			found: false,
		},
		{
			name:  "a section at the end is dropped",
			body:  "remember the invoice\n\n<!-- quay:skills -->\n- git: Branch first.\n  /home/agent/skills/git/SKILL.md",
			want:  "remember the invoice",
			found: true,
		},
		{
			name:  "a section mid body ends at the next mark",
			body:  "<!-- quay:skills -->\n- git: Branch first.\n<!-- quay:session -->\nthe account number is 4471",
			want:  "<!-- quay:session -->\nthe account number is 4471",
			found: true,
		},
		{
			name:  "a body that is only the section comes back empty",
			body:  "<!-- quay:skills -->\n- git: Branch first.",
			want:  "",
			found: true,
		},
		{
			name:  "another scope's mark is not touched",
			body:  "<!-- quay:session -->\nthe account number is 4471",
			want:  "<!-- quay:session -->\nthe account number is 4471",
			found: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := sandbox.WithoutSection(tc.body, sandbox.SkillsScope)
			if got != tc.want {
				t.Fatalf("the body came back as %q, want %q", got, tc.want)
			}
			if found != tc.found {
				t.Fatalf("found is %v, want %v", found, tc.found)
			}
		})
	}
}
