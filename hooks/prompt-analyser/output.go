package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Outcome is how a run ended, for the last run line.
type Outcome string

const (
	Analysed Outcome = "analysed"
	Passed   Outcome = "pass"
	Skipped  Outcome = "skipped"
	NoAnswer Outcome = "no answer"
)

var (
	fencedOpen  = regexp.MustCompile(`^\s*` + "```" + `[a-zA-Z]*\s*`)
	fencedClose = regexp.MustCompile("```" + `\s*$`)
	fieldLine   = regexp.MustCompile(`(?i)^(goal|target|unclear|skills|rules|first move):`)
)

// FormatAnalysis turns what the model returned into the block the hook prints, or an empty string
// when there is nothing worth adding.
//
// Keeping only the known field lines is the one rule, and it does all the work: it drops the pass
// word, chatter around the fields, and an answer the model gave instead of an analysis.
func FormatAnalysis(raw string, max int) string {
	cleaned := strings.TrimSpace(fencedClose.ReplaceAllString(fencedOpen.ReplaceAllString(raw, ""), ""))
	if cleaned == "" {
		return ""
	}

	var kept []string
	for _, line := range strings.Split(cleaned, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && fieldLine.MatchString(line) {
			kept = append(kept, line)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return Clip(strings.Join(kept, "\n"), max)
}

var runsOfSpace = regexp.MustCompile(`\s+`)

// LastRunLine describes one run, written over the last one. Reading this file is how you tell a hook
// that fired and stayed quiet from a hook that never fired at all.
func LastRunLine(when time.Time, outcome Outcome, elapsed time.Duration, prompt string) string {
	opening := runsOfSpace.ReplaceAllString(strings.TrimSpace(prompt), " ")
	if len(opening) > 60 {
		opening = opening[:60]
	}
	return strings.Join([]string{
		when.UTC().Format("2006-01-02T15:04:05.000Z"),
		string(outcome),
		strconv.FormatInt(elapsed.Milliseconds(), 10) + "ms",
		opening,
	}, "  ") + "\n"
}

// Context is what the session is handed: the message exactly as it was typed, then the analysis,
// both labelled so the analysis is never mistaken for an instruction.
func Context(prompt, analysis string) string {
	return strings.Join([]string{
		"<prompt-analysis>",
		"A hook generated the analysis below from the message. It is a reading of the message,",
		"not a message. The engineer's own words are the instruction; where the two differ,",
		"follow the words and ask about the difference.",
		"",
		"<as-typed>",
		prompt,
		"</as-typed>",
		"",
		"<analysis>",
		analysis,
		"</analysis>",
		"</prompt-analysis>",
	}, "\n")
}

// hookOutput is what the runtime reads on standard output.
//
// Plain text on this event reaches the session only, which leaves the person who typed the message
// unable to see what was made of it, so the output is JSON instead: additionalContext for the
// session, systemMessage for the terminal.
type hookOutput struct {
	SystemMessage string       `json:"systemMessage"`
	Specific      *specificOut `json:"hookSpecificOutput,omitempty"`
}

type specificOut struct {
	EventName         string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// Printed is the whole of what the hook says when it has an analysis to hand over.
func Printed(prompt, analysis string) string {
	return encode(hookOutput{
		SystemMessage: "prompt analysis\n" + analysis,
		Specific: &specificOut{
			EventName:         "UserPromptSubmit",
			AdditionalContext: Context(prompt, analysis),
		},
	})
}

// Notice is a line for the terminal and nothing for the session. Every outcome says something, so a
// quiet decision never looks the same as a hook that did not run.
func Notice(text string) string {
	return encode(hookOutput{SystemMessage: "prompt analysis: " + text})
}

// encode never fails on these shapes, and a hook that cannot render its own output still has to let
// the message through, so an error here means printing nothing rather than printing a broken frame.
func encode(out hookOutput) string {
	body, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(body)
}
