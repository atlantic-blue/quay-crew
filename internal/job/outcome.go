package job

import (
	"fmt"
	"strings"
)

// A job ends by stating one outcome from a fixed set, and the prose sits underneath it.
//
// The failure this answers is on the record of an acceptance run. Jobs reported "done", "complete",
// "the pull request is open" and "I could not finish because the credential expired". All four
// settled the same way, because the crew read the prose to decide the job was over. A job that could
// not do its work and a job that did it read identically to anything downstream, so the operator had
// to open each one to tell them apart, and nothing could be counted.
//
// So the signal is a word rather than a reading. The set is small and it is the system's, not the
// session's: a session that could invent its own word would be back to prose with a colon in front
// of it. What a flow branches on, what a listing filters by, and what a count of a job is made of is
// this word. The prose stays for the person.
//
// It is read off the answer, the way a pull request address and a base line already are, and for the
// same reason: the model reporting on its own job is the thing this exists to stop. See
// repository.go and resume.go, which are the same shape.

// The four outcomes. Nothing else is one.
const (
	// OutcomeProved is the work is done and something the session ran proves it.
	OutcomeProved = "proved"
	// OutcomeUnproved is the work is done and nothing proves it. It is its own word rather than a
	// missing one: work nobody checked and work that was checked must never read the same, and the
	// gap between them is what issue 536 exists to close.
	OutcomeUnproved = "unproved"
	// OutcomeBlocked is the work cannot be done, and the reason is under the line. A credential that
	// ran out, a repository nobody can reach, a decision the brief did not make.
	OutcomeBlocked = "blocked"
	// OutcomeDecide is a person has to decide before this goes any further. It is not a question on
	// the record, which is what krewe job ask writes: it is the session saying the job stops with a
	// person rather than with the work.
	OutcomeDecide = "decide"
)

// OutcomeMarker opens the line a session states its outcome on.
//
// One shape the system can find, so what it finds is what the session meant to say rather than a
// sentence that happened to hold the word. "The tests proved it" is prose about the work, and a
// reader that took it for the signal would be reading prose again.
const OutcomeMarker = "Outcome:"

// Outcomes is every outcome a job may end with, in the order a person reads them: the two that
// finished the work, then the two that did not.
func Outcomes() []string {
	return []string{OutcomeProved, OutcomeUnproved, OutcomeBlocked, OutcomeDecide}
}

// KnownOutcome says whether a word is an outcome, which is what a listing filter and a flow's
// choice node are both held to.
func KnownOutcome(word string) bool {
	for _, known := range Outcomes() {
		if word == known {
			return true
		}
	}
	return false
}

// OutcomeMeans is what one outcome says, in a sentence, for whoever is being offered the four.
func OutcomeMeans(outcome string) string {
	switch outcome {
	case OutcomeProved:
		return "the work is done and something you ran proves it"
	case OutcomeUnproved:
		return "the work is done and nothing proves it"
	case OutcomeBlocked:
		return "the work cannot be done, and the reason is under the line"
	case OutcomeDecide:
		return "a person has to decide before this goes any further"
	default:
		return ""
	}
}

// OutcomeOffered is the four words with what each one means, one per line, so a refusal and a brief
// offer the same list rather than two lists that drift.
func OutcomeOffered() string {
	lines := make([]string, 0, len(Outcomes()))
	for _, one := range Outcomes() {
		lines = append(lines, fmt.Sprintf("  %s: %s", one, OutcomeMeans(one)))
	}
	return strings.Join(lines, "\n")
}

// OutcomeIn is the outcome an answer states, and empty where it states none.
//
// Matched exactly. The line carries the marker and one of the four words and nothing else, so
// "Outcome: proved, once the deploy is checked" states no outcome and neither does "the tests proved
// it". Both are prose, and prose is what the crew was reading before this.
//
// A bullet or a heading in front of the marker is still the line, and so is the word wrapped in the
// emphasis a model reaches for when it is writing a report. Refusing over a dash or a pair of
// asterisks would refuse work that was done, and the shape being looked for is unmistakable either
// way.
//
// The first such line wins. An answer that states two different outcomes has not decided, and a
// reader taking the last would be taking whichever one the model wrote nearest the end.
func OutcomeIn(answer string) string {
	for _, line := range strings.Split(answer, "\n") {
		said := strings.TrimLeft(strings.TrimSpace(line), "-*#> \t")
		if len(said) < len(OutcomeMarker) || !strings.EqualFold(said[:len(OutcomeMarker)], OutcomeMarker) {
			continue
		}
		// The emphasis and the full stop come off, and nothing else does. A word with anything left
		// beside it is a sentence, and a sentence is not one of the four.
		word := strings.ToLower(strings.Trim(said[len(OutcomeMarker):], "*_`. \t"))
		if KnownOutcome(word) {
			return word
		}
	}
	return ""
}

