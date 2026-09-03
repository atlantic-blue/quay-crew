package job_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A session lists what it would build, at whatever length the list needs, and the words survive.
//
// The failure is the one job a3d72b11 already paid for, one stage further on. A reading of 859 bytes
// was refused for its length, the session was asked once more, and the job was stopped with nobody
// having read a word of it. The reading now warns and keeps its text. The list of verticals still
// refuses: a line over its guide, an eighth vertical, or a record over three thousand bytes each
// throw the whole reply away, and a person is shown nothing.
//
// So the guides here tell a person something rather than taking something away. The list reaches the
// person whole, and a line above it says which part is long, how many bytes it is, and what the
// guide is.
//
// Every case here asserts the text comes back byte for byte, rather than asserting only that nothing
// failed. Text that is accepted and then cut is text the session lost.

// aLineOfAListOf is one line of a list, of exactly this many bytes, naming the person it serves so
// the deliverable rule leaves it alone. It opens and ends with words an assertion can look for, so a
// line cut at either end shows as a cut rather than as a pass.
func aLineOfAListOf(size int) string {
	const opens, ends = "a person pastes a link and gets the text back", "and this line ends here"
	middle := size - len(opens) - len(ends) - 2
	if middle < 1 {
		panic("a line this short cannot carry both ends")
	}
	return opens + " " + strings.Repeat("x", middle) + " " + ends
}

// aListSaying is a reply a session gives when it is asked what it would build, with one vertical and
// what it shows.
func aListSaying(vertical, shown string) string {
	return "Here is what I would build.\n\nVertical 1: " + vertical + "\nShown 1: " + shown
}

// The line a person reads is the one the guide is about, so a line over the guide is the case the
// whole requirement turns on.
func TestAVerticalOfAnyLengthIsReadAndKeptWordForWord(t *testing.T) {
	long := aLineOfAListOf(job.DesignLineLimit * 2)

	list, err := job.ReadDesign(aListSaying(long, "the transcript prints in the terminal"))
	if err != nil {
		t.Fatalf("a vertical of %d bytes was refused: %v", len(long), err)
	}
	if len(list.Verticals) != 1 {
		t.Fatalf("the reply read as %d verticals, want the one written", len(list.Verticals))
	}
	if list.Verticals[0].Text != long {
		t.Fatalf("the vertical was read as %d bytes of the %d it was written with",
			len(list.Verticals[0].Text), len(long))
	}
	// And it survives the road it travels: the system writes the list down in its own rendering and
	// hands that back to a person and to the session that plans from it.
	again := job.DesignIn(job.DesignText(list))
	if len(again.Verticals) != 1 || again.Verticals[0].Text != long {
		t.Fatalf("the kept list reads back as %+v, want the vertical it was written with", again.Verticals)
	}
}

// What a person is shown is the other half of a vertical, and it carries the same guide.
func TestWhatAVerticalShowsIsKeptWordForWordAtAnyLength(t *testing.T) {
	long := aLineOfAListOf(job.DesignLineLimit * 2)

	list, err := job.ReadDesign(aListSaying("a person pastes a link and gets the text back", long))
	if err != nil {
		t.Fatalf("a shown line of %d bytes was refused: %v", len(long), err)
	}
	if len(list.Verticals) != 1 || list.Verticals[0].Shown != long {
		t.Fatalf("what the vertical shows was read as %q, and it was written as %d bytes",
			shownOf(list), len(long))
	}
}

// shownOf is what the first vertical of a list shows, for a failure that has to print it.
func shownOf(list job.Design) string {
	if len(list.Verticals) == 0 {
		return ""
	}
	return list.Verticals[0].Shown
}

