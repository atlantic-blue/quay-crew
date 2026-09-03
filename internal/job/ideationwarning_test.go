package job_test

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A field longer than its guide is warned about, and the words are kept.
//
// The failure these hold is on the record of job a3d72b11. It wrote a correct reading of 859 bytes
// against a guide of 600, saying which of its findings a person told it and which it filled in
// itself. The system refused the whole reply for the length, asked once more, and stopped the job.
// Ten million tokens were spent and nothing was delivered, and the text nobody ever read was the
// only thing that job produced.
//
// So a guide tells a person something rather than taking something away. The record reaches the
// person whole, and a line above the long field says which field is long, how many bytes it is, and
// what the guide is, which is the whole of what the operator has to know to say "that is fine" or
// "say it shorter next time".

// theLongReading is the size the reading on that job actually was, and the field it was in. The
// number is the incident's, not a round one, because what these tests hold is that a person is told
// the measurement rather than a category.
const theLongReading = 859

// aSayingOf is a line of exactly this many bytes, with a start and an end an assertion can look for,
// so text cut at either end shows as a cut rather than as a pass. Single spaces throughout, because
// the system tidies the whitespace in a line before it keeps it.
func aSayingOf(bytes int) string {
	const opens, ends = "this saying opens here", "and this saying ends here"
	middle := bytes - len(opens) - len(ends) - 2
	if middle < 1 {
		panic("a saying this short cannot carry both ends")
	}
	return opens + " " + strings.Repeat("x", middle) + " " + ends
}

// aReadingSaying is a reply a session gives when it is asked what it understood, with one field
// replaced by whatever this test wants to be long.
func aReadingSaying(understood, notThis, told string) string {
	return "Here is what I make of it.\n\n" +
		"Understood: " + understood + "\n" +
		"Not: " + notThis + "\n" +
		"Told: " + told + "\n" +
		"Confidence: sure of the shape, less sure of the surface\n" +
		"Question 1: which surface does a person read this on"
}

// theNumbers is every number a line carries, for an assertion that has to say the warning measured
// something rather than that it mentioned a word.
var theNumbers = regexp.MustCompile(`\d+`)

// warningAbout is the line that says a named field is long, and empty where nothing says it.
//
// A line rather than the whole text: an operator reads this in a terminal, and a measurement spread
// over a paragraph is a measurement they have to assemble. The line has to carry the field, what it
// measured, and the guide it measured against, because two numbers with no field named do not say
// which of the fields to shorten.
func warningAbout(shown, field string, size, guide int) string {
	for _, line := range strings.Split(shown, "\n") {
		if !strings.Contains(line, field) {
			continue
		}
		numbers := theNumbers.FindAllString(line, -1)
		found := map[string]bool{}
		for _, one := range numbers {
			found[one] = true
		}
		if found[strconv.Itoa(size)] && found[strconv.Itoa(guide)] {
			return line
		}
	}
	return ""
}

// The whole requirement, at the surface the operator actually reads: the question the job stops on.
//
// It is driven through the controller rather than through the rendering alone, because what is being
// held is that the person is told. A warning composed anywhere that never reaches the question is a
// warning nobody gets.
func TestALongReadingReachesAPersonWithAWarningAndTheWholeText(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(readingJob())
	ctx := context.Background()
	long := aSayingOf(theLongReading)

	controller.Tick(ctx)
	plane.lands(aReadingSaying(long, "a page that takes an identifier", "the person pastes a link"))
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Question == "" {
		t.Fatalf("the job is %q and put no question to a person: %s", got.Phase, got.Reason)
	}
	if !strings.Contains(got.Question, long) {
		t.Fatalf("the question a person is put lost the %d bytes it was about: %q",
			theLongReading, got.Question)
	}
	warning := warningAbout(got.Question, "Understood", theLongReading, job.UnderstandingLimit)
	if warning == "" {
		t.Fatalf("nothing in the question says Understood is %d bytes where the guide is %d: %q",
			theLongReading, job.UnderstandingLimit, got.Question)
	}
}

