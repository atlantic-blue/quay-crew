package main

import (
	"os"
	"strings"
	"testing"
)

// saying pipes text into the next invocation, the way a shell redirection does, and puts standard
// input back afterwards.
func saying(t *testing.T, text string) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	was := os.Stdin
	os.Stdin = file
	t.Cleanup(func() { os.Stdin = was; _ = file.Close() })
}

// Writing an org's context into a fresh workspace and then being told the crew holds nothing was the
// listing walking projects: a workspace with none contributed no row, so what had been written was
// invisible until a project happened to exist.
func TestAWorkspaceWithNoProjectsStillShowsItsContext(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "juliantellez")

	saying(t, "the org context")
	if set := mustRun(t, client, "context", "set", "juliantellez"); !strings.Contains(set, "15 characters") {
		t.Fatalf("setting did not report what it wrote: %q", set)
	}

	listed := mustRun(t, client, "context")
	if !strings.Contains(listed, "juliantellez") {
		t.Fatalf("the workspace has no row at all: %q", listed)
	}
	if !strings.Contains(listed, "the org context") {
		t.Errorf("the workspace's row does not show what was written: %q", listed)
	}
}

// There is no undo. An empty standard input is a forgotten redirection or a file that turned out to
// be empty, and obeying it silently erased whatever the level said.
func TestNothingOnStandardInputDoesNotEraseWhatIsThere(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "juliantellez")
	saying(t, "the org context")
	mustRun(t, client, "context", "set", "juliantellez")

	saying(t, "")
	err := refused(t, client, "context", "set", "juliantellez")
	for _, want := range []string{"untouched", "15 characters", "quay context clear"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %s", want, err)
		}
	}

	if listed := mustRun(t, client, "context"); !strings.Contains(listed, "the org context") {
		t.Fatalf("the context was erased anyway: %q", listed)
	}
}

// Whitespace is what a file of blank lines gives, and it reads as empty to a person.
func TestWhitespaceIsTreatedAsNothing(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "juliantellez")
	saying(t, "the org context")
	mustRun(t, client, "context", "set", "juliantellez")

	saying(t, "  \n\n  ")
	if err := refused(t, client, "context", "set", "juliantellez"); !strings.Contains(err.Error(), "untouched") {
		t.Errorf("a file of blank lines was written over the context: %s", err)
	}
}

// Emptying a level on purpose still has to be possible, and it says what it removed.
func TestClearEmptiesALevelAndSaysWhatItRemoved(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "juliantellez")
	saying(t, "the org context")
	mustRun(t, client, "context", "set", "juliantellez")

	cleared := mustRun(t, client, "context", "clear", "juliantellez")
	if !strings.Contains(cleared, "emptied") || !strings.Contains(cleared, "15 characters") {
		t.Fatalf("clear did not say what it removed: %q", cleared)
	}
	if listed := mustRun(t, client, "context"); strings.Contains(listed, "the org context") {
		t.Fatalf("clear left the context in place: %q", listed)
	}
	// Clearing what is already empty is not a failure, it is a no op that says so.
	if again := mustRun(t, client, "context", "clear", "juliantellez"); !strings.Contains(again, "already empty") {
		t.Errorf("clearing an empty level did not say so: %q", again)
	}
}

// The command that loads a file was the one nobody could find: the usage offered only the listing
// and the editor, and an editor cannot be scripted.
func TestTheUsageNamesEveryContextCommand(t *testing.T) {
	client := testClient(t)

	printed := mustRun(t, client, "help")
	for _, want := range []string{"context set", "context edit", "context clear"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the usage does not name %q", want)
		}
	}
}
