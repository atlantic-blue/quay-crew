// Package promise reads a change and says whether it keeps what this repository promises.
//
// The readme says the behaviour of this system is written out as scenarios in features/, and that
// anything not in there does not exist. That is a promise to a reader, and until this existed nothing
// asked whether a change kept it. A change shipped 200 lines of new behaviour with no scenario, and
// every check was green. Nothing was wrong with the checks. They were never asked the question, so
// the promise held for exactly as long as whoever opened the pull request remembered it.
//
// A change may legitimately carry none, so the answer is not a rule with no way out. The way out is a
// stated reason in the pull request body, which a reviewer reads, rather than silence, which nobody
// sees.
package promise

import (
	"fmt"
	"strings"
)

// Status is what a change did to one file.
type Status string

const (
	Added    Status = "added"
	Modified Status = "modified"
	Deleted  Status = "deleted"
)

// File is one path a change touched, and what it did to it.
type File struct {
	Path   string
	Status Status
}

// Change is what one pull request did: the files it touched, and the body it was opened with.
type Change struct {
	Files []File
	Body  string
}

// The promise, in the words the refusal uses and the words the body's line spells.
const Scenario = "scenario"

// reasonWords is the shortest a stated reason can be. "No scenario: none" is silence with a colon in
// front, and a rule that accepts it is a rule that is never kept. Whether the sentence is a good one
// is the reviewer's to judge; this only makes it impossible to say nothing.
const reasonWords = 3

// Finding is one promise a change did not keep.
type Finding struct {
	// Promise is what is missing, which is Scenario.
	Promise string
	// Because is what put this change under the rule, so a refusal names the files rather than
	// asserting that behaviour changed.
	Because []string
	// Wanted is what writing the promise looks like, and Excuse is the line that stands in for it.
	Wanted string
	Excuse string
}

// String is the refusal a person reads, which says what is missing, what made this a behaviour
// change, and both ways out. A check that only says no is a check somebody works around.
func (f Finding) String() string {
	var out strings.Builder
	fmt.Fprintf(&out, "this change touches behaviour and carries no %s.\n\n", f.Promise)
	fmt.Fprintf(&out, "What makes it a behaviour change:\n")
	for _, why := range f.Because {
		fmt.Fprintf(&out, "  %s\n", why)
	}
	fmt.Fprintf(&out, "\nWrite one:\n\n  %s\n", f.Wanted)
	fmt.Fprintf(&out, "\nOr say in the pull request body why this change has none:\n\n  %s <why>\n", f.Excuse)
	return out.String()
}

// Check reads a change and returns the promises it did not keep, in the order they are asked for.
//
// A change that touches no behaviour is asked for neither. That is the whole of the exemption: a
// change to documentation, to a test, to the continuous integration workflow or to generated code
// carries nothing, because there is no behaviour for a reader to be told about.
func Check(change Change) []Finding {
	because := Behaviour(change.Files)
	if len(because) == 0 {
		return nil
	}

	var findings []Finding
	if !carries(change.Files, isScenario) && !excused(change.Body, Scenario) {
		findings = append(findings, Finding{
			Promise: Scenario,
			Because: because,
			Wanted:  "a Scenario in features/<capability>.feature",
			Excuse:  excuseLine(Scenario),
		})
	}
	return findings
}

// Behaviour returns the paths that put a change under the rule, so a refusal can name them.
func Behaviour(files []File) []string {
	var found []string
	for _, file := range files {
		if isBehaviour(file.Path) {
			found = append(found, file.Path)
		}
	}
	return found
}

// isBehaviour says whether one path is a source of behaviour: Go the product runs, or the contracts
// it serves.
//
// A test file is how behaviour is proved rather than what it is, so a change that only touches tests
// is asked for nothing. gen/ is written by buf rather than by a person, and the proto file it is
// generated from is already counted here.
func isBehaviour(name string) bool {
	if strings.HasPrefix(name, "gen/") || strings.HasSuffix(name, "_test.go") {
		return false
	}
	return strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".proto")
}

// isScenario says whether one file is a feature file this change wrote or added to. Deleting the last
// one is not carrying one, which is the case a "did the diff touch features/" check gets wrong.
func isScenario(file File) bool {
	return file.Status != Deleted &&
		strings.HasPrefix(file.Path, "features/") &&
		strings.HasSuffix(file.Path, ".feature")
}

func carries(files []File, is func(File) bool) bool {
	for _, file := range files {
		if is(file) {
			return true
		}
	}
	return false
}

// excuseLine is how the pull request body says a change has none of one promise.
func excuseLine(promise string) string {
	return "No " + promise + ":"
}

// excused reads the body for the line that stands in for a promise, and accepts it only when a reason
// follows the colon. The line exists so a reviewer has something to disagree with, so a line with
// nothing after it is refused the same as no line at all.
func excused(body, promise string) bool {
	want := strings.ToLower(excuseLine(promise))
	fenced := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		// A body that explains the rule, or quotes the refusal, holds these words as an example. A
		// fence is where prose stops being a statement, so the check reads past it. The pull request
		// that added this check had both lines in a fenced block and would have excused itself.
		if strings.HasPrefix(trimmed, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		// A body is markdown, and the line is usually written as a bullet or in bold.
		trimmed = strings.TrimLeft(trimmed, "-*# ")
		trimmed = strings.ReplaceAll(trimmed, "**", "")
		if !strings.HasPrefix(strings.ToLower(trimmed), want) {
			continue
		}
		reason := strings.TrimSpace(trimmed[len(want):])
		if len(strings.Fields(reason)) >= reasonWords {
			return true
		}
	}
	return false
}
