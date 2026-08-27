package controlplane

import (
	"context"
	"log/slog"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The kinds a session emits. A kind names something that happened, in the past tense, at one moment,
// and it is the field a consumer switches on.
//
// "idle" and "running" are not here on purpose. They are what a session's row says now, which is the
// fold of these, and a consumer handed a state learns nothing about what changed.
const (
	KindSessionCreated   = "session.created"
	KindSessionStarted   = "session.started"
	KindSessionCompleted = "session.completed"
	KindSessionErrored   = "session.errored"
	KindSessionStopped   = "session.stopped"
	KindSessionArchived  = "session.archived"
	KindSessionRestored  = "session.restored"
	KindSessionDeleted   = "session.deleted"
)

// sessionsStream is the logical stream a session's lifecycle is published on, within a workspace's
// namespace. It is beside the tasks stream rather than mixed into it: a consumer that wants to know
// what the crew is doing should not have to read every prompt and reply to find out.
const sessionsStream = "sessions"

// detailLine is how much of a detail is kept. A reply runs to paragraphs and this is one line about
// one event, so the whole of it belongs in the task record that already holds it.
const detailLine = 240

// emit records one thing that happened to a session, and offers it to the export.
//
// The store first and the log after, for the same reason a task is written that way: the store is
// the truth, so a crew whose broker is down still knows what happened, and a view reading the store
// works whether or not anything is listening. Neither write ever fails what it describes, because
// the thing already happened.
//
// The context is detached, so an event about a session outlives the request that caused it.
func (s *Server) emit(ctx context.Context, session *quaycrewv1.Session, kind, detail string) {
	if session == nil || session.GetId() == "" {
		return
	}
	ctx = context.WithoutCancel(ctx)

	event := &quaycrewv1.SessionEvent{
		Id:        store.NewID(),
		Kind:      kind,
		Session:   session.GetId(),
		Workspace: session.GetWorkspace(),
		Project:   session.GetProject(),
		Handle:    session.GetHandle(),
		// What the model said and what a failure said both reach this line, and either can carry
		// something the operator pasted, so it goes through the same redactor a task does.
		Detail:     oneShortLine(model.Redact(detail, s.sealedValues(ctx, session))),
		OccurredAt: timestamppb.Now(),
	}

	if err := s.store.AppendSessionEvent(ctx, event); err != nil {
		slog.WarnContext(ctx, "a session event could not be written",
			"session", session.GetId(), "kind", kind, "error", err)
	}
	s.exportSessionEvent(ctx, session, event)
}

// exportSessionEvent offers one already redacted event to the log, keyed by session so one session's
// records stay in order on one partition. An export that cannot land is logged and dropped: the log
// is the copy, and the store already holds the record.
func (s *Server) exportSessionEvent(ctx context.Context, session *quaycrewv1.Session, event *quaycrewv1.SessionEvent) {
	workspace, err := s.store.GetWorkspace(ctx, session.GetWorkspace())
	if err != nil {
		slog.WarnContext(ctx, "no workspace for this session event, so it is not exported",
			"session", session.GetId(), "kind", event.GetKind(), "error", err)
		return
	}
	topic, err := messaging.Topic(workspace.GetName(), sessionsStream)
	if err != nil {
		slog.WarnContext(ctx, "no topic for this session event, so it is not exported",
			"session", session.GetId(), "kind", event.GetKind(), "error", err)
		return
	}
	value, err := proto.Marshal(event)
	if err != nil {
		slog.WarnContext(ctx, "a session event could not be encoded, so it is not exported",
			"session", session.GetId(), "kind", event.GetKind(), "error", err)
		return
	}
	if err := s.export(ctx, session.GetId(), topic, value); err != nil {
		slog.WarnContext(ctx, "a session event could not be exported",
			"session", session.GetId(), "kind", event.GetKind(), "topic", topic, "error", err)
	}
}

// oneShortLine flattens a detail onto one line and caps it. A listing of what the crew is doing is
// for finding the moment you want; the task record holds the whole of what was said.
func oneShortLine(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	if len(flat) <= detailLine {
		return flat
	}
	return flat[:detailLine-1] + "…"
}

// ListSessionEvents says what happened to a session, oldest first, or to the whole crew when the
// request names none.
func (s *Server) ListSessionEvents(ctx context.Context, req *quaycrewv1.ListSessionEventsRequest) (*quaycrewv1.ListSessionEventsResponse, error) {
	events, err := s.store.ListSessionEvents(ctx, req.GetSession(), int(req.GetLimit()))
	if err != nil {
		return nil, storeError(err, "session events")
	}
	return &quaycrewv1.ListSessionEventsResponse{Events: events}, nil
}
