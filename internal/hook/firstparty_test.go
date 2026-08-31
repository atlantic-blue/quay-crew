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
// example everybody copies, and a constraint the system believes it seeded.
//
// The entry points are built rather than committed, so `make hooks` comes first. Every failure here
// that names a missing entry point means that step was skipped.

func TestTheShippedHooksLoad(t *testing.T) {
	hooks, err := Load("../../hooks")
	if err != nil {
		t.Fatalf("loading the shipped hooks: %v (run `make hooks` first: the entry points are built)", err)
	}
	// A directory that finds nothing reports success just the same, so this suite would prove nothing.
	if len(hooks) == 0 {
		t.Fatal("no hooks found in hooks/, so this test proves nothing")
	}
}

func TestTheShippedPromptAnalyserIsWholeAndRunnable(t *testing.T) {
	analyser := shipped(t, "prompt-analyser")

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
	// hook failing inside the container with nothing pointing back here. The hook is a compiled
	// binary, so the only command it cannot work without is the one it shells out to.
	if len(analyser.Binaries) != 1 || analyser.Binaries[0] != "claude" {
		t.Errorf("the analyser declares %v, and it needs claude and nothing else", analyser.Binaries)
	}

	// Whole: the entry point and its configuration. A hook missing either imports cleanly and dies on
	// its first message.
	carried := map[string]bool{}
	for _, file := range analyser.Files {
		carried[file.Path] = true
	}
	for _, needed := range []string{"bin/hook", "hook.config.json"} {
		if !carried[needed] {
			t.Errorf("the analyser does not carry %s, so it would fail on its first message", needed)
		}
	}
}

// The way off the runtime the hook used to need.
//
// It was TypeScript run by node, and node is gone from the image's reach as far as this hook is
// concerned. A hook that still shipped a .ts entry point beside a binary would run neither reliably,
// and the two would disagree about what the hook does.
func TestTheAnalyserCarriesNoneOfTheTypeScriptItReplaced(t *testing.T) {
	analyser := shipped(t, "prompt-analyser")

	for _, file := range analyser.Files {
		if strings.HasSuffix(file.Path, ".ts") {
			t.Errorf("the analyser still carries %s, and it is a compiled binary now", file.Path)
		}
	}
	for _, binary := range analyser.Binaries {
		if binary == "node" {
			t.Error("the analyser still declares node, which it no longer runs on")
		}
	}
}

// A hook is a plugin: somebody reviews it, versions it and hands it to another system. It does not
// share the system's dependencies and it cannot import the system's internals, and its own module is
// what enforces both.
func TestEachShippedHookIsItsOwnModuleWithNothingBehindIt(t *testing.T) {
	hooks, err := Load("../../hooks")
	if err != nil {
		t.Fatalf("loading the shipped hooks: %v", err)
	}
	for _, one := range hooks {
		body, err := os.ReadFile(filepath.Join("../../hooks", one.Name, "go.mod"))
		if err != nil {
			t.Errorf("%s has no go.mod, so it is part of the system rather than a plugin: %v", one.Name, err)
			continue
		}
		if strings.Contains(string(body), "require") {
			t.Errorf("%s depends on something outside the standard library, which a sandbox has to carry: %s",
				one.Name, body)
		}
	}
}

