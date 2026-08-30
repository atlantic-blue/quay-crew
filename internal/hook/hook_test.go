package hook_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/hook"
)

// A hook is imported by somebody who did not write it, and the only moment anybody is looking at it
// is the import. Everything a hook can be wrong about has to be refused there, by name, because the
// failure mode afterwards is silence: a constraint that never fires is indistinguishable from a
// constraint that approves of everything.

// manifest is a well formed hook, which each case then breaks in exactly one way.
func files(manifest string, extra ...hook.File) []hook.File {
	out := []hook.File{
		{Path: "hook.yaml", Body: []byte(manifest)},
		{Path: "bin/hook", Body: []byte("#!/bin/sh\nexit 0\n"), Executable: true},
	}
	return append(out, extra...)
}

const good = `
name: prompt-analyser
version: 1
summary: Reads every message and hands the session a short brief beside it.
events:
  - on: UserPromptSubmit
    entry: bin/hook
    timeoutSeconds: 20
binaries:
  - node
`

func TestAHookReadsOutOfItsFiles(t *testing.T) {
	read, err := hook.FromFiles(files(good))
	if err != nil {
		t.Fatalf("FromFiles: %v", err)
	}
	if read.Name != "prompt-analyser" || read.Version != 1 {
		t.Fatalf("bad hook: %+v", read)
	}
	if len(read.Events) != 1 {
		t.Fatalf("want one binding, got %d", len(read.Events))
	}
	binding := read.Events[0]
	if binding.On != "UserPromptSubmit" || binding.Entry != "bin/hook" || binding.TimeoutSeconds != 20 {
		t.Fatalf("bad binding: %+v", binding)
	}
	if len(read.Binaries) != 1 || read.Binaries[0] != "node" {
		t.Fatalf("the declared binaries did not survive: %+v", read.Binaries)
	}
}

// Almost every hook has exactly one entry point, so the common case says nothing.
func TestABindingThatNamesNoEntryRunsTheDefaultOne(t *testing.T) {
	read, err := hook.FromFiles(files(`
name: quiet
version: 1
summary: fires on everything and says nothing
events:
  - on: UserPromptSubmit
`))
	if err != nil {
		t.Fatalf("FromFiles: %v", err)
	}
	if read.Events[0].Entry != hook.DefaultEntry {
		t.Fatalf("want the default entry %q, got %q", hook.DefaultEntry, read.Events[0].Entry)
	}
}

// This is the one that matters most. A misspelled event is accepted by anything that does not check,
// and then the hook is imported, attached, mounted, and never called. Nothing anywhere says so.
func TestAnEventTheRuntimeNeverRaisesIsRefusedByName(t *testing.T) {
	_, err := hook.FromFiles(files(`
name: guard
version: 1
summary: refuses a commit without approval
events:
  - on: PreToolUseHook
    entry: bin/hook
`))
	if err == nil {
		t.Fatal("an event the runtime never raises was accepted, so the hook would never be called")
	}
	if !strings.Contains(err.Error(), "PreToolUseHook") {
		t.Fatalf("the refusal does not name the event: %v", err)
	}
	// The refusal has to say what is allowed, or the next move is a search through source.
	if !strings.Contains(err.Error(), "UserPromptSubmit") {
		t.Fatalf("the refusal does not say which events exist: %v", err)
	}
}

func TestAHookThatFiresOnNothingIsRefused(t *testing.T) {
	_, err := hook.FromFiles(files(`
name: idle
version: 1
summary: does nothing at all
events: []
`))
	if err == nil {
		t.Fatal("a hook that fires on nothing was accepted")
	}
}

// A matcher only means something on the two events that fire per tool. Anywhere else it reads as
// configured and does nothing.
func TestAMatcherOnAnEventThatDoesNotFirePerToolIsRefused(t *testing.T) {
	_, err := hook.FromFiles(files(`
name: guard
version: 1
summary: refuses a commit without approval
events:
  - on: UserPromptSubmit
    matcher: Bash
    entry: bin/hook
`))
	if err == nil {
		t.Fatal("a matcher was accepted on an event that does not fire per tool")
	}
	if !strings.Contains(err.Error(), "PreToolUse") {
		t.Fatalf("the refusal does not say where a matcher works: %v", err)
	}
}

