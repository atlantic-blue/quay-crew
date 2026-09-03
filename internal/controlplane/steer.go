package controlplane

import (
	"context"
	"errors"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The score of a job is how many times the operator had to steer it, and this is where that number
// is kept.
//
// It is the operator's call, and a session cannot reach it. Both calls are absent from jobVerbs in
// deny.go, and a job credential is refused everything that is not in that list, so the thing being
// scored cannot write its own score. That is not a detail: a number the scored thing can move is not
// a measurement, and the whole reason for keeping it is to compare one job with the one before it.

// RecordSteer marks one moment the operator had to say something the system should have known.
//
// The mark is made while it is happening, in one command, because a mark that waits for the evening
// does not get made: the whole of what this replaces is thirteen of them written out by hand two days
// later, from memory.
func (s *Server) RecordSteer(ctx context.Context, req *quaycrewv1.RecordSteerRequest) (*quaycrewv1.RecordSteerResponse, error) {
	if req.GetJob() == "" {
		return nil, status.Error(codes.InvalidArgument,
			"which job did you have to steer: give the identifier krewe job list prints")
	}
	if err := job.Steered(req.GetText()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	landed, err := s.jobOr(ctx, req.GetJob())
	if err != nil {
		return nil, err
	}
	marked := &job.Steer{
		ID: store.NewID(), Job: landed.ID,
		Workspace: landed.Workspace, Project: landed.Project,
		// Through the same redactor a job's own records go through. What the operator typed is
		// persisted and read back months later, and what the system can know is every value the
		// workspace keeps sealed.
		Text:       s.RedactFor(ctx, landed.Workspace, job.TidySteer(req.GetText())),
		OccurredAt: time.Now().UTC(),
	}
	if err := s.store.RecordSteer(ctx, marked); err != nil {
		return nil, storeError(err, "record steer")
	}
	// Read back rather than answered from memory, so the count the operator is shown is the count the
	// store holds.
	kept, err := s.store.GetJob(ctx, landed.ID)
	if err != nil {
		return nil, storeError(err, "job")
	}
	return &quaycrewv1.RecordSteerResponse{Steer: asSteer(marked), Job: asJob(kept)}, nil
}

// ListSteers reads one job's marks back, oldest first, which is the order they happened in.
//
// The marks of that job, because a steer belongs to the job the operator was looking at when they
// made it and to nothing else.
func (s *Server) ListSteers(ctx context.Context, req *quaycrewv1.ListSteersRequest) (*quaycrewv1.ListSteersResponse, error) {
	if req.GetJob() == "" {
		return nil, status.Error(codes.InvalidArgument,
			"which job: give the identifier krewe job list prints")
	}
	asked, err := s.jobOr(ctx, req.GetJob())
	if err != nil {
		return nil, err
	}
	listed, err := s.store.ListSteers(ctx, asked.ID)
	if err != nil {
		return nil, storeError(err, "steers")
	}
	out := make([]*quaycrewv1.Steer, 0, len(listed))
	for _, one := range listed {
		out = append(out, asSteer(one))
	}
	return &quaycrewv1.ListSteersResponse{Steers: out, Job: asJob(asked)}, nil
}

// jobOr reads one job, or says the system does not hold it in the words every other job call uses.
func (s *Server) jobOr(ctx context.Context, id string) (*job.Job, error) {
	found, err := s.store.GetJob(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, status.Errorf(codes.NotFound,
				"the system holds no job %s: krewe job list says what there is", id)
		}
		return nil, storeError(err, "job")
	}
	return found, nil
}

func asSteer(from *job.Steer) *quaycrewv1.Steer {
	return &quaycrewv1.Steer{
		Id: from.ID, Job: from.Job,
		Workspace: from.Workspace, Project: from.Project, Text: from.Text,
		OccurredAt: timestamppb.New(from.OccurredAt),
	}
}
