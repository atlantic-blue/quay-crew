package controlplane

import (
	"context"
	"errors"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A job that failed is continued rather than declared a second time.
//
// The three calls are one behaviour read from three sides. The session says what it finished, as it
// finishes it. The operator continues a job that failed, and what it already finished goes in front
// of the session that carries it on. Or the operator refuses it, which ends the job, because a
// failure that was the work being wrong must not be offered a second attempt.

// RecordJobStep writes down one thing the session doing this job finished.
//
// The caller is the session doing the work, and the job is read from the credential it presented
// rather than from the request. That is the same reading a question makes, and for the same reason:
// a caller that could name any job could write on any job's record.
//
// Recording is not a verb a role grants, again like asking. A role that could withhold it would
// leave a job that can only ever be started again from nothing, which is the cost this exists to
// stop paying.
func (s *Server) RecordJobStep(ctx context.Context, req *quaycrewv1.RecordJobStepRequest) (
	*quaycrewv1.RecordJobStepResponse, error) {
	grant, carried := auth.GrantFrom(ctx)
	if !carried || grant.Job == "" {
		return nil, status.Error(codes.PermissionDenied,
			"a step is recorded by the session doing the job, and this caller is doing none: "+
				"what a person finished is not a step of anybody's job")
	}
	if named := req.GetId(); named != "" && named != grant.Job {
		return nil, status.Errorf(codes.PermissionDenied,
			"a session records against the job it is doing and no other: this credential is for %s, and %s is somebody else's",
			grant.Job, named)
	}
	summary, err := job.TidyStep(req.GetSummary())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	found, err := s.store.GetJob(ctx, grant.Job)
	if err != nil {
		return nil, storeError(err, "the job this session is doing")
	}
	// The ceiling is read here rather than in the statement, because what it protects is the task that
	// continues this job: every step goes in front of that session beside its brief. A step already on
	// the record does not count against it, or a session that repeats itself would be refused for
	// saying nothing new.
	if !job.Recorded(found.Steps, summary) {
		if err := job.RoomForAStep(found.Steps); err != nil {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
	}

	stepped := s.jobEvent(ctx, found, job.EventStepped, summary)
	// What the step names against this job's repository, read off the step rather than reported: the
	// same reading an answer gets. A job that failed after opening its pull request said so nowhere
	// else, so the address would die with the attempt.
	recorded, err := s.store.RecordJobStep(ctx, found.ID, summary,
		job.PullRequestIn(found.Repository, summary), stepped)
	if err != nil {
		if errors.Is(err, job.ErrNotRunning) {
			return nil, status.Errorf(codes.FailedPrecondition,
				"this job is %s, and a step is recorded while the job is being done: say what you "+
					"finished in your answer instead", found.Phase)
		}
		return nil, storeError(err, "record a step")
	}
	// After the transaction, never inside it. The store is the truth and the log is the copy.
	s.ExportJob(ctx, stepped)
	return &quaycrewv1.RecordJobStepResponse{Job: asJob(recorded)}, nil
}

// ResumeJob continues a job that failed, from the first step its session did not finish.
//
// The operator's call, and deliberately not a session's. A session that could continue its own job
// would be a run deciding that its own failure was somebody else's fault, and the whole point of the
// pair below is that a person decides which of the two a failure was.
//
// Nothing is dispatched here. The row goes back to pending with its session, and the controller
// starts it the way it starts anything else, so the task reaches the session through the one path
// that reserves room, mints the credential and writes the record. A dispatch here would be a second
// road into a container, and the two would drift.
func (s *Server) ResumeJob(ctx context.Context, req *quaycrewv1.ResumeJobRequest) (
	*quaycrewv1.ResumeJobResponse, error) {
	found, err := s.jobToAnswerFor(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	if !job.Resumable(found.Phase) {
		return nil, status.Error(codes.FailedPrecondition, job.NotResumable(found.ID, found.Phase))
	}

	resumed := s.jobEvent(ctx, found, job.EventResumed, found.Reason)
	one, err := s.store.ResumeJob(ctx, found.ID, resumed)
	if err != nil {
		if errors.Is(err, job.ErrNotFailed) {
			// It moved between the read and the write, which is somebody else continuing it or refusing
			// it a moment ago. The row is the truth, so the refusal is read off the row as it is now.
			return nil, status.Error(codes.FailedPrecondition, job.NotResumable(found.ID, found.Phase))
		}
		return nil, storeError(err, "continue")
	}
	s.ExportJob(ctx, resumed)
	return &quaycrewv1.ResumeJobResponse{Job: asJob(one)}, nil
}

// RefuseJob ends a job that failed, on purpose, so nothing continues it.
//
// It is the other half of a resume rather than a second way of stopping a job. `StopJob` refuses a
// job that already ended, deliberately: how it ended is the useful part. This one applies to exactly
// the rows a resume applies to, and the reason it writes carries the failure it is refusing, so the
// record does not lose which failure the operator was answering.
func (s *Server) RefuseJob(ctx context.Context, req *quaycrewv1.RefuseJobRequest) (
	*quaycrewv1.RefuseJobResponse, error) {
	found, err := s.jobToAnswerFor(ctx, req.GetId())
	if err != nil {
		return nil, err
	}
	if !job.Refusable(found.Phase) {
		return nil, status.Error(codes.FailedPrecondition, job.NotRefusable(found.ID, found.Phase))
	}

	reason := job.Refused(strings.TrimSpace(req.GetReason()), found.Reason)
	refused := s.jobEvent(ctx, found, job.EventRefused, reason)
	one, err := s.store.RefuseJob(ctx, found.ID, reason, refused)
	if err != nil {
		if errors.Is(err, job.ErrNotFailed) {
			return nil, status.Error(codes.FailedPrecondition, job.NotRefusable(found.ID, found.Phase))
		}
		return nil, storeError(err, "refuse")
	}
	s.ExportJob(ctx, refused)
	// The job is over, so the credentials minted for it are too, the same way a stop takes them back.
	s.RevokeJobCredentials(one.ID, one.Phase)
	return &quaycrewv1.RefuseJobResponse{Job: asJob(one)}, nil
}

// jobToAnswerFor is the job an operator named, read off the store, and the refusal where they named
// none or named one the system does not hold.
func (s *Server) jobToAnswerFor(ctx context.Context, id string) (*job.Job, error) {
	if strings.TrimSpace(id) == "" {
		return nil, status.Error(codes.InvalidArgument,
			"which job: give the identifier krewe job list --phase failed prints beside the ones that failed")
	}
	found, err := s.store.GetJob(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound,
				"the system holds no job %s: krewe job list --phase failed says which ones failed", id)
		}
		return nil, storeError(err, "job")
	}
	return found, nil
}
