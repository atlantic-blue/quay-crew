package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// Requirement 3 of quay-krewe#675: a person reads a long reason as one line of 40 characters, and
// sees the mark that says the text goes on.
//
// The listing is one row for one job. A reason is free text somebody types, so it can run to a
// paragraph and it can carry a line break. Either one takes the listing with it: a long reason
// pushes the title off the terminal, and a line break turns one job into two rows that each read as
// half a job. So the row draws the reason in a column of its own width and marks the cut, the same
// way the claim column beside it does.

// theWidthAReasonIsDrawnIn is the column the row gives a reason, counting the mark that says the
// text goes on. It is the number the requirement states.
const theWidthAReasonIsDrawnIn = 40

// theMarkOfMoreText is the one character that says a reading stops before the text does. It is the
// character the claim column already cuts with.
const theMarkOfMoreText = "…"

// enoughOfATextToFindItAgain is the shortest run of a text this file will accept as that text on a
// row. A shorter run can be a coincidence of the other columns.
const enoughOfATextToFindItAgain = 8

// theReasonAJobStoppedOn is what an operator types when the cause takes a sentence. It is 87
// characters, so the row draws 39 of them and no more.
func theReasonAJobStoppedOn() string {
	return "the meter reading the supplier billed on is not the reading on the meter in the hallway"
}

// A long reason is cut where the column ends, and the row says the text goes on.
func TestALongReasonIsCutToTheColumnAndTheCutIsMarked(t *testing.T) {
	reason := theReasonAJobStoppedOn()
	if utf8.RuneCountInString(reason) <= theWidthAReasonIsDrawnIn {
		t.Fatalf("this reason is %d characters and proves nothing in a column of %d",
			utf8.RuneCountInString(reason), theWidthAReasonIsDrawnIn)
	}
	client := aSystemToJobIn(t)
	id := declaredHere(t, client, "read the electricity bill")
	mustRun(t, client, "job", "stop", id, reason)

	listed := mustRun(t, client, "job", "list")
	row := theRowFor(t, listed, "read the electricity bill")

	// The cut, counted. The column is 40 characters and the mark is one of them, so the row carries
	// the first 39 characters of the reason and then the mark.
	cut := reason[:theWidthAReasonIsDrawnIn-1] + theMarkOfMoreText
	if !strings.Contains(row, cut) {
		t.Errorf("the row is:\n%s\nand it does not draw the reason as %q, which is %d characters "+
			"counting the mark that says the text goes on", row, cut, theWidthAReasonIsDrawnIn)
	}
	// And nothing past the column reaches the row. A row that draws the whole reason has no cut to
	// mark, and it takes the title off the terminal with it.
	if beyond := reason[:theWidthAReasonIsDrawnIn+1]; strings.Contains(row, beyond) {
		t.Errorf("the row is:\n%s\nand it draws at least %q, which is past the column of %d",
			row, beyond, theWidthAReasonIsDrawnIn)
	}
	if !strings.Contains(row, theMarkOfMoreText) {
		t.Errorf("the row is:\n%s\nand nothing on it says the reason goes on", row)
	}
}

// A reason that fits is drawn whole, so the mark means what it says. A row that marks every reason
// tells a person nothing about which ones they have read all of.
func TestAReasonThatFitsTheColumnIsDrawnWholeAndUnmarked(t *testing.T) {
	reason := "the bill is not due yet"
	client := aSystemToJobIn(t)
	id := declaredHere(t, client, "read the electricity bill")
	mustRun(t, client, "job", "stop", id, reason)

	row := theRowFor(t, mustRun(t, client, "job", "list"), "read the electricity bill")

	if !strings.Contains(row, reason) {
		t.Errorf("the row is:\n%s\nand it does not carry the whole of %q, which fits in %d characters",
			row, reason, theWidthAReasonIsDrawnIn)
	}
	if strings.Contains(row, theMarkOfMoreText) {
		t.Errorf("the row is:\n%s\nand it says the reason goes on, and %q is all of it", row, reason)
	}
}

