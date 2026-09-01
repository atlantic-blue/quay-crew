package main

import (
	"encoding/json"
	"errors"
	"testing"
)

// too is prose that breaks the length rule, used wherever a test needs the gate to have something to
// find. Thirty one words.
const too = "The control plane reads the row and answers the question the caller asked, and it does " +
	"that before the session starts, because nothing else reads that row at all."

// What this gate does not look at, and this is the half that decides whether it can be attached at
// all. It fires on every write and every command a session makes, so anything it reads wrongly is
// work it refuses.
func TestTheGateReadsNoProseInWhatIsNotProse(t *testing.T) {
	for _, one := range []struct {
		name  string
		tool  string
		input string
	}{
		{
			// The one that matters most. A Go file is not prose, and a gate that measured sentence
			// length in source would refuse every file in this repository on its first firing.
			name: "a Go file", tool: "Write",
			input: `{"file_path":"internal/hook/hook.go","content":"// ` + too + `"}`,
		},
		{name: "a yaml file", tool: "Write", input: `{"file_path":"deploy/compose.yml","content":"# ` + too + `"}`},
		{name: "a json file", tool: "Edit", input: `{"file_path":"hook.config.json","new_string":"` + too + `"}`},
		{name: "a feature file", tool: "Write", input: `{"file_path":"features/hooks.feature","content":"` + too + `"}`},
		{name: "a file with no extension", tool: "Write", input: `{"file_path":"Makefile","content":"` + too + `"}`},
		{name: "reading a document", tool: "Read", input: `{"file_path":"docs/HOOKS.md"}`},
		{name: "a command that carries no prose", tool: "Bash", input: `{"command":"go test -count=1 ./..."}`},
		{
			name: "a commit message quoted inside a command that is not a commit",
			tool: "Bash", input: `{"command":"echo -m ` + "\\\"" + too + "\\\"" + `"}`,
		},
		{
			// gh api sends -F as a field rather than a file, so reading it as one would send this
			// gate looking for a file that is a key and a value.
			name: "a field passed to gh api",
			tool: "Bash", input: `{"command":"gh api repos/o/r/issues -F title=one"}`,
		},
		{
			name: "a body handed over on standard input, where there is no file to read",
			tool: "Bash", input: `{"command":"gh pr create --body-file -"}`,
		},
		{name: "a payload with no tool input at all", tool: "Bash", input: `{}`},
	} {
		t.Run(one.name, func(t *testing.T) {
			found := Findings(one.tool, json.RawMessage(one.input), refuseToRead)
			if len(found) > 0 {
				t.Errorf("the gate refused something that is not prose for a person: %s", found[0])
			}
		})
	}
}

// Where the prose actually is. Each of these is a way a role in this system hands prose to a person,
// and a shape that goes unread is a rule that does not apply to it.
func TestTheGateFindsTheProseInEachShapeThatCarriesIt(t *testing.T) {
	for _, one := range []struct {
		name  string
		tool  string
		input string
		where string
	}{
		{
			name: "a document being written", tool: "Write", where: "docs/HOOKS.md",
			input: `{"file_path":"docs/HOOKS.md","content":"` + too + `"}`,
		},
		{
			name: "a changelog fragment being edited", tool: "Edit", where: "changelog.d/508-a-gate.md",
			input: `{"file_path":"changelog.d/508-a-gate.md","new_string":"` + too + `"}`,
		},
		{
			name: "one edit of several", tool: "MultiEdit", where: "README.md",
			input: `{"file_path":"README.md","edits":[{"new_string":"fine."},{"new_string":"` + too + `"}]}`,
		},
		{
			name: "a pull request body", tool: "Bash", where: "gh --body",
			input: `{"command":"gh pr create --title \"508: feat: a gate\" --body \"` + too + `\""}`,
		},
		{
			name: "an issue body", tool: "Bash", where: "gh --body",
			input: `{"command":"gh issue create --body \"` + too + `\""}`,
		},
		{
			name: "a pull request comment", tool: "Bash", where: "gh --body",
			input: `{"command":"gh pr comment 12 --body \"` + too + `\""}`,
		},
		{
			name: "a commit message", tool: "Bash", where: "git -m",
			input: `{"command":"git commit -m \"` + too + `\""}`,
		},
		{
			name: "a commit message written with the joined form", tool: "Bash", where: "git --message",
			input: `{"command":"git commit --message=\"` + too + `\""}`,
		},
		{
			name: "prose inside a shell handed a command line of its own", tool: "Bash", where: "gh --body",
			input: `{"command":"bash -c \"gh pr create --body '` + too + `'\""}`,
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			found := Findings(one.tool, json.RawMessage(one.input), refuseToRead)
			if len(found) == 0 {
				t.Fatalf("the gate read no prose here, so this shape is not held to the standard")
			}
			if found[0].Where != one.where {
				t.Errorf("the refusal is about %q, and it came from %q", found[0].Where, one.where)
			}
		})
	}
}

// The same prose handed over as a file rather than as an argument. A rule that a body typed on the
// command line has to meet and a body in a file does not is a rule with a way around it.
func TestTheGateReadsABodyHandedOverAsAFile(t *testing.T) {
	read := func(name string) ([]byte, error) {
		if name != "/w/body.md" {
			return nil, errors.New("no such file")
		}
		return []byte(too), nil
	}
	found := Findings("Bash", json.RawMessage(`{"command":"gh pr create --body-file /w/body.md"}`), read)
	if len(found) == 0 {
		t.Fatal("the gate did not read the body file, so a role gets its prose through by writing it to one")
	}
	if found[0].Where != "/w/body.md" {
		t.Errorf("the refusal is about %q, and the prose is in /w/body.md", found[0].Where)
	}
}

// A file that cannot be read is prose the gate did not see, and it goes through. A gate that refuses
// what it cannot read refuses the work.
func TestAFileTheGateCannotReadGoesThrough(t *testing.T) {
	found := Findings("Bash", json.RawMessage(`{"command":"gh pr create --body-file /w/gone.md"}`), refuseToRead)
	if len(found) > 0 {
		t.Errorf("the gate refused a command because it could not read a file: %s", found[0])
	}
}

func TestWhichFilesAreProse(t *testing.T) {
	for path, want := range map[string]bool{
		"docs/HOOKS.md":      true,
		"README.MD":          true,
		"notes.txt":          true,
		"docs/index.rst":     true,
		"internal/a.go":      false,
		"deploy/env.yml":     false,
		"hook.yaml":          false,
		"features/a.feature": false,
		"Makefile":           false,
	} {
		if IsProse(path) != want {
			t.Errorf("IsProse(%q) is %t, want %t", path, IsProse(path), want)
		}
	}
}

func refuseToRead(string) ([]byte, error) { return nil, errors.New("nothing reads a file here") }
