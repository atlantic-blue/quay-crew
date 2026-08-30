package changelog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repository is this repository's own fragment directory, from this package.
const repository = "../../" + Dir

// TestThisRepositoryAssembles.
//
// The unit tests above run against directories of their own, so they prove the assembler and say
// nothing about what is committed. This runs the real command's work over the real fragments: a
// fragment that lands misnamed or empty fails here, on the pull request that added it, rather than
// on the evening somebody cuts a release.
func TestThisRepositoryAssembles(t *testing.T) {
	if _, err := os.Stat(repository); err != nil {
		t.Fatalf("this repository has no %s: %v", Dir, err)
	}

	fragments, err := Collect(repository)
	if err != nil {
		t.Fatalf("the committed fragments do not assemble: %v", err)
	}

	// The convention is written down next to the fragments, because the person who needs it is
	// looking at the directory rather than at this test.
	if _, err := os.Stat(filepath.Join(repository, "README.md")); err != nil {
		t.Errorf("%s does not say how to write one: %v", Dir, err)
	}

	// No floor on the count. A release empties this directory in the same commit that assembles it,
	// so demanding a fragment here would fail the release. The names are logged instead, because a
	// run that read nothing and a run that read everything are otherwise the same ok line.
	names := make([]string, 0, len(fragments))
	for _, fragment := range fragments {
		names = append(names, fragment.File)
	}
	t.Logf("%s holds %d: %s", Dir, len(fragments), strings.Join(names, ", "))
	for _, fragment := range fragments {
		first, _, _ := strings.Cut(fragment.Body, "\n")
		if !strings.HasPrefix(first, "**") {
			t.Errorf("%s starts %q, and an entry starts with the bold sentence that says what changed", fragment.File, first)
		}
	}
}
