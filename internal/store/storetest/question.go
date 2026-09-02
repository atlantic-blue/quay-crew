package storetest

import (
	"context"
	"errors"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// runJobQuestionConformance holds both stores to what a plan read by several roles means.
//
// Three writes, and the compare and sets in them are the whole of it. A question is written only
// against a job somebody is reading. A row settles only while it is open, so two readers settling at
// once leave one answer. And the carry between readings is idempotent, because the same reading
// merged twice must leave one row rather than two.
//
// Neither store may be the lenient one. The lenient one would let a settled row be settled again, so
// the reading a person is shown would depend on which store answered.
func runJobQuestionConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	// The quiet case first. A store that fires on every job passes every test about carrying
	// questions and is worth nothing.
	t.Run("a job that writes no question carries none", func(t *testing.T) {
		s := newDataset(t)(t)
		id := aRunningJob(t, s)
		found, err := s.GetJob(context.Background(), id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(found.Questions) != 0 {
			t.Fatalf("a job nobody read carries %d questions", len(found.Questions))
		}
	})

	t.Run("a question is refused against a job nobody is reading", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aFailedJob(t, s, "the sandbox went away")

		_, err := s.RecordJobQuestion(ctx, id, job.Question{Text: "what does a person type"},
			questionedEvent(t, s, id, "what does a person type"))
		if !errors.Is(err, job.ErrNotRunning) {
			t.Fatalf("RecordJobQuestion against a failed job: %v, want ErrNotRunning", err)
		}
	})

	// The whole of it on one job: three rows written, one settled by a later lens, and every row read
	// back with its status. Reading it back is the point: a row that reached the memory store and not
	// the table is a row a person is never asked about.
	t.Run("three rows are written, one is settled, and every row survives the read", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)

		for _, asking := range []string{
			"what does a person type", "which store holds the text", "who reads the report",
		} {
			if _, err := s.RecordJobQuestion(ctx, id, job.Question{
				Text: asking, AskedBy: "plan-critic", AskedIn: id,
			}, questionedEvent(t, s, id, asking)); err != nil {
				t.Fatalf("RecordJobQuestion(%q): %v", asking, err)
			}
		}
		if _, err := s.SettleJobQuestion(ctx, id, 2, "the key value store, on demand", "architect",
			settledEvent(t, s, id, "2")); err != nil {
			t.Fatalf("SettleJobQuestion: %v", err)
		}

		found, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(found.Questions) != 3 {
			t.Fatalf("the job carries %d rows, want 3", len(found.Questions))
		}
		for at, want := range []string{"what does a person type", "which store holds the text", "who reads the report"} {
			one := found.Questions[at]
			if one.Seq != at+1 {
				t.Fatalf("row %d is numbered %d, and a later reader settles by that number", at+1, one.Seq)
			}
			if one.Text != want {
				t.Fatalf("row %d reads %q, want %q", one.Seq, one.Text, want)
			}
			if one.AskedBy != "plan-critic" {
				t.Fatalf("row %d says it was asked by %q", one.Seq, one.AskedBy)
			}
			if one.AskedIn != id {
				t.Fatalf("row %d says it was written in %q, and the ceiling counts by that", one.Seq, one.AskedIn)
			}
		}
		settled := found.Questions[1]
		if settled.Status != job.QuestionSettled {
			t.Fatalf("the settled row reads %q", settled.Status)
		}
		if settled.Answer != "the key value store, on demand" || settled.SettledBy != "architect" {
			t.Fatalf("the settled row says %q by %q", settled.Answer, settled.SettledBy)
		}
		if settled.SettledAt.IsZero() {
			t.Fatal("the settled row says nothing about when it was settled")
		}
		if open := job.OpenQuestions(found.Questions); len(open) != 2 {
			t.Fatalf("%d rows are still open, want the two nobody settled", len(open))
		}
	})

	t.Run("a row nobody wrote is not settled", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)

		_, err := s.SettleJobQuestion(ctx, id, 4, "it does not matter", "architect",
			settledEvent(t, s, id, "4"))
		if !errors.Is(err, job.ErrNoSuchQuestion) {
			t.Fatalf("settling a row nobody wrote: %v, want ErrNoSuchQuestion", err)
		}
	})

	t.Run("a settled row is not settled twice", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)
		if _, err := s.RecordJobQuestion(ctx, id, job.Question{Text: "which store holds the text"},
			questionedEvent(t, s, id, "which store")); err != nil {
			t.Fatalf("RecordJobQuestion: %v", err)
		}
		if _, err := s.SettleJobQuestion(ctx, id, 1, "the key value store", "architect",
			settledEvent(t, s, id, "1")); err != nil {
			t.Fatalf("SettleJobQuestion: %v", err)
		}

		_, err := s.SettleJobQuestion(ctx, id, 1, "no, the relational one", "test-writer",
			settledEvent(t, s, id, "1"))
		if !errors.Is(err, job.ErrQuestionSettled) {
			t.Fatalf("settling a settled row: %v, want ErrQuestionSettled", err)
		}
		found, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.Questions[0].Answer != "the key value store" {
			t.Fatalf("the row reads %q, want the answer that settled it first", found.Questions[0].Answer)
		}
	})

	// The carry, which is what makes a later reading able to settle an earlier one's row. It runs
	// against a job that is not running, because the plan a run is carried by is held back while its
	// readings are out.
	t.Run("rows are carried onto a plan that is not running, and carrying twice leaves one row", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		plan := aWaitingJob(t, s)
		rows := []job.Question{
			{Seq: 1, Text: "what does a person type", AskedBy: "test-writer", AskedIn: "reader-1",
				Status: job.QuestionOpen},
			{Seq: 2, Text: "which store holds the text", AskedBy: "test-writer", AskedIn: "reader-1",
				Status: job.QuestionOpen},
		}
		if _, err := s.CarryJobQuestions(ctx, plan, rows); err != nil {
			t.Fatalf("CarryJobQuestions: %v", err)
		}
		if _, err := s.CarryJobQuestions(ctx, plan, rows); err != nil {
			t.Fatalf("CarryJobQuestions again: %v", err)
		}
		found, err := s.GetJob(ctx, plan)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(found.Questions) != 2 {
			t.Fatalf("the plan carries %d rows after carrying two twice", len(found.Questions))
		}

		// And the settling travels the same way, because a later reading settles on its own job and
		// the answer has to reach the plan.
		rows[1].Status, rows[1].Answer, rows[1].SettledBy = job.QuestionSettled, "the key value store", "architect"
		if _, err := s.CarryJobQuestions(ctx, plan, rows); err != nil {
			t.Fatalf("CarryJobQuestions with a settled row: %v", err)
		}
		found, err = s.GetJob(ctx, plan)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(found.Questions) != 2 {
			t.Fatalf("settling a carried row added one: %d rows", len(found.Questions))
		}
		if found.Questions[1].Status != job.QuestionSettled || found.Questions[1].Answer != "the key value store" {
			t.Fatalf("the row the later reading settled reads %+v", found.Questions[1])
		}
		if open := job.OpenQuestions(found.Questions); len(open) != 1 || open[0].Seq != 1 {
			t.Fatalf("%d rows are open, want only the one nobody settled", len(open))
		}
	})

	// A reading handed rows one to three writes its own as four. Numbering by the count instead would
	// have the reader settle a row it was never handed.
	t.Run("a reading handed rows numbers its own past them", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		id := aRunningJob(t, s)
		if _, err := s.CarryJobQuestions(ctx, id, []job.Question{
			{Seq: 1, Text: "one", Status: job.QuestionOpen},
			{Seq: 2, Text: "two", Status: job.QuestionSettled, Answer: "settled already"},
		}); err != nil {
			t.Fatalf("CarryJobQuestions: %v", err)
		}
		if _, err := s.RecordJobQuestion(ctx, id, job.Question{Text: "three", AskedIn: id},
			questionedEvent(t, s, id, "three")); err != nil {
			t.Fatalf("RecordJobQuestion: %v", err)
		}
		found, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(found.Questions) != 3 {
			t.Fatalf("the reading carries %d rows, want the two it was handed and its own", len(found.Questions))
		}
		if found.Questions[2].Seq != 3 {
			t.Fatalf("the reading numbered its own row %d, want 3", found.Questions[2].Seq)
		}
	})
}

// aWaitingJob is a plan whose readings are out: held back rather than running, which is the phase the
// carry has to work against.
func aWaitingJob(t *testing.T, s store.Store) string {
	t.Helper()
	workspace, project := aProject(t, s)
	return jobShaped(t, s, workspace, project, "the plan several roles read", func(one *job.Job) {
		one.Phase = job.PhaseWaiting
	})
}

func questionedEvent(t *testing.T, s store.Store, id, text string) *job.Event {
	t.Helper()
	return aJobEvent(t, s, id, job.EventQuestioned, text)
}

func settledEvent(t *testing.T, s store.Store, id, seq string) *job.Event {
	t.Helper()
	return aJobEvent(t, s, id, job.EventSettled, seq)
}
