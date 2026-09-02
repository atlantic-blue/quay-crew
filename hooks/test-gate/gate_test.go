package main

import (
	"strings"
	"testing"
)

// The table is the gate. A reader who wants to know what a build worker may do reads this rather than
// starting a container, and a line that moves from one column to the other is a change somebody has
// to argue for.
func TestWhatABuildWorkerMayNotDo(t *testing.T) {
	lines := []struct {
		name    string
		tool    string
		input   Input
		refused bool
	}{
		// Writing a test, in each of the shapes the runtime offers.
		{name: "writing a test file", tool: "Write",
			input: Input{FilePath: "/repo/internal/job/build_test.go"}, refused: true},
		{name: "editing a test file", tool: "Edit",
			input: Input{FilePath: "internal/job/build_test.go"}, refused: true},
		{name: "editing several places in a test file", tool: "MultiEdit",
			input: Input{FilePath: "features/build_steps_test.go"}, refused: true},
		{name: "a feature file is a test", tool: "Write",
			input: Input{FilePath: "features/build.feature"}, refused: true},
		{name: "a notebook of tests", tool: "NotebookEdit",
			input: Input{NotebookPath: "analysis/test_totals.ipynb"}, refused: true},
		{name: "a fixture the test asserts against", tool: "Write",
			input: Input{FilePath: "internal/store/testdata/rows.json"}, refused: true},
		{name: "another ecosystem's spelling", tool: "Write",
			input: Input{FilePath: "web/src/basket.spec.ts"}, refused: true},
		{name: "python's spelling", tool: "Write",
			input: Input{FilePath: "api/test_basket.py"}, refused: true},

		// Building, which is the work.
		{name: "writing the code under test", tool: "Write",
			input: Input{FilePath: "internal/job/build.go"}},
		{name: "editing a file whose name merely holds the word", tool: "Edit",
			input: Input{FilePath: "internal/job/latest.go"}},
		{name: "a file about testing that is not a test", tool: "Write",
			input: Input{FilePath: "docs/TESTING.md"}},

		// Reading a test, which this boundary allows on purpose.
		{name: "reading a test with cat", tool: "Bash",
			input: Input{Command: "cat internal/job/build_test.go"}},
		{name: "reading part of a test", tool: "Bash",
			input: Input{Command: "sed -n '1,40p' internal/job/build_test.go"}},
		{name: "searching the tests", tool: "Bash",
			input: Input{Command: "grep -rn TestBuild features/ internal/"}},
		{name: "running the tests", tool: "Bash",
			input: Input{Command: "go test -count=1 ./internal/job/ -run TestBuild"}},
		{name: "a commit message that holds the word test", tool: "Bash",
			input: Input{Command: `git commit -m "make the failing test pass"`}},

		// Writing a test through the shell, in the shapes a session reaches for next.
		{name: "a redirect into a test", tool: "Bash",
			input:   Input{Command: "echo 'func TestNothing(t *testing.T) {}' > internal/job/build_test.go"},
			refused: true},
		{name: "appending to a test", tool: "Bash",
			input: Input{Command: "cat extra >> internal/job/build_test.go"}, refused: true},
		{name: "an in place edit", tool: "Bash",
			input: Input{Command: "sed -i 's/want 3/want 2/' internal/job/build_test.go"}, refused: true},
		{name: "perl in place", tool: "Bash",
			input: Input{Command: "perl -pi -e 's/3/2/' features/build_steps_test.go"}, refused: true},
		{name: "moving a test out of the way", tool: "Bash",
			input: Input{Command: "mv internal/job/build_test.go /tmp/aside"}, refused: true},
		{name: "deleting a test", tool: "Bash",
			input: Input{Command: "rm -f internal/job/build_test.go"}, refused: true},
		{name: "copying over a test", tool: "Bash",
			input: Input{Command: "cp /tmp/mine internal/job/build_test.go"}, refused: true},
		{name: "teeing into a test", tool: "Bash",
			input: Input{Command: "echo x | tee internal/job/build_test.go"}, refused: true},
		{name: "restoring a test from another revision", tool: "Bash",
			input: Input{Command: "git checkout origin/main -- internal/job/build_test.go"}, refused: true},
		{name: "under a shell of its own", tool: "Bash",
			input: Input{Command: `bash -c "rm internal/job/build_test.go"`}, refused: true},
		{name: "under sudo", tool: "Bash",
			input: Input{Command: "sudo rm internal/job/build_test.go"}, refused: true},
		{name: "second on the line", tool: "Bash",
			input: Input{Command: "go build ./... && rm features/build.feature"}, refused: true},
		{name: "inside a loop", tool: "Bash",
			input: Input{Command: "for f in a b; do rm internal/job/build_test.go; done"}, refused: true},
	}
	for _, line := range lines {
		t.Run(line.name, func(t *testing.T) {
			refusal, refused := Decide(line.tool, line.input, true)
			if refused != line.refused {
				t.Fatalf("refused=%v, want %v: %s", refused, line.refused, refusal)
			}
			if !refused {
				return
			}
			// Both halves, every time. A refusal that does not name the file leaves the session guessing
			// which of its edits was stopped, and one that does not name the way through leaves it trying
			// the next spelling of the same thing.
			if said := refusal.String(); !strings.Contains(said, "say so in your answer") {
				t.Fatalf("the refusal does not say what to do instead: %s", said)
			}
		})
	}
}

