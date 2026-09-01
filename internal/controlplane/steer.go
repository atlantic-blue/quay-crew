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

// ancestryLimit is how far up a tree this walks before it gives up. A parent is set once, when a job
// is declared, and never moves, so a cycle cannot form. The cap is here so a store that somehow held
// one would answer rather than spin.
const ancestryLimit = 64

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
	counted, err := s.ancestry(ctx, landed)
	if err != nil {
		return nil, err
	}
	root := counted[len(counted)-1]

	marked := &job.Steer{
		ID: store.NewID(), Job: landed.ID, Root: root.ID,
		Workspace: landed.Workspace, Project: landed.Project,
		// Through the same redactor a job's own records go through. What the operator typed is
		// persisted and read back months later, and what the system can know is every value the
		// workspace keeps sealed.
		Text:       s.RedactFor(ctx, landed.Workspace, job.TidySteer(req.GetText())),
		OccurredAt: time.Now().UTC(),
	}
	if err := s.store.RecordSteer(ctx, marked, identifiers(counted)); err != nil {
		return nil, storeError(err, "record steer")
	}
	// Read back rather than answered from memory, so the count the operator is shown is the count the
	// store holds.
	kept, err := s.store.GetJob(ctx, root.ID)
	if err != nil {
		return nil, storeError(err, "job")
	}
	return &quaycrewv1.RecordSteerResponse{Steer: asSteer(marked), Root: asJob(kept)}, nil
}

// ListSteers reads one job's marks back, oldest first, which is the order they happened in.
//
// Any job in the tree answers with the whole tree's, because the score belongs to the job at the top
// and not to whichever child happened to be running at the time.
func (s *Server) ListSteers(ctx context.Context, req *quaycrewv1.ListSteersRequest) (*quaycrewv1.ListSteersResponse, error) {
	if req.GetJob() == "" {
		return nil, status.Error(codes.InvalidArgument,
			"which job: give the identifier krewe job list prints")
	}
	asked, err := s.jobOr(ctx, req.GetJob())
	if err != nil {
		return nil, err
	}
	chain, err := s.ancestry(ctx, asked)
	if err != nil {
		return nil, err
	}
	root := chain[len(chain)-1]

	listed, err := s.store.ListSteers(ctx, root.ID)
	if err != nil {
		return nil, storeError(err, "steers")
	}
	out := make([]*quaycrewv1.Steer, 0, len(listed))
	for _, one := range listed {
		out = append(out, asSteer(one))
	}
	return &quaycrewv1.ListSteersResponse{Steers: out, Root: asJob(root)}, nil
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

// ancestry is the job it landed on and every job above it, the last of them being the job at the top
// of the tree.
//
// Every one of them counts the steer. A count on the job it landed on alone would leave the number a
// person compares sitting at zero on the job that was actually steered, and a count on the top alone
// would say nothing about which part of the tree kept needing a person.
func (s *Server) ancestry(ctx context.Context, from *job.Job) ([]*job.Job, error) {
	chain := []*job.Job{from}
	for one := from; one.Parent != ""; {
		if len(chain) >= ancestryLimit {
			return nil, status.Errorf(codes.FailedPrecondition,
				"job %s hangs under more than %d jobs, which is not a tree", from.ID, ancestryLimit)
		}
		parent, err := s.jobOr(ctx, one.Parent)
		if err != nil {
			return nil, err
		}
		chain = append(chain, parent)
		one = parent
	}
	return chain, nil
}

func identifiers(jobs []*job.Job) []string {
	said := make([]string, 0, len(jobs))
	for _, one := range jobs {
		said = append(said, one.ID)
	}
	return said
}

func asSteer(from *job.Steer) *quaycrewv1.Steer {
	return &quaycrewv1.Steer{
		Id: from.ID, Job: from.Job, Root: from.Root,
		Workspace: from.Workspace, Project: from.Project, Text: from.Text,
		OccurredAt: timestamppb.New(from.OccurredAt),
	}
}
