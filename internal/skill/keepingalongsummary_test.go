package skill_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/skill"
)

// A skill whose summary runs past the guide is still a skill, and its words are kept.
//
// The summary is the line every session holding the skill pays for, which is the argument for a
// short one and not an argument for throwing the skill away. Today an import refuses the whole
// skill, so the system loses a capability over a line in a listing.

// aSkillSummaryOf is one line of exactly this many bytes, with a start and an end an assertion can
// look for, and no colon in it, because it is written into a manifest.
func aSkillSummaryOf(size int) string {
	const opens, ends = "branch, stage and commit, the way this system does it", "and this line ends here"
	middle := size - len(opens) - len(ends) - 2
	if middle < 1 {
		panic("a summary this short cannot carry both ends")
	}
	return opens + " " + strings.Repeat("x", middle) + " " + ends
}

func TestASkillSummaryOfAnyLengthIsKeptWordForWord(t *testing.T) {
	summary := aSkillSummaryOf(skill.SummaryLimit * 2)

	loaded, err := skill.FromFiles(files("name: github\nversion: 1\nsummary: "+summary+"\n", "brief\n"))
	if err != nil {
		t.Fatalf("a skill with a summary of %d bytes was refused: %v", len(summary), err)
	}
	if loaded.Summary != summary {
		t.Fatalf("the summary was kept as %d bytes of the %d it was written with",
			len(loaded.Summary), len(summary))
	}
}
