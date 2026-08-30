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

// The size the acceptance run of 29 August 2026 read out of the store, because nothing in the tool
// would say it.
const crewOnTheDay = 100_179

func longBody(length int) string { return strings.Repeat("a", length) }

// A listing that says which levels are set and nothing about how big they are is how a level reached
// a hundred thousand characters without anybody deciding to make it that big.
func TestTheListingSaysHowBigEachLevelIs(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "atlantic-blue")

	saying(t, longBody(1_886))
	mustRun(t, client, "context", "set", "atlantic-blue")

	listed := mustRun(t, client, "context")
	if !strings.Contains(listed, "1,886") {
		t.Errorf("the listing never says how big the workspace's level is:\n%s", listed)
	}
	if !strings.Contains(listed, "characters") {
		t.Errorf("the listing has a column of numbers and no unit on it:\n%s", listed)
	}
	if !strings.Contains(listed, "nothing written yet") {
		t.Errorf("a level nobody has written to no longer says so:\n%s", listed)
	}
}

// The finding itself: whoever writes a hundred thousand characters into the crew's level is told at
// the moment they write it, rather than by reading the contexts table in Postgres.
func TestSettingALevelOverTheMarkSaysWhoCarriesIt(t *testing.T) {
	client := testClient(t)

	saying(t, longBody(crewOnTheDay))
	set := mustRun(t, client, "context", "set", "crew")
	for _, want := range []string{
		"100,179 characters",
		"over the 20,000 character mark",
		"Every session in every workspace reads it",
		"quay context set <workspace>",
	} {
		if !strings.Contains(set, want) {
			t.Errorf("setting the crew's level never says %q:\n%s", want, set)
		}
	}
}

// A level under the mark is information and nothing else. A tool that warned on every write is a
// tool whose warnings nobody reads.
func TestSettingASmallLevelDoesNotWarn(t *testing.T) {
	client := testClient(t)

	saying(t, "no acronyms")
	set := mustRun(t, client, "context", "set", "crew")
	if !strings.Contains(set, "11 characters") {
		t.Errorf("setting did not report what it wrote: %q", set)
	}
	if strings.Contains(set, "mark") {
		t.Errorf("a level of eleven characters was warned about: %q", set)
	}
}

// The listing carries the same warning, so somebody who never ran the set finds out by looking.
func TestTheListingWarnsAboutALevelOverTheMark(t *testing.T) {
	client := testClient(t)
	saying(t, longBody(crewOnTheDay))
	mustRun(t, client, "context", "set", "crew")

	listed := mustRun(t, client, "context")
	if !strings.Contains(listed, "100,179 over the mark") {
		t.Errorf("the crew's row does not say it is over the mark:\n%s", listed)
	}
	if !strings.Contains(listed, "Every session in every workspace reads it") {
		t.Errorf("the listing says over the mark and never says what that costs:\n%s", listed)
	}
}
