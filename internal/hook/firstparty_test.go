package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The hooks this build ships, in hooks/ at the root of this repository, held to the same rules an
// imported hook answers to. A first party hook that does not load is worse than none: it is the
// example everybody copies, and a constraint the crew believes it seeded.

func TestTheShippedHooksLoad(t *testing.T) {
	hooks, err := Load("../../hooks")
	if err != nil {
		t.Fatalf("loading the shipped hooks: %v", err)
	}
	// A directory that finds nothing reports success just the same, so this suite would prove nothing.
	if len(hooks) == 0 {
		t.Fatal("no hooks found in hooks/, so this test proves nothing")
	}
}

func TestTheShippedPromptAnalyserIsWholeAndRunnable(t *testing.T) {
	hooks, err := Load("../../hooks")
	if err != nil {
		t.Fatalf("loading the shipped hooks: %v", err)
	}
	var analyser *Hook
	for i := range hooks {
		if hooks[i].Name == "prompt-analyser" {
			analyser = &hooks[i]
		}
	}
	if analyser == nil {
		t.Fatal("hooks/ does not hold the prompt analyser")
	}

	if len(analyser.Events) != 1 || analyser.Events[0].On != "UserPromptSubmit" {
		t.Fatalf("the analyser fires on %+v, and it reads a message, so it fires on UserPromptSubmit",
			analyser.Events)
	}
	// It calls the model, which takes as long as the model takes. With no timeout the runtime applies
	// its own, and a message would sit behind a hook nobody bounded.
	if analyser.Events[0].TimeoutSeconds == 0 {
		t.Error("the analyser has no timeout, and it makes a model call on every message")
	}

	// Declared, so a sandbox image without one refuses the session with a sentence rather than the
	// hook failing inside the container with nothing pointing back here.
	for _, needed := range []string{"node", "claude"} {
		found := false
		for _, binary := range analyser.Binaries {
			if binary == needed {
				found = true
			}
		}
		if !found {
			t.Errorf("the analyser does not declare %s, which it cannot run without", needed)
		}
	}

	// Whole: the entry point, the library it imports, and its configuration. A hook missing one of
	// these imports cleanly and dies on its first message.
	carried := map[string]bool{}
	for _, file := range analyser.Files {
		carried[file.Path] = true
	}
	for _, needed := range []string{"bin/hook.ts", "lib/analyser.ts", "hook.config.json"} {
		if !carried[needed] {
			t.Errorf("the analyser does not carry %s, so it would fail on its first message", needed)
		}
	}
}

// The entry point imports its library by a relative path. Left pointing at where it came from, it
// resolves to nothing inside a sandbox, and the hook dies on the first message with a module error.
func TestTheAnalyserImportsTheLibraryItShipsWith(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("../../hooks/prompt-analyser/bin/hook.ts"))
	if err != nil {
		t.Fatalf("read the entry point: %v", err)
	}
	source := string(body)
	if strings.Contains(source, "../bin/lib/") {
		t.Error("the entry point still imports the library from the hub layout, which is not in a sandbox")
	}
	if !strings.Contains(source, `"../lib/analyser.ts"`) {
		t.Error("the entry point does not import the library it ships beside")
	}
	// Run directly by the runtime, by absolute path, so it needs its own interpreter line.
	if !strings.HasPrefix(source, "#!") {
		t.Error("the entry point has no shebang, and the runtime runs it as a command")
	}
}

// Node decides whether to strip types by the file extension, not by the flag. Named bin/hook, this
// entry point was read as plain JavaScript and died on its own type imports with
// "SyntaxError: Unexpected identifier 'AnalysisFacts'". Every test passed: the hook loaded, the
// manifest was valid, the settings bound it, and it failed on the first message inside a container.
//
// So any TypeScript entry point has to end in .ts, whatever the shebang says.
func TestATypeScriptEntryPointIsNamedSoNodeStripsItsTypes(t *testing.T) {
	hooks, err := Load("../../hooks")
	if err != nil {
		t.Fatalf("loading the shipped hooks: %v", err)
	}
	for _, one := range hooks {
		for _, binding := range one.Events {
			body, err := os.ReadFile(filepath.Join("../../hooks", one.Name, binding.Entry))
			if err != nil {
				t.Fatalf("read %s entry %s: %v", one.Name, binding.Entry, err)
			}
			if !strings.Contains(string(body), "strip-types") {
				continue
			}
			if !strings.HasSuffix(binding.Entry, ".ts") {
				t.Errorf("%s runs %q with type stripping and the name does not end in .ts, so node reads it as JavaScript and it dies on its own types",
					one.Name, binding.Entry)
			}
		}
	}
}

// The paths inside a sandbox are not the paths on the operator's machine, which is the whole reason
// this is configuration and not code.
func TestTheAnalyserIsConfiguredForASandboxRatherThanTheOperatorsMachine(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("../../hooks/prompt-analyser/hook.config.json"))
	if err != nil {
		t.Fatalf("read the config: %v", err)
	}
	var config struct {
		SkillDirs   []string `json:"skillDirs"`
		RulesFile   string   `json:"rulesFile"`
		LastRunFile string   `json:"lastRunFile"`
	}
	if err := json.Unmarshal(body, &config); err != nil {
		t.Fatalf("the config is not valid json: %v", err)
	}
	if len(config.SkillDirs) != 1 || config.SkillDirs[0] != "/home/agent/skills" {
		t.Errorf("the analyser looks for skills in %v, and the crew mounts them at /home/agent/skills",
			config.SkillDirs)
	}
	// The crew renders what a session is told into its memory file. There is no RULES.md in a sandbox.
	if !strings.HasPrefix(config.RulesFile, "/home/agent/") {
		t.Errorf("the analyser reads its rules from %q, which is not a path inside a sandbox", config.RulesFile)
	}
	// Everything else in the sandbox is read only or mounted; /tmp is the writable place.
	if !strings.HasPrefix(config.LastRunFile, "/tmp/") {
		t.Errorf("the analyser writes its record to %q, which it may not be able to write",
			config.LastRunFile)
	}
	if strings.Contains(string(body), "~/") {
		t.Error("the config still carries a home relative path from the operator's machine")
	}
}
