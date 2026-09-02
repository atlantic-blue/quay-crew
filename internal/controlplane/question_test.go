package controlplane_test

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A plan read by several roles, driven through the control plane's own interface: a reading writes
// what it could not settle, a later reading settles a row from its own lens, and what is left is
// what a person is asked.
//
// The assertions go past the call. What decides whether this works is what the next reader and the
// person at the end are handed, so these read the rows back off the job rather than trusting the
// answer of the write.

// The quiet case first. A change that fires on every job passes every test about finding questions
// and is worth nothing.
func TestAJobThatWritesNoQuestionBehavesExactlyAsItDidBefore(t *testing.T) {
	system := aJobUnderWay(t)
	reading := system.reading(t)
	if len(reading.GetQuestions()) != 0 {
		t.Fatalf("a job nobody read carries %d questions", len(reading.GetQuestions()))
	}
	if reading.GetPhase() != job.PhaseRunning {
		t.Fatalf("the job is %q, want a job that carries no question to be exactly as it was", reading.GetPhase())
	}
}

// The whole of it: three rows, one settled by a later lens, and every row read back with its status.
func TestAReadingWritesItsRowsAndALaterLensSettlesOne(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := asJobCredential(context.Background(), system.job.GetId())

	for _, asking := range []string{
		"what does a person type, and what comes back",
		"which store holds the text",
	} {
		if _, err := system.server.RecordJobQuestion(ctx,
			&quaycrewv1.RecordJobQuestionRequest{Text: asking}); err != nil {
			t.Fatalf("RecordJobQuestion(%q): %v", asking, err)
		}
	}
	settled, err := system.server.SettleJobQuestion(ctx, &quaycrewv1.SettleJobQuestionRequest{
		Seq: 2, Answer: "the key value store, on demand, because nothing bills while nobody uses it",
	})
	if err != nil {
		t.Fatalf("SettleJobQuestion: %v", err)
	}

	rows := settled.GetJob().GetQuestions()
	if len(rows) != 2 {
		t.Fatalf("the reading carries %d rows, want two", len(rows))
	}
	if rows[0].GetStatus() != job.QuestionOpen {
		t.Fatalf("row 1 reads %q, and nobody settled it", rows[0].GetStatus())
	}
	if rows[1].GetStatus() != job.QuestionSettled {
		t.Fatalf("row 2 reads %q after a lens settled it", rows[1].GetStatus())
	}
	if !strings.Contains(rows[1].GetAnswer(), "key value store") {
		t.Fatalf("row 2 says %q settled it", rows[1].GetAnswer())
	}

	// What a person would be asked, which is the half that stopping at the row never sees: the open
	// row and not the settled one.
	open := job.RenderQuestions(questionsOf(settled.GetJob()))
	if !strings.Contains(open, "what does a person type") {
		t.Fatalf("the person would be asked %q, want the row nobody settled", open)
	}
	if strings.Contains(open, "which store holds the text") {
		t.Fatalf("a settled row reached the person: %q", open)
	}

	// And the record says which of the two happened, because a question nobody settled and a question
	// a second lens closed are the two outcomes this whole reading exists to tell apart.
	listed, err := system.kept.ListJobEvents(context.Background(), system.job.GetId())
	if err != nil {
		t.Fatalf("ListJobEvents: %v", err)
	}
	kinds := kindsOf(listed)
	for _, want := range []string{job.EventQuestioned, job.EventSettled} {
		if !contains(kinds, want) {
			t.Fatalf("the records read %v, want %q among them", kinds, want)
		}
	}
}

func TestTheFourthQuestionFromOneReadingIsRefusedByName(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := asJobCredential(context.Background(), system.job.GetId())

	for _, asking := range []string{"the first hole", "a second hole", "a third hole"} {
		if _, err := system.server.RecordJobQuestion(ctx,
			&quaycrewv1.RecordJobQuestionRequest{Text: asking}); err != nil {
			t.Fatalf("RecordJobQuestion(%q): %v", asking, err)
		}
	}
	_, err := system.server.RecordJobQuestion(ctx,
		&quaycrewv1.RecordJobQuestionRequest{Text: "a fourth hole nobody has room for"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a fourth question answered %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), "3") {
		t.Fatalf("the refusal is %q, want it to name the ceiling of three", err)
	}
	if got := len(system.reading(t).GetQuestions()); got != 3 {
		t.Fatalf("the reading carries %d rows after a refused fourth", got)
	}
}

