package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// driven is one run of the whole hook against a fake model and a fake filesystem, so what the
// session is actually handed can be asserted rather than what the hook called along the way.
type driven struct {
	out     string
	fs      *fakeFS
	asked   string
	system  string
	calls   int
	lastRun string
}

func drive(t *testing.T, config Config, payload string, answer string, files map[string]string) driven {
	t.Helper()
	if files == nil {
		files = map[string]string{}
	}

	fs := newFakeFS(files)
	out := &strings.Builder{}
	run := driven{fs: fs}
	// A clock that moves a fixed amount per reading, so an elapsed time can be asserted.
	tick := time.Date(2026, 8, 15, 9, 30, 0, 0, time.UTC)

	Run(Options{
		Config: config,
		Stdin:  strings.NewReader(payload),
		Stdout: out,
		FS:     fs,
		Now: func() time.Time {
			tick = tick.Add(500 * time.Millisecond)
			return tick
		},
		Facts: func(cwd string) Facts { return Facts{Cwd: cwd} },
		Ask: func(system, user string) string {
			run.calls++
			run.system, run.asked = system, user
			return answer
		},
	})

	run.out = out.String()
	run.lastRun = fs.written[config.LastRunFile]
	return run
}

func testConfig() Config {
	config := Default("/home/agent")
	config.SkillDirs = nil
	config.RulesFile = ""
	config.LastRunFile = "/tmp/last"
	return config
}

// The whole point of the hook: the session gets the message it was sent and a reading of it beside
// it, both labelled, and the reading never replaces the words.
func TestTheSessionGetsTheMessageAndTheAnalysisTogether(t *testing.T) {
	run := drive(t, testConfig(),
		`{"prompt":"fix the flaky test","cwd":"/home/agent/workspace"}`,
		"goal: make the test deterministic\nfirst move: read the test",
		nil)

	var out struct {
		SystemMessage string `json:"systemMessage"`
		Specific      struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(run.out), &out); err != nil {
		t.Fatalf("the hook printed something that is not JSON: %q", run.out)
	}
	if !strings.Contains(out.Specific.AdditionalContext, "fix the flaky test") {
		t.Errorf("the message as typed did not reach the session: %q", out.Specific.AdditionalContext)
	}
	if !strings.Contains(out.Specific.AdditionalContext, "goal: make the test deterministic") {
		t.Errorf("the analysis did not reach the session: %q", out.Specific.AdditionalContext)
	}
	if !strings.Contains(run.lastRun, string(Analysed)) {
		t.Errorf("last run: got %q, want it to say analysed", run.lastRun)
	}
}

func TestTheWorkingDirectoryFromThePayloadReachesTheModel(t *testing.T) {
	run := drive(t, testConfig(),
		`{"prompt":"fix it","cwd":"/somewhere/else"}`, "goal: fix it", nil)

	if !strings.Contains(run.asked, "working directory: /somewhere/else") {
		t.Errorf("the model was not told where the session is:\n%s", run.asked)
	}
}

// The model is never called for a message the config says to leave alone, because the call is the
// expensive part and a skipped message must cost nothing.
func TestASkippedMessageNeverReachesTheModel(t *testing.T) {
	config := testConfig()
	config.Skip = []string{"^/"}

	run := drive(t, config, `{"prompt":"/clear","cwd":"/home/agent"}`, "goal: nope", nil)

	if run.calls != 0 {
		t.Errorf("the model was called %d times for a skipped message", run.calls)
	}
	if run.out != "" {
		t.Errorf("a skipped message printed %q, want nothing", run.out)
	}
	if !strings.Contains(run.lastRun, string(Skipped)) {
		t.Errorf("last run: got %q, want it to say skipped", run.lastRun)
	}
}

// Failing open is the whole design: a message always gets through, whatever the model did.
func TestAModelThatSaysNothingStillLetsTheMessageThrough(t *testing.T) {
	run := drive(t, testConfig(), `{"prompt":"fix it","cwd":"/home/agent"}`, "", nil)

	if strings.Contains(run.out, "hookSpecificOutput") {
		t.Errorf("a failed analysis reached the session: %q", run.out)
	}
	if !strings.Contains(run.out, "no answer") {
		t.Errorf("the terminal was told nothing about the failure: %q", run.out)
	}
	if !strings.Contains(run.lastRun, string(NoAnswer)) {
		t.Errorf("last run: got %q, want it to say no answer", run.lastRun)
	}
}

func TestAMessageThatNeedsNoAnalysisSaysSoAndAddsNothing(t *testing.T) {
	run := drive(t, testConfig(), `{"prompt":"thanks","cwd":"/home/agent"}`, Pass, nil)

	if strings.Contains(run.out, "hookSpecificOutput") {
		t.Errorf("the pass word reached the session: %q", run.out)
	}
	if !strings.Contains(run.out, "nothing to add") {
		t.Errorf("the terminal was told nothing: %q", run.out)
	}
	if !strings.Contains(run.lastRun, string(Passed)) {
		t.Errorf("last run: got %q, want it to say pass", run.lastRun)
	}
}

