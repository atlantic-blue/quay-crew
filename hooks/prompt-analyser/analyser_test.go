package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
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

// fakeFS is a filesystem in a map, so what the model is given can be tested without a directory tree.
type fakeFS struct {
	files   map[string]string
	written map[string]string
}

func newFakeFS(files map[string]string) *fakeFS {
	return &fakeFS{files: files, written: map[string]string{}}
}

func (f *fakeFS) List(dir string) ([]string, error) {
	dir = strings.TrimSuffix(dir, "/")
	seen := map[string]bool{}
	var names []string
	for path := range f.files {
		rest, under := strings.CutPrefix(path, dir+"/")
		if !under {
			continue
		}
		name, _, _ := strings.Cut(rest, "/")
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	if names == nil {
		return nil, os.ErrNotExist
	}
	return names, nil
}

func (f *fakeFS) Read(file string) ([]byte, error) {
	body, found := f.files[file]
	if !found {
		return nil, os.ErrNotExist
	}
	return []byte(body), nil
}

func (f *fakeFS) Write(file string, body []byte) error {
	f.written[file] = string(body)
	return nil
}

func skillFile(name, description string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\nthe brief\n", name, description)
}

func TestSkillsAreCollectedOneLevelDeepAndSortedByName(t *testing.T) {
	fs := newFakeFS(map[string]string{
		"/skills/git/SKILL.md": skillFile("git", "how work is done in a repository"),
		"/skills/aws/SKILL.md": skillFile("aws", "read cloud state"),
	})

	skills := CollectSkills([]string{"/skills"}, fs)

	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2: %v", len(skills), skills)
	}
	if skills[0].Name != "aws" || skills[1].Name != "git" {
		t.Errorf("got %v, want them sorted by name", skills)
	}
	if skills[1].Description != "how work is done in a repository" {
		t.Errorf("description: got %q", skills[1].Description)
	}
}

// The description is the whole of what the model is given, so an entry without one is a name with
// nothing to decide on.
func TestASkillWithNoDescriptionIsLeftOut(t *testing.T) {
	fs := newFakeFS(map[string]string{
		"/skills/git/SKILL.md":     "---\nname: git\n---\n",
		"/skills/aws/SKILL.md":     skillFile("aws", "read cloud state"),
		"/skills/notes/README.md":  "not a skill",
		"/skills/empty/SKILL.md":   "",
		"/skills/nofront/SKILL.md": "no frontmatter here",
	})

	skills := CollectSkills([]string{"/skills"}, fs)

	if len(skills) != 1 || skills[0].Name != "aws" {
		t.Errorf("got %v, want aws alone", skills)
	}
}

func TestAStarInAPathStandsForOneLevelOfSubdirectories(t *testing.T) {
	fs := newFakeFS(map[string]string{
		"/orgs/one/skills/git/SKILL.md": skillFile("git", "repositories"),
		"/orgs/two/skills/aws/SKILL.md": skillFile("aws", "cloud"),
	})

	skills := CollectSkills([]string{"/orgs/*/skills"}, fs)

	if len(skills) != 2 {
		t.Fatalf("got %d skills, want both orgs: %v", len(skills), skills)
	}
}

// The first directory listed wins, so a personal skill shadows the system's one of the same name
// rather than appearing twice.
func TestTheFirstDirectoryWinsWhenTwoHoldTheSameName(t *testing.T) {
	fs := newFakeFS(map[string]string{
		"/mine/git/SKILL.md":   skillFile("git", "mine"),
		"/system/git/SKILL.md": skillFile("git", "the system's"),
		"/system/aws/SKILL.md": skillFile("aws", "cloud"),
	})

	skills := CollectSkills([]string{"/mine", "/system"}, fs)

	if len(skills) != 2 {
		t.Fatalf("got %d skills, want 2: %v", len(skills), skills)
	}
	for _, skill := range skills {
		if skill.Name == "git" && skill.Description != "mine" {
			t.Errorf("git: got %q, want the first directory to win", skill.Description)
		}
	}
}

