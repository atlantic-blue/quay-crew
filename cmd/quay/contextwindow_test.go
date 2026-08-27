package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// The only way the crew ever learns how big the model's context window is. The runtime says it to the
// status line and to nothing else, so a status line that draws the line and writes nothing down
// leaves the console with a count and no share on every session in the crew.
func TestTheStatusLineWritesTheWindowSizeDownForTheCrew(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, sandbox.ContextWindowFile)

	rememberWindowSize(dir, 1_000_000)
	said, err := os.ReadFile(at)
	if err != nil {
		t.Fatalf("the size was not written: %v", err)
	}
	if string(said) != "1000000\n" {
		t.Errorf("the size reads %q, want %q", said, "1000000\n")
	}

	// Read back before writing, because this runs on every draw of a line under somebody's prompt and
	// the number almost never changes.
	before, err := os.Stat(at)
	if err != nil {
		t.Fatalf("stat the size: %v", err)
	}
	rememberWindowSize(dir, 1_000_000)
	after, err := os.Stat(at)
	if err != nil {
		t.Fatalf("stat the size again: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("the same size was written again, so every draw writes to the operator's disk")
	}

	rememberWindowSize(dir, 200_000)
	if said, err = os.ReadFile(at); err != nil || string(said) != "200000\n" {
		t.Errorf("a workspace that moved to another model still reads %q: %v", said, err)
	}
}

// A status line that cannot write says nothing about it. The line under the prompt is what the
// operator came for, and a listing without a share in it is a smaller loss than a conversation with
// an error where its context should be.
func TestAWindowSizeThatCannotBeWrittenIsNotAnError(t *testing.T) {
	rememberWindowSize(filepath.Join(t.TempDir(), "nowhere"), 1_000_000)
	rememberWindowSize("", 1_000_000)
	rememberWindowSize(t.TempDir(), 0)
}
