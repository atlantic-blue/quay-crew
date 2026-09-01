// prose-gate holds the prose this system writes to Simplified Technical English, for the part of it
// that is measurable.
//
// Every role here writes prose for a person: pull request descriptions, changelog fragments, issue
// bodies, commit messages, documentation. The standard for that prose is ASD-STE100, and until this
// hook it was a sentence in a brief, which is the position the merge rule was in before merge-gate.
//
// A hook can hold part of it. Not all of it, and being honest about which part is the whole design.
// The rules in rules.go are the part a program can measure exactly. The approved vocabulary and the
// ban on idiom are not, they are not guessed at here, and they stay in the brief where a person
// reads them.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Refused is the exit code the model runtime reads as "do not run this, and tell the session why".
// Anything the hook writes to standard error goes to the session as the reason.
//
// The runtime also takes a refusal as a document on standard output. This one is an exit code
// because that contract is the older and simpler of the two, and a refusal that a runtime does not
// understand is a gate that quietly opens.
const Refused = 2

func main() {
	os.Exit(Run(os.Stdin, os.Stderr))
}

// Run reads what the runtime sends and answers it.
//
// Everything that carries no prose is allowed, including a payload this hook cannot read: a gate
// that refuses what it does not understand refuses the work, and a broken hook must not be able to
// stop a system.
func Run(in io.Reader, errs io.Writer) int {
	body, err := io.ReadAll(in)
	if err != nil {
		return 0
	}
	var event struct {
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
		Cwd       string          `json:"cwd"`
	}
	if err := json.Unmarshal(body, &event); err != nil {
		return 0
	}
	found := Findings(event.ToolName, event.ToolInput, reader(event.Cwd))
	if len(found) == 0 {
		return 0
	}
	fmt.Fprintln(errs, Say(found))
	return Refused
}

// Findings is every refusal in one tool call.
func Findings(tool string, input json.RawMessage, read func(string) ([]byte, error)) []Finding {
	var found []Finding
	for _, piece := range Pieces(tool, input, read) {
		found = append(found, Check(piece.Where, piece.Text)...)
	}
	return found
}

// Limit is how many refusals one firing reports.
//
// A writer who is handed forty of them rewrites the whole document by guessing, which is how a
// session burns its budget on prose. The first few are enough to say what the standard wants, and
// the rest are still there on the next attempt.
const Limit = 5

// Say is what the session is told instead of what it asked for.
func Say(found []Finding) string {
	var out strings.Builder
	fmt.Fprintf(&out, "This prose does not meet Simplified Technical English, which is the standard for prose in this system.\n\n")
	shown := found
	if len(shown) > Limit {
		shown = shown[:Limit]
	}
	for _, one := range shown {
		fmt.Fprintf(&out, "%s\n\n", one)
	}
	if len(found) > len(shown) {
		fmt.Fprintf(&out, "%d more, reported once these are fixed.\n\n", len(found)-len(shown))
	}
	out.WriteString(theStandard)
	return out.String()
}

// theStandard is the second half of every refusal, one sentence and one place, because a session
// that reads two explanations of one rule believes it has found an exception.
const theStandard = "The standard is ASD-STE100, at https://www.asd-ste100.org/. " +
	"Rewrite the prose above and write the file again. " +
	"This gate measures sentence length, paragraph length, tense and punctuation only: " +
	"the approved vocabulary and the ban on idiom and metaphor are in the brief, and a person reads those."

// reader fetches a file named on a command line, from where the session is standing.
func reader(cwd string) func(string) ([]byte, error) {
	return func(name string) ([]byte, error) {
		if !filepath.IsAbs(name) && cwd != "" {
			name = filepath.Join(cwd, name)
		}
		return os.ReadFile(name)
	}
}
