package controlplane

import (
	"context"
	"errors"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/auth"
	"github.com/atlantic-blue/krewe/internal/job"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// A session at its workspace's context ceiling says what it leaves behind, and the rest of the job
// goes to a fresh conversation.
//
// The gate itself is the controller's, in internal/job. This is the one call the session makes: the
// state a fresh session starts from. Without it the gate would only refuse, and a job stranded in a
// full conversation is worse than a job finished badly in one.

// RecordJobHandoff writes down the state a fresh session starts this job from.
//
// The caller is the session doing the work, and the job comes off the credential it presented rather
// than off the request. That is the reading a step and a question already make, and for the same
// reason: a caller that could name any job could write on any job's record.
//
// Handing over is not a verb a role grants, for the reason recording a step is not. A role that could
// withhold it would leave a session at the ceiling with no way to stop except by carrying on badly,
// which is the whole failure this answers.
func (s *Server) RecordJobHandoff(ctx context.Context, req *quaycrewv1.RecordJobHandoffRequest) (
	*quaycrewv1.RecordJobHandoffResponse, error) {
	grant, carried := auth.GrantFrom(ctx)
	if !carried || grant.Job == "" {
		return nil, status.Error(codes.PermissionDenied,
			"a handoff is written by the session doing the job, and this caller is doing none: "+
				"what a person leaves behind is not a handoff of anybody's job")
	}
	if named := req.GetId(); named != "" && named != grant.Job {
		return nil, status.Errorf(codes.PermissionDenied,
			"a session hands over the job it is doing and no other: this credential is for %s, and %s is somebody else's",
			grant.Job, named)
	}
	left, tried, err := job.TidyHandoff(req.GetLeft(), req.GetTried())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	found, err := s.store.GetJob(ctx, grant.Job)
	if err != nil {
		return nil, storeError(err, "the job this session is doing")
	}

	// The conversation that wrote it is the system's to supply and never the caller's to name. It is
	// what tells a handoff waiting to be taken up from one a fresh session already holds, so a session
	// that could write any name here could make the system hand the same words over for ever.
	handed := s.jobEvent(ctx, found, job.EventHandedOver, left)
	recorded, err := s.store.RecordJobHandoff(ctx, found.ID, left, tried, found.Session, handed)
	if err != nil {
		if errors.Is(err, job.ErrNotRunning) {
			return nil, status.Errorf(codes.FailedPrecondition,
				"this job is %s, and a handoff is written while the job is being done: there is nothing "+
					"left for a fresh session to carry on", found.Phase)
		}
		return nil, storeError(err, "record a handoff")
	}
	// After the transaction, never inside it. The store is the truth and the log is the copy.
	s.ExportJob(ctx, handed)
	return &quaycrewv1.RecordJobHandoffResponse{Job: asJob(recorded)}, nil
}

// SessionWindow is how full one session's context window is, which is what the ceiling is checked
// against, and how big that window is.
//
// Both come from where the listing reads them: the used figure from the transcript the model keeps,
// and the size from whatever the model runtime last told a session in that workspace. A session that
// is gone, has said nothing yet, or whose transcript cannot be read answers zero, and a window of no
// size never refuses anything. A gate that read silence as a full window would stop every job on a
// system nothing has told the size of yet.
func (s *Server) SessionWindow(ctx context.Context, id string) (used, size int64) {
	session, err := s.store.GetSession(ctx, id)
	if err != nil {
		return 0, 0
	}
	carried := s.storage.ConversationContext(boxOf(session), session.GetModelSessionId())
	if carried.Empty() {
		return 0, 0
	}
	held, _ := s.storage.ContextWindowSize(session.GetWorkspace())
	return carried.Carried(), held
}