// The mark on a cut reason is the mark the claim column cuts with. The two columns sit beside each
// other on the one row, so a row that cuts a claim with one character and a reason with another
// reads as two different kinds of cut, and a person learns the mark twice.
//
// Neither cut is counted here. Each mark is read off the row itself, so this test holds an opinion
// about the two agreeing and no opinion about where either column ends.
func TestAReasonIsCutWithTheMarkTheClaimColumnCutsWith(t *testing.T) {
	claim := "atlantic-blue/quay-krewe#675 the row says why the job stopped"
	reason := theReasonAJobStoppedOn()
	client := aSystemToJobIn(t)
	said := mustRun(t, client, "job", "create", "--title", "read the electricity bill",
		"--brief", "open the bill and say when it is due", "--claim", claim)
	id := strings.Fields(said)[1]
	mustRun(t, client, "job", "stop", id, reason)

	row := theRowFor(t, mustRun(t, client, "job", "list"), "read the electricity bill")

	onTheClaim := theMarkTheRowCutWith(t, row, claim)
	onTheReason := theMarkTheRowCutWith(t, row, reason)
	if onTheReason != onTheClaim {
		t.Errorf("the row is:\n%s\nand it cuts the claim with %q and the reason with %q",
			row, onTheClaim, onTheReason)
	}
}

// theMarkTheRowCutWith is the character a row put after a text it drew part of. It takes the longest
// run of the text the row carries and reads the character that follows it, so it says where the row
// stopped without being told where that should be.
func theMarkTheRowCutWith(t *testing.T, row, text string) string {
	t.Helper()
	if strings.Contains(row, text) {
		t.Fatalf("the row is:\n%s\nand it carries the whole of %q, so nothing on it was cut", row, text)
	}
	letters := []rune(text)
	for n := len(letters) - 1; n >= enoughOfATextToFindItAgain; n-- {
		part := string(letters[:n])
		at := strings.Index(row, part)
		if at < 0 {
			continue
		}
		after := []rune(row[at+len(part):])
		if len(after) == 0 {
			t.Fatalf("the row is:\n%s\nand it ends on %q, with nothing to say the text goes on", row, part)
		}
		return string(after[0])
	}
	t.Fatalf("the row is:\n%s\nand no run of %q reaches it, so the row does not draw that text at all",
		row, text)
	return ""
}

// A reason with a line break in it reads as one line. The server writes its own words when nobody
// types any, and some of those run to a paragraph, so this is not only what an operator types.
func TestAReasonWithALineBreakIsDrawnAsOneLine(t *testing.T) {
	reason := "no meter reading\nthe supplier says so"
	client := aSystemToJobIn(t)
	id := declaredHere(t, client, "read the electricity bill")
	declaredHere(t, client, "pay the gas bill")
	mustRun(t, client, "job", "stop", id, reason)

	listed := mustRun(t, client, "job", "list")

	// Two jobs are two rows. A line break drawn as a line break makes three, and the third one reads
	// as a job with no identifier and no title.
	rows := theRowsOf(listed)
	if len(rows) != 2 {
		t.Fatalf("the listing has %d rows for 2 jobs, so the line break in the reason broke the row:\n%s",
			len(rows), listed)
	}
	row := theRowFor(t, listed, "read the electricity bill")
	// Both halves, on the one line, because a row that keeps the first half and drops the second
	// gives a person half a cause and no sign that there was more.
	for _, half := range []string{"no meter reading", "the supplier says so"} {
		if !strings.Contains(row, half) {
			t.Errorf("the row is:\n%s\nand %q is not on it", row, half)
		}
	}
}

// theRowsOf is the rows of a listing, which is everything above the line that says where the
// listing looked.
func theRowsOf(listed string) []string {
	rows, _, _ := strings.Cut(strings.TrimSpace(listed), "\n\n")
	if strings.TrimSpace(rows) == "" {
		return nil
	}
	return strings.Split(rows, "\n")
}