func TestADuplicateQuestionIsRefusedAndNamesTheRowItRepeats(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := asJobCredential(context.Background(), system.job.GetId())

	if _, err := system.server.RecordJobQuestion(ctx, &quaycrewv1.RecordJobQuestionRequest{
		Text: "What does a person type, and what comes back?",
	}); err != nil {
		t.Fatalf("RecordJobQuestion: %v", err)
	}
	_, err := system.server.RecordJobQuestion(ctx, &quaycrewv1.RecordJobQuestionRequest{
		Text: "what does a person type and what comes back",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("the same question twice answered %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), "question 1") {
		t.Fatalf("the refusal is %q, want it to name the row it repeats", err)
	}
}

func TestAQuestionOverTheCeilingIsRefusedAndSaysHowLongItIs(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := asJobCredential(context.Background(), system.job.GetId())

	_, err := system.server.RecordJobQuestion(ctx, &quaycrewv1.RecordJobQuestionRequest{
		Text: strings.Repeat("a", job.QuestionRowLimit+1),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a question of %d bytes answered %v", job.QuestionRowLimit+1, err)
	}
	if !strings.Contains(err.Error(), "201") || !strings.Contains(err.Error(), "200") {
		t.Fatalf("the refusal is %q, want it to say how long the row is and how long one may be", err)
	}
}

// A row nobody wrote is a reader that has read the wrong list, so the refusal shows it the right
// one rather than only saying no.
func TestSettlingARowNobodyWroteShowsTheRowsThatAreOpen(t *testing.T) {
	system := aJobUnderWay(t)
	ctx := asJobCredential(context.Background(), system.job.GetId())
	if _, err := system.server.RecordJobQuestion(ctx, &quaycrewv1.RecordJobQuestionRequest{
		Text: "which store holds the text",
	}); err != nil {
		t.Fatalf("RecordJobQuestion: %v", err)
	}

	_, err := system.server.SettleJobQuestion(ctx, &quaycrewv1.SettleJobQuestionRequest{
		Seq: 7, Answer: "it does not matter",
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("settling a row nobody wrote answered %v", err)
	}
	if !strings.Contains(err.Error(), "which store holds the text") {
		t.Fatalf("the refusal is %q, want it to show the rows that are open", err)
	}
}

// The identifier is checked against the credential rather than trusted, the way a step and a question
// to a person already are. A caller that could name any job could write on any job's record.
func TestASessionCannotWriteAQuestionOnSomebodyElsesJob(t *testing.T) {
	system := aJobUnderWay(t)
	other := declaredIn(t, system.server, "read the electricity bill")
	ctx := asJobCredential(context.Background(), system.job.GetId())

	_, err := system.server.RecordJobQuestion(ctx, &quaycrewv1.RecordJobQuestionRequest{
		Text: "what does a person type", Id: other.GetId(),
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("writing on another job answered %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), system.job.GetId()) {
		t.Fatalf("the refusal is %q, want it to name the job the credential is for", err)
	}
}

func TestAPersonWithNoJobWritesNoQuestion(t *testing.T) {
	system := aJobUnderWay(t)
	_, err := system.server.RecordJobQuestion(context.Background(),
		&quaycrewv1.RecordJobQuestionRequest{Text: "what does a person type"})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a caller doing no job answered %v, want it refused", err)
	}
}

// questionsOf is the wire rows as the domain reads them, so a test can ask what a person would be
// shown rather than working it out again by hand.
func questionsOf(one *quaycrewv1.Job) []job.Question {
	rows := make([]job.Question, 0, len(one.GetQuestions()))
	for _, asked := range one.GetQuestions() {
		rows = append(rows, job.Question{
			Seq: int(asked.GetSeq()), Text: asked.GetText(), AskedBy: asked.GetAskedBy(),
			Status: asked.GetStatus(), Answer: asked.GetAnswer(), SettledBy: asked.GetSettledBy(),
		})
	}
	return rows
}

func contains(said []string, want string) bool {
	for _, one := range said {
		if one == want {
			return true
		}
	}
	return false
}
