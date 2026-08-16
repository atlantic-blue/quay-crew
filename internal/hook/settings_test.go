package hook_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/hook"
)

// The settings file is the only thing that makes a hook run. Everything else about a hook can be
// right while this is wrong, and the failure is silent: the runtime reads a file that binds nothing
// and every constraint the crew believes it has is off.

func settingsOf(t *testing.T, hooks ...hook.Hook) map[string]any {
	t.Helper()
	rendered, err := hook.Settings("/home/agent/hooks", hooks)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	var read map[string]any
	if err := json.Unmarshal(rendered, &read); err != nil {
		t.Fatalf("the rendered settings are not valid json: %v\n%s", err, rendered)
	}
	return read
}

func one(name string, bindings ...hook.Binding) hook.Hook {
	return hook.Hook{Name: name, Version: 1, Summary: "x", Events: bindings}
}

func TestAHookIsBoundToItsEventWithAnAbsolutePathIntoTheSandbox(t *testing.T) {
	read := settingsOf(t, one("prompt-analyser",
		hook.Binding{On: "UserPromptSubmit", Entry: "bin/hook", TimeoutSeconds: 20}))

	hooks, ok := read["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("no hooks in the settings: %+v", read)
	}
	groups, ok := hooks["UserPromptSubmit"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("want one group on UserPromptSubmit, got %+v", hooks)
	}
	group := groups[0].(map[string]any)
	actions := group["hooks"].([]any)
	if len(actions) != 1 {
		t.Fatalf("want one command, got %+v", actions)
	}
	run := actions[0].(map[string]any)
	if run["type"] != "command" {
		t.Errorf("the runtime only runs commands, got type %v", run["type"])
	}
	// The path is inside the container, not on the host. A host path here runs nothing and says
	// nothing about why.
	if run["command"] != "/home/agent/hooks/prompt-analyser/bin/hook" {
		t.Errorf("the command is %v, want the entry point inside the sandbox", run["command"])
	}
	if run["timeout"] != float64(20) {
		t.Errorf("the timeout is %v, want 20", run["timeout"])
	}
}

// Told to wait no time at all, the runtime would apply zero rather than its own default.
func TestAHookWithNoTimeoutLeavesTheFieldOutRatherThanSendingZero(t *testing.T) {
	rendered, err := hook.Settings("/home/agent/hooks",
		[]hook.Hook{one("guard", hook.Binding{On: "Stop", Entry: "bin/hook"})})
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if strings.Contains(string(rendered), "timeout") {
		t.Fatalf("a hook with no timeout sent one anyway:\n%s", rendered)
	}
}

// A matcher on an event that fires per tool is what makes a gate specific. Losing it makes the hook
// fire on every tool, which is a hook that refuses things nobody asked it to.
func TestAMatcherReachesTheSettings(t *testing.T) {
	read := settingsOf(t, one("git-approval",
		hook.Binding{On: "PreToolUse", Matcher: "Bash", Entry: "bin/hook"}))

	group := read["hooks"].(map[string]any)["PreToolUse"].([]any)[0].(map[string]any)
	if group["matcher"] != "Bash" {
		t.Fatalf("the matcher is %v, want Bash", group["matcher"])
	}
}

// An empty matcher means every tool, and the runtime reads an absent field that way. Writing an
// empty string instead would be a pattern that matches a tool called "".
func TestAnEmptyMatcherIsLeftOutRatherThanWrittenEmpty(t *testing.T) {
	rendered, err := hook.Settings("/home/agent/hooks",
		[]hook.Hook{one("guard", hook.Binding{On: "PreToolUse", Entry: "bin/hook"})})
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if strings.Contains(string(rendered), "matcher") {
		t.Fatalf("an empty matcher was written out:\n%s", rendered)
	}
}

// Two hooks guarding the same thing is the normal case, not the exception: refusing a commit and
// checking the message are separate hooks on one event.
func TestTwoHooksOnOneEventAndMatcherShareAGroup(t *testing.T) {
	read := settingsOf(t,
		one("git-approval", hook.Binding{On: "PreToolUse", Matcher: "Bash", Entry: "bin/hook"}),
		one("no-force-push", hook.Binding{On: "PreToolUse", Matcher: "Bash", Entry: "bin/gate"}),
	)
	groups := read["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(groups) != 1 {
		t.Fatalf("want the two hooks in one group, got %d groups", len(groups))
	}
	actions := groups[0].(map[string]any)["hooks"].([]any)
	if len(actions) != 2 {
		t.Fatalf("want two commands in the group, got %d", len(actions))
	}
	if actions[0].(map[string]any)["command"] != "/home/agent/hooks/git-approval/bin/hook" {
		t.Errorf("the hooks are out of the order they were written: %+v", actions)
	}
}

func TestTwoMatchersOnOneEventAreSeparateGroups(t *testing.T) {
	read := settingsOf(t,
		one("git-approval", hook.Binding{On: "PreToolUse", Matcher: "Bash", Entry: "bin/hook"}),
		one("no-em-dash", hook.Binding{On: "PreToolUse", Matcher: "Write|Edit", Entry: "bin/hook"}),
	)
	if groups := read["hooks"].(map[string]any)["PreToolUse"].([]any); len(groups) != 2 {
		t.Fatalf("want a group per matcher, got %d", len(groups))
	}
}

func TestAHookFiringOnTwoEventsReachesBoth(t *testing.T) {
	read := settingsOf(t, one("watcher",
		hook.Binding{On: "SessionStart", Entry: "bin/hook"},
		hook.Binding{On: "Stop", Entry: "bin/hook"},
	))
	hooks := read["hooks"].(map[string]any)
	if len(hooks) != 2 {
		t.Fatalf("want two events bound, got %+v", hooks)
	}
}

// A file that reorders itself between renders is a diff nobody can review, and it rewrites on every
// task for no reason.
func TestTheSameHooksRenderTheSameFileEveryTime(t *testing.T) {
	hooks := []hook.Hook{
		one("git-approval", hook.Binding{On: "PreToolUse", Matcher: "Bash", Entry: "bin/hook"}),
		one("prompt-analyser", hook.Binding{On: "UserPromptSubmit", Entry: "bin/hook"}),
		one("no-em-dash", hook.Binding{On: "PreToolUse", Matcher: "Write", Entry: "bin/hook"}),
	}
	first, err := hook.Settings("/home/agent/hooks", hooks)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	for range 20 {
		again, err := hook.Settings("/home/agent/hooks", hooks)
		if err != nil {
			t.Fatalf("Settings: %v", err)
		}
		if string(again) != string(first) {
			t.Fatalf("the same hooks rendered two different files:\n%s\n---\n%s", first, again)
		}
	}
}

// A session holding no hooks still gets a valid document. An empty file, or a broken one, is a
// settings file the runtime refuses, and then nothing runs at all.
func TestNoHooksStillRendersSomethingTheRuntimeCanRead(t *testing.T) {
	rendered, err := hook.Settings("/home/agent/hooks", nil)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	var read map[string]any
	if err := json.Unmarshal(rendered, &read); err != nil {
		t.Fatalf("an empty set rendered something unreadable: %v\n%s", err, rendered)
	}
}

func TestRenderingWithNowhereToMountIsRefused(t *testing.T) {
	if _, err := hook.Settings("", []hook.Hook{one("guard",
		hook.Binding{On: "Stop", Entry: "bin/hook"})}); err == nil {
		t.Fatal("settings rendered with no root, so every command would be a relative path")
	}
}
