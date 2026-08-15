package main

import (
	"testing"
	"time"
)

func TestAConfigFileOverridesOnlyWhatItNames(t *testing.T) {
	config := LoadConfig([]byte(`{"model":"sonnet","timeoutMs":3000}`), "/home/agent")

	if config.Model != "sonnet" {
		t.Errorf("model: got %q, want sonnet", config.Model)
	}
	if config.Timeout != 3*time.Second {
		t.Errorf("timeout: got %v, want 3s", config.Timeout)
	}
	if want := Default("/home/agent").MaxAnalysisChars; config.MaxAnalysisChars != want {
		t.Errorf("maxAnalysisChars: got %d, want the default %d", config.MaxAnalysisChars, want)
	}
}

// A half written config file leaves a working hook rather than a broken one, so a field of the wrong
// type is passed over and everything beside it in the same file still applies.
func TestAFieldOfTheWrongTypeKeepsItsDefaultAndTheRestOfTheFileStillApplies(t *testing.T) {
	config := LoadConfig([]byte(`{"model":42,"maxDocChars":"lots","skip":["^ok$"]}`), "/home/agent")

	if want := Default("/home/agent").Model; config.Model != want {
		t.Errorf("model: got %q, want the default %q", config.Model, want)
	}
	if want := Default("/home/agent").MaxDocChars; config.MaxDocChars != want {
		t.Errorf("maxDocChars: got %d, want the default %d", config.MaxDocChars, want)
	}
	if len(config.Skip) != 1 || config.Skip[0] != "^ok$" {
		t.Errorf("skip: got %v, want the file's own value to survive the bad fields beside it", config.Skip)
	}
}

func TestAConfigFileNobodyCanParseLeavesTheDefaults(t *testing.T) {
	config := LoadConfig([]byte("not json at all"), "/home/agent")

	if config.Model != Default("/home/agent").Model {
		t.Errorf("model: got %q, want the default", config.Model)
	}
}

// Zero and below are refused rather than taken. A timeout of zero is a hook that never waits for the
// model, which reads as the model always failing.
func TestANumberAtOrBelowZeroKeepsItsDefault(t *testing.T) {
	config := LoadConfig([]byte(`{"timeoutMs":0,"maxAnalysisChars":-5}`), "/home/agent")

	if config.Timeout != Default("/home/agent").Timeout {
		t.Errorf("timeout: got %v, want the default", config.Timeout)
	}
	if config.MaxAnalysisChars != Default("/home/agent").MaxAnalysisChars {
		t.Errorf("maxAnalysisChars: got %d, want the default", config.MaxAnalysisChars)
	}
}

func TestALeadingTildeResolvesAgainstTheHomeDirectory(t *testing.T) {
	config := LoadConfig([]byte(`{
		"skillDirs":["~/skills","/absolute/skills"],
		"rulesFile":"~/RULES.md",
		"lastRunFile":""
	}`), "/home/agent")

	want := []string{"/home/agent/skills", "/absolute/skills"}
	for index, path := range want {
		if config.SkillDirs[index] != path {
			t.Errorf("skillDirs[%d]: got %q, want %q", index, config.SkillDirs[index], path)
		}
	}
	if config.RulesFile != "/home/agent/RULES.md" {
		t.Errorf("rulesFile: got %q", config.RulesFile)
	}
	// Empty means the thing is turned off, so it must not become the home directory itself.
	if config.LastRunFile != "" {
		t.Errorf("lastRunFile: got %q, want it left empty", config.LastRunFile)
	}
}

func TestAnEmptyMessageIsNeverAnalysed(t *testing.T) {
	config := Default("/home/agent")

	for _, prompt := range []string{"", "   ", "\n\t "} {
		if !config.Skipped(prompt) {
			t.Errorf("a message of %q was analysed", prompt)
		}
	}
}

func TestEveryMessageIsAnalysedUntilTheConfigNamesAPatternForIt(t *testing.T) {
	config := Default("/home/agent")
	if config.Skipped("fix the flaky test") {
		t.Error("the default skipped a message, and the default is to analyse every one")
	}

	config.Skip = []string{"^/"}
	if !config.Skipped("/clear") {
		t.Error("a message matching a skip pattern was analysed")
	}
	if config.Skipped("fix the flaky test") {
		t.Error("a message matching no skip pattern was skipped")
	}
}

// A pattern nobody can compile is passed over rather than allowed to break the hook, because the
// alternative is every message failing on one bad line of configuration.
func TestAnUnparseablePatternIsPassedOverRatherThanBreakingTheHook(t *testing.T) {
	config := Default("/home/agent")
	config.Skip = []string{"([unclosed", "^ok$"}

	if config.Skipped("fix the flaky test") {
		t.Error("a message was skipped by a pattern that does not compile")
	}
	if !config.Skipped("ok") {
		t.Error("the usable pattern beside the broken one stopped working")
	}
}
