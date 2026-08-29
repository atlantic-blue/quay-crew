package controlplane

import (
	"context"
	"log/slog"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/messaging"
	"github.com/atlantic-blue/quay-crew/internal/telemetry"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// jobStream is the logical stream a job's movements are published on, within a
// workspace's namespace. It is beside the tasks and sessions streams rather than mixed into them: a
// consumer that wants to know what the crew was asked to do should not have to read every prompt and
// reply to find out.
const jobStream = "job"

// traceJob gives a job the trace it belongs to, before it is written.
//
// A root is minted, and a child inherits its parent's unchanged, so one identifier covers a whole
// tree however many controllers and processes it passes through. The span identifier is the caller's
// own at the moment of the write, which is what ties a child's declaration to the attempt that asked
// for it.
//
// Both go on the row rather than into a process. That is what makes the trace survive a controller
// that died: the context is in the declaration, the way a wait is a column rather than a timer.
func (s *Server) traceJob(ctx context.Context, declared *job.Job, parent *job.Job) {
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

// ExportJob offers each record of a movement to the event log, after the transaction that wrote it.
//
// The store is the truth and the log is the copy. So this is called after the write has landed, it
// never fails what it describes, and a crew with no broker configured loses the export and nothing
// else. It is exported to `<workspace>.job`, keyed by the job identifier, so one job's
// records stay in order on one partition. A consumer rebuilding a tree depends on that.
//
// It is a method on the server rather than a function because the controller writes movements too,
// and both roads have to reach the same stream in the same shape. See job.Exporter.
func (s *Server) ExportJob(ctx context.Context, events ...*job.Event) {
	ctx = context.WithoutCancel(ctx)
	for _, event := range events {
		if event == nil {
			continue
		}
		s.exportJobEvent(ctx, event)
	}
}

func (s *Server) exportJobEvent(ctx context.Context, event *job.Event) {
	workspace, err := s.store.GetWorkspace(ctx, event.Workspace)
	if err != nil {
		slog.WarnContext(ctx, "no workspace for this job event, so it is not exported",
			"job", event.Job, "kind", event.Kind, "error", err)
		return
	}
	topic, err := messaging.Topic(workspace.GetName(), jobStream)
	if err != nil {
		slog.WarnContext(ctx, "no topic for this job event, so it is not exported",
			"job", event.Job, "kind", event.Kind, "error", err)
		return
	}
	value, err := proto.Marshal(asJobEvent(event))
	if err != nil {
		slog.WarnContext(ctx, "a job event could not be encoded, so it is not exported",
			"job", event.Job, "kind", event.Kind, "error", err)
		return
	}
	if err := s.export(ctx, event.Job, topic, value); err != nil {
		slog.WarnContext(ctx, "a job event could not be exported",
			"job", event.Job, "kind", event.Kind, "topic", topic, "error", err)
	}
}

// asJobEvent puts one record on the wire. The detail was redacted before it was written, so what is
// exported is what the store holds and nothing more.
func asJobEvent(event *job.Event) *quaycrewv1.JobEvent {
	return &quaycrewv1.JobEvent{
		Id: event.ID, Kind: event.Kind, Job: event.Job,
		Workspace: event.Workspace, Project: event.Project,
		Parent: event.Parent, Depth: int32(event.Depth),
		Detail: event.Detail, TraceId: event.TraceID,
		OccurredAt: timestamppb.New(event.OccurredAt),
	}
}
