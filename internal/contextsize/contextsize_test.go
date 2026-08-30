package contextsize_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/contextsize"
)

// The size in the issue, so the numbers in these tests are the ones a person read out of the store
// on 29 August 2026 rather than round figures chosen to pass.
const systemOnTheDay = 100_179

func body(length int) string { return strings.Repeat("a", length) }

// A level under the mark is information and nothing else. A system that warned about every level would
// be a system nobody reads the warnings of.
func TestALevelUnderTheMarkSaysNothing(t *testing.T) {
	reading := contextsize.Read("workspace", "atlantic-blue", body(1_886))
	if reading.Over() {
		t.Fatalf("1,886 characters is over the %d mark", contextsize.Mark)
	}
	if got := reading.Note(); got != "" {
		t.Errorf("a level under the mark carries a note: %q", got)
	}
	if got := reading.Say(); got != "" {
		t.Errorf("a level under the mark warns: %q", got)
	}
	if got := reading.Cell(); got != "1,886" {
		t.Errorf("the listing cell says %q, want %q", got, "1,886")
	}
}

// The mark is a mark, so the character that reaches it is still under it and the one past it is not.
func TestTheMarkIsWhereTheWarningStarts(t *testing.T) {
	for _, tc := range []struct {
		length int
		over   bool
	}{
		{length: contextsize.Mark - 1, over: false},
		{length: contextsize.Mark, over: false},
		{length: contextsize.Mark + 1, over: true},
	} {
		if got := contextsize.Read("system", "", body(tc.length)).Over(); got != tc.over {
			t.Errorf("%d characters reports over=%v, want %v", tc.length, got, tc.over)
		}
	}
}

// The whole finding: a hundred thousand characters at the system level, and nothing anywhere said so.
func TestTheSystemLevelOnTheDaySaysHowBigItIsAndWhoCarriesIt(t *testing.T) {
	reading := contextsize.Read("system", "", body(systemOnTheDay))
	if !reading.Over() {
		t.Fatalf("%d characters is not over the %d mark", systemOnTheDay, contextsize.Mark)
	}
	said := reading.Say()
	for _, want := range []string{
		"100,179",                          // the size, grouped, as a person reads it
		"20,000 character mark",            // what it is measured against
		"Every session in every workspace", // why the system's level is the one that matters
		"quay context set <workspace>",     // the move that makes it smaller
	} {
		if !strings.Contains(said, want) {
			t.Errorf("the warning never says %q:\n%s", want, said)
		}
	}
	if got := reading.Cell(); got != "100,179 over the mark" {
		t.Errorf("the listing cell says %q, and a reader scanning the column learns nothing", got)
	}
}

// A note is one line, because a listing prints one per level that is over and a paragraph each would
// bury the listing it is under.
func TestTheNoteIsOneLine(t *testing.T) {
	note := contextsize.Read("system", "", body(systemOnTheDay)).Note()
	if note == "" {
		t.Fatal("the system's level carries no note at all")
	}
	if strings.Contains(note, "\n") {
		t.Errorf("the note is more than one line:\n%s", note)
	}
	if !strings.HasPrefix(contextsize.Read("system", "", body(systemOnTheDay)).Say(), note) {
		t.Error("what is said at set time does not start with the line the listing shows, so the two drift")
	}
}

// Each level names who reads it and where to move what does not belong. A workspace told to move
// things to a workspace is advice that moves nothing.
func TestEachScopeNamesItsOwnReachAndItsOwnMove(t *testing.T) {
	for _, tc := range []struct {
		scope, name string
		reach, move string
	}{
		{scope: "system", name: "", reach: "every session in every workspace",
			move: "quay context set <workspace>            what one organisation does"},
		{scope: "workspace", name: "atlantic-blue", reach: "every session in this workspace",
			move: "quay context set <workspace>/<project>"},
		{scope: "project", name: "transcript", reach: "every session in this project",
			move: "repository"},
	} {
		said := contextsize.Read(tc.scope, tc.name, body(systemOnTheDay)).Say()
		if !strings.Contains(strings.ToLower(said), tc.reach) {
			t.Errorf("the %s warning never says %q reads it:\n%s", tc.scope, tc.reach, said)
		}
		if !strings.Contains(said, tc.move) {
			t.Errorf("the %s warning never says %q:\n%s", tc.scope, tc.move, said)
		}
		if tc.name != "" && !strings.Contains(said, tc.name) {
			t.Errorf("the %s warning does not name which one it is about:\n%s", tc.scope, said)
		}
	}
}

// A level nobody has written to is the normal state of a fresh system, and a column of noughts under a
// heading that says characters reads as something broken.
func TestALevelWithNothingInItSaysSo(t *testing.T) {
	if got := contextsize.Read("project", "house-bills", "").Cell(); got != "nothing written yet" {
		t.Errorf("an empty level's cell says %q", got)
	}
}

func TestACountReadsTheWayAPersonSaysOne(t *testing.T) {
	for _, tc := range []struct {
		count int
		want  string
	}{
		{count: 0, want: "0 characters"},
		{count: 1, want: "1 character"},
		{count: 15, want: "15 characters"},
		{count: 1_886, want: "1,886 characters"},
		{count: systemOnTheDay, want: "100,179 characters"},
		{count: 1_000_000, want: "1,000,000 characters"},
	} {
		if got := contextsize.Characters(tc.count); got != tc.want {
			t.Errorf("%d reads as %q, want %q", tc.count, got, tc.want)
		}
	}
}

// The number the issue reported came from `select length(body) from contexts`, and Postgres counts
// characters. Counting bytes instead makes the system report a level as larger than the database says
// it is, and the gap grows with every accented letter in it. Context is prose, so it has them.
func TestASizeCountsCharactersAndNotBytes(t *testing.T) {
	// Twelve characters, and more than twelve bytes: two of them are outside the ASCII range.
	prose := "café déjà vu"
	if got := contextsize.Read("workspace", "acme", prose).Characters; got != 12 {
		t.Errorf("%q reads as %d characters, want 12 (it is %d bytes)", prose, got, len(prose))
	}
	if got := contextsize.Characters(contextsize.Read("system", "", prose).Characters); got != "12 characters" {
		t.Errorf("the count reads %q", got)
	}
	// And the mark is measured the same way, or a level of accented prose is called large while the
	// database says it is under.
	accented := strings.Repeat("é", contextsize.Mark)
	if contextsize.Read("system", "", accented).Over() {
		t.Errorf("%d accented characters is called over the %d mark, and it is exactly on it",
			contextsize.Mark, contextsize.Mark)
	}
}