// A payload the hook cannot read is a hook that prints nothing and lets the message through, rather
// than one that prints a broken frame the runtime then has to make sense of.
func TestAPayloadTheHookCannotReadPrintsNothing(t *testing.T) {
	for _, payload := range []string{"", "not json", "[1,2,3]"} {
		run := drive(t, testConfig(), payload, "goal: never asked", nil)

		if run.out != "" {
			t.Errorf("payload %q printed %q, want nothing", payload, run.out)
		}
		if run.calls != 0 {
			t.Errorf("payload %q reached the model", payload)
		}
	}
}

func TestTheSkillsAndRulesOnTheMachineReachTheModel(t *testing.T) {
	config := testConfig()
	config.SkillDirs = []string{"/skills"}
	config.RulesFile = "/RULES.md"

	run := drive(t, config, `{"prompt":"clone it","cwd":"/home/agent"}`, "goal: clone it", map[string]string{
		"/skills/git/SKILL.md": skillFile("git", "how work is done in a repository"),
		"/RULES.md":            "1. **Never commit without permission.**\n",
	})

	if !strings.Contains(run.asked, "git: how work is done in a repository") {
		t.Errorf("the skill catalogue did not reach the model:\n%s", run.asked)
	}
	if !strings.Contains(run.asked, "1. Never commit without permission.") {
		t.Errorf("the rule index did not reach the model:\n%s", run.asked)
	}
}

func TestADocumentIsSentUnderItsOwnNameAndCutToTheCeiling(t *testing.T) {
	config := testConfig()
	config.Docs = []string{"/home/agent/.claude/CLAUDE.md"}
	config.MaxDocChars = 20

	run := drive(t, config, `{"prompt":"fix it","cwd":"/home/agent"}`, "goal: fix it", map[string]string{
		"/home/agent/.claude/CLAUDE.md": strings.Repeat("rules ", 100),
	})

	if !strings.Contains(run.asked, "<CLAUDE>") {
		t.Errorf("the document was not labelled by its own name:\n%s", run.asked)
	}
	if !strings.Contains(run.asked, "[cut at 20 characters]") {
		t.Errorf("the document was not cut to the ceiling:\n%s", run.asked)
	}
}

// A document that is not there is passed over, rather than sent as an empty section the model then
// has to reason about.
func TestADocumentThatIsNotThereIsPassedOver(t *testing.T) {
	config := testConfig()
	config.Docs = []string{"/nowhere.md"}

	run := drive(t, config, `{"prompt":"fix it","cwd":"/home/agent"}`, "goal: fix it", nil)

	if strings.Contains(run.asked, "<nowhere>") {
		t.Errorf("an absent document was sent as a section:\n%s", run.asked)
	}
	if run.calls != 1 {
		t.Error("an absent document stopped the analysis happening at all")
	}
}

// A hook that cannot write its own log still has a job to do.
func TestTurningOffTheLastRunFileStillLetsTheMessageThrough(t *testing.T) {
	config := testConfig()
	config.LastRunFile = ""

	run := drive(t, config, `{"prompt":"fix it","cwd":"/home/agent"}`, "goal: fix it", nil)

	if !strings.Contains(run.out, "hookSpecificOutput") {
		t.Errorf("the analysis did not reach the session: %q", run.out)
	}
	if len(run.fs.written) != 0 {
		t.Errorf("something was written with the last run file turned off: %v", run.fs.written)
	}
}

// The guard is what stops the hook analysing its own model call. Losing it means a hook that calls
// itself, so it is asserted rather than assumed.
func TestTheChildIsGuardedAgainstAnalysingItsOwnPrompt(t *testing.T) {
	child := childEnv([]string{"PATH=/usr/bin", "CLAUDECODE=1", "CLAUDE_CODE_SSE_PORT=1", "HOME=/home/agent"})

	if !has(child, Guard+"=1") {
		t.Errorf("the guard is not set on the child: %v", child)
	}
	if !has(child, "MAX_THINKING_TOKENS=0") {
		t.Errorf("the thinking budget is not zero, which is what makes this fast enough to run: %v", child)
	}
	for _, unwanted := range []string{"CLAUDECODE=1", "CLAUDE_CODE_SSE_PORT=1"} {
		if has(child, unwanted) {
			t.Errorf("the child inherited %q, which the running session set for itself", unwanted)
		}
	}
	if !has(child, "PATH=/usr/bin") || !has(child, "HOME=/home/agent") {
		t.Errorf("the child lost the environment it needs to run at all: %v", child)
	}
}

// In a quay sandbox there is no credentials file: the subscription arrives as this variable, and
// dropping it left the child with nothing to authenticate with. The hook still exited 0 and the only
// sign anywhere was the word "no answer" in a file in /tmp.
func TestTheChildKeepsTheCredentialItNeedsToAuthenticate(t *testing.T) {
	child := childEnv([]string{
		"CLAUDE_CODE_OAUTH_TOKEN=secret",
		"CLAUDE_CONFIG_DIR=/home/agent/.claude",
		"CLAUDE_SOMETHING_ELSE=dropped",
	})

	if !has(child, "CLAUDE_CODE_OAUTH_TOKEN=secret") {
		t.Errorf("the subscription token was dropped, so the child cannot authenticate: %v", child)
	}
	if !has(child, "CLAUDE_CONFIG_DIR=/home/agent/.claude") {
		t.Errorf("the config directory was dropped: %v", child)
	}
	if has(child, "CLAUDE_SOMETHING_ELSE=dropped") {
		t.Errorf("the child inherited a variable the session set for itself: %v", child)
	}
}

func has(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