func TestAMatcherIsKeptOnAnEventThatFiresPerTool(t *testing.T) {
	read, err := hook.FromFiles(files(`
name: guard
version: 1
summary: refuses a commit without approval
events:
  - on: PreToolUse
    matcher: Bash
    entry: bin/hook
`))
	if err != nil {
		t.Fatalf("FromFiles: %v", err)
	}
	if read.Events[0].Matcher != "Bash" {
		t.Fatalf("the matcher did not survive: %q", read.Events[0].Matcher)
	}
}

func TestAnEntryTheHookDoesNotCarryIsRefused(t *testing.T) {
	_, err := hook.FromFiles(files(`
name: guard
version: 1
summary: refuses a commit without approval
events:
  - on: UserPromptSubmit
    entry: bin/missing
`))
	if err == nil {
		t.Fatal("an entry point the hook does not carry was accepted")
	}
	if !strings.Contains(err.Error(), "bin/missing") {
		t.Fatalf("the refusal does not name the file: %v", err)
	}
}

// The bit is the whole reason files carry more than a path and a body. Without it the hook fails
// inside a container, and nothing there points back at the import.
func TestAnEntryThatIsNotExecutableIsRefused(t *testing.T) {
	_, err := hook.FromFiles([]hook.File{
		{Path: "hook.yaml", Body: []byte(good)},
		{Path: "bin/hook", Body: []byte("#!/bin/sh\nexit 0\n")},
	})
	if err == nil {
		t.Fatal("an entry point with no executable bit was accepted")
	}
	if !strings.Contains(err.Error(), "executable") {
		t.Fatalf("the refusal does not say what is wrong: %v", err)
	}
}

// A hook is written by one person and imported by another, so a path that climbs out of the
// directory writes wherever the system happens to be standing.
func TestAnEntryThatClimbsOutOfTheDirectoryIsRefused(t *testing.T) {
	for _, entry := range []string{"../escape", "/etc/passwd", "bin/../../escape"} {
		_, err := hook.FromFiles(files(`
name: guard
version: 1
summary: refuses a commit without approval
events:
  - on: UserPromptSubmit
    entry: ` + entry + `
`))
		if err == nil {
			t.Fatalf("entry %q was accepted, and it does not stay inside the hook's directory", entry)
		}
	}
}

func TestAFileThatClimbsOutOfTheDirectoryIsRefused(t *testing.T) {
	_, err := hook.FromFiles(files(good, hook.File{Path: "../escape", Body: []byte("x")}))
	if err == nil {
		t.Fatal("a file outside the hook's directory was accepted")
	}
}

// The same rule a skill lives under: the system's own configuration and the model's token are not
// something content asks for.
func TestAHookCannotAskForTheSystemsOwnSecrets(t *testing.T) {
	for _, name := range []string{"QC_SANDBOX_SECRETS", "CLAUDE_CODE_OAUTH_TOKEN"} {
		_, err := hook.FromFiles(files(`
name: greedy
version: 1
summary: wants what it should not have
events:
  - on: UserPromptSubmit
    entry: bin/hook
secrets:
  ` + name + `: the system's own
`))
		if err == nil {
			t.Fatalf("a hook was allowed to name %s", name)
		}
	}
}

func TestASecretWithNothingSayingWhatItIsIsRefused(t *testing.T) {
	_, err := hook.FromFiles(files(`
name: guard
version: 1
summary: refuses a commit without approval
events:
  - on: UserPromptSubmit
    entry: bin/hook
secrets:
  GH_TOKEN: ""
`))
	if err == nil {
		t.Fatal("a secret with no explanation was accepted, so a refusal could not say what to go and get")
	}
}

// Ignored, an unknown field looks configured and does nothing, which sends whoever wrote it looking
// somewhere else entirely.
func TestAManifestFieldTheSystemDoesNotKnowIsRefusedByName(t *testing.T) {
	_, err := hook.FromFiles(files(`
name: guard
version: 1
summary: refuses a commit without approval
events:
  - on: UserPromptSubmit
    entry: bin/hook
blocking: true
`))
	if err == nil {
		t.Fatal("an unknown manifest field was ignored rather than refused")
	}
	if !strings.Contains(err.Error(), "blocking") {
		t.Fatalf("the refusal does not name the field: %v", err)
	}
}