// The same writes, with the gate off. Nothing here is refused, because the stage before this one
// writes the tests and a gate that refused every session would refuse the worker that fills the suite.
func TestASessionThatIsNotBuildingIsRefusedNothing(t *testing.T) {
	writes := []struct {
		tool  string
		input Input
	}{
		{tool: "Write", input: Input{FilePath: "internal/job/build_test.go"}},
		{tool: "Edit", input: Input{FilePath: "features/build.feature"}},
		{tool: "Bash", input: Input{Command: "rm internal/job/build_test.go"}},
		{tool: "Bash", input: Input{Command: "sed -i 's/a/b/' features/build_steps_test.go"}},
	}
	for _, one := range writes {
		if refusal, refused := Decide(one.tool, one.input, false); refused {
			t.Fatalf("a session that is not building was refused %v: %s", one.input, refusal)
		}
	}
}

// The variable is the boundary, so a session that sets it decides its own boundary. Refused whether
// the gate is on or off, because the shape it is reached for in is a session that is under it.
func TestASessionCannotSetTheVariableItself(t *testing.T) {
	lines := []string{
		"KREWE_BUILDING= go test ./...",
		"export KREWE_BUILDING=",
		"unset KREWE_BUILDING",
		"env -u KREWE_BUILDING rm internal/job/build_test.go",
		"KREWE_BUILDING=1 rm internal/job/build_test.go",
	}
	for _, line := range lines {
		for _, building := range []bool{true, false} {
			refusal, refused := Decide("Bash", Input{Command: line}, building)
			if !refused {
				t.Fatalf("%q was allowed with building=%v", line, building)
			}
			if !strings.Contains(refusal.String(), Building) {
				t.Fatalf("the refusal does not name the variable: %s", refusal)
			}
		}
	}
}

// The refusal names the file and says why the file is read as a test. A session told only that
// something is a test argues with the verdict; one told the rule knows which of its files it covers.
func TestTheRefusalNamesTheFileAndTheRule(t *testing.T) {
	refusal, refused := Decide("Edit", Input{FilePath: "internal/job/build_test.go"}, true)
	if !refused {
		t.Fatal("editing a test was allowed")
	}
	said := refusal.String()
	for _, want := range []string{"internal/job/build_test.go", "_test.go", "read", "name the file"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the refusal does not carry %q: %s", want, said)
		}
	}
}
