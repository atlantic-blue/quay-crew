package promise

import (
	"os"
	"strings"
	"testing"
)

// The tests above prove the check. This one proves the check is asked, which is the whole shape of
// the defect it exists for: nothing was wrong with continuous integration, it was simply never asked
// the question. A package that refuses correctly and is never run refuses nothing.

const workflow = "../../.github/workflows/ci.yml"

// TestContinuousIntegrationRunsThisCheck.
func TestContinuousIntegrationRunsThisCheck(t *testing.T) {
	body, err := os.ReadFile(workflow)
	if err != nil {
		t.Fatalf("reading %s: %v", workflow, err)
	}
	said := string(body)

	for _, want := range []string{
		// The command, so the job runs this package rather than something that looks like it.
		"cmd/promises",
		// The pull request body, which is where the reason for having neither is written. Without
		// it every change that legitimately has none is refused and the gate gets turned off.
		"github.event.pull_request.body",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("%s never mentions %q, so this check runs nowhere", workflow, want)
		}
	}

	// The base branch has to be fetched before it can be diffed against. A shallow checkout has the
	// pull request's own commits and not the ref it was cut from, and the check would then refuse
	// every change for a range it cannot read.
	if !strings.Contains(said, "git fetch") {
		t.Errorf("%s never fetches the base ref, so the check has nothing to diff against", workflow)
	}
}

// TestTheMakefileRunsItToo, so the question can be asked before pushing rather than only after.
func TestTheMakefileRunsItToo(t *testing.T) {
	body, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatalf("reading the Makefile: %v", err)
	}
	if !strings.Contains(string(body), "cmd/promises") {
		t.Error("the Makefile has no target that runs this check, so nobody can ask before pushing")
	}
}
