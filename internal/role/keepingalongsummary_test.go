package role

import (
	"strings"
	"testing"
)

// A role whose summary runs past the guide is still a role, and its words are kept.
//
// The summary is the line a listing of roles shows. A guide on it is advice about a listing, and
// today it is a refusal at import: a role directory whose summary is one sentence too long does not
// load, so an operator loses the role rather than the line, and every job that names it fails.

// aSummaryOf is one line of exactly this many bytes, with a start and an end an assertion can look
// for, and no colon in it, because it is written into a manifest.
func aSummaryOf(size int) string {
	const opens, ends = "writes the tests for a job, from the job alone", "and this line ends here"
	middle := size - len(opens) - len(ends) - 2
	if middle < 1 {
		panic("a summary this short cannot carry both ends")
	}
	return opens + " " + strings.Repeat("x", middle) + " " + ends
}

func TestARoleSummaryOfAnyLengthIsKeptWordForWord(t *testing.T) {
	summary := aSummaryOf(SummaryLimit * 2)

	loaded, err := FromFiles(replace(good(), ManifestFile, manifestOf(
		"name: test-writer",
		"version: 1",
		"summary: "+summary,
		"model: opus",
		"receives:",
		"  - job",
		"  - context",
	)))
	if err != nil {
		t.Fatalf("a role with a summary of %d bytes was refused: %v", len(summary), err)
	}
	if loaded.Summary != summary {
		t.Fatalf("the summary was kept as %d bytes of the %d it was written with",
			len(loaded.Summary), len(summary))
	}
}
