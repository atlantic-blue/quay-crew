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

// recordHistory writes a turn into the store, in the same breath as the turn itself, and then
// offers it to the export. The store is the truth: history is complete whether or not any broker is
// configured, reachable, or behind. It never fails the turn, because the turn already happened, and
// a history write that could not land is a warning about the store, which the whole crew depends on
// anyway.
//
// The context is detached first: a client hanging up after a long turn used to cancel the write and
// silently lose the record of the very turn they were waiting on.
func (s *Server) recordHistory(ctx context.Context, session *quaycrewv1.Thread, event *quaycrewv1.TurnEvent) {
	ctx = context.WithoutCancel(ctx)

	// What an operator pastes into a conversation can be a credential, and everything recorded here
	// is persisted. So the payload goes through the same redaction a failure message does, before it
	// is written anywhere. A value the crew could not know about cannot be protected; what it can
	// know is every value the workspace keeps sealed, the driver's token, and the published token
	// shape.
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

	turn := &quaycrewv1.Turn{
		Id:         event.GetId(),
		Thread:     event.GetThread(),
		Prompt:     event.GetPrompt(),
		Reply:      event.GetReply(),
		Status:     event.GetStatus(),
		Failure:    event.GetFailure(),
		OccurredAt: event.GetOccurredAt(),
	}
	if err := s.store.AppendTurn(ctx, turn, event.GetWorkspace(), event.GetProject(), event.GetHandle()); err != nil {
		slog.Warn("a turn could not be written to history", "session", session.GetId(), "error", err)
	}

	s.exportTurn(ctx, session, event)
}

// exportTurn offers one already redacted turn to the event log. The log is an audit export for
// whatever second consumer eventually wants it, so a crew with no broker configured loses nothing
// but the export, and an export that could not land is logged and dropped rather than failing
// anything.
//
// The record is keyed by session id, so every event for one session lands on one partition and stays
// in the order it happened. A consumer rebuilding a conversation depends on that.
func (s *Server) exportTurn(ctx context.Context, session *quaycrewv1.Thread, event *quaycrewv1.TurnEvent) {
	topic, err := s.turnsTopic(ctx, session.GetWorkspace())
	if err != nil {
		slog.Warn("no topic for this turn, so it is not exported", "session", session.GetId(), "error", err)
		return
	}
	value, err := proto.Marshal(event)
	if err != nil {
		slog.Warn("a turn could not be encoded, so it is not exported", "session", session.GetId(), "error", err)
		return
	}
	if err := s.events.Publish(ctx, topic, []byte(session.GetId()), value); err != nil {
		slog.Warn("a turn could not be exported", "session", session.GetId(), "topic", topic, "error", err)
	}
}

// sealedValues is everything the crew holds that must never be persisted in the clear, keyed by the
// name it would be redacted under: every secret the workspace has sealed, whether or not any
// sandbox was handed it, and the driver's token for a driver session. A name whose value cannot be
// read is skipped rather than failing the turn, because the redactor still catches the published
// token shape without it.
func (s *Server) sealedValues(ctx context.Context, session *quaycrewv1.Thread) map[string]string {
	values := map[string]string{}
	refs, err := s.secrets.List(ctx, session.GetWorkspace())
	if err != nil {
		slog.Warn("the workspace's secret names could not be listed for redaction", "session", session.GetId(), "error", err)
	}
	// Every projection, because a value is worth redacting wherever it came from. A mounted secret
	// reaches the sandbox as a file rather than as an environment variable, and a session that reads
	// that file can still say what is in it.
	for _, ref := range refs {
		value, err := s.secrets.Get(ctx, session.GetWorkspace(), ref.Name)
		if err != nil || value == "" {
			continue
		}
		values[ref.Name] = value
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