// theRowFor is the one row of a listing that carries a title.
func theRowFor(t *testing.T, listed, title string) string {
	t.Helper()
	found := ""
	for _, row := range theRowsOf(listed) {
		if strings.Contains(row, title) {
			if found != "" {
				t.Fatalf("two rows carry %q:\n%s", title, listed)
			}
			found = row
		}
	}
	if found == "" {
		t.Fatalf("no row of this listing carries %q:\n%s", title, listed)
	}
	return found
}

// The list of every length cap in this system names this one too. A cap nobody wrote down is a cap
// that cuts text next month with nobody able to say why. The list is
// changelog.d/647-every-length-cap-and-what-it-became.md, and the tests in internal/job hold the
// list to every cap the source declares.
func TestTheCapListNamesTheWidthAReasonIsDrawnIn(t *testing.T) {
	entries := theEntriesOfTheCapList(t)

	// The reader is proved on the column beside this one, which the list already names. A reader
	// that finds nothing at all would otherwise report the same thing as a list with no entry.
	if theEntryNaming(entries, "cmd/krewe/job.go", "claimWidth") == "" {
		t.Fatalf("this reading of the cap list finds %d entries and none of them is claimWidth, "+
			"which the list already carries, so the reading is wrong", len(entries))
	}

	entry := theEntryNaming(entries, "cmd/krewe/job.go", "reason")
	if entry == "" {
		t.Fatalf("the cap list says nothing about the width a reason is drawn in, in cmd/krewe/job.go")
	}
	for _, want := range []string{"40", "cut for display"} {
		if !strings.Contains(entry, want) {
			t.Errorf("the cap list entry reads %q, and it does not say %q", entry, want)
		}
	}
}

// theEntryNaming is the one entry of the list that names a file and a word.
func theEntryNaming(entries []string, file, word string) string {
	for _, one := range entries {
		if strings.Contains(one, file) && strings.Contains(one, word) {
			return one
		}
	}
	return ""
}

// theEntriesOfTheCapList reads the list as entries rather than as lines. One entry is one cap: it
// starts at a bullet and runs to the blank line or the next bullet, because the prose in that
// document wraps, and a cap's number and its marking can sit on the second line of its own entry.
func theEntriesOfTheCapList(t *testing.T) []string {
	t.Helper()
	var entries []string
	running := false
	for _, line := range strings.Split(theCapList(t), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "- "):
			entries = append(entries, trimmed)
			running = true
		case trimmed == "":
			running = false
		case running:
			entries[len(entries)-1] += " " + trimmed
		}
	}
	return entries
}

// theCapList is the document that names every length cap in this system.
func theCapList(t *testing.T) string {
	t.Helper()
	at := filepath.Join("..", "..", "changelog.d", "647-every-length-cap-and-what-it-became.md")
	body, err := os.ReadFile(at)
	if err != nil {
		t.Fatalf("reading the cap list at %s: %v", at, err)
	}
	return string(body)
}

// The change carries its own entry in the changelog, named after the issue it answers. A person
// reading the release learns that a stopped row now says why it stopped.
func TestTheChangelogSaysTheRowNowCarriesTheReason(t *testing.T) {
	found, err := filepath.Glob(filepath.Join("..", "..", "changelog.d", "675-*.md"))
	if err != nil {
		t.Fatalf("reading changelog.d: %v", err)
	}
	if len(found) == 0 {
		t.Fatalf("changelog.d holds no fragment for quay-krewe#675, so the release says nothing " +
			"about the reason on the row")
	}
	body, err := os.ReadFile(found[0])
	if err != nil {
		t.Fatalf("reading %s: %v", found[0], err)
	}
	entry := string(body)
	if !strings.HasPrefix(entry, "**") {
		first, _, _ := strings.Cut(entry, "\n")
		t.Errorf("%s starts %q, and an entry starts with the bold sentence that says what changed",
			found[0], first)
	}
	if !strings.Contains(entry, "reason") {
		t.Errorf("%s says nothing about the reason a job stopped:\n%s", found[0], entry)
	}
}
