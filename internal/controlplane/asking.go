package controlplane

import (
	"context"
	"errors"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/auth"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AskJob puts a question to a person about the job the caller is running, and stops the job there.
//
// The caller is the session doing the work, and the job is read from the credential it presented
// rather than from the request. That is the same reading a declaration makes of its parent, and for
// the same reason: a caller that could name any job could stop any job, and the identifier in the
// request is here to be checked against the credential rather than to choose.
//
// Asking is not a fifth verb, so no role has to grant it. A session may always ask about the job it
// is itself running, because the alternative to asking is guessing, and a session that guesses about
// something no measurement settles is the failure this exists to end.
func (s *Server) AskJob(ctx context.Context, req *quaycrewv1.AskJobRequest) (*quaycrewv1.AskJobResponse, error) {
	grant, carried := auth.GrantFrom(ctx)
	if !carried || grant.Job == "" {
		return nil, status.Error(codes.PermissionDenied,
			"a question is put by the session running the job, and this caller is running none: "+
				"a person asking a job something dispatches a task, which is the other direction")
	}
	if named := req.GetId(); named != "" && named != grant.Job {
		return nil, status.Errorf(codes.PermissionDenied,
			"a session asks about the job it is running and no other: this credential is for %s, and %s is somebody else's",
			grant.Job, named)
	}
	question, err := job.TidyQuestion(req.GetQuestion())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	found, err := s.store.GetJob(ctx, grant.Job)
	if err != nil {
		return nil, storeError(err, "the job this session is running")
	}

	asked := s.jobEvent(ctx, found, job.EventAsked, question)
	asking, err := s.store.AskJob(ctx, found.ID, question, asked)
	if err != nil {
		if errors.Is(err, job.ErrNotRunning) {
			return nil, status.Errorf(codes.FailedPrecondition,
				"this job is %s, and only a job that is running has anybody waiting on its answer: "+
					"say what you needed in your answer instead", found.Phase)
		}
		return nil, storeError(err, "ask")
	}
	// After the transaction, never inside it. The store is the truth and the log is the copy.
	s.ExportJob(ctx, asked)
	return &quaycrewv1.AskJobResponse{Job: asJob(asking)}, nil
}

// AnswerJob tells an asking job what a person decided, and puts it back in the queue to be started
// again with that answer.
//
// The operator's call, and deliberately not a session's. A session that could answer a question a
// person was asked would be a run taking its own word for a decision, which is a gate that decorates
// rather than holds. The verb `job.answer` exists and no call is mapped to it, so a role cannot be
// written that answers.
//
// Nothing is dispatched here. The row goes back to pending and the controller starts it the way it
// starts anything else, so the answer reaches the session through the one path that reserves room,
// mints the credential and writes the record. A control plane that dispatched here would be a second
// road into a container, and the two would drift.
func (s *Server) AnswerJob(ctx context.Context, req *quaycrewv1.AnswerJobRequest) (*quaycrewv1.AnswerJobResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument,
			"which job: give the identifier quay job list prints beside the one that is asking")
	}
	answer, err := job.TidyTelling(req.GetAnswer())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	found, err := s.store.GetJob(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound,
				"the system holds no job %s: quay job list --phase asking says which are waiting", req.GetId())
		}
		return nil, storeError(err, "job")
	}

	told := s.jobEvent(ctx, found, job.EventTold, answer)
	answered, err := s.store.AnswerJob(ctx, found.ID, answer, told)
	if err != nil {
		if errors.Is(err, job.ErrNotAsking) {
			return nil, status.Errorf(codes.FailedPrecondition,
				"job %s is %s and asked nothing, so there is nothing this answers: "+
					"quay job list --phase asking says which are waiting", found.ID, found.Phase)
		}
		return nil, storeError(err, "answer")
	}
	s.ExportJob(ctx, told)
	return &quaycrewv1.AnswerJobResponse{Job: asJob(answered)}, nil
}