// The entry point is what the runtime executes by absolute path. It has to exist, it has to be
// runnable, and it has to be the built thing rather than a source file somebody committed by mistake.
func TestEveryShippedEntryPointIsABuiltExecutable(t *testing.T) {
	hooks, err := Load("../../hooks")
	if err != nil {
		t.Fatalf("loading the shipped hooks: %v", err)
	}
	for _, one := range hooks {
		for _, binding := range one.Events {
			at := filepath.Join("../../hooks", one.Name, binding.Entry)
			info, err := os.Stat(at)
			if err != nil {
				t.Errorf("%s runs %s and it is not there; run `make hooks`: %v", one.Name, binding.Entry, err)
				continue
			}
			if info.Mode().Perm()&0o111 == 0 {
				t.Errorf("%s runs %s and it is not executable, so it would fail inside a container",
					one.Name, binding.Entry)
			}
			// A shell script or a source file here would mean the sandbox needs an interpreter the
			// manifest never declared.
			body, err := os.ReadFile(at)
			if err != nil {
				t.Errorf("read %s: %v", at, err)
				continue
			}
			if strings.HasPrefix(string(body), "#!") {
				t.Errorf("%s runs %s and it starts with an interpreter line, so it is a script rather than the built binary",
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
		t.Errorf("the analyser looks for skills in %v, and the system mounts them at /home/agent/skills",
			config.SkillDirs)
	}
	// The system renders what a session is told into its memory file. There is no RULES.md in a sandbox.
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

// shipped is one hook out of the directory this build ships, or a failure naming what is missing.
func shipped(t *testing.T, name string) Hook {
	t.Helper()
	hooks, err := Load("../../hooks")
	if err != nil {
		t.Fatalf("loading the shipped hooks: %v (run `make hooks` first)", err)
	}
	for _, one := range hooks {
		if one.Name == name {
			return one
		}
	}
	t.Fatalf("hooks/ does not hold %s", name)
	return Hook{}
}

// The merge gate is bound where it can see the command it refuses.
//
// Its whole job is to read a shell command before it runs, so a binding on any other event, or on
// any other tool, is a hook that is never called on the thing it exists to stop. That failure is
// silent: the manifest validates, the settings render, the mount is right, and the gate approves of
// everything because it never fires.
func TestTheShippedMergeGateFiresWhereACommandIsAboutToRun(t *testing.T) {
	gate := shipped(t, "merge-gate")

	if len(gate.Events) != 1 {
		t.Fatalf("the merge gate fires on %d events, and it reads one thing: a command about to run",
			len(gate.Events))
	}
	binding := gate.Events[0]
	if binding.On != "PreToolUse" {
		t.Errorf("the merge gate fires on %q, and a refusal after the merge has run is not a gate",
			binding.On)
	}
	if binding.Matcher != "Bash" {
		t.Errorf("the merge gate matches %q, and the command it refuses is run with Bash", binding.Matcher)
	}
	// It reads a string and answers. There is nothing for it to wait on, so a session waiting on the
	// runtime's own default would be waiting on a bug.
	if binding.TimeoutSeconds == 0 {
		t.Error("the merge gate has no timeout, and it fires on every command a session runs")
	}
	// It shells out to nothing, so a declared binary would be a requirement the image has to meet
	// for no reason, and a missing one refuses every session in the workspace.
	if len(gate.Binaries) != 0 {
		t.Errorf("the merge gate declares %v, and it runs nothing but itself", gate.Binaries)
	}
	// A gate that reads a credential is a gate with something to lose.
	if len(gate.Secrets) != 0 {
		t.Errorf("the merge gate names %v, and it decides from the command alone", gate.Secrets)
	}
}

// The deploy identity gate is bound where it can see the command it refuses, on the same argument as
// the merge gate above: it reads a command line before it runs, so any other event or any other tool
// is a hook that never fires on the thing it exists to stop.
//
// What it declares matters more here than for either of the others, because it is seeded to the whole
// system. A binary it names is a binary every sandbox image has to carry, and a secret it names is a
// secret every workspace has to set, or the constraint is missing from exactly the sessions it exists
// for. The deploy-identity skill was written that way for the same reason.
func TestTheShippedDeployIdentityGateFiresWhereACommandIsAboutToRun(t *testing.T) {
	gate := shipped(t, "deploy-identity-gate")

	if len(gate.Events) != 1 {
		t.Fatalf("the deploy identity gate fires on %d events, and it reads one thing: a command about to run",
			len(gate.Events))
	}
	binding := gate.Events[0]
	if binding.On != "PreToolUse" {
		t.Errorf("the deploy identity gate fires on %q, and a refusal after the pull request is open is not a gate",
			binding.On)
	}
	if binding.Matcher != "Bash" {
		t.Errorf("the deploy identity gate matches %q, and the command it refuses is run with Bash", binding.Matcher)
	}
	// It reads the change with git, which is a process rather than a string comparison, so a session
	// waiting on the runtime's own default would be waiting longer than it has to.
	if binding.TimeoutSeconds == 0 {
		t.Error("the deploy identity gate has no timeout, and it fires on every command a session runs")
	}
	// git is used where it is there and the gate answers with no files where it is not, so declaring
	// it would refuse every session on an image that lags for a check that degrades safely anyway.
	if len(gate.Binaries) != 0 {
		t.Errorf("the deploy identity gate declares %v, and one missing binary would take the rule out of every session",
			gate.Binaries)
	}
	// A workspace whose pipeline authenticates by federated identity holds no cloud credential, and it
	// is exactly the workspace this gate exists for.
	if len(gate.Secrets) != 0 {
		t.Errorf("the deploy identity gate names %v, and it decides from the command and the change alone",
			gate.Secrets)
	}
	// Whole: the entry point. A hook missing it imports cleanly and dies on its first command.
	carried := false
	for _, file := range gate.Files {
		if file.Path == "bin/hook" {
			carried = true
		}
	}
	if !carried {
		t.Error("the deploy identity gate carries no bin/hook, so it would fail on its first command")
	}
}
