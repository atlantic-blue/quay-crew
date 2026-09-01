package controlplane

import (
	"context"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ReclaimSession takes a settled session's container back and leaves everything else where it is.
//
// The row, the conversation handle, the workspace's conversation store and the project's files are
// all untouched, so the next task builds a fresh container over the same state and the conversation
// carries on from where it was. That property is not new here: it is the same one attaching to a
// stopped session already relies on, and it is why a reclaim costs a resume rather than a history.
//
// Three states are refused, and each refusal exists because taking the container would overwrite
// something somebody else decided:
//
//   - running, because a task is in flight and reclaiming would kill it with nothing saying why.
//   - stopped, because an operator put the session down and this is bookkeeping.
//   - archived, because it is already filed and its container has already gone.
//
// Reclaiming one that is already reclaimed is refused too, so a caller that asked twice learns it
// rather than reading the second answer as a second reclaim: the stamp is what the archive time is
// measured against, and rewriting it would hold a session out of the archive forever.
func (s *Server) ReclaimSession(ctx context.Context, req *quaycrewv1.ReclaimSessionRequest) (
	*quaycrewv1.ReclaimSessionResponse, error) {
	session, err := s.store.GetSession(ctx, req.GetId())
	if err != nil {
		return nil, storeError(err, "session")
	}
	where := display.ShortID(session.GetHandle())
	switch {
	case session.GetArchivedAt() != nil:
		return nil, status.Errorf(codes.FailedPrecondition,
			"session %s is archived, so its container has already gone", where)
	case session.GetStatus() == StatusRunning:
		return nil, status.Errorf(codes.FailedPrecondition,
			"session %s has a task under way: taking its container back now would end that task", where)
	case session.GetStatus() == StatusStopped:
		return nil, status.Errorf(codes.FailedPrecondition,
			"session %s was stopped, and a stop is the operator's: restart it before reclaiming", where)
	case session.GetStatus() == StatusReclaimed:
		return nil, status.Errorf(codes.FailedPrecondition,
			"session %s is already reclaimed", where)
	}

	if err := s.store.ReclaimSession(ctx, req.GetId()); err != nil {
		return nil, storeError(err, "session")
	}
	// After the row, the way archiving does it. The container is what the row now says has gone, and
	// a close that ran first would leave a window where the session says it holds one and does not.
	s.closeSandbox(ctx, req.GetId())
	reclaimed := s.reread(ctx, req.GetId())
	s.emit(ctx, reclaimed, KindSessionReclaimed, "")
	return &quaycrewv1.ReclaimSessionResponse{Session: reclaimed}, nil
}

// SessionAttached says whether an operator has this session's conversation open.
//
// The provider asks the container, which is the signal that needs no new state and nothing to keep
// fresh: `krewe attach` opens the conversation as a tmux session inside the sandbox, and tmux knows
// whether a client is on it.
//
// Asked by name rather than through a handle this process holds, and never by creating one. The
// handles are a map in one process and the containers are not, so after a restart the map is empty
// while every container runs on; a question that built a sandbox to answer would start the very
// container it is asked about taking away. A session with none answers that nobody is in it.
//
// The session is read first so that a session the system does not have comes back as an error. Nobody
// is in a session that does not exist, but the caller reads a false here as licence to close a
// container, and it must not get that answer from a lookup that failed.
func (s *Server) SessionAttached(ctx context.Context, id string) (bool, error) {
	if _, err := s.store.GetSession(ctx, id); err != nil {
		return false, err
	}
	return s.provider.Attached(ctx, id)
}
