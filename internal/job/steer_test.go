package job_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

func TestASteerIsTidiedIntoOneLine(t *testing.T) {
	tidy := job.TidySteer("  the workspace\n  has no secrets  ")
	if tidy != "the workspace has no secrets" {
		t.Fatalf("the steer tidies to %q", tidy)
	}
}

func TestASteerWithNoWordsIsRefused(t *testing.T) {
	err := job.Steered("   ")
	if err == nil {
		t.Fatal("a steer with no words was accepted, so the report would carry a blank line")
	}
	if !strings.Contains(err.Error(), "say what you had to say") {
		t.Fatalf("the refusal does not say what to type: %v", err)
	}
}

// A steer longer than a line is taken, and cut only where the listing draws it.
//
// A steer is made in the moment. The command refused one over 200 bytes, so the operator who had
// most to say was the one the system would not hear, and the mark was lost with the words.
func TestASteerLongerThanALineIsTakenAndCutOnlyWhereItIsDrawn(t *testing.T) {
	said := "the workspace has no secrets, " + strings.Repeat("and the run needs one to read the store, ", 20)
	if err := job.Steered(said); err != nil {
		t.Fatalf("a steer of %d bytes was refused: %v", len(said), err)
	}
	if kept := job.TidySteer(said); kept != strings.TrimSpace(said) {
		t.Fatalf("the steer is kept as %d bytes and it was typed as %d", len(kept), len(said))
	}

	drawn := job.SteerLine(job.TidySteer(said))
	if len(drawn) > job.SteerLimit {
		t.Fatalf("the listing draws %d bytes on a line held to %d: %q", len(drawn), job.SteerLimit, drawn)
	}
	if !strings.Contains(drawn, "the workspace has no secrets") {
		t.Fatalf("the cut took the start of what the operator said: %q", drawn)
	}
	if !strings.Contains(drawn, "krewe job show") {
		t.Fatalf("the cut does not say where the rest is: %q", drawn)
	}
}

// A steer that fits is drawn as it was typed, with no mark on it.
func TestASteerThatFitsIsDrawnWhole(t *testing.T) {
	said := "the workspace has no secrets"
	if drawn := job.SteerLine(said); drawn != said {
		t.Fatalf("a steer that fits is drawn as %q", drawn)
	}
}

func TestASteerOfExactlyTheLimitIsTaken(t *testing.T) {
	if err := job.Steered(strings.Repeat("x", job.SteerLimit)); err != nil {
		t.Fatalf("a steer of exactly the ceiling was refused: %v", err)
	}
}

func TestTheDefinitionShipsWithTheTool(t *testing.T) {
	said := job.WhatASteerIs
	for _, must := range []string{
		"should have known",
		"is not a steer",
	} {
		if !strings.Contains(said, must) {
			t.Fatalf("the definition does not say %q: %s", must, said)
		}
	}
}

// The comparison is the question the count exists to answer, so it is a sentence rather than two
// numbers a reader subtracts.
func TestOneJobIsComparedWithTheJobBeforeIt(t *testing.T) {
	for _, one := range []struct {
		name   string
		count  int
		before int
		says   string
	}{
		{name: "fewer", count: 3, before: 5, says: "2 fewer than the job before it"},
		{name: "more", count: 7, before: 5, says: "2 more than the job before it"},
		{name: "the same", count: 5, before: 5, says: "the same as the job before it"},
		{name: "nothing before it", count: 5, before: -1, says: "the first job here, so there is nothing to compare it with"},
	} {
		t.Run(one.name, func(t *testing.T) {
			if got := job.Compared(one.count, one.before); got != one.says {
				t.Fatalf("the comparison reads %q and not %q", got, one.says)
			}
		})
	}
}
