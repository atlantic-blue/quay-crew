package controlplane

import (
	"context"
	"log/slog"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// turnsStream is the logical stream turn events are published on, within a workspace's namespace.
const turnsStream = "turns"

// publishTurn writes a turn to the event log, and never fails a turn because it could not.
//
// The turn already happened by the time this runs. Telling the operator their turn failed because a
// broker was unreachable would be a lie about the thing they care about, so a publish that does not
// work is logged and dropped. That makes the log lossy on purpose: it is the audit record, not the
// source of truth, which is the store.
//
// The record is keyed by session id, so every event for one session lands on one partition and stays
// in the order it happened. A consumer rebuilding a conversation depends on that.
func (s *Server) publishTurn(ctx context.Context, session *quaycrewv1.Session, event *quaycrewv1.TurnEvent) {
	topic, err := s.turnsTopic(ctx, session.GetWorkspace())
	if err != nil {
		slog.Warn("no topic for this turn, so it is not on the log", "session", session.GetId(), "error", err)
		return
	}

	// The id is minted here rather than derived from the turn, because two turns can carry the same
	// prompt in the same session and still be two different things that happened.
	event.Id = store.NewID()
	event.Session = session.GetId()
	event.Workspace = session.GetWorkspace()
	event.Project = session.GetProject()
	event.ThreadId = session.GetThreadId()
	event.OccurredAt = timestamppb.Now()

	value, err := proto.Marshal(event)
	if err != nil {
		slog.Warn("a turn could not be encoded, so it is not on the log", "session", session.GetId(), "error", err)
		return
	}
	if err := s.events.Publish(ctx, topic, []byte(session.GetId()), value); err != nil {
		slog.Warn("a turn could not be published, so it is not on the log", "session", session.GetId(), "topic", topic, "error", err)
	}
}

// turnsTopic names a workspace's turn stream.
//
// It uses the workspace's name rather than its identifier, because somebody reading `rpk topic list`
// should be able to tell which workspace they are looking at. A workspace cannot be renamed today;
// if that ever lands, its old topic stays where it is and this is the reason why.
func (s *Server) turnsTopic(ctx context.Context, workspaceID string) (string, error) {
	workspace, err := s.store.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	return messaging.Topic(workspace.GetName(), turnsStream)
}
