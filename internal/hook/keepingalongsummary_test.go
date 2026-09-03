package hook_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/hook"
)

// A hook whose summary runs past the guide is still a hook, and its words are kept.
//
// The summary is the line a listing shows. A guide on it is advice about a listing, and today it is
// a refusal at import: a hook whose summary is one sentence too long does not load at all, so the
// system loses the hook rather than the line. The listing is the place to shorten a line, because
// the listing is where the width is.

// aLongSummary is one line of exactly this many bytes, with a start and an end an assertion can look
// for, and no colon in it, because it is written into a manifest.
func aLongSummary(size int) string {
	const opens, ends = "reads every message and hands the session a short brief", "and this line ends here"
	middle := size - len(opens) - len(ends) - 2
	if middle < 1 {
		panic("a summary this short cannot carry both ends")
	}
	return opens + " " + strings.Repeat("x", middle) + " " + ends
}

func TestAHookSummaryOfAnyLengthIsKeptWordForWord(t *testing.T) {
	summary := aLongSummary(hook.SummaryLimit * 2)

	loaded, err := hook.FromFiles(files(`
name: guard
version: 1
summary: ` + summary + `
events:
  - on: UserPromptSubmit
    entry: bin/hook
`))
	if err != nil {
		t.Fatalf("a hook with a summary of %d bytes was refused: %v", len(summary), err)
	}
	if loaded.Summary != summary {
		t.Fatalf("the summary was kept as %d bytes of the %d it was written with",
			len(loaded.Summary), len(summary))
	}
}
