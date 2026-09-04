package controlplane

import (
	"context"
	"log/slog"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"github.com/atlantic-blue/quay-krewe/internal/telemetry"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// beginExec writes an exec the moment it starts, and hands back the record it opened so the landing
// can close that same record rather than write a second one.
//
// It is written at the start because that is when the operator needs it. An exec takes minutes, and a
// history that only holds finished execs says nothing at all about the one burning the tokens: the
// session reads as though it were asked nothing, while the job it was actually asked for is
// invisible until it lands.
//
// It is not exported here. The log carries one record per exec, at the end, and a consumer handed a
// exec twice would have to work out which of the two to believe.
func (s *Server) beginExec(ctx context.Context, session *quaycrewv1.Session, prompt string) *quaycrewv1.ExecEvent {
	exec := &quaycrewv1.ExecEvent{Prompt: prompt, Status: StatusRunning}
	s.writeExec(ctx, session, exec)
	return exec
}

// landExec closes the record beginExec opened, with what the exec came to and what it said. The
// prompt and the time it started stay as they were written, so the history says when the operator
// asked rather than when the answer arrived.
//
// This is where the exec reaches the export, once, whole.
func (s *Server) landExec(ctx context.Context, session *quaycrewv1.Session, exec *quaycrewv1.ExecEvent, landed, reply, failure string) {
	ctx = context.WithoutCancel(ctx)
	sealed := s.sealedValues(ctx, session)
	exec.Status = landed
	exec.Reply = model.Redact(reply, sealed)
	exec.Failure = model.Redact(failure, sealed)

	if err := s.store.FinishExec(ctx, exec.GetId(), exec.GetStatus(), exec.GetReply(), exec.GetFailure()); err != nil {
		slog.WarnContext(ctx, "an exec could not be closed in history", "session", session.GetId(), "error", err)
	}
}

// recordHistory writes an exec into the store, in the same breath as the exec itself, and then
// offers it to the export. The store is the truth: history is complete whether or not any broker is
// configured, reachable, or behind. It never fails the exec, because the exec already happened, and
// a history write that could not land is a warning about the store, which the whole system depends on
// anyway.
//
// The context is detached first: a client hanging up after a long exec used to cancel the write and
// silently lose the record of the very exec they were waiting on.
func (s *Server) recordHistory(ctx context.Context, session *quaycrewv1.Session, event *quaycrewv1.ExecEvent) {
	s.writeExec(context.WithoutCancel(ctx), session, event)
}

// writeExec redacts an exec, stamps it with where it belongs, and stores it.
func (s *Server) writeExec(ctx context.Context, session *quaycrewv1.Session, event *quaycrewv1.ExecEvent) {
	ctx = context.WithoutCancel(ctx)

	// What an operator pastes into a conversation can be a credential, and everything recorded here
	// is persisted. So the payload goes through the same redaction a failure message does, before it
	// is written anywhere. A value the system could not know about cannot be protected; what it can
	// know is every value the workspace keeps sealed, the driver's token, and the published token
	// shape.
	sealed := s.sealedValues(ctx, session)
	event.Prompt = model.Redact(event.Prompt, sealed)
	event.Reply = model.Redact(event.Reply, sealed)
	event.Failure = model.Redact(event.Failure, sealed)

	// The id is minted here rather than derived from the exec, because two execs can carry the same
	// prompt in the same session and still be two different things that happened.
	event.Id = store.NewID()
	event.Session = session.GetId()
	event.Workspace = session.GetWorkspace()
	event.Project = session.GetProject()
	event.Handle = session.GetHandle()
	event.OccurredAt = timestamppb.Now()
	// The trace the call that ran this exec belonged to, which is the same value every log line
	// written under it carries. Without it the durable record of what the system did joins to neither
	// the trace nor the lines: weeks later the logs are gone and this row is all that is left. An exec
	// nothing was tracing leaves it empty rather than inventing one. See issue 346.
	event.TraceId = telemetry.TraceIDFrom(ctx)

	exec := &quaycrewv1.Exec{
		Id:         event.GetId(),
		Session:    event.GetSession(),
		Prompt:     event.GetPrompt(),
		Reply:      event.GetReply(),
		Status:     event.GetStatus(),
		Failure:    event.GetFailure(),
		TraceId:    event.GetTraceId(),
		OccurredAt: event.GetOccurredAt(),
	}
	if err := s.store.AppendExec(ctx, exec, event.GetWorkspace(), event.GetProject(), event.GetHandle()); err != nil {
		slog.WarnContext(ctx, "an exec could not be written to history", "session", session.GetId(), "error", err)
	}
}

// sealedValues is everything the system holds that must never be persisted in the clear, keyed by the
// name it would be redacted under: every secret the workspace has sealed, whether or not any
// sandbox was handed it, and the driver's token for a driver session. A name whose value cannot be
// read is skipped rather than failing the exec, because the redactor still catches the published
// token shape without it.
func (s *Server) sealedValues(ctx context.Context, session *quaycrewv1.Session) map[string]string {
	values := s.sealedForWorkspace(ctx, session.GetWorkspace())
	if session.GetDriver() && s.driverToken != "" {
		values[auth.TokenEnv] = s.driverToken
	}
	return values
}

// sealedForWorkspace is the same set for a workspace rather than for a session, which is what
// anything redacting outside a conversation has: a job has no session yet.
func (s *Server) sealedForWorkspace(ctx context.Context, workspace string) map[string]string {
	values := map[string]string{}
	refs, err := s.secrets.List(ctx, workspace)
	if err != nil {
		slog.WarnContext(ctx, "the workspace's secret names could not be listed for redaction",
			"workspace", workspace, "error", err)
	}
	// Every projection, because a value is worth redacting wherever it came from. A mounted secret
	// reaches the sandbox as a file rather than as an environment variable, and a session that reads
	// that file can still say what is in it.
	for _, ref := range refs {
		value, err := s.secrets.Get(ctx, workspace, ref.Name)
		if err != nil || value == "" {
			continue
		}
		values[ref.Name] = value
	}
	return values
}