// The same measurement, field by field, at the rendering the question is built from. Each of the two
// paragraphs a person reads first has its own guide, and a warning that named neither would leave
// the operator to find which one is long by counting.
func TestTheWarningNamesTheFieldItIsAboutAndBothNumbers(t *testing.T) {
	long := aSayingOf(theLongReading)
	for _, one := range []struct {
		field string
		reply string
	}{
		{"Understood", aReadingSaying(long, "a page that takes an identifier", "the person pastes a link")},
		{"Not", aReadingSaying("a page that takes a link", long, "the person pastes a link")},
	} {
		t.Run(one.field, func(t *testing.T) {
			read, err := job.ReadIdeation(one.reply)
			if err != nil {
				t.Fatalf("a reading of %d bytes was refused rather than warned about: %v",
					theLongReading, err)
			}
			shown := job.AskingWhetherThisIsRight("you paste a link and get the text back",
				job.IdeationText(read))
			if !strings.Contains(shown, long) {
				t.Fatalf("what a person is shown lost the long %s: %q", one.field, shown)
			}
			if warningAbout(shown, one.field, theLongReading, job.UnderstandingLimit) == "" {
				t.Fatalf("nothing says %s is %d bytes where the guide is %d: %q",
					one.field, theLongReading, job.UnderstandingLimit, shown)
			}
		})
	}

	// And a reading inside its guides is not warned about, because a warning on everything is a
	// warning nobody reads. This half already holds today; it is here so the pair cannot be satisfied
	// by warning about every field.
	short, err := job.ReadIdeation(aReadingSaying("a page that takes a link and gives back the text",
		"a page that takes an identifier", "the person pastes a link"))
	if err != nil {
		t.Fatalf("ReadIdeation: %v", err)
	}
	shown := job.AskingWhetherThisIsRight("you paste a link and get the text back",
		job.IdeationText(short))
	if strings.Contains(shown, strconv.Itoa(job.UnderstandingLimit)) {
		t.Fatalf("a reading inside its guides was warned about: %q", shown)
	}
}

// The warning sits above the text it is about, which is the shape a person accepted: the measurement
// first, and the words still there under it. A measurement at the far end of a long record is a
// measurement the reader meets after the thing it was about.
func TestTheWarningIsAboveTheTextItIsAbout(t *testing.T) {
	long := aSayingOf(theLongReading)
	read, err := job.ReadIdeation(
		aReadingSaying(long, "a page that takes an identifier", "the person pastes a link"))
	if err != nil {
		t.Fatalf("a reading of %d bytes was refused rather than warned about: %v", theLongReading, err)
	}
	shown := job.AskingWhetherThisIsRight("you paste a link and get the text back",
		job.IdeationText(read))
	warning := warningAbout(shown, "Understood", theLongReading, job.UnderstandingLimit)
	if warning == "" {
		t.Fatalf("nothing says Understood is %d bytes where the guide is %d: %q",
			theLongReading, job.UnderstandingLimit, shown)
	}
	if strings.Index(shown, warning) > strings.Index(shown, long) {
		t.Fatalf("the warning is under the text it is about, and a reader meets it too late: %q", shown)
	}
}

// A line of a list has its own guide, and it is measured the same way. The lists are where a session
// says which of its footings a person gave it, so a Told line thrown away for length is exactly the
// thing this stage exists to keep.
func TestALongLineOfAListIsWarnedAboutAndKeptWhole(t *testing.T) {
	long := aSayingOf(job.IdeationLineLimit + 59)
	read, err := job.ReadIdeation(
		aReadingSaying("a page that takes a link", "a page that takes an identifier", long))
	if err != nil {
		t.Fatalf("a Told line of %d bytes was refused rather than warned about: %v", len(long), err)
	}
	shown := job.AskingWhetherThisIsRight("you paste a link and get the text back",
		job.IdeationText(read))
	if !strings.Contains(shown, long) {
		t.Fatalf("what a person is shown lost the long Told line: %q", shown)
	}
	if warningAbout(shown, "Told", len(long), job.IdeationLineLimit) == "" {
		t.Fatalf("nothing says the Told line is %d bytes where the guide is %d: %q",
			len(long), job.IdeationLineLimit, shown)
	}
}

