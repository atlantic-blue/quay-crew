package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/ste100"
)

// runSTE100Hook is what `quay hook-ste100` does: read a Stop hook's input from standard input, check
// the model's last message against Simplified Technical English, and print a hook decision.
//
// Not in the usage text and not a command anybody types, the same as header and console: this is run
// by Claude Code itself, from a Stop hook entry in .claude/settings.json, once per turn.
func runSTE100Hook(args []string, in io.Reader, out io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: quay hook-ste100, and a Stop hook runs it for you")
	}

	body, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("reading the hook's input: %w", err)
	}

	var input stopHookInput
	if err := json.Unmarshal(body, &input); err != nil {
		// A hook that cannot read its own input has nothing to check. Failing the turn over a shape
		// this tool does not recognise would be worse than saying nothing, so this passes rather than
		// blocks: half a capability, per the skills design, is worse than none.
		return nil
	}
	if input.TranscriptPath == "" {
		return nil
	}

	text, found := lastAssistantText(input.TranscriptPath)
	if !found {
		return nil
	}

	violations := ste100.Check(text)
	if len(violations) == 0 {
		return nil
	}

	_, err = fmt.Fprintln(out, hookBlockJSON(violations))
	return err
}

// stopHookInput is the part of Claude Code's Stop hook input this cares about. The full shape carries
// a session id too, which nothing here needs.
type stopHookInput struct {
	TranscriptPath string `json:"transcript_path"`
}

// transcriptLine is the part of a conversation record this cares about: whether it is the model's own
// message, and the text blocks in it. A line can be a tool call, a tool result, or a handful of other
// shapes this tool does not write to this file, and Unmarshal leaves those fields at their zero value
// rather than failing, which is what lets every kind of line share one scan.
type transcriptLine struct {
	Type    string `json:"type"`
	Message struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// lastAssistantText is the text of the most recent assistant message in a transcript, and whether one
// was found. A turn can end on a message that carries only a tool call and no text of its own, so this
// walks backward from the end and answers with the most recent message that has any.
//
// A line the decoder cannot read is skipped rather than failing the whole file: the tool appends to
// this as it goes, so the last line of a conversation still being written is regularly half written.
func lastAssistantText(path string) (string, bool) {
	file, err := os.Open(path) //nolint:gosec // the path a Stop hook was told, not one this process chose
	if err != nil {
		return "", false
	}
	defer func() { _ = file.Close() }()

	var lines []string
	scanner := bufio.NewScanner(file)
	// A conversation record carries a whole message, so the default line limit is not enough.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	for i := len(lines) - 1; i >= 0; i-- {
		var record transcriptLine
		if err := json.Unmarshal([]byte(lines[i]), &record); err != nil {
			continue
		}
		if record.Type != "assistant" || record.Message.Role != "assistant" {
			continue
		}
		var text strings.Builder
		for _, block := range record.Message.Content {
			if block.Type != "text" {
				continue
			}
			if text.Len() > 0 {
				text.WriteString("\n\n")
			}
			text.WriteString(block.Text)
		}
		if text.Len() > 0 {
			return text.String(), true
		}
	}
	return "", false
}

// maxViolationsInReason caps how many violations a hook's reason names outright. A response can fail
// many sentences at once, and a wall of them is as unreadable as the prose it is complaining about, so
// past the cap this says how many more there were rather than listing every one.
const maxViolationsInReason = 8

// hookBlockJSON is a Stop hook's decision, telling Claude Code to hand the turn back with why.
func hookBlockJSON(violations []ste100.Violation) string {
	shown := violations
	more := 0
	if len(shown) > maxViolationsInReason {
		shown = violations[:maxViolationsInReason]
		more = len(violations) - maxViolationsInReason
	}

	var reason strings.Builder
	reason.WriteString("The last message does not follow Simplified Technical English (ASD-STE100), which rule 53 requires everywhere. Rewrite it, then stop again.\n")
	for _, v := range shown {
		fmt.Fprintf(&reason, "- %s\n", v.String())
	}
	if more > 0 {
		fmt.Fprintf(&reason, "- and %d more\n", more)
	}

	body, err := json.Marshal(struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}{Decision: "block", Reason: reason.String()})
	if err != nil {
		// Marshal fails only on a value JSON cannot represent, and a string is never that. This
		// exists so the function's error is handled rather than ignored, not because it can happen.
		return `{"decision":"block","reason":"the message does not follow Simplified Technical English, rewrite it"}`
	}
	return string(body)
}
