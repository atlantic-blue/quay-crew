package main

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/display"
)

// The console showed ten columns and the command line four, so a session's cost, its mode and how
// long ago it was touched were visible in one place and invisible in the other. Whichever surface
// somebody learns first should teach them the other.
func TestTheListingHasTheSameColumnsAsTheConsole(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "dispatch", "hello")

	listed := mustRun(t, client, "sessions")
	for _, column := range display.SessionColumns() {
		if !strings.Contains(listed, column) {
			t.Errorf("the listing has no %q column:\n%s", column, listed)
		}
	}
	// A header is what makes ten columns readable. Reading 102 as a task count rather than as input
	// tokens is what happens without one.
	if !strings.HasPrefix(listed, "session ") {
		t.Errorf("the listing has no header row:\n%s", listed)
	}
	if !strings.Contains(listed, "edits") {
		t.Errorf("the listing does not say what mode a session runs in:\n%s", listed)
	}
}

// A listing narrowed to where you are standing looks exactly like a crew with fewer sessions in it.
func TestANarrowedListingSaysWhatItWasNarrowedTo(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")
	mustRun(t, client, "dispatch", "hello")
	mustRun(t, client, "workspace", "create", "elsewhere")
	mustRun(t, client, "project", "create", "other")
	mustRun(t, client, "dispatch", "hello there")

	// Standing in the second one, so the listing is narrower than the crew.
	narrowed := mustRun(t, client, "sessions")
	if !strings.Contains(narrowed, "elsewhere/other") {
		t.Errorf("the listing does not say what it was narrowed to:\n%s", narrowed)
	}
	if !strings.Contains(narrowed, "lists the whole crew") {
		t.Errorf("the listing does not say how to see everything:\n%s", narrowed)
	}
	if strings.Contains(narrowed, "house-bills") {
		t.Fatalf("the listing was not narrowed at all:\n%s", narrowed)
	}

	// And the whole crew is still one command away.
	whole := mustRun(t, client, "sessions", "me")
	if !strings.Contains(whole, "house-bills") {
		t.Errorf("naming a workspace did not widen the listing:\n%s", whole)
	}
}

// An empty listing has the same problem in reverse: "no sessions" reads as an empty crew.
func TestAnEmptyListingSaysWhereItLooked(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "me")
	mustRun(t, client, "project", "create", "house-bills")

	if said := mustRun(t, client, "sessions"); !strings.Contains(said, "no sessions in me/house-bills") {
		t.Errorf("an empty listing does not say where it looked: %q", said)
	}
}

// Columns as wide as their widest cell, because the command line has the whole terminal and cutting
// a value there helps nobody.
func TestNothingIsCutOutOfTheListing(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "a-workspace-with-a-long-name")
	mustRun(t, client, "project", "create", "and-a-project-with-a-longer-one")
	mustRun(t, client, "dispatch", "hello")

	listed := mustRun(t, client, "sessions")
	for _, whole := range []string{"a-workspace-with-a-long-name", "and-a-project-with-a-longer-one"} {
		if !strings.Contains(listed, whole) {
			t.Errorf("%q was cut:\n%s", whole, listed)
		}
	}
	// No trailing spaces: they are invisible and get copied along with whatever is selected.
	for _, line := range strings.Split(strings.TrimRight(listed, "\n"), "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("a line ends in spaces: %q", line)
		}
	}
}
