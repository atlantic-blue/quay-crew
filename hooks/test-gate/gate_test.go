package main

import (
	"strings"
	"testing"
)

// aRepository answers the way a repository does: these directories hold tests and nothing else does.
// It stands in for the disk so the table below stays a table, and the walk that reads the real disk
// is held to its own cases in walk_test.go.
func aRepository(where string) bool {
	switch where {
	case ".", "internal", "internal/", "internal/session", "internal/session/", "features", "features/",
		"internal/store/testdata":
		return true
	}
	return false
}

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
			input: Input{FilePath: "/repo/internal/session/build_test.go"}, refused: true},
		{name: "editing a test file", tool: "Edit",
			input: Input{FilePath: "internal/session/build_test.go"}, refused: true},
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
		// A tool this gate has never heard of, sending its path under either name. The field is what
		// is read, because a runtime that adds a write tool would otherwise walk past a list of names.
		{name: "a write tool by another name", tool: "Update",
			input: Input{FilePath: "internal/session/build_test.go"}, refused: true},
		{name: "a write tool using the bare path field", tool: "str_replace_editor",
			input: Input{Path: "internal/session/build_test.go"}, refused: true},

		// Building, which is the work.
		{name: "writing the code under test", tool: "Write",
			input: Input{FilePath: "internal/session/build.go"}},
		{name: "editing a file whose name merely holds the word", tool: "Edit",
			input: Input{FilePath: "internal/session/latest.go"}},
		{name: "a file about testing that is not a test", tool: "Write",
			input: Input{FilePath: "docs/TESTING.md"}},

		// Reading a test, which this boundary allows on purpose.
		{name: "reading a test with cat", tool: "Bash",
			input: Input{Command: "cat internal/session/build_test.go"}},
		{name: "reading part of a test", tool: "Bash",
			input: Input{Command: "sed -n '1,40p' internal/session/build_test.go"}},
		{name: "searching the tests", tool: "Bash",
			input: Input{Command: "grep -rn TestBuild features/ internal/"}},
		{name: "running the tests", tool: "Bash",
			input: Input{Command: "go test -count=1 ./internal/session/ -run TestBuild"}},
		{name: "running the whole suite by its target", tool: "Bash",
			input: Input{Command: "make features"}},
		{name: "running the tests of a directory named after them", tool: "Bash",
			input: Input{Command: "go test ./features/"}},
		{name: "a commit message that holds the word test", tool: "Bash",
			input: Input{Command: `git commit -m "make the failing test pass"`}},
		{name: "formatting the code under test", tool: "Bash",
			input: Input{Command: "gofmt -w internal/session/build.go"}},
		{name: "deleting a directory that holds no test", tool: "Bash",
			input: Input{Command: "rm -rf build/"}},
		{name: "listing tests without acting on them", tool: "Bash",
			input: Input{Command: "find . -name '*_test.go'"}},
		{name: "restoring the code under test", tool: "Bash",
			input: Input{Command: "git checkout -- internal/session/build.go"}},

		// Writing a test through the shell, in the shapes a session reaches for next.
		{name: "a redirect into a test", tool: "Bash",
			input:   Input{Command: "echo 'func TestNothing(t *testing.T) {}' > internal/session/build_test.go"},
			refused: true},
		{name: "appending to a test", tool: "Bash",
			input: Input{Command: "cat extra >> internal/session/build_test.go"}, refused: true},
		{name: "an in place edit", tool: "Bash",
			input: Input{Command: "sed -i 's/want 3/want 2/' internal/session/build_test.go"}, refused: true},
		{name: "an in place edit spelled long", tool: "Bash",
			input:   Input{Command: "sed --in-place 's/want 3/want 2/' internal/session/build_test.go"},
			refused: true},
		{name: "an in place edit keeping a copy", tool: "Bash",
			input: Input{Command: "sed -i.bak 's/a/b/' internal/session/build_test.go"}, refused: true},
		{name: "perl in place", tool: "Bash",
			input: Input{Command: "perl -pi -e 's/3/2/' features/build_steps_test.go"}, refused: true},
		{name: "formatting a test back into shape", tool: "Bash",
			input: Input{Command: "gofmt -w internal/session/build_test.go"}, refused: true},
		{name: "moving a test out of the way", tool: "Bash",
			input: Input{Command: "mv internal/session/build_test.go /tmp/aside"}, refused: true},
		{name: "moving the directory of tests away", tool: "Bash",
			input: Input{Command: "mv features /tmp/aside"}, refused: true},
		{name: "deleting a test", tool: "Bash",
			input: Input{Command: "rm -f internal/session/build_test.go"}, refused: true},
		{name: "deleting the directory of tests", tool: "Bash",
			input: Input{Command: "rm -rf features/"}, refused: true},
		{name: "deleting the fixtures a test asserts against", tool: "Bash",
			input: Input{Command: "rm -r internal/store/testdata"}, refused: true},
		{name: "deleting a directory that holds tests", tool: "Bash",
			input: Input{Command: "rm -rf internal/session"}, refused: true},
		{name: "copying over a test", tool: "Bash",
			input: Input{Command: "cp /tmp/mine internal/session/build_test.go"}, refused: true},
		{name: "linking over a test", tool: "Bash",
			input: Input{Command: "ln -sf /tmp/mine internal/session/build_test.go"}, refused: true},
		{name: "teeing into a test", tool: "Bash",
			input: Input{Command: "echo x | tee internal/session/build_test.go"}, refused: true},
		{name: "piping a test into a writer", tool: "Bash",
			input: Input{Command: "echo internal/session/build_test.go | xargs rm"}, refused: true},
		{name: "finding every test and deleting it", tool: "Bash",
			input: Input{Command: "find . -name '*_test.go' -delete"}, refused: true},
		{name: "finding every scenario and running a writer over it", tool: "Bash",
			input:   Input{Command: `find features -name '*.feature' -exec rm {} \;`},
			refused: true},
		{name: "restoring a test from another revision", tool: "Bash",
			input: Input{Command: "git checkout origin/main -- internal/session/build_test.go"}, refused: true},
		{name: "restoring the whole tree", tool: "Bash",
			input: Input{Command: "git checkout -- ."}, refused: true},
		{name: "restoring the tree without the marker", tool: "Bash",
			input: Input{Command: "git checkout ."}, refused: true},
		{name: "restoring a directory of tests", tool: "Bash",
			input: Input{Command: "git restore internal/session/"}, refused: true},
		{name: "stashing everything, tests included", tool: "Bash",
			input: Input{Command: "git stash"}, refused: true},
		{name: "cleaning the untracked tests away", tool: "Bash",
			input: Input{Command: "git clean -fd"}, refused: true},
		{name: "an interpreter writing a test", tool: "Bash",
			input:   Input{Command: `python3 -c "open('internal/session/build_test.go','w').write('')"`},
			refused: true},
		{name: "another interpreter writing a test", tool: "Bash",
			input:   Input{Command: `node -e "require('fs').writeFileSync('features/build.feature','')"`},
			refused: true},
		{name: "under a shell of its own", tool: "Bash",
			input: Input{Command: `bash -c "rm internal/session/build_test.go"`}, refused: true},
		{name: "under sudo", tool: "Bash",
			input: Input{Command: "sudo rm internal/session/build_test.go"}, refused: true},
		{name: "second on the line", tool: "Bash",
			input: Input{Command: "go build ./... && rm features/build.feature"}, refused: true},
		{name: "inside a loop", tool: "Bash",
			input: Input{Command: "for f in a b; do rm internal/session/build_test.go; done"}, refused: true},
		{name: "truncating a test", tool: "Bash",
			input: Input{Command: "truncate -s 0 internal/session/build_test.go"}, refused: true},
		{name: "a program this gate has never heard of", tool: "Bash",
			input: Input{Command: "somewriter --out internal/session/build_test.go"}, refused: true},
		{name: "an editor opened on a test", tool: "Bash",
			input: Input{Command: "vim internal/session/build_test.go"}, refused: true},

		// Content from somewhere the line does not show: an archive, a patch, another commit. What it
		// writes cannot be read off the line at all, so where it lands is read as a directory taken whole.
		{name: "a patch applied over the tree", tool: "Bash",
			input: Input{Command: "git apply /tmp/fix.diff"}, refused: true},
		{name: "a commit taken back over the tree", tool: "Bash",
			input: Input{Command: "git cherry-pick 8f21ac0e"}, refused: true},
		{name: "an archive unpacked over the tree", tool: "Bash",
			input: Input{Command: "tar -xzf /tmp/x.tgz"}, refused: true},
		{name: "an archive unpacked into a directory of tests", tool: "Bash",
			input: Input{Command: "tar -xzf /tmp/x.tgz -C internal/session"}, refused: true},
		{name: "a patch read from a pipe", tool: "Bash",
			input: Input{Command: "patch -p1 < /tmp/fix.diff"}, refused: true},
		{name: "an archive made, which writes no test", tool: "Bash",
			input: Input{Command: "tar -czf /tmp/out.tgz internal/session"}},
	}
	for _, line := range lines {
		t.Run(line.name, func(t *testing.T) {
			refusal, refused := Decide(line.tool, line.input, true, aRepository)
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
		{tool: "Write", input: Input{FilePath: "internal/session/build_test.go"}},
		{tool: "Edit", input: Input{FilePath: "features/build.feature"}},
		{tool: "Bash", input: Input{Command: "rm internal/session/build_test.go"}},
		{tool: "Bash", input: Input{Command: "rm -rf features/"}},
		{tool: "Bash", input: Input{Command: "git checkout -- ."}},
		{tool: "Bash", input: Input{Command: "sed -i 's/a/b/' features/build_steps_test.go"}},
	}
	for _, one := range writes {
		if refusal, refused := Decide(one.tool, one.input, false, aRepository); refused {
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
		"env -u KREWE_BUILDING rm internal/session/build_test.go",
		"KREWE_BUILDING=1 rm internal/session/build_test.go",
	}
	for _, line := range lines {
		for _, building := range []bool{true, false} {
			refusal, refused := Decide("Bash", Input{Command: line}, building, aRepository)
			if !refused {
				t.Fatalf("%q was allowed with building=%v", line, building)
			}
			if !strings.Contains(refusal.String(), Building) {
				t.Fatalf("the refusal does not name the variable: %s", refusal)
			}
		}
	}
}

// A directory is refused for what is in it rather than for its name, so the same command is ordinary
// work in one repository and a boundary crossing in another.
func TestADirectoryIsRefusedForWhatIsInIt(t *testing.T) {
	empty := func(string) bool { return false }
	line := Input{Command: "rm -rf internal/session"}

	if _, refused := Decide("Bash", line, true, aRepository); !refused {
		t.Fatal("a directory holding tests was taken whole")
	}
	if refusal, refused := Decide("Bash", line, true, empty); refused {
		t.Fatalf("a directory holding no test was refused: %s", refusal)
	}
	// And with nothing to ask, the names still answer: a directory named after tests is a test.
	if _, refused := Decide("Bash", Input{Command: "rm -rf features/"}, true, empty); !refused {
		t.Fatal("the directory of scenarios was taken whole while nothing could read the disk")
	}
}

// The refusal names the file and says why the file is read as a test. A session told only that
// something is a test argues with the verdict; one told the rule knows which of its files it covers.
func TestTheRefusalNamesTheFileAndTheRule(t *testing.T) {
	refusal, refused := Decide("Edit", Input{FilePath: "internal/session/build_test.go"}, true, aRepository)
	if !refused {
		t.Fatal("editing a test was allowed")
	}
	said := refusal.String()
	for _, want := range []string{"internal/session/build_test.go", "_test.go", "read", "name the file"} {
		if !strings.Contains(said, want) {
			t.Fatalf("the refusal does not carry %q: %s", want, said)
		}
	}
}
