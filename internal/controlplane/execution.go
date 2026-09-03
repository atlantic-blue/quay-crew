package controlplane

import (
	"context"
	"errors"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// An execution is one run of one stage of one job, and it is not a job.
//
// Nothing here writes one. A stage of a job fans out and the system writes its runs, so what a
// caller may do is read them and stop one that has not ended. A run used to be a job row, which is
// how it came to be listed beside the work a person declared; it is a table of its own now, and this
// is the only road to it.

// ListExecutions is the runs the request narrows to, oldest first: one job's, one stage of one job's,
// one project's, or every run the system holds.
//
// A listing that narrows by nothing mirrors the jobs listing, because the caller is the same one: a
// console drawing every job in the crew draws each job's runs beneath it, and one call for the whole
// listing is what keeps that from being a call per row.
func (s *Server) ListExecutions(ctx context.Context, req *quaycrewv1.ListExecutionsRequest) (
	*quaycrewv1.ListExecutionsResponse, error) {
	if stage := req.GetStage(); stage != "" && !job.StageBuilt(stage) {
		return nil, status.Errorf(codes.InvalidArgument,
			"%q is not a stage; use one of %s", stage, strings.Join(job.Stages, ", "))
	}
	runs, err := s.store.ListExecutions(ctx, job.ExecutionFilter{
		Job: req.GetJob(), Stage: req.GetStage(), Project: req.GetProject(),
	})
	if err != nil {
		return nil, storeError(err, "executions")
	}
	on := make([]*quaycrewv1.Execution, 0, len(runs))
	for _, run := range runs {
		on = append(on, asExecution(run))
	}
	return &quaycrewv1.ListExecutionsResponse{Executions: on}, nil
}

// StopExecution halts one run that has not ended, keeping the reason.
//
// It stops that run and nothing else. The job carries on, and its stage reads a run that ended: a
// number nothing holds is one the stage writes again or puts to a person, which is the road it
// already takes for a run whose session died.
func (s *Server) StopExecution(ctx context.Context, req *quaycrewv1.StopExecutionRequest) (
	*quaycrewv1.StopExecutionResponse, error) {
	if req.GetId() == "" {
		return nil, status.Error(codes.InvalidArgument,
			"which run: give the identifier krewe job show prints under the stage")
	}
	reason := strings.TrimSpace(req.GetReason())
	if reason == "" {
		reason = "stopped by the operator"
	}
	found, err := s.store.GetExecution(ctx, req.GetId())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound, "the system holds no run %s", req.GetId())
		}
		return nil, storeError(err, "execution")
	}
	one, err := s.store.GetJob(ctx, found.Job)
	if err != nil {
		return nil, storeError(err, "the job this run belongs to")
	}
	record := s.jobEvent(ctx, one, job.EventRunEnded, "stopped: "+reason)
	record.Execution = found.ID
	stopped, err := s.store.StopExecution(ctx, found.ID, reason, record)
	if err != nil {
		if errors.Is(err, job.ErrNotRunning) {
			return nil, status.Errorf(codes.FailedPrecondition,
				"run %s is %s already, and a run that already ended is not stopped again",
				found.ID, found.Phase)
		}
		return nil, storeError(err, "stop execution")
	}
	s.ExportJob(ctx, record)
	return &quaycrewv1.StopExecutionResponse{Execution: asExecution(stopped)}, nil
}

// asExecution puts a run on the wire.
func asExecution(from *job.Execution) *quaycrewv1.Execution {
	on := &quaycrewv1.Execution{
		Id: from.ID, Job: from.Job, Stage: from.Stage, Number: int32(from.Number),
		Claim: from.Claim, Phase: from.Phase, Session: from.Session,
		Attempts: int32(from.Attempts), Answer: from.Answer, Outcome: from.Outcome,
		Reason: from.Reason, Branch: from.Branch, PullRequest: from.PullRequest,
		SpentTokens: from.SpentTokens, TraceId: from.TraceID,
		CreatedAt: timestamppb.New(from.CreatedAt), UpdatedAt: timestamppb.New(from.UpdatedAt),
	}
	if from.StartedAt != nil {
		on.StartedAt = timestamppb.New(*from.StartedAt)
	}
	if from.FinishedAt != nil {
		on.FinishedAt = timestamppb.New(*from.FinishedAt)
	}
	return on
}