// WithoutTheOutcome is the answer with the line that states the outcome taken out.
//
// The two are separate things, and a reader that wants one should not be handed the other. The whole
// answer stays on the job, because that is the record of what the session said. What is passed on to
// whatever comes next is the prose, and the word travels beside it as a field: a flow that compared a
// reply against "green" would otherwise be comparing it against the system's own line as well, and a
// prompt built from the answer would carry a machine signal into a person's sentence.
func WithoutTheOutcome(answer string) string {
	lines := strings.Split(answer, "\n")
	kept := make([]string, 0, len(lines))
	taken := false
	for _, line := range lines {
		if !taken && OutcomeIn(line) != "" {
			taken = true
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// EndsWithAnOutcome is the line every session doing a job is given beside its brief.
//
// It is added by the system rather than left to whoever wrote the brief, for the reason the pull
// request line and the step line are: a brief that forgets it produces a job nothing downstream can
// read, and every brief forgets eventually.
func EndsWithAnOutcome() string {
	return fmt.Sprintf("End your answer with one line of its own that starts with %s and carries one "+
		"of these four words and nothing else:\n%s\nEverything you want to say about the work goes "+
		"under that line. An answer with no such line does not end this job.",
		OutcomeMarker, OutcomeOffered())
}

// NoOutcomeStated is why a job whose answer stated no outcome does not settle.
//
// It stops rather than being asked again. A pull request is work that was done and not published, so
// asking for it costs one task and buys the work back; an outcome is one line the session was told
// to write in the task it has just answered, and asking again for it is paying a model to read its
// own instructions. The job is left saying what is missing and where the work is, which is what
// unfinished has to read as.
func NoOutcomeStated(answered string) string {
	return fmt.Sprintf("this job's answer states no outcome, so nothing can say whether the work was "+
		"done: it ends with %q rather than with a line carrying %s and one of %s. The answer is on the "+
		"record and the session is where it was: read it, and declare what is left.",
		lastWords(answered), OutcomeMarker, strings.Join(Outcomes(), ", "))
}

// lastWords is the end of an answer, for a refusal that has to say what was there instead. The end
// rather than the start, because the line being looked for is the last one, and a refusal quoting the
// opening of a report says nothing about why it did not match.
func lastWords(answer string) string {
	said := oneLine(answer)
	const shown = 60
	if len(said) <= shown {
		return said
	}
	return "..." + said[len(said)-shown:]
}

// nothingToDecide is the question a person is put when a session stopped for them and said nothing
// under the line. The session is named so the conversation can be read, because that is the only
// place the reason can still be.
const nothingToDecide = "This session stopped for a person and wrote nothing under the line, so " +
	"what it needs is only in its conversation. Read it, and tell it what to do."

// TheDecisionPutToAPerson is the question a session's answer asks, where that answer says a person
// has to decide.
//
// The prose under the outcome line is the question. The session was told to put everything it wants
// to say there, so reading it back is reading what it wrote rather than inventing a sentence on its
// behalf, and a person is answering the session's own words.
//
// The whole answer is kept, however long it is. The surfaces that draw a question are one line
// wide and cut what they draw there, so a record cut first would leave nothing behind the cut for
// krewe job show to print.
//
// It never comes back empty. A session that states the word and says nothing is still a session
// waiting on a person, and a record that dropped it because the prose was blank would be the failure
// this exists to end, one case further along.
func TheDecisionPutToAPerson(answer, session string) string {
	asked := strings.TrimSpace(WithoutTheOutcome(answer))
	if asked == "" {
		if session == "" {
			return nothingToDecide
		}
		return fmt.Sprintf("%s The conversation is %s.", nothingToDecide, session)
	}
	return asked
}
