package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A seventeenth label is kept, and the operator is told how many labels the job carries.
//
// The declaration refused the whole job for it. An operator who labelled a job one way too many
// times got no job at all, and the labels are how anybody finds work again later. The count is this
// system's own number rather than a rule from anywhere else, so it says what it is and lets the
// person decide.
//
// This goes through the tool rather than through the store, because the store never refused any of
// it. What refused it stands between the two, so a case that wrote to the store would pass today
// while a person still could not declare the job.

// numbersOnTheLine is every number a line carries, so an assertion says the line measured something
// rather than that it holds a word.
var numbersOnTheLine = regexp.MustCompile(`\d+`)

// theCountLine is the line that says how many of something there are and what the guide is, and
// empty where nothing says it. One line, because an operator reads this in a terminal and a
// measurement spread over a paragraph is a measurement they assemble themselves.
func theCountLine(shown, about string, count, guide int) string {
	for _, line := range strings.Split(shown, "\n") {
		if !strings.Contains(line, about) {
			continue
		}
		found := map[string]bool{}
		for _, one := range numbersOnTheLine.FindAllString(line, -1) {
			found[one] = true
		}
		if found[strconv.Itoa(count)] && found[strconv.Itoa(guide)] {
			return line
		}
	}
	return ""
}

func TestJobShowReadsSeventeenLabelsBackAndSaysHowManyThereAre(t *testing.T) {
	client := aSystemToJobIn(t)
	over := job.LabelCount + 1

	args := []string{"job", "create", "--title", "read the electricity bill", "--brief", "open it"}
	for i := 1; i <= over; i++ {
		args = append(args, "--label", fmt.Sprintf("key-%d=value-%d", i, i))
	}
	said := mustRun(t, client, args...)

	shown := mustRun(t, client, "job", "show", strings.Fields(said)[1])
	for i := 1; i <= over; i++ {
		if !strings.Contains(shown, fmt.Sprintf("label key-%d=value-%d", i, i)) {
			t.Fatalf("krewe job show says:\n%s\nwant it to print label %d of %d", shown, i, over)
		}
	}
	line := theCountLine(shown, "label", over, job.LabelCount)
	if line == "" {
		t.Fatalf("krewe job show says:\n%s\nwant a line saying this job carries %d labels where the "+
			"guide is %d", shown, over, job.LabelCount)
	}
	if strings.Index(shown, line) > strings.Index(shown, "label key-1=value-1") {
		t.Fatalf("the count is under the labels it is about, and a reader meets it too late:\n%s", shown)
	}
}

// A job inside the guide is not told how many labels it has, because a line on every job is a line
// nobody reads. This half holds today, and it is here so the pair cannot be answered by counting on
// everything.
func TestJobShowSaysNothingAboutTheCountOfAFewLabels(t *testing.T) {
	client := aSystemToJobIn(t)

	said := mustRun(t, client, "job", "create", "--title", "read the electricity bill",
		"--brief", "open it", "--label", "owner=house", "--label", "kind=bills")

	shown := mustRun(t, client, "job", "show", strings.Fields(said)[1])
	if strings.Contains(shown, "guide") {
		t.Fatalf("a job carrying two labels was told about the guide:\n%s", shown)
	}
}
