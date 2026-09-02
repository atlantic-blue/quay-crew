package job_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/model"
)

// What a session says it understood before it plans, read off its reply the way a plan is.
//
// The tests below are about the two halves that make the record worth having: that the system can
// read it back exactly as it was meant, and that what a person answered is held against what was
// asked rather than taken as agreement with all of it.

// aReading is a reply a session gives when it is asked what it understood, with prose around the
// lines, because a model's answer has prose around it.
const aReading = "Here is what I make of it.\n\n" +
	"Understood: a page that takes a link and gives back the text\n" +
	"Not: a page that takes an identifier\n" +
	"Told: the person pastes a link\n" +
	"Assumed: the transcript is already stored\n" +
	"Unknown: which surface a person reads it on\n" +
	"Confidence: fairly sure of the shape, least sure of the surface\n" +
	"Question 1: which surface does a person read this on\n" +
	"Question 2: does a link to a live broadcast count\n\n" +
	"That is the whole of it."

func TestAReadingIsReadOffTheReplyAndKeptInTheSystemsOwnWords(t *testing.T) {
	understood, err := job.ReadIdeation(aReading)
	if err != nil {
		t.Fatalf("ReadIdeation: %v", err)
	}
	if understood.Understood != "a page that takes a link and gives back the text" {
		t.Fatalf("what it understood is %q", understood.Understood)
	}
	if understood.NotThis != "a page that takes an identifier" {
		t.Fatalf("what the work is not is %q", understood.NotThis)
	}
	// The two lists that are the point of the whole record. What a person stated and what the session
	// filled in read the same on a row today, and this is where they stop reading the same.
	if len(understood.Told) != 1 || understood.Told[0] != "the person pastes a link" {
		t.Fatalf("what it was told is %q", understood.Told)
	}
	if len(understood.Assumed) != 1 || understood.Assumed[0] != "the transcript is already stored" {
		t.Fatalf("what it assumed is %q", understood.Assumed)
	}
	if len(understood.Questions) != 2 {
		t.Fatalf("it asked %d questions, want 2", len(understood.Questions))
	}
	// Kept as the system's own rendering, so what a person reads and what the session is handed later
	// are the same lines. The prose around the answer is not part of it.
	kept := job.IdeationText(understood)
	if strings.Contains(kept, "That is the whole of it") {
		t.Fatalf("the record kept the prose around it: %q", kept)
	}
	again, err := job.ReadIdeation(kept)
	if err != nil {
		t.Fatalf("the system cannot read back what it wrote: %v", err)
	}
	if job.IdeationText(again) != kept {
		t.Fatalf("the record does not survive being read back: %q against %q",
			job.IdeationText(again), kept)
	}
}

// Every way a reply can fail to be a reading, and the refusal that says what to write instead. A
// reply the system cannot read is prose about understanding, and putting that in front of a person
// is the same compression fault one level up.
func TestAReplyThatIsNotAReadingIsRefusedAndSaysWhy(t *testing.T) {
	line := "Understood: a page that takes a link\nNot: a page that takes an identifier\n" +
		"Confidence: fairly sure\nQuestion 1: which surface is this read on"
	for _, one := range []struct {
		name  string
		reply string
		says  string
	}{
		{"nothing the system can read", "I have read the brief and I understand it.", "Understood:"},
		{"what it is and never what it is not",
			"Understood: a page\nConfidence: sure\nQuestion 1: which surface", "Not:"},
		{"no confidence at all",
			"Understood: a page\nNot: an identifier\nQuestion 1: which surface", "Confidence:"},
		{"asks nothing at all",
			"Understood: a page\nNot: an identifier\nConfidence: sure", "asks nothing"},
		{"asks whether to go on",
			"Understood: a page\nNot: an identifier\nConfidence: sure\n" +
				"Question 1: shall I proceed with this", "asks whether to go on"},
		{"questions numbered from two",
			"Understood: a page\nNot: an identifier\nConfidence: sure\nQuestion 2: which surface",
			"number them from 1"},
		{"more questions than a person reads", line + "\nQuestion 2: a\nQuestion 3: b\n" +
			"Question 4: c\nQuestion 5: d\nQuestion 6: e", "may ask 5"},
		{"a line longer than a line", "Understood: a page\nNot: an identifier\nConfidence: sure\n" +
			"Told: " + strings.Repeat("x", job.IdeationLineLimit+1) + "\nQuestion 1: which surface",
			"it is one line a person reads"},
		{"more of itself than fits one question",
			"Understood: " + strings.Repeat("x", job.UnderstandingLimit+1) +
				"\nNot: an identifier\nConfidence: sure\nQuestion 1: which surface",
			"the paragraph a person reads first"},
	} {
		t.Run(one.name, func(t *testing.T) {
			_, err := job.ReadIdeation(one.reply)
			if err == nil {
				t.Fatalf("the system read %q as a reading", one.reply)
			}
			if !strings.Contains(err.Error(), one.says) {
				t.Fatalf("the refusal is %q, want it to say %q", err, one.says)
			}
		})
	}
}