func TestADirectoryThatIsNotThereIsPassedOver(t *testing.T) {
	fs := newFakeFS(map[string]string{"/skills/git/SKILL.md": skillFile("git", "repositories")})

	skills := CollectSkills([]string{"/nowhere", "/skills", "/nowhere/*/skills"}, fs)

	if len(skills) != 1 {
		t.Errorf("got %v, want the one directory that exists to still be read", skills)
	}
}

func TestFrontmatterReadsFlatFieldsAndContinuesAWrappedValue(t *testing.T) {
	fields := Frontmatter(strings.Join([]string{
		"---",
		"name: git",
		"description: how work is done here,",
		"  and it wraps onto a second line",
		"---",
		"body: not a field",
	}, "\n"))

	if fields["name"] != "git" {
		t.Errorf("name: got %q", fields["name"])
	}
	want := "how work is done here, and it wraps onto a second line"
	if fields["description"] != want {
		t.Errorf("description: got %q, want %q", fields["description"], want)
	}
	if _, found := fields["body"]; found {
		t.Error("a line after the closing marker was read as a field")
	}
}

func TestAFileWithNoFrontmatterHasNoFields(t *testing.T) {
	if fields := Frontmatter("# just a heading\n"); len(fields) != 0 {
		t.Errorf("got %v, want nothing", fields)
	}
}

func TestTheRuleIndexIsOneLinePerNumberedHeadline(t *testing.T) {
	rules := RuleIndex(strings.Join([]string{
		"# Working rules",
		"",
		"1. **Never commit without permission.**",
		"   Stop and ask first.",
		"",
		"10. **Never force-push without",
		"   explicit permission.**",
		"",
		"Some prose that is not a rule.",
	}, "\n"))

	if len(rules) != 2 {
		t.Fatalf("got %d rules, want 2: %v", len(rules), rules)
	}
	if rules[0] != "1. Never commit without permission." {
		t.Errorf("got %q", rules[0])
	}
	// A headline that wraps comes back as one line, because the index is one line per rule.
	if rules[1] != "10. Never force-push without explicit permission." {
		t.Errorf("got %q", rules[1])
	}
}

func TestClipCutsToTheCeilingAndSaysSo(t *testing.T) {
	clipped := Clip(strings.Repeat("a", 100), 10)

	if !strings.HasPrefix(clipped, strings.Repeat("a", 10)) {
		t.Errorf("got %q, want the first ten characters", clipped)
	}
	if !strings.Contains(clipped, "[cut at 10 characters]") {
		t.Errorf("got %q, want it to say it was cut", clipped)
	}
}

func TestClipLeavesTextInsideTheCeilingAlone(t *testing.T) {
	if got := Clip("short", 10); got != "short" {
		t.Errorf("got %q, want it untouched", got)
	}
}

// The count is characters rather than bytes, so a cut never lands in the middle of one and hands the
// model a broken character where a word was.
func TestClipNeverCutsAcrossACharacter(t *testing.T) {
	clipped := Clip(strings.Repeat("é", 100), 10)

	if !strings.HasPrefix(clipped, strings.Repeat("é", 10)) {
		t.Errorf("got %q, want ten whole characters", clipped)
	}
	if strings.ContainsRune(clipped, '�') {
		t.Errorf("got %q, which holds a broken character", clipped)
	}
}

func TestTheMessageAsTypedIsAlwaysSent(t *testing.T) {
	sent := Ask{Prompt: "fix the flaky test"}.UserMessage()

	if !strings.Contains(sent, "<message>\nfix the flaky test\n</message>") {
		t.Errorf("got:\n%s", sent)
	}
}

// An empty checkout must not send the model a page of empty headings to reason about.
func TestASectionWithNothingInItIsLeftOutEntirely(t *testing.T) {
	sent := Ask{Prompt: "hello"}.UserMessage()

	for _, absent := range []string{"<facts>", "<skills>", "<rules>"} {
		if strings.Contains(sent, absent) {
			t.Errorf("%s was sent with nothing in it:\n%s", absent, sent)
		}
	}
}

