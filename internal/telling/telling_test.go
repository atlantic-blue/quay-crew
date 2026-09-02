package telling_test

import (
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/telling"
)

// One wording for every surface. The console, the command line and the line under a conversation all
// draw from here, so a wait cannot be described three different ways depending on where a person is
// standing.

func waiting(id, why, want string, seconds int64, over bool) *quaycrewv1.Waiting {
	return &quaycrewv1.Waiting{Job: id, Why: why, Want: want, WaitedSeconds: seconds, OverLimit: over}
}

// The line names the job and what it wants, because "something waits for you" sends a person looking.
func TestTheLineNamesTheJobAndWhatItWants(t *testing.T) {
	said := telling.Line(waiting("f71415ba9c2e4d1a8b3c5d7e", job.WaitingAsking,
		"aurora or a key value store?", 30, false))

	for _, want := range []string{"f71415ba", "asks", "aurora or a key value store?"} {
		if !strings.Contains(said, want) {
			t.Errorf("the line does not say %q: %q", want, said)
		}
	}
	if strings.Contains(said, "waited") {
		t.Errorf("the line names an age on a wait inside the limit: %q", said)
	}
}

// Past the limit the age is named, in the words a person uses.
func TestTheLineNamesTheAgePastTheLimit(t *testing.T) {
	said := telling.Line(waiting("f71415ba9c2e4d1a8b3c5d7e", job.WaitingAsking, "which store?", 3840, true))
	if !strings.Contains(said, "waited 1 hour 4 minutes") {
		t.Errorf("the line does not carry the age: %q", said)
	}
}

// Each kind of wait reads as what it is. A job that failed and a job that asked need different things
// from a person, and one word for both would send them to the wrong screen.
func TestEachKindOfWaitReadsAsWhatItIs(t *testing.T) {
	for _, one := range []struct {
		why  string
		says string
	}{
		{job.WaitingAsking, "asks"},
		{job.WaitingBlocked, "stopped"},
		{job.WaitingChecks, "is red"},
	} {
		said := telling.Line(waiting("f71415ba9c2e4d1a8b3c5d7e", one.why, "something", 10, false))
		if !strings.Contains(said, one.says) {
			t.Errorf("a %s wait reads as %q, want it to say %q", one.why, said, one.says)
		}
	}
}

// One line whatever it carries. These are printed above a command's output and drawn inside a
// bordered panel, so a question that wrapped over four lines would push the output off the screen.
func TestALongQuestionIsHeldToOneLine(t *testing.T) {
	question := strings.Repeat("a very long question that nobody would read to the end of. ", 10)
	said := telling.Line(waiting("f71415ba9c2e4d1a8b3c5d7e", job.WaitingAsking, question, 10, false))

	if strings.Contains(said, "\n") {
		t.Fatalf("the line wraps: %q", said)
	}
	if len(said) > 100 {
		t.Fatalf("the line is %d characters, which is wider than a narrow terminal: %q", len(said), said)
	}
}

// A job that says nothing about what it wants still gets a line, because the job identifier is what
// a person acts on. A blank after the colon reads as the system being broken.
func TestAJobThatSaysNothingStillReadsAsALine(t *testing.T) {
	said := telling.Line(waiting("f71415ba9c2e4d1a8b3c5d7e", job.WaitingBlocked, "", 10, false))
	if !strings.Contains(said, "f71415ba") || strings.HasSuffix(strings.TrimSpace(said), ":") {
		t.Errorf("a job with no reason reads as %q", said)
	}
}

// The count, and the silence. Nothing waiting says nothing at all: a line that prints on every
// command forever is a line nobody reads by the second day.
func TestTheCountIsSaidAndNothingWaitingSaysNothing(t *testing.T) {
	if said := telling.Count(nil); said != "" {
		t.Errorf("nothing waiting says %q", said)
	}
	if said := telling.Count([]*quaycrewv1.Waiting{{}}); said != "1 job waits for you" {
		t.Errorf("one job waiting says %q", said)
	}
	if said := telling.Count([]*quaycrewv1.Waiting{{}, {}, {}, {}}); said != "4 jobs wait for you" {
		t.Errorf("four jobs waiting says %q", said)
	}
}

// Requirement 4 of quay-krewe#647: an operator reads a question that fits the terminal, and reaches
// the whole text behind it.
//
// A cap that refused a long question becomes a warning, so the record now carries questions of any
// length. This line is the narrow place such a question is drawn in, so this is where the cut lives.
// Two things decide whether it works for the person reading it, and they pull against each other:
// the line has to fit a terminal, and the reader has to know there is more and where to get it. A
// cut that says nothing reads as the whole question, and a person answers half a question.
func TestALongQuestionSaysInWordsThatItWasCut(t *testing.T) {
	question := "aurora serverless version two bills a minimum capacity continuously, and a key " +
		"value store on demand bills nothing at rest, " + strings.Repeat("and there is more to it. ", 40)
	said := telling.Line(waiting("f71415ba9c2e4d1a8b3c5d7e", job.WaitingAsking, question, 10, false))

	// Eighty columns is the narrowest terminal this line is written for, and it prints above whatever
	// command the person actually typed.
	if strings.Contains(said, "\n") {
		t.Fatalf("the line wraps: %q", said)
	}
	if len(said) > 80 {
		t.Errorf("the line is %d characters and a narrow terminal is 80: %q", len(said), said)
	}
	// It still names the job and the start of the question, or the cut took the meaning with it.
	for _, want := range []string{"f71415ba", "asks", "aurora serverless"} {
		if !strings.Contains(said, want) {
			t.Errorf("the line does not say %q: %q", want, said)
		}
	}
	// The words, not a mark. An ellipsis says the text stops and says nothing about where the rest
	// is, and the rest is the reason the question was worth writing at that length.
	if !strings.Contains(said, "krewe job show") {
		t.Errorf("the cut does not say where the whole question is: %q", said)
	}
	aQuestionThatFitsIsNotMarkedAsCut(t)
}

// A question that fits is left alone. It is checked inside the test above rather than beside it,
// because on its own it passes against the behaviour this replaces and proves nothing.
func aQuestionThatFitsIsNotMarkedAsCut(t *testing.T) {
	t.Helper()
	said := telling.Line(waiting("f71415ba9c2e4d1a8b3c5d7e", job.WaitingAsking,
		"aurora or a key value store?", 10, false))

	for _, marker := range []string{"krewe job show", "…"} {
		if strings.Contains(said, marker) {
			t.Errorf("a question that fits is marked as cut: %q", said)
		}
	}
}