func TestAHookWithoutTheThingsEveryHookNeedsIsRefused(t *testing.T) {
	cases := map[string]string{
		"no name": `
version: 1
summary: something
events:
  - on: UserPromptSubmit
    entry: bin/hook
`,
		"a name that is not a directory name": `
name: Prompt Analyser
version: 1
summary: something
events:
  - on: UserPromptSubmit
    entry: bin/hook
`,
		"no version": `
name: guard
summary: something
events:
  - on: UserPromptSubmit
    entry: bin/hook
`,
		"no summary": `
name: guard
version: 1
events:
  - on: UserPromptSubmit
    entry: bin/hook
`,
		"a summary of more than one line": `
name: guard
version: 1
summary: "one\ntwo"
events:
  - on: UserPromptSubmit
    entry: bin/hook
`,
	}
	for what, manifest := range cases {
		t.Run(what, func(t *testing.T) {
			if _, err := hook.FromFiles(files(manifest)); err == nil {
				t.Fatalf("a hook with %s was accepted", what)
			}
		})
	}
}

func TestASummaryLongerThanTheLimitIsRefused(t *testing.T) {
	_, err := hook.FromFiles(files(`
name: guard
version: 1
summary: ` + strings.Repeat("a", hook.SummaryLimit+1) + `
events:
  - on: UserPromptSubmit
    entry: bin/hook
`))
	if err == nil {
		t.Fatal("a summary over the limit was accepted")
	}
}

func TestFilesWithNoManifestAreNotAHook(t *testing.T) {
	_, err := hook.FromFiles([]hook.File{{Path: "bin/hook", Body: []byte("x"), Executable: true}})
	if err == nil {
		t.Fatal("files with no manifest were read as a hook")
	}
}

// Reading from a directory.

func writeHook(t *testing.T, root, name, manifest string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hook.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "hook"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReadsEveryHookInADirectorySortedByName(t *testing.T) {
	root := t.TempDir()
	writeHook(t, root, "zulu", strings.Replace(good, "prompt-analyser", "zulu", 1))
	writeHook(t, root, "alpha", strings.Replace(good, "prompt-analyser", "alpha", 1))

	loaded, err := hook.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 2 || loaded[0].Name != "alpha" || loaded[1].Name != "zulu" {
		t.Fatalf("want alpha then zulu, got %+v", loaded)
	}
	// The executable bit has to survive the read, or the entry point is refused at the far end.
	for _, one := range loaded {
		for _, file := range one.Files {
			if file.Path == "bin/hook" && !file.Executable {
				t.Fatalf("%s lost the executable bit on its entry point", one.Name)
			}
		}
	}
}

// Notes and a README sit beside hooks without being read as broken ones.
func TestADirectoryWithNoManifestIsPassedOver(t *testing.T) {
	root := t.TempDir()
	writeHook(t, root, "alpha", strings.Replace(good, "prompt-analyser", "alpha", 1))
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}

	loaded, err := hook.Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("want 1 hook, got %d", len(loaded))
	}
}

// A hook that got itself wrong says so. Skipping it silently means a constraint is simply absent
// later, with nothing anywhere saying why.
func TestAHookWithABrokenManifestIsAnErrorRatherThanASkip(t *testing.T) {
	root := t.TempDir()
	writeHook(t, root, "broken", "name: broken\nversion: 0\nsummary: x\nevents: []\n")
	if _, err := hook.Load(root); err == nil {
		t.Fatal("a broken hook was skipped rather than reported")
	}
}

func TestAHookIsTheDirectoryItLivesIn(t *testing.T) {
	root := t.TempDir()
	writeHook(t, root, "elsewhere", good)
	_, err := hook.One(filepath.Join(root, "elsewhere"))
	if err == nil {
		t.Fatal("a hook calling itself something other than its directory was accepted")
	}
	if !strings.Contains(err.Error(), "prompt-analyser") {
		t.Fatalf("the refusal does not name what it calls itself: %v", err)
	}
}

func TestLoadingSomewhereThatDoesNotExistIsNotAnError(t *testing.T) {
	loaded, err := hook.Load(filepath.Join(t.TempDir(), "nowhere"))
	if err != nil {
		t.Fatalf("a missing directory should be no hooks, not an error: %v", err)
	}
	if len(loaded) != 0 {
		t.Fatalf("want no hooks, got %d", len(loaded))
	}
}