func TestEveryFactThatIsKnownIsSent(t *testing.T) {
	sent := Ask{
		Prompt: "fix it",
		Facts: Facts{
			Cwd:    "/home/agent/workspace",
			Repo:   "atlantic-blue/quay-crew",
			Branch: "gun-1620-fix-the-thing",
			Ticket: "GUN-1620",
			State:  "stage: building",
		},
	}.UserMessage()

	for _, want := range []string{
		"working directory: /home/agent/workspace",
		"repository: atlantic-blue/quay-crew",
		"branch: gun-1620-fix-the-thing",
		"ticket: GUN-1620",
		"ticket state:\nstage: building",
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("%q was not sent:\n%s", want, sent)
		}
	}
}

func TestAFactThatIsNotKnownIsNotSentAsAnEmptyLine(t *testing.T) {
	sent := Ask{Prompt: "fix it", Facts: Facts{Cwd: "/home/agent"}}.UserMessage()

	if strings.Contains(sent, "repository:") || strings.Contains(sent, "branch:") {
		t.Errorf("an empty fact was sent:\n%s", sent)
	}
	if !strings.Contains(sent, "working directory: /home/agent") {
		t.Errorf("the one known fact was not sent:\n%s", sent)
	}
}

func TestSkillsRulesAndDocumentsEachGetTheirOwnSection(t *testing.T) {
	sent := Ask{
		Prompt: "fix it",
		Skills: []Skill{{Name: "git", Description: "how work is done"}},
		Rules:  []string{"1. Never commit without permission."},
		Docs:   []Document{{Label: "CLAUDE", Text: "the rules"}},
	}.UserMessage()

	for _, want := range []string{
		"<skills>\ngit: how work is done\n</skills>",
		"<rules>\n1. Never commit without permission.\n</rules>",
		"<CLAUDE>\nthe rules\n</CLAUDE>",
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("%q was not sent:\n%s", want, sent)
		}
	}
}

func TestTheSystemPromptTellsTheModelHowToStayQuiet(t *testing.T) {
	system := SystemPrompt()

	if !strings.Contains(system, Pass) {
		t.Error("the model is never told the word that means a message needs no analysis")
	}
	for _, field := range []string{"goal:", "target:", "unclear:", "skills:", "rules:", "first move:"} {
		if !strings.Contains(system, field) {
			t.Errorf("the model is never asked for %q", field)
		}
	}
}

func TestOnlyTheKnownFieldLinesSurvive(t *testing.T) {
	analysis := FormatAnalysis(strings.Join([]string{
		"Here is my analysis of your message:",
		"goal: rewrite the hook in Go",
		"target: atlantic-blue/quay-crew",
		"random chatter that is not a field",
		"first move: read the TypeScript",
		"",
		"Let me know if you want anything else.",
	}, "\n"), 1400)

	want := strings.Join([]string{
		"goal: rewrite the hook in Go",
		"target: atlantic-blue/quay-crew",
		"first move: read the TypeScript",
	}, "\n")
	if analysis != want {
		t.Errorf("got:\n%s\nwant:\n%s", analysis, want)
	}
}

// The pass word is how the model says a message needs no analysis, and it is dropped by the same
// rule that drops everything else: it is not a field line.
func TestThePassWordLeavesNothingToPrint(t *testing.T) {
	for _, raw := range []string{Pass, "  pass  ", "PASS", "pass\n"} {
		if analysis := FormatAnalysis(raw, 1400); analysis != "" {
			t.Errorf("%q gave %q, want nothing", raw, analysis)
		}
	}
}

func TestAnEmptyAnswerLeavesNothingToPrint(t *testing.T) {
	for _, raw := range []string{"", "   \n\t", "```\n```"} {
		if analysis := FormatAnalysis(raw, 1400); analysis != "" {
			t.Errorf("%q gave %q, want nothing", raw, analysis)
		}
	}
}

