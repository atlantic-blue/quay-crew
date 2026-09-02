package job_test

import (
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A plan read by several lenses, and only what none of them settled put to a person. These hold the
// rules that decide which of the two a row is.

// The quiet case first. A change that fires on every job passes every test about finding questions
// and is worth nothing, so the job that records no question is the first thing here.
func TestAReadingThatSettledEverythingAsksNothing(t *testing.T) {
	if open := job.OpenQuestions(nil); len(open) != 0 {
		t.Fatalf("a job with no rows holds %d open rows", len(open))
	}
	if rendered := job.RenderQuestions(nil); rendered != "" {
		t.Fatalf("a job with no rows renders %q, and a graph reads that as something to ask about", rendered)
	}
	settled := []job.Question{
		{Seq: 1, Text: "what does a person type", Status: job.QuestionSettled, Answer: "a link"},
		{Seq: 2, Text: "where does the text come from", Status: job.QuestionSettled, Answer: "the captions"},
	}
	if rendered := job.RenderQuestions(settled); rendered != "" {
		t.Fatalf("a reading that settled every row renders %q, want nothing to ask", rendered)
	}
}

func TestAnOpenRowIsRenderedByTheNumberAReaderSettlesBy(t *testing.T) {
	rows := []job.Question{
		{Seq: 1, Text: "which store", Status: job.QuestionSettled, Answer: "the key value one"},
		{Seq: 2, Text: "what does a person type", AskedBy: "test-writer", Status: job.QuestionOpen},
	}
	rendered := job.RenderQuestions(rows)
	if !strings.HasPrefix(rendered, "2. what does a person type") {
		t.Fatalf("the open row renders as %q, want it numbered 2", rendered)
	}
	if strings.Contains(rendered, "which store") {
		t.Fatalf("a settled row reached the person: %q", rendered)
	}
	if !strings.Contains(rendered, "test-writer") {
		t.Fatalf("the row does not say which lens asked it: %q", rendered)
	}
}

// The reader that comes second is handed the rows and never the reading behind them. It cannot be
// led by the earlier answer, and it cannot use it: that is the trade, and it is deliberate.
func TestALaterReaderIsHandedTheOpenRowsAndNoAnswer(t *testing.T) {
	rows := []job.Question{
		{Job: "first", Seq: 1, Text: "which store", Status: job.QuestionSettled,
			Answer: "the key value one", SettledBy: "architect"},
		{Job: "first", Seq: 2, Text: "what does a person type", AskedIn: "first", Status: job.QuestionOpen},
	}
	carried := job.CarriedQuestions(rows, "second")
	if len(carried) != 1 {
		t.Fatalf("the next reader was handed %d rows, want the one still open", len(carried))
	}
	if carried[0].Seq != 2 || carried[0].Job != "second" {
		t.Fatalf("the row reached the reader as %+v, want row 2 on the second job", carried[0])
	}
	if carried[0].AskedIn != "first" {
		t.Fatalf("the row says it was written in %q, and the ceiling counts by that", carried[0].AskedIn)
	}
	if carried[0].Answer != "" || carried[0].SettledBy != "" {
		t.Fatalf("the earlier reader's answer travelled with the row: %+v", carried[0])
	}
}

// The ceiling counts what this reading wrote and never what it was handed. A reader refused its
// first question for somebody else's work would read the plan and report nothing.
func TestTheFourthQuestionFromOneReadingIsRefused(t *testing.T) {
	rows := []job.Question{
		{Seq: 1, Text: "one", AskedIn: "reader"},
		{Seq: 2, Text: "two", AskedIn: "reader"},
		{Seq: 3, Text: "three", AskedIn: "reader"},
	}
	err := job.RoomForAQuestion(rows, "reader")
	if err == nil {
		t.Fatal("a fourth question from one reading was accepted")
	}
	for _, phrase := range []string{"3", "one reading may write"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Fatalf("the refusal is %q, want it to name the ceiling of %d", err, job.QuestionRowCount)
		}
	}
}

func TestAReaderHandedThreeRowsMayStillWriteItsOwn(t *testing.T) {
	rows := []job.Question{
		{Seq: 1, Text: "one", AskedIn: "critic"},
		{Seq: 2, Text: "two", AskedIn: "critic"},
		{Seq: 3, Text: "three", AskedIn: "critic"},
	}
	if err := job.RoomForAQuestion(rows, "architect"); err != nil {
		t.Fatalf("the second reader was refused its first question: %v", err)
	}
}

// The same hole named twice is one hole, however it was punctuated. The refusal names the row it
// repeats, because a reader told only that it was refused spends its next question saying it again.
func TestADuplicateQuestionIsTheRowItRepeats(t *testing.T) {
	rows := []job.Question{{Seq: 4, Text: "What does a person type, and what comes back?"}}
	repeated, already := job.AlreadyAsked(rows, "what does a person type and what comes back")
	if !already {
		t.Fatal("the same question in different punctuation was read as a new one")
	}
	if repeated.Seq != 4 {
		t.Fatalf("the duplicate names row %d, want 4", repeated.Seq)
	}
	if _, already := job.AlreadyAsked(rows, "which store holds the text"); already {
		t.Fatal("a question about something else was refused as a duplicate")
	}
}

func TestAQuestionOverTheCeilingIsRefusedAndTheRefusalSaysHowLong(t *testing.T) {
	_, err := job.TidyQuestionRow(strings.Repeat("a", job.QuestionRowLimit+1))
	if err == nil {
		t.Fatal("a question of 201 bytes was accepted")
	}
	for _, phrase := range []string{"201", "200"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Fatalf("the refusal is %q, want it to say how long the row is and how long one may be", err)
		}
	}
	if _, err := job.TidyQuestionRow(strings.Repeat("a", job.QuestionRowLimit)); err != nil {
		t.Fatalf("a question at the ceiling was refused: %v", err)
	}
}

func TestAQuestionIsKeptAsOneLine(t *testing.T) {
	kept, err := job.TidyQuestionRow("  what does a person\n  type  ")
	if err != nil {
		t.Fatalf("TidyQuestionRow: %v", err)
	}
	if kept != "what does a person type" {
		t.Fatalf("the question is kept as %q", kept)
	}
}

func TestARowSettledWithNothingIsRefused(t *testing.T) {
	if _, err := job.TidyRowAnswer("   "); err == nil {
		t.Fatal("a row was settled with nothing, which leaves it open while reading as closed")
	}
}

func TestTheRowWithThatNumber(t *testing.T) {
	rows := []job.Question{{Seq: 1, Text: "one"}, {Seq: 4, Text: "four"}}
	if held, there := job.TheQuestion(rows, 4); !there || held.Text != "four" {
		t.Fatalf("row 4 read back as %+v, %v", held, there)
	}
	if _, there := job.TheQuestion(rows, 2); there {
		t.Fatal("a row nobody wrote was found")
	}
}
