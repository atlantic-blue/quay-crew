package job

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/krewe/internal/model"
)

// The refusals first. A reader that always finds an outcome satisfies every test about finding one,
// so what has to be proved is the answers this does not read as a signal.

func TestAnAnswerWithNoOutcomeLineStatesNone(t *testing.T) {
	for name, answer := range map[string]string{
		"nothing at all":     "",
		"prose about proof":  "The tests proved the fix works, and the pull request is open.",
		"the word alone":     "proved",
		"a report with none": "I read the issue, cut the worktree and pushed the branch.\n\nThe suite is green.",
	} {
		t.Run(name, func(t *testing.T) {
			if got := OutcomeIn(answer); got != "" {
				t.Fatalf("OutcomeIn(%q) = %q, want no outcome", answer, got)
			}
		})
	}
}

// The case the issue names: the word inside a sentence rather than on the line as the outcome. A
// reader that matched the word anywhere would settle a job on a sentence saying the opposite.
func TestTheWordInsideASentenceIsNotTheOutcome(t *testing.T) {
	for name, answer := range map[string]string{
		"mid sentence":               "I would call the outcome: proved, but the deploy is untested.",
		"a sentence ending in it":    "I would call the outcome: proved",
		"decorated sentence":         "Outcome: proved, once somebody checks the deploy",
		"two words":                  "Outcome: proved and unproved",
		"the marker alone":           "Outcome:",
		"a word not in the set":      "Outcome: finished",
		"the phase, not the outcome": "Outcome: done",
	} {
		t.Run(name, func(t *testing.T) {
			if got := OutcomeIn(answer); got != "" {
				t.Fatalf("OutcomeIn(%q) = %q, want no outcome", answer, got)
			}
		})
	}
}

func TestAnAnswerStatesOneOutcomeOnALineOfItsOwn(t *testing.T) {
	for name, one := range map[string]struct {
		answer string
		want   string
	}{
		"on its own":       {"Outcome: proved", OutcomeProved},
		"under the prose":  {"I ran the suite and it is green.\n\nOutcome: proved\n", OutcomeProved},
		"over the prose":   {"Outcome: unproved\n\nNothing here runs the deploy.", OutcomeUnproved},
		"as a bullet":      {"- Outcome: blocked\n\nThe credential ran out.", OutcomeBlocked},
		"emphasised":       {"**Outcome:** decide\n\nThe brief does not say which store.", OutcomeDecide},
		"with a full stop": {"Outcome: blocked.", OutcomeBlocked},
		"capitalised":      {"Outcome: Proved", OutcomeProved},
		"lower marker":     {"outcome: proved", OutcomeProved},
	} {
		t.Run(name, func(t *testing.T) {
			if got := OutcomeIn(one.answer); got != one.want {
				t.Fatalf("OutcomeIn(%q) = %q, want %q", one.answer, got, one.want)
			}
		})
	}
}

// Two outcomes is a session that did not decide, and the first is what the record takes. Taking the
// last would take whichever one the model happened to write nearest the end.
func TestTheFirstOutcomeStatedIsTheOne(t *testing.T) {
	answer := "Outcome: blocked\n\nOn reflection.\n\nOutcome: proved"
	if got := OutcomeIn(answer); got != OutcomeBlocked {
		t.Fatalf("OutcomeIn(%q) = %q, want %q", answer, got, OutcomeBlocked)
	}
}

func TestOnlyTheFourWordsAreOutcomes(t *testing.T) {
	for _, known := range Outcomes() {
		if !KnownOutcome(known) {
			t.Fatalf("KnownOutcome(%q) says no, and it is in the set", known)
		}
		if OutcomeMeans(known) == "" {
			t.Fatalf("the outcome %q means nothing, so nothing can offer it back", known)
		}
	}
	for _, word := range []string{"", "done", "failed", "stopped", "finished", "Proved"} {
		if KnownOutcome(word) {
			t.Fatalf("KnownOutcome(%q) says yes, and it is not an outcome", word)
		}
	}
	if len(Outcomes()) != 4 {
		t.Fatalf("there are %d outcomes, and the set is four", len(Outcomes()))
	}
}

// The set is one set. A session offered one list of words and a listing filtered by another is the
// prose problem again, one layer down.
func TestTheSameWordsAreOfferedEverywhere(t *testing.T) {
	for _, known := range Outcomes() {
		if !strings.Contains(EndsWithAnOutcome(), known) {
			t.Fatalf("the line beside every brief does not offer %q", known)
		}
		if !strings.Contains(NoOutcomeStated("I finished"), known) {
			t.Fatalf("the refusal does not name %q", known)
		}
	}
	if !strings.Contains(EndsWithAnOutcome(), OutcomeMarker) {
		t.Fatalf("the line beside every brief does not say to write %q", OutcomeMarker)
	}
}

// A session that answers the way it was told is read the way it was told it would be. The brief and
// the reader are two halves of one contract, and nothing else holds them together.
func TestTheAnswerTheBriefAsksForIsRead(t *testing.T) {
	for _, known := range Outcomes() {
		answer := "I did the work.\n\n" + OutcomeMarker + " " + known
		if got := OutcomeIn(answer); got != known {
			t.Fatalf("an answer written as the brief asks reads as %q, want %q", got, known)
		}
	}
}

// The refusal has to say what was there instead, or a reader is left comparing the answer against a
// rule nobody restated.
func TestTheRefusalQuotesHowTheAnswerEnded(t *testing.T) {
	said := NoOutcomeStated("the suite is green and the branch is pushed")
	if !strings.Contains(said, "the branch is pushed") {
		t.Fatalf("the refusal does not say how the answer ended: %s", said)
	}
	long := NoOutcomeStated(veryLongAnswer())
	if len(long) > 600 {
		t.Fatalf("the refusal is %d bytes, and it is read on a listing", len(long))
	}
}

func veryLongAnswer() string {
	said := ""
	for range 200 {
		said += "the suite is green. "
	}
	return said
}

// The test double for a model spells the marker itself, because this package imports that one and a
// double cannot import what imports it. Two spellings of one line is a double that stops following
// the rule the moment the rule moves, and every test about a job would quietly become a test about
// that. So they are held together here.
func TestTheModelDoubleWritesTheLineThisPackageReads(t *testing.T) {
	if model.OutcomeMarker != OutcomeMarker {
		t.Fatalf("the double writes %q and this package reads %q", model.OutcomeMarker, OutcomeMarker)
	}
	if got := OutcomeIn(model.OutcomeMarker + " " + model.FakeOutcome); got != model.FakeOutcome {
		t.Fatalf("the line the double writes reads as %q, want %q", got, model.FakeOutcome)
	}
	if !KnownOutcome(model.FakeOutcome) {
		t.Fatalf("the double states %q, which is not an outcome", model.FakeOutcome)
	}
}
