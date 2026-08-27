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

// tasksStream is the logical stream task events are published on, within a workspace's namespace.
const tasksStream = "tasks"

// beginTask writes a task the moment it starts, and hands back the record it opened so the landing
// can close that same record rather than write a second one.
//
// It is written at the start because that is when the operator needs it. A task takes minutes, and a
// history that only holds finished tasks says nothing at all about the one burning the tokens: the
// session reads as though it were asked nothing, while the work it was actually asked for is
// invisible until it lands.
//
// It is not exported here. The log carries one record per task, at the end, and a consumer handed a
// task twice would have to work out which of the two to believe.
func (s *Server) beginTask(ctx context.Context, session *quaycrewv1.Session, prompt string) *quaycrewv1.TaskEvent {
	task := &quaycrewv1.TaskEvent{Prompt: prompt, Status: StatusRunning}
	s.writeTask(ctx, session, task)
	return task
}

// landTask closes the record beginTask opened, with what the task came to and what it said. The
// prompt and the time it started stay as they were written, so the history says when the operator
// asked rather than when the answer arrived.
//
// This is where the task reaches the export, once, whole.
func (s *Server) landTask(ctx context.Context, session *quaycrewv1.Session, task *quaycrewv1.TaskEvent, landed, reply, failure string) {
	ctx = context.WithoutCancel(ctx)
	sealed := s.sealedValues(ctx, session)
	task.Status = landed
	task.Reply = model.Redact(reply, sealed)
	task.Failure = model.Redact(failure, sealed)

	if err := s.store.FinishTask(ctx, task.GetId(), task.GetStatus(), task.GetReply(), task.GetFailure()); err != nil {
		slog.WarnContext(ctx, "a task could not be closed in history", "session", session.GetId(), "error", err)
	}
	s.exportTask(ctx, session, task)
}

// recordHistory writes a task into the store, in the same breath as the task itself, and then
// offers it to the export. The store is the truth: history is complete whether or not any broker is
// configured, reachable, or behind. It never fails the task, because the task already happened, and
// a history write that could not land is a warning about the store, which the whole crew depends on
// anyway.
//
// The context is detached first: a client hanging up after a long task used to cancel the write and
// silently lose the record of the very task they were waiting on.
func (s *Server) recordHistory(ctx context.Context, session *quaycrewv1.Session, event *quaycrewv1.TaskEvent) {
	s.writeTask(ctx, session, event)
	s.exportTask(context.WithoutCancel(ctx), session, event)
}

// writeTask redacts a task, stamps it with where it belongs, and stores it.
func (s *Server) writeTask(ctx context.Context, session *quaycrewv1.Session, event *quaycrewv1.TaskEvent) {
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

	// The id is minted here rather than derived from the task, because two tasks can carry the same
	// prompt in the same session and still be two different things that happened.
	event.Id = store.NewID()
	event.Session = session.GetId()
	event.Workspace = session.GetWorkspace()
	event.Project = session.GetProject()
	event.Handle = session.GetHandle()
	event.OccurredAt = timestamppb.Now()

	task := &quaycrewv1.Task{
		Id:         event.GetId(),
		Session:    event.GetSession(),
		Prompt:     event.GetPrompt(),
		Reply:      event.GetReply(),
		Status:     event.GetStatus(),
		Failure:    event.GetFailure(),
		OccurredAt: event.GetOccurredAt(),
	}
	if err := s.store.AppendTask(ctx, task, event.GetWorkspace(), event.GetProject(), event.GetHandle()); err != nil {
		slog.WarnContext(ctx, "a task could not be written to history", "session", session.GetId(), "error", err)
	}
}

// exportTask offers one already redacted task to the event log. The log is an audit export for
// whatever second consumer eventually wants it, so a crew with no broker configured loses nothing
// but the export, and an export that could not land is logged and dropped rather than failing
// anything.
//
// The record is keyed by session id, so every event for one session lands on one partition and stays
// in the order it happened. A consumer rebuilding a conversation depends on that.
func (s *Server) exportTask(ctx context.Context, session *quaycrewv1.Session, event *quaycrewv1.TaskEvent) {
	topic, err := s.tasksTopic(ctx, session.GetWorkspace())
	if err != nil {
		slog.WarnContext(ctx, "no topic for this task, so it is not exported", "session", session.GetId(), "error", err)
		return
	}
	value, err := proto.Marshal(event)
	if err != nil {
		slog.WarnContext(ctx, "a task could not be encoded, so it is not exported", "session", session.GetId(), "error", err)
		return
	}
	if err := s.export(ctx, session.GetId(), topic, value); err != nil {
		slog.WarnContext(ctx, "a task could not be exported", "session", session.GetId(), "topic", topic, "error", err)
	}
}

// sealedValues is everything the crew holds that must never be persisted in the clear, keyed by the
// name it would be redacted under: every secret the workspace has sealed, whether or not any
// sandbox was handed it, and the driver's token for a driver session. A name whose value cannot be
// read is skipped rather than failing the task, because the redactor still catches the published
// token shape without it.
func (s *Server) sealedValues(ctx context.Context, session *quaycrewv1.Session) map[string]string {
	values := map[string]string{}
	refs, err := s.secrets.List(ctx, session.GetWorkspace())
	if err != nil {
		slog.WarnContext(ctx, "the workspace's secret names could not be listed for redaction", "session", session.GetId(), "error", err)
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

// tasksTopic names a workspace's task stream.
//
// It uses the workspace's name rather than its identifier, because somebody reading `rpk topic list`
// should be able to tell which workspace they are looking at. A workspace cannot be renamed today;
// if that ever lands, its old topic stays where it is and this is the reason why.
func (s *Server) tasksTopic(ctx context.Context, workspaceID string) (string, error) {
	workspace, err := s.store.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return "", err
	}
	return messaging.Topic(workspace.GetName(), tasksStream)
}
