package controlplane

import (
	"context"
	"log/slog"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/auth"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/model"
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
func (s *Server) publishTurn(ctx context.Context, session *quaycrewv1.Thread, event *quaycrewv1.TurnEvent) {
	topic, err := s.turnsTopic(ctx, session.GetWorkspace())
	if err != nil {
		slog.Warn("no topic for this turn, so it is not on the log", "session", session.GetId(), "error", err)
		return
	}

	// What an operator pastes into a conversation can be a credential, and everything published
	// here is persisted twice: the log keeps the record and the projection writes it to the store.
	// So the payload goes through the same redaction a failure message does, before it is written
	// anywhere. A value the crew could not know about cannot be protected; what it can know is
	// every value the workspace keeps sealed, the driver's token, and the published token shape.
	sealed := s.sealedValues(ctx, session)
	event.Prompt = model.Redact(event.Prompt, sealed)
	event.Reply = model.Redact(event.Reply, sealed)
	event.Failure = model.Redact(event.Failure, sealed)

	// The id is minted here rather than derived from the turn, because two turns can carry the same
	// prompt in the same session and still be two different things that happened.
	event.Id = store.NewID()
	event.Thread = session.GetId()
	event.Workspace = session.GetWorkspace()
	event.Project = session.GetProject()
	event.Handle = session.GetHandle()
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

// sealedValues is everything the crew holds that must never be persisted in the clear, keyed by the
// name it would be redacted under: every secret the workspace has sealed, whether or not any
// sandbox was handed it, and the driver's token for a driver session. A name whose value cannot be
// read is skipped rather than failing the turn, because the redactor still catches the published
// token shape without it.
func (s *Server) sealedValues(ctx context.Context, session *quaycrewv1.Thread) map[string]string {
	values := map[string]string{}
	names, err := s.secrets.Names(ctx, session.GetWorkspace())
	if err != nil {
		slog.Warn("the workspace's secret names could not be listed for redaction", "session", session.GetId(), "error", err)
	}
	for _, name := range names {
		value, err := s.secrets.Get(ctx, session.GetWorkspace(), name)
		if err != nil || value == "" {
			continue
		}
		values[name] = value
	}
	if session.GetDriver() && s.driverToken != "" {
		values[auth.TokenEnv] = s.driverToken
	}
	return values
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