// The whole record has a guide of its own, and it is the one that decides whether a person sees the
// record at all. Every line stays, and the warning says how far over the record is.
func TestAWholeRecordOverItsGuideIsWarnedAboutAndKeptWhole(t *testing.T) {
	lines := []string{
		"Understood: " + aSayingOf(job.UnderstandingLimit),
		"Not: " + aSayingOf(job.UnderstandingLimit),
		"Confidence: sure of the shape, less sure of the surface",
		"Question 1: which surface does a person read this on",
	}
	for _, heading := range []string{"Told", "Assumed", "Unknown"} {
		for i := 0; i < job.IdeationPoints; i++ {
			lines = append(lines, fmt.Sprintf("%s: %s", heading, aSayingOf(job.IdeationLineLimit)))
		}
	}
	reply := strings.Join(lines, "\n")

	read, err := job.ReadIdeation(reply)
	if err != nil {
		t.Fatalf("a record whose every field is inside its guide was refused for its total: %v", err)
	}
	kept := job.IdeationText(read)
	if len(kept) <= job.IdeationLimit {
		t.Fatalf("this record is %d bytes and the guide is %d, so it proves nothing about a record "+
			"over its guide", len(kept), job.IdeationLimit)
	}
	shown := job.AskingWhetherThisIsRight("you paste a link and get the text back", kept)
	for _, line := range lines {
		if !strings.Contains(shown, line) {
			t.Fatalf("what a person is shown lost the line %q", line)
		}
	}
	warned := false
	for _, line := range strings.Split(shown, "\n") {
		numbers := theNumbers.FindAllString(line, -1)
		guide, size := false, false
		for _, one := range numbers {
			if one == strconv.Itoa(job.IdeationLimit) {
				guide = true
			}
			// Over the guide, rather than a byte count this test pins: the record grows by whatever the
			// warning itself costs, and what a person needs is the measurement.
			if measured, err := strconv.Atoi(one); err == nil && measured > job.IdeationLimit {
				size = true
			}
		}
		if guide && size {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("nothing says this record is %d bytes where the guide is %d: %q",
			len(kept), job.IdeationLimit, shown)
	}
}

// A sixth question is kept and warned about, where it used to take the other five with it.
//
// The count refused the whole reply, so a session that asked one question too many delivered
// nothing at all. A count is a guide like a length: the operator is told there are more than the
// guide, and reads them.
func TestASixthQuestionIsKeptAndWarnedAbout(t *testing.T) {
	asked := []string{
		"Understood: a page that takes a link",
		"Not: a page that takes an identifier",
		"Confidence: sure of the shape, less sure of the surface",
	}
	for i := 1; i <= job.IdeationQuestions+1; i++ {
		asked = append(asked, fmt.Sprintf("Question %d: which surface does a person read part %d on", i, i))
	}

	read, err := job.ReadIdeation(strings.Join(asked, "\n"))
	if err != nil {
		t.Fatalf("a reply asking %d questions was refused rather than warned about: %v",
			job.IdeationQuestions+1, err)
	}
	shown := job.AskingWhetherThisIsRight("you paste a link and get the text back",
		job.IdeationText(read))
	for _, question := range asked {
		if !strings.Contains(shown, question) {
			t.Fatalf("what a person is shown lost %q", question)
		}
	}
	if warningAbout(shown, "questions", job.IdeationQuestions+1, job.IdeationQuestions) == "" {
		t.Fatalf("nothing says the record asks %d questions where the guide is %d: %q",
			job.IdeationQuestions+1, job.IdeationQuestions, shown)
	}
}

// A sixth line of a list is kept and warned about, for the same reason.
func TestASixthLineOfAListIsKeptAndWarnedAbout(t *testing.T) {
	said := []string{
		"Understood: a page that takes a link",
		"Not: a page that takes an identifier",
		"Confidence: sure of the shape, less sure of the surface",
	}
	for i := 1; i <= job.IdeationPoints+1; i++ {
		said = append(said, fmt.Sprintf("Told: the person pastes a link on surface %d", i))
	}
	said = append(said, "Question 1: which surface does a person read this on")

	read, err := job.ReadIdeation(strings.Join(said, "\n"))
	if err != nil {
		t.Fatalf("a reply carrying %d Told lines was refused rather than warned about: %v",
			job.IdeationPoints+1, err)
	}
	shown := job.AskingWhetherThisIsRight("you paste a link and get the text back",
		job.IdeationText(read))
	for _, line := range said {
		if !strings.Contains(shown, line) {
			t.Fatalf("what a person is shown lost %q", line)
		}
	}
	if warningAbout(shown, "Told", job.IdeationPoints+1, job.IdeationPoints) == "" {
		t.Fatalf("nothing says the record carries %d Told lines where the guide is %d: %q",
			job.IdeationPoints+1, job.IdeationPoints, shown)
	}
}

// A record larger than a question may be still reaches a person whole.
//
// This is where the old ceiling sat. The record was held small so that it would fit the ceiling a
// question is held to, and the two numbers had to agree. Neither number takes anything away now, so
// what is held here is the outcome: the reading goes to the person as it was written, through the
// controller and the store, at a size the ceiling used to refuse.
func TestARecordLargerThanAQuestionReachesAPersonWhole(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(readingJob())
	ctx := context.Background()
	long := aSayingOf(job.QuestionLimit + 1000)

	controller.Tick(ctx)
	plane.lands(aReadingSaying(long, "a page that takes an identifier", "the person pastes a link"))
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Question == "" {
		t.Fatalf("the job is %q and put no question to a person: %s", got.Phase, got.Reason)
	}
	if len(got.Question) <= job.QuestionLimit {
		t.Fatalf("the question is %d bytes and a question may be %d, so this proves nothing about a "+
			"record over that size", len(got.Question), job.QuestionLimit)
	}
	if !strings.Contains(got.Question, long) {
		t.Fatalf("the question a person is put lost the %d bytes it was about: %q", len(long), got.Question)
	}
}
