package job_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A session at the context ceiling writes down what it left behind, and every word of it survives.
//
// The handoff is the last thing that session is ever asked for. It is what a fresh session starts
// the job from, so a handoff refused for its length is a session that wrote the only record of its
// work and got nothing back, at the one moment it cannot write it again: the gate gives it no
// further task on that job. What was tried has the same shape, and the same guide.

// aHandoffSaying is prose of at least this many bytes, numbered so no two lines are the same, so a
// case that passes proves the words came back in the order they went in.
func aHandoffSaying(atLeast int) string {
	return paragraphs(atLeast, "What is left is the second half of the sweep. The reader keeps every "+
		"word and the warning goes above it, and the caps in the list still have to move. ")
}

func TestAHandoffOfAnyLengthIsKeptWordForWord(t *testing.T) {
	left := aHandoffSaying(job.HandoffLimit * 2)

	kept, _, err := job.TidyHandoff(left, "")
	if err != nil {
		t.Fatalf("a handoff of %d bytes was refused: %v", len(left), err)
	}
	if kept != left {
		t.Fatalf("the handoff was kept as %d bytes of the %d it was written with", len(kept), len(left))
	}
}

// What was tried is the half that stops the next session repeating a dead end, and it is the half a
// session writes at length, because a dead end takes a paragraph to describe.
func TestWhatASessionTriedIsKeptWordForWordAtAnyLength(t *testing.T) {
	tried := aHandoffSaying(job.HandoffLimit * 2)

	_, kept, err := job.TidyHandoff("the sweep is half done", tried)
	if err != nil {
		t.Fatalf("%d bytes of what a session tried were refused: %v", len(tried), err)
	}
	if kept != tried {
		t.Fatalf("what was tried was kept as %d bytes of the %d it was written with", len(kept), len(tried))
	}
}

// Carried through to the reader. A handoff kept whole and then cut on the way into the next task is
// the same loss one step later, and the next session is the only reader a handoff has.
func TestTheFreshSessionIsGivenTheWholeHandoff(t *testing.T) {
	left, tried := aHandoffSaying(job.HandoffLimit*2), aHandoffSaying(job.HandoffLimit*2)

	keptLeft, keptTried, err := job.TidyHandoff(left, tried)
	if err != nil {
		t.Fatalf("a handoff of %d bytes was refused: %v", len(left), err)
	}
	one := &job.Job{
		ID:      "b75f5bf6",
		Title:   "move the caps that still refuse text",
		Brief:   "the second half of the sweep",
		Product: "a session writing a long handoff keeps its words",
		Handoffs: []job.Handoff{{
			Job: "b75f5bf6", Seq: 1, Left: keptLeft, Tried: keptTried, Session: "session-1",
		}},
	}

	said := job.HandedOn(one)
	if !strings.Contains(said, left) {
		t.Fatalf("the fresh session is given %d bytes and the handoff it starts from is %d",
			len(said), len(left))
	}
	if !strings.Contains(said, tried) {
		t.Fatalf("the fresh session is not told the %d bytes of dead ends the session before it wrote",
			len(tried))
	}
}