// A model that answers the message instead of analysing it produces no field lines at all, and the
// hook stays quiet rather than passing the answer off as an analysis.
func TestAnAnswerInsteadOfAnAnalysisLeavesNothingToPrint(t *testing.T) {
	raw := "Sure, here is how to fix the flaky test. First, add a retry to the assertion."

	if analysis := FormatAnalysis(raw, 1400); analysis != "" {
		t.Errorf("got %q, want nothing", analysis)
	}
}

func TestAFencedAnswerIsUnwrapped(t *testing.T) {
	analysis := FormatAnalysis("```text\ngoal: ship it\n```", 1400)

	if analysis != "goal: ship it" {
		t.Errorf("got %q", analysis)
	}
}

func TestTheAnalysisIsCappedAtTheCeiling(t *testing.T) {
	analysis := FormatAnalysis("goal: "+strings.Repeat("x", 200), 50)

	if !strings.Contains(analysis, "[cut at 50 characters]") {
		t.Errorf("got %q, want it cut at the ceiling", analysis)
	}
}

func TestTheLastRunLineSaysWhenWhatAndHowLong(t *testing.T) {
	when := time.Date(2026, 8, 15, 9, 30, 0, 0, time.UTC)

	line := LastRunLine(when, Analysed, 1234*time.Millisecond, "  fix the\n  flaky test  ")

	for _, want := range []string{"2026-08-15T09:30:00.000Z", "analysed", "1234ms", "fix the flaky test"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q does not carry %q", line, want)
		}
	}
	if !strings.HasSuffix(line, "\n") {
		t.Error("the line does not end in a newline")
	}
}

func TestTheLastRunLineKeepsOnlyTheOpeningOfALongMessage(t *testing.T) {
	line := LastRunLine(time.Unix(0, 0).UTC(), Passed, 0, strings.Repeat("a", 500))

	if len(line) > 120 {
		t.Errorf("the line is %d bytes, which is a message rather than a log line", len(line))
	}
}

