package controlplane

import (
	"context"
	"log/slog"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/telemetry"
	"github.com/atlantic-blue/quay-crew/internal/work"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// workStream is the logical stream a piece of work's movements are published on, within a
// workspace's namespace. It is beside the tasks and sessions streams rather than mixed into them: a
// consumer that wants to know what the crew was asked to do should not have to read every prompt and
// reply to find out.
const workStream = "work"

// traceWork gives a piece of work the trace it belongs to, before it is written.
//
// A root is minted, and a child inherits its parent's unchanged, so one identifier covers a whole
// tree however many controllers and processes it passes through. The span identifier is the caller's
// own at the moment of the write, which is what ties a child's declaration to the attempt that asked
// for it.
//
// Both go on the row rather than into a process. That is what makes the trace survive a controller
// that died: the context is in the declaration, the way a wait is a column rather than a timer.
func (s *Server) traceWork(ctx context.Context, declared *work.Work, parent *work.Work) {
	declared.ParentSpanID = telemetry.SpanIDFrom(ctx)
	if parent != nil && parent.TraceID != "" {
		declared.TraceID = parent.TraceID
		return
	}
	if inherited := telemetry.TraceIDFrom(ctx); inherited != "" {
		declared.TraceID = inherited
		return
	}
	// Nothing was tracing this call, which is a crew with no exporter configured or a caller whose
	// own tool starts no trace. The identifier is minted anyway, because it is what joins the tree
	// together afterwards and a root with none leaves every descendant unjoined.
	declared.TraceID = telemetry.NewTraceID()
}

// ExportWork offers each record of a movement to the event log, after the transaction that wrote it.
//
// The store is the truth and the log is the copy. So this is called after the write has landed, it
// never fails what it describes, and a crew with no broker configured loses the export and nothing
// else. It is exported to `<workspace>.work`, keyed by the work identifier, so one piece of work's
// records stay in order on one partition. A consumer rebuilding a tree depends on that.
//
// It is a method on the server rather than a function because the controller writes movements too,
// and both roads have to reach the same stream in the same shape. See work.Exporter.
func (s *Server) ExportWork(ctx context.Context, events ...*work.Event) {
	ctx = context.WithoutCancel(ctx)
	for _, event := range events {
		if event == nil {
			continue
		}
		s.exportWorkEvent(ctx, event)
	}
}

func (s *Server) exportWorkEvent(ctx context.Context, event *work.Event) {
	workspace, err := s.store.GetWorkspace(ctx, event.Workspace)
	if err != nil {
		slog.WarnContext(ctx, "no workspace for this work event, so it is not exported",
			"work", event.Work, "kind", event.Kind, "error", err)
		return
	}
	topic, err := messaging.Topic(workspace.GetName(), workStream)
	if err != nil {
		slog.WarnContext(ctx, "no topic for this work event, so it is not exported",
			"work", event.Work, "kind", event.Kind, "error", err)
		return
	}
	value, err := proto.Marshal(asWorkEvent(event))
	if err != nil {
		slog.WarnContext(ctx, "a work event could not be encoded, so it is not exported",
			"work", event.Work, "kind", event.Kind, "error", err)
		return
	}
	if err := s.export(ctx, event.Work, topic, value); err != nil {
		slog.WarnContext(ctx, "a work event could not be exported",
			"work", event.Work, "kind", event.Kind, "topic", topic, "error", err)
	}
}

// asWorkEvent puts one record on the wire. The detail was redacted before it was written, so what is
// exported is what the store holds and nothing more.
func asWorkEvent(event *work.Event) *quaycrewv1.WorkEvent {
	return &quaycrewv1.WorkEvent{
		Id: event.ID, Kind: event.Kind, Work: event.Work,
		Workspace: event.Workspace, Project: event.Project,
		Parent: event.Parent, Depth: int32(event.Depth),
		Detail: event.Detail, TraceId: event.TraceID,
		OccurredAt: timestamppb.New(event.OccurredAt),
	}
}
