package main

import (
	"os"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
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

// Writing an org's context into a fresh workspace and then being told the system holds nothing was the
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
	for _, want := range []string{"untouched", "15 characters", "krewe context clear"} {
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
	for _, want := range []string{"context show", "context set", "context edit", "context clear"} {
		if !strings.Contains(printed, want) {
			t.Errorf("the usage does not name %q", want)
		}
	}
}

// A level could be written and never read back, so it could only be overwritten. Adding a paragraph
// meant already holding the whole text, and recovering what the system held meant reading the contexts
// table in the database.
func TestShowPrintsWhatALevelSaysAndNothingElse(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "juliantellez")
	body := "# atlantic-blue\n\nNever touch production data.\n"

	saying(t, body)
	mustRun(t, client, "context", "set", "juliantellez")

	if got := mustRun(t, client, "context", "show", "juliantellez"); got != body {
		t.Fatalf("show printed %q, and the level says %q", got, body)
	}
}

// The pair is the point: what comes out goes back in unchanged. A heading, a count or a newline
// added for the look of it becomes part of the level on the next set.
func TestShowAndSetAreAPairThatRoundTrips(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "juliantellez")
	// No trailing newline, so an added one would show. Trailing spaces and a blank line, because a
	// body is prose somebody wrote and a tidy up would eat them.
	body := "Never touch production data.  \n\nDeploy through the pipeline, never from a shell."

	saying(t, body)
	mustRun(t, client, "context", "set", "juliantellez")

	first := mustRun(t, client, "context", "show", "juliantellez")
	saying(t, first)
	mustRun(t, client, "context", "set", "juliantellez")
	second := mustRun(t, client, "context", "show", "juliantellez")

	if first != body {
		t.Fatalf("show printed %q, and the level says %q", first, body)
	}
	if second != first {
		t.Fatalf("the round trip changed the level: %q became %q", first, second)
	}
}

// Reading back the level a paragraph is being added to, appending to it, and writing it back. This
// is the whole thing the command exists for, so it is here as one test rather than implied by three.
func TestALevelCanBeAddedToRatherThanOverwritten(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "juliantellez")
	saying(t, "Never touch production data.\n")
	mustRun(t, client, "context", "set", "juliantellez")

	held := mustRun(t, client, "context", "show", "juliantellez")
	saying(t, held+"\nDeploy through the pipeline.\n")
	mustRun(t, client, "context", "set", "juliantellez")

	got := mustRun(t, client, "context", "show", "juliantellez")
	for _, want := range []string{"Never touch production data.", "Deploy through the pipeline."} {
		if !strings.Contains(got, want) {
			t.Errorf("the level no longer says %q: %q", want, got)
		}
	}
}

// The system's level is the one `krewe context edit` refuses by name, and it is the one an operator
// most wants to read: every session in the system is told it.
func TestShowReadsTheSystemLevelByName(t *testing.T) {
	client := testClient(t)
	saying(t, "no acronyms\n")
	mustRun(t, client, "context", "set", "system")

	if got := mustRun(t, client, "context", "show", "system"); got != "no acronyms\n" {
		t.Fatalf("the system level printed %q", got)
	}
}

// The project level, reached by its address, because that is the level most jobs are told at.
func TestShowReadsAProjectByItsAddress(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "juliantellez")
	mustRun(t, client, "project", "create", "house-bills")
	saying(t, "pay the water bill first\n")
	mustRun(t, client, "context", "set", "juliantellez/house-bills")

	got := mustRun(t, client, "context", "show", "juliantellez/house-bills")
	if got != "pay the water bill first\n" {
		t.Fatalf("the project level printed %q", got)
	}
	// A sibling level is not answered by the one that was asked for.
	if err := refused(t, client, "context", "show", "juliantellez"); err == nil {
		t.Error("the workspace level answered with the project's body")
	}
}

// Silence is what a broken read looks like too. A level that says nothing exits non zero and names
// how to write it, so `krewe context show x > file` cannot leave an empty file and a clean status.
func TestShowRefusesALevelThatSaysNothing(t *testing.T) {
	client := testClient(t)
	mustRun(t, client, "workspace", "create", "juliantellez")

	err := refused(t, client, "context", "show", "juliantellez")
	for _, want := range []string{"juliantellez", "says nothing yet", "krewe context set"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %s", want, err)
		}
	}
}

// A level standing among every other level in the listing, which is where a body is read from.
func TestPickContextFindsOneLevelAmongMany(t *testing.T) {
	dirs := []*quaycrewv1.ContextDir{
		{Scope: "system", Owner: "", Body: "no acronyms"},
		{Scope: "workspace", Owner: "w1", Body: "the org context"},
		{Scope: "workspace", Owner: "w2", Body: "another org"},
		{Scope: "project", Owner: "p1", Body: "pay the water bill first"},
	}
	for _, one := range []struct {
		scope, owner, want string
	}{
		{"system", "", "no acronyms"},
		{"workspace", "w1", "the org context"},
		{"workspace", "w2", "another org"},
		{"project", "p1", "pay the water bill first"},
		// An owner belonging to another scope is not a match: identifiers are unique, and reading
		// one scope's body under another's name would be worse than reading nothing.
		{"project", "w1", ""},
		// A level the listing does not carry says nothing, which is what a level that is there and
		// empty says too. Neither has anything to read back.
		{"workspace", "w3", ""},
	} {
		if got := pickContext(dirs, one.scope, one.owner); got != one.want {
			t.Errorf("%s %q read %q, want %q", one.scope, one.owner, got, one.want)
		}
	}
}

// The size the acceptance run of 29 August 2026 read out of the store, because nothing in the tool
// would say it.
const systemOnTheDay = 100_179

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

// The finding itself: whoever writes a hundred thousand characters into the system's level is told at
// the moment they write it, rather than by reading the contexts table in Postgres.
func TestSettingALevelOverTheMarkSaysWhoCarriesIt(t *testing.T) {
	client := testClient(t)

	saying(t, longBody(systemOnTheDay))
	set := mustRun(t, client, "context", "set", "system")
	for _, want := range []string{
		"100,179 characters",
		"over the 20,000 character mark",
		"Every session in every workspace reads it",
		"krewe context set <workspace>",
	} {
		if !strings.Contains(set, want) {
			t.Errorf("setting the system's level never says %q:\n%s", want, set)
		}
	}
}

// A level under the mark is information and nothing else. A tool that warned on every write is a
// tool whose warnings nobody reads.
func TestSettingASmallLevelDoesNotWarn(t *testing.T) {
	client := testClient(t)

	saying(t, "no acronyms")
	set := mustRun(t, client, "context", "set", "system")
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
	saying(t, longBody(systemOnTheDay))
	mustRun(t, client, "context", "set", "system")

	listed := mustRun(t, client, "context")
	if !strings.Contains(listed, "100,179 over the mark") {
		t.Errorf("the system's row does not say it is over the mark:\n%s", listed)
	}
	if !strings.Contains(listed, "Every session in every workspace reads it") {
		t.Errorf("the listing says over the mark and never says what that costs:\n%s", listed)
	}
}