// The output is JSON so the person who typed the message can see what was made of it: plain text on
// this event reaches the session only.
func TestWhatIsPrintedCarriesTheContextForTheSessionAndALineForTheTerminal(t *testing.T) {
	var out struct {
		SystemMessage string `json:"systemMessage"`
		Specific      struct {
			EventName         string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(Printed("fix the flaky test", "goal: fix it")), &out); err != nil {
		t.Fatalf("what the hook printed is not JSON: %v", err)
	}

	if !strings.Contains(out.SystemMessage, "goal: fix it") {
		t.Errorf("systemMessage: got %q", out.SystemMessage)
	}
	if out.Specific.EventName != "UserPromptSubmit" {
		t.Errorf("hookEventName: got %q", out.Specific.EventName)
	}
	// The message as typed goes back beside the analysis, because the words are the instruction and
	// the analysis is a guess at them.
	if !strings.Contains(out.Specific.AdditionalContext, "fix the flaky test") {
		t.Errorf("the context does not carry the message as typed: %q", out.Specific.AdditionalContext)
	}
	if !strings.Contains(out.Specific.AdditionalContext, "goal: fix it") {
		t.Errorf("the context does not carry the analysis: %q", out.Specific.AdditionalContext)
	}
}

// A quiet decision must never look the same as a hook that did not run.
func TestANoticeSaysSomethingToTheTerminalAndNothingToTheSession(t *testing.T) {
	var out map[string]any
	if err := json.Unmarshal([]byte(Notice("nothing to add")), &out); err != nil {
		t.Fatalf("a notice is not JSON: %v", err)
	}

	if out["systemMessage"] != "prompt analysis: nothing to add" {
		t.Errorf("systemMessage: got %v", out["systemMessage"])
	}
	if _, found := out["hookSpecificOutput"]; found {
		t.Error("a notice reached the session, and it is for the terminal only")
	}
}

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
		Ask: func(system, user string) (string, string) {
			run.calls++
			run.system, run.asked = system, user
			return answer, ""
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
	if !strings.Contains(run.out, "the model answered with nothing") {
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
func TestTaskingOffTheLastRunFileStillLetsTheMessageThrough(t *testing.T) {
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

// The failure every sandbox hits. It must name the cause and the next move, because the hook fails
// open and a silent failure is indistinguishable from a hook with nothing to add. This one ran that
// way in every sandbox from the day it shipped.
func TestNotBeingLoggedInSaysSoAndSaysWhatToDo(t *testing.T) {
	// What the child actually prints, captured from the sandbox: standard output, then exit 1.
	trouble := Trouble(nil, errors.New("exit status 1"), "Not logged in · Please run /login", Default("/home/agent"))

	if !strings.Contains(trouble, "not logged in") {
		t.Errorf("it does not say what is wrong: %q", trouble)
	}
	if !strings.Contains(trouble, "quay hook detach system prompt-analyser") {
		t.Errorf("it does not say what to do next: %q", trouble)
	}
	// The reason it is confusing is worth saying: the token is there, it just does not come down.
	if !strings.Contains(trouble, "not what the session starts") {
		t.Errorf("it does not explain why a token that exists does not reach the hook: %q", trouble)
	}
}

func TestATimeoutSaysHowLongItWaitedAndWhatToChange(t *testing.T) {
	config := Default("/home/agent")

	trouble := Trouble(context.DeadlineExceeded, errors.New("signal: killed"), "", config)

	if !strings.Contains(trouble, config.Timeout.String()) {
		t.Errorf("it does not say how long it waited: %q", trouble)
	}
	if !strings.Contains(trouble, "timeoutMs") {
		t.Errorf("it does not name the setting to change: %q", trouble)
	}
}

func TestAMissingClaudeSaysTheBinaryIsNotThere(t *testing.T) {
	trouble := Trouble(nil, errors.New(`exec: "claude": executable file not found in $PATH`), "", Default("/home/agent"))

	if !strings.Contains(trouble, "no claude on the path") {
		t.Errorf("it does not name the missing binary: %q", trouble)
	}
}

// Whatever the child said, rather than a guess at it. A failure nobody predicted still has to reach
// the person, or the next unknown failure is another silent one.
func TestAnUnknownFailureRepeatsWhatTheChildSaid(t *testing.T) {
	trouble := Trouble(nil, errors.New("exit status 1"), "Overloaded: try again later\nstack trace here", Default("/home/agent"))

	if !strings.Contains(trouble, "Overloaded: try again later") {
		t.Errorf("it does not repeat what the child said: %q", trouble)
	}
	if strings.Contains(trouble, "stack trace") {
		t.Errorf("it dumped more than one line into the terminal: %q", trouble)
	}
}

// The whole point: what the person sees changes from a shrug to an instruction.
func TestTheTerminalIsToldTheCauseRatherThanThatSomethingWentWrong(t *testing.T) {
	config := testConfig()
	fs := newFakeFS(map[string]string{})
	out := &strings.Builder{}
	tick := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)

	Run(Options{
		Config: config,
		Stdin:  strings.NewReader(`{"prompt":"fix it","cwd":"/home/agent"}`),
		Stdout: out,
		FS:     fs,
		Now:    func() time.Time { tick = tick.Add(time.Second); return tick },
		Facts:  func(cwd string) Facts { return Facts{Cwd: cwd} },
		Ask:    func(system, user string) (string, string) { return "", NotLoggedIn },
	})

	if strings.Contains(out.String(), "no answer, carrying on") {
		t.Errorf("still the old shrug: %s", out.String())
	}
	if !strings.Contains(out.String(), "not logged in") {
		t.Errorf("the terminal was not told the cause: %s", out.String())
	}
	if !strings.Contains(out.String(), "quay hook detach") {
		t.Errorf("the terminal was not told what to do: %s", out.String())
	}
	// It still fails open. The message must get through whatever the hook could not do.
	if strings.Contains(out.String(), "hookSpecificOutput") {
		t.Errorf("a failed analysis reached the session: %s", out.String())
	}
}