// A question that asks about the work is a question, even where the words look like a request for
// permission. A check that refused both would cost the person the questions worth asking, so it is
// narrow in both directions: it catches a request to start and leaves a question about scope alone.
func TestAQuestionAboutTheWorkIsNotARequestToGoOn(t *testing.T) {
	for _, asked := range []string{
		"which environment should the deploy proceed against",
		"do you want me to include the briefing panel, or the command line alone",
	} {
		t.Run(asked, func(t *testing.T) {
			if _, err := job.ReadIdeation("Understood: a deploy\nNot: a rollback\n" +
				"Confidence: sure\nQuestion 1: " + asked); err != nil {
				t.Fatalf("a question about the work was refused: %v", err)
			}
		})
	}
}

// The gate, which is the plan's gate one stage earlier and by the same person.
func TestWhoOwesAReadingAndWhoDoesNot(t *testing.T) {
	for _, one := range []struct {
		name  string
		job   *job.Job
		reads bool
	}{
		{"a job at the top that states the sentence",
			&job.Job{Product: "you paste a link and get the text back"}, true},
		{"an errand, which states no sentence", &job.Job{Title: "read the bill"}, false},
		// A child is one part of a plan a person already approved. Stopping at every job in a tree puts
		// them back in the loop for all of them, which is the cost the system exists to remove.
		{"a job declared under another",
			&job.Job{Product: "you paste a link and get the text back", Parent: "parent-job"}, false},
		{"a job whose reading a person answered",
			&job.Job{Product: "you paste a link and get the text back", Ideation: "Understood: a page",
				IdeationAnswer: "1: on the command line"}, false},
		// Rows written before this existed carry an approved plan and no reading, and dragging those
		// back to the beginning would restart work a person already agreed to.
		{"a job whose plan a person already approved",
			&job.Job{Product: "you paste a link and get the text back", PlanApproved: true}, false},
	} {
		t.Run(one.name, func(t *testing.T) {
			if got := job.WaitingForItsIdeation(one.job); got != one.reads {
				t.Fatalf("it owes a reading: %t, want %t", got, one.reads)
			}
		})
	}
}

// The plan waits behind the reading. A session asked to plan before anybody agreed with its reading
// would be marking its own reading, which is the gap this closes.
func TestAJobOwesNoPlanUntilItsReadingIsAnswered(t *testing.T) {
	one := &job.Job{Brief: "build what the design describes",
		Product: "you paste a link and get the text back"}
	if job.WaitingForItsPlan(one) {
		t.Fatal("a job that has said nothing about what it understood already owes a plan")
	}
	asked := job.Asked(one)
	for _, phrase := range []string{"write no plan yet", "Understood:", "Question 1:"} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the session was asked %q, want it to say %q", asked, phrase)
		}
	}
	if strings.Contains(asked, "Step 1:") {
		t.Fatalf("a session that owes a reading was asked for a plan: %q", asked)
	}
	// And the sentence goes above it, because this stage is about what was asked for.
	if !strings.Contains(asked, "you paste a link and get the text back") {
		t.Fatalf("the reading was asked for without the sentence: %q", asked)
	}

	one.Ideation, one.IdeationAnswer = "Understood: a page", "1: on the command line"
	if !job.WaitingForItsPlan(one) {
		t.Fatal("a job whose reading was answered does not owe a plan")
	}
}

// What a person answered, held against what they were asked. An answer opens with the number it
// answers, and a number nothing carries is a question nobody answered.
func TestAQuestionAnAnswerLeavesAloneStaysUnknown(t *testing.T) {
	understood := job.IdeationText(readingOrFail(t, aReading))
	for _, one := range []struct {
		name   string
		answer string
		left   []int
	}{
		{"an answer that touches both", "1: on the command line\n2: no, recorded only", nil},
		{"an answer that touches one", "1: on the command line", []int{2}},
		// The reason this is not an approval. A person who wrote yes has said nothing about the work,
		// and reading that silence as agreement is the failure the whole stage exists for.
		{"the word that approves a plan", "yes", []int{1, 2}},
		{"prose carrying no numbers at all", "read the design first", []int{1, 2}},
	} {
		t.Run(one.name, func(t *testing.T) {
			left := job.StillUnknown(understood, one.answer)
			if len(left) != len(one.left) {
				t.Fatalf("%d questions are still unknown, want %d: %v", len(left), len(one.left), left)
			}
			for i, number := range one.left {
				if left[i].Number != number {
					t.Fatalf("question %d is still unknown, want question %d", left[i].Number, number)
				}
			}
		})
	}
}