// An eighth vertical is a thing a person gets. A list that carries one is a list a person reads and
// answers, and today the eighth line throws the other seven away.
func TestAListLongerThanTheGuideIsKeptWholeWithEveryVerticalOnIt(t *testing.T) {
	const verticals = 9
	var written strings.Builder
	written.WriteString("Here is what I would build.\n\n")
	for at := 1; at <= verticals; at++ {
		fmt.Fprintf(&written, "Vertical %d: a person opens the transcript on surface %d\n", at, at)
		fmt.Fprintf(&written, "Shown %d: the page renders that transcript for the person on surface %d\n", at, at)
	}

	list, err := job.ReadDesign(written.String())
	if err != nil {
		t.Fatalf("a list of %d verticals was refused, and the guide is %d: %v",
			verticals, job.DesignVerticals, err)
	}
	if len(list.Verticals) != verticals {
		t.Fatalf("the list read as %d verticals, want the %d written", len(list.Verticals), verticals)
	}
	kept := job.DesignText(list)
	for at := 1; at <= verticals; at++ {
		if !strings.Contains(kept, fmt.Sprintf("Vertical %d: a person opens the transcript on surface %d", at, at)) {
			t.Fatalf("the kept list lost vertical %d: %q", at, kept)
		}
	}
}

// The whole record carries a guide of its own. Nothing can pass that guide and no other, because
// seven lines inside the line guide come to less than three thousand bytes, so this list is over
// both. What it holds is that the record survives whole.
func TestAListLongerThanTheWholeGuideIsKeptWordForWord(t *testing.T) {
	var written strings.Builder
	written.WriteString("Here is what I would build.\n\n")
	lines := make([]string, 0, job.DesignVerticals)
	for at := 1; at <= job.DesignVerticals; at++ {
		line := aLineOfAListOf(job.DesignLineLimit + 40)
		lines = append(lines, line)
		fmt.Fprintf(&written, "Vertical %d: %s\nShown %d: %s\n", at, line, at, line)
	}

	list, err := job.ReadDesign(written.String())
	if err != nil {
		t.Fatalf("a list the system renders as more than %d bytes was refused: %v", job.DesignLimit, err)
	}
	kept := job.DesignText(list)
	if len(kept) <= job.DesignLimit {
		t.Fatalf("this case wrote a list of %d bytes, which is inside the guide of %d and proves nothing",
			len(kept), job.DesignLimit)
	}
	for at, line := range lines {
		if !strings.Contains(kept, line) {
			t.Fatalf("the kept list lost line %d of %d bytes", at+1, len(line))
		}
	}
}

// The whole requirement, at the surface the operator actually reads: the question the job stops on.
//
// Driven through the controller rather than through the rendering alone, because what is held here
// is that the person is told. A warning composed anywhere that never reaches the question is a
// warning nobody gets.
func TestALongListReachesAPersonWithAWarningAndTheWholeText(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(listingJob())
	ctx := context.Background()
	long := aLineOfAListOf(job.DesignLineLimit * 2)

	controller.Tick(ctx)
	plane.lands(aListSaying(long, "the transcript prints in the terminal for the person"))
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Question == "" {
		t.Fatalf("the job is %q and put no question to a person: %s", got.Phase, got.Reason)
	}
	if !strings.Contains(got.Question, long) {
		t.Fatalf("the question a person is put lost the %d bytes it was about: %q", len(long), got.Question)
	}
	warning := warningAbout(got.Question, "Vertical 1", len(long), job.DesignLineLimit)
	if warning == "" {
		t.Fatalf("nothing in the question says Vertical 1 is %d bytes where the guide is %d: %q",
			len(long), job.DesignLineLimit, got.Question)
	}
	// Above the text rather than below it. A measurement at the far end of a long list is one the
	// reader meets after the thing it was about.
	if strings.Index(got.Question, warning) > strings.Index(got.Question, long) {
		t.Fatalf("the warning %q sits under the list it is about, and a person reads it after the text it "+
			"measured", warning)
	}
	// One task. A list that is asked for again costs a second one, and the second reply over the old
	// ceiling is what stopped the job.
	if plane.sent() != 1 {
		t.Fatalf("the system was asked to run %d tasks, want 1: it asked again for a shorter list", plane.sent())
	}
}
