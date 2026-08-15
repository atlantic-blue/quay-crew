package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

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
