// Package logging is the system's log event shape. Every service writes one JSON object per event to
// its own stdout, and every line carries the same three fields on top of what the call site adds:
//
//	service         which service wrote the line
//	correlation_id  the call the line happened under, equal to the trace id
//	level, msg      what slog writes for any record
//
// The correlation id is read from the context rather than passed by hand, so a line written five
// calls deep inside a request carries the same id as the span around it. That is the whole point of
// the field: it is what joins a log line in Loki to a trace in Tempo, and it only works if the call
// site logs with a context. A line logged without one carries no correlation id, which is correct
// for a line written on the way up before any call has arrived.
//
// Init makes this logger the default as well as returning it, because most of the system logs through
// the package level slog.WarnContext rather than through a logger it was handed.
package logging

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/trace"
)

// The keys every line carries. Named here rather than spelled at each call site so a dashboard
// query and the code cannot drift apart.
const (
	ServiceKey     = "service"
	CorrelationKey = "correlation_id"
)

// Init builds the system's logger, writing JSON to out, and makes it the default.
func Init(service string, out io.Writer) *slog.Logger {
	return install(service, correlated{slog.NewJSONHandler(out, nil)})
}

// AlsoExport sends every line to the OpenTelemetry log pipeline as well as to out, and returns the
// new default logger.
//
// A copy rather than a move. A container's stdout is the signal that works when everything else is
// broken, including the collector, so it keeps carrying every line whatever happens downstream. Call
// this after telemetry.Init, because the bridge reads the global logger provider that call installs.
func AlsoExport(service string, out io.Writer) *slog.Logger {
	return install(service, both{
		correlated{slog.NewJSONHandler(out, nil)},
		// The bridge stamps the trace id onto the record itself from the context, so a line in Loki
		// carries the correlation twice: once as our own field and once where Grafana looks for it.
		otelslog.NewHandler(service),
	})
}

func install(service string, handler slog.Handler) *slog.Logger {
	logger := slog.New(handler).With(ServiceKey, service)
	slog.SetDefault(logger)
	return logger
}

// both writes each record to two handlers. A failure in one is returned and does not stop the other,
// because the whole point of keeping stdout is that it survives the exporter being unreachable.
type both struct {
	first  slog.Handler
	second slog.Handler
}

func (b both) Enabled(ctx context.Context, level slog.Level) bool {
	return b.first.Enabled(ctx, level) || b.second.Enabled(ctx, level)
}

func (b both) Handle(ctx context.Context, record slog.Record) error {
	firstErr := b.first.Handle(ctx, record.Clone())
	secondErr := b.second.Handle(ctx, record)
	return errors.Join(firstErr, secondErr)
}

func (b both) WithAttrs(attrs []slog.Attr) slog.Handler {
	return both{b.first.WithAttrs(attrs), b.second.WithAttrs(attrs)}
}

func (b both) WithGroup(name string) slog.Handler {
	return both{b.first.WithGroup(name), b.second.WithGroup(name)}
}

// CorrelationID is the id of the call ctx is under, or empty when ctx is under no recorded call.
//
// It is the trace id, not a second identifier beside it: an id that has to be joined to the trace id
// to be useful is one nobody joins.
func CorrelationID(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if !span.HasTraceID() {
		return ""
	}
	return span.TraceID().String()
}

// correlated stamps the correlation id from the context onto every record.
type correlated struct {
	inner slog.Handler
}

func (c correlated) Enabled(ctx context.Context, level slog.Level) bool {
	return c.inner.Enabled(ctx, level)
}

func (c correlated) Handle(ctx context.Context, record slog.Record) error {
	if id := CorrelationID(ctx); id != "" {
		record = record.Clone()
		record.AddAttrs(slog.String(CorrelationKey, id))
	}
	return c.inner.Handle(ctx, record)
}

func (c correlated) WithAttrs(attrs []slog.Attr) slog.Handler {
	return correlated{c.inner.WithAttrs(attrs)}
}

// WithGroup nests the correlation id inside the group, because a record's attributes are added
// wherever the handler is currently open. Nothing in the system opens a group, and a caller that does
// should read the id from CorrelationID instead of expecting it at the top level.
func (c correlated) WithGroup(name string) slog.Handler {
	return correlated{c.inner.WithGroup(name)}
}