// The marks travel into the plan. What was assumed is still an assumption after a person answered,
// unless they answered about it, and a question nobody touched is named rather than dropped.
func TestThePlanIsWrittenAgainstTheAnswerAndKeepsTheAssumedMarks(t *testing.T) {
	one := &job.Job{
		Brief: "build what the design describes", Product: "you paste a link and get the text back",
		Ideation:       job.IdeationText(readingOrFail(t, aReading)),
		IdeationAnswer: "1: on the command line, the way every other listing is read",
	}
	asked := job.Asked(one)
	for _, phrase := range []string{
		"Assumed: the transcript is already stored",
		"on the command line, the way every other listing is read",
		"still unknown", "does a link to a live broadcast count",
		"still an assumption", "Step 1:",
	} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the plan task is %q, want it to say %q", asked, phrase)
		}
	}
}

// The double and the reader have to agree, because a double looser than the engine manufactures a
// green suite: every test about a planned job runs through this reply.
func TestTheDoubleAnswersSomethingTheSystemCanRead(t *testing.T) {
	if model.UnderstandingAsk != job.TheUnderstandingAsk {
		t.Fatalf("the double watches for %q and the system asks with %q",
			model.UnderstandingAsk, job.TheUnderstandingAsk)
	}
	if model.UnderstandingMarker != job.UnderstandingMarker {
		t.Fatalf("the double marks a reading with %q and the system reads %q",
			model.UnderstandingMarker, job.UnderstandingMarker)
	}
	if _, err := job.ReadIdeation(model.FakeUnderstanding); err != nil {
		t.Fatalf("the system cannot read what the double answers: %v", err)
	}
}

// readingOrFail is a reading read off a reply, for the tests that start from one.
func readingOrFail(t *testing.T, reply string) job.Ideation {
	t.Helper()
	understood, err := job.ReadIdeation(reply)
	if err != nil {
		t.Fatalf("ReadIdeation: %v", err)
	}
	return understood
}

// The record is put to a person as one question, and a question has its own ceiling in asking.go.
// The two have to agree, or the system would write a question it could not ask. This holds the
// largest record the reader accepts against that ceiling.
func TestTheBiggestReadingStillFitsAQuestion(t *testing.T) {
	line := strings.Repeat("x", job.IdeationLineLimit)
	said := []string{
		"Understood: " + strings.Repeat("y", job.UnderstandingLimit),
		"Not: " + strings.Repeat("z", job.UnderstandingLimit),
		"Confidence: " + line,
	}
	for i := range job.IdeationPoints {
		said = append(said, "Told: "+line, "Assumed: "+line, "Unknown: "+line)
		said = append(said, fmt.Sprintf("Question %d: %s", i+1, line))
	}
	understood, err := job.ReadIdeation(strings.Join(said, "\n"))
	if err != nil {
		// The reader refuses it, which is the other way the two ceilings can agree, and the refusal
		// has to be about the size rather than about the shape.
		if !strings.Contains(err.Error(), "put to a person as one") {
			t.Fatalf("the biggest reading is refused for the wrong reason: %v", err)
		}
		return
	}
	asked := job.AskingWhetherThisIsRight(strings.Repeat("s", job.ProductLimit),
		job.IdeationText(understood))
	if _, err := job.TidyQuestion(asked); err != nil {
		t.Fatalf("the biggest reading makes a question the system cannot ask: %v", err)
	}
}

// A session can put a question of its own while it still owes a reading, and a person can answer it.
// That answer reaches the session in front of the ask rather than being dropped, because a task that
// arrived without it would be the system asking a person to answer twice.
func TestAnAnswerToTheSessionsOwnQuestionReachesItWithTheAsk(t *testing.T) {
	asked := job.Asked(&job.Job{
		Brief: "build what the design describes", Product: "you paste a link and get the text back",
		Question: "which of the two stores does this workspace already pay for",
		Told:     "neither, use the key value one",
	})
	for _, phrase := range []string{
		"which of the two stores", "neither, use the key value one", "write no plan yet",
	} {
		if !strings.Contains(asked, phrase) {
			t.Fatalf("the session was asked %q, want it to say %q", asked, phrase)
		}
	}
}
