package controlplane

import (
	"context"
	"errors"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A plan read by several roles, and only what none of them could settle put to a person.
//
// The two calls are one behaviour from two sides. A reading says what its own lens could not settle,
// and a later reading, handed the open rows and never the earlier reader's prose, settles what its
// lens can. What is left is what a person is asked.
//
// Both are written by the session doing the reading, over the credential the system minted for its
// job, for the reason a step is: a caller that could name any job could write on any job's record.

// RecordJobQuestion writes down one thing this reading could not settle.
func (s *Server) RecordJobQuestion(ctx context.Context, req *quaycrewv1.RecordJobQuestionRequest) (
	*quaycrewv1.RecordJobQuestionResponse, error) {
	grant, carried := auth.GrantFrom(ctx)
	if !carried || grant.Job == "" {
		return nil, status.Error(codes.PermissionDenied,
			"a question is written by the session reading the plan, and this caller is reading none: "+
				"what a person cannot settle is not a row on anybody's job")
	}
	if named := req.GetId(); named != "" && named != grant.Job {
		return nil, status.Errorf(codes.PermissionDenied,
			"a session writes on the job it is doing and no other: this credential is for %s, and %s is somebody else's",
			grant.Job, named)
	}
	text, err := job.TidyQuestionRow(req.GetText())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	found, err := s.store.GetJob(ctx, grant.Job)
	if err != nil {
		return nil, storeError(err, "the job this session is doing")
	}
	// The same hole named twice is one hole. It is refused rather than dropped, because a reader that
	// wrote its second question and was told nothing would spend its third saying it again.
	if repeated, already := job.AlreadyAsked(found.Questions, text); already {
		return nil, status.Errorf(codes.FailedPrecondition,
			"question %d on this job already asks that: %q. Ask something the rows do not cover, or settle "+
				"that row if this reading can answer it", repeated.Seq, repeated.Text)
	}
	// The ceiling counts what this reading wrote, not what it was handed. A later reader carries every
	// open row, and one that could be refused its first question for an earlier reader's work would
	// read and report nothing.
	if err := job.RoomForAQuestion(found.Questions, found.ID); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}

	asked := s.jobEvent(ctx, found, job.EventQuestioned, text)
	recorded, err := s.store.RecordJobQuestion(ctx, found.ID, job.Question{
		Text: text, AskedBy: found.Role, AskedIn: found.ID,
	}, asked)
	if err != nil {
		if errors.Is(err, job.ErrNotRunning) {
			return nil, status.Errorf(codes.FailedPrecondition,
				"this job is %s, and a question is written while the reading is being done: say what you "+
					"could not settle in your answer instead", found.Phase)
		}
		return nil, storeError(err, "record a question")
	}
	// After the transaction, never inside it. The store is the truth and the log is the copy.
	s.ExportJob(ctx, asked)
	return &quaycrewv1.RecordJobQuestionResponse{Job: asJob(recorded)}, nil
}

// SettleJobQuestion answers a row an earlier reading left open.
//
// A settled row does not reach a person. This is the whole reason to run more than one reader: the
// count of rows a later lens closed is what says the readings are different readings.
func (s *Server) SettleJobQuestion(ctx context.Context, req *quaycrewv1.SettleJobQuestionRequest) (
	*quaycrewv1.SettleJobQuestionResponse, error) {
	grant, carried := auth.GrantFrom(ctx)
	if !carried || grant.Job == "" {
		return nil, status.Error(codes.PermissionDenied,
			"a row is settled by the session reading the plan, and this caller is reading none")
	}
	if named := req.GetId(); named != "" && named != grant.Job {
		return nil, status.Errorf(codes.PermissionDenied,
			"a session settles a row on the job it is doing and no other: this credential is for %s, and %s is somebody else's",
			grant.Job, named)
	}
	answer, err := job.TidyRowAnswer(req.GetAnswer())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	found, err := s.store.GetJob(ctx, grant.Job)
	if err != nil {
		return nil, storeError(err, "the job this session is doing")
	}
	seq := int(req.GetSeq())
	held, there := job.TheQuestion(found.Questions, seq)
	if !there {
		return nil, status.Error(codes.FailedPrecondition, noSuchQuestion(seq, found.Questions))
	}
	if !held.Open() {
		return nil, status.Errorf(codes.FailedPrecondition,
			"question %d is settled already, by %s: %q", seq, settlerOf(held), held.Answer)
	}

	settled := s.jobEvent(ctx, found, job.EventSettled, fmt.Sprintf("%d: %s", seq, answer))
	recorded, err := s.store.SettleJobQuestion(ctx, found.ID, seq, answer, found.Role, settled)
	if err != nil {
		switch {
		case errors.Is(err, job.ErrNotRunning):
			return nil, status.Errorf(codes.FailedPrecondition,
				"this job is %s, and a row is settled while the reading is being done", found.Phase)
		case errors.Is(err, job.ErrNoSuchQuestion):
			return nil, status.Error(codes.FailedPrecondition, noSuchQuestion(seq, found.Questions))
		case errors.Is(err, job.ErrQuestionSettled):
			return nil, status.Errorf(codes.FailedPrecondition, "question %d is settled already", seq)
		}
		return nil, storeError(err, "settle a question")
	}
	s.ExportJob(ctx, settled)
	return &quaycrewv1.SettleJobQuestionResponse{Job: asJob(recorded)}, nil
}

// noSuchQuestion says which rows this reading does hold, because a reader settling a number it was
// never handed has read the wrong list and needs to see the right one.
func noSuchQuestion(seq int, questions []job.Question) string {
	open := job.RenderQuestions(questions)
	if open == "" {
		return fmt.Sprintf("there is no question %d on this job, and no row on it is open: this reading "+
			"has nothing to settle", seq)
	}
	return fmt.Sprintf("there is no question %d on this job. The rows still open are:\n%s", seq, open)
}

// settlerOf is who settled a row, in words, for a refusal a reader can act on.
func settlerOf(held job.Question) string {
	if held.SettledBy == "" {
		return "an earlier reading"
	}
	return held.SettledBy
}
