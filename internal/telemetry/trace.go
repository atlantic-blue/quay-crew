package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TraceparentEnv is the environment variable a task carries its trace context in, spelled the way
// the standard header is so anything that already reads one needs no translation.
//
// It goes on the task and never on the sandbox. A sandbox is born with its environment and is reused
// across tasks, so a value written at birth labels every later task with the first task's span. That
// is the trap the crew's own refusal message names: a capability granted after birth does not reach
// the container that is already running, and a trace context written at birth never changes again.
const TraceparentEnv = "QC_TRACEPARENT"

// Tracer is where the crew's own spans come from. One name, so a reader filtering a trace by
// instrumentation scope gets the crew's spans and not the library's.
const Tracer = "github.com/atlantic-blue/quay-crew"

// TraceIDFrom is the trace the context belongs to, and empty when nothing is tracing it. It is the
// same value internal/logging writes on every line as the correlation id, deliberately: one
// identifier joins a row, a record on the log, a span and a log line.
func TraceIDFrom(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if !span.HasTraceID() {
		return ""
	}
	return span.TraceID().String()
}

// SpanIDFrom is the span the context is inside, and empty when there is none.
func SpanIDFrom(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if !span.HasSpanID() {
		return ""
	}
	return span.SpanID().String()
}

// NewTraceID mints a trace identifier for a record that starts one, which is a root nothing was
// tracing: work declared by a poller, or by a caller whose own tool starts no trace.
//
// It is minted rather than left empty because the identifier is what joins the tree together
// afterwards. A root with none leaves every descendant unjoined.
func NewTraceID() string {
	// Minted here rather than by opening a span, because a crew with no tracer provider installed
	// would get nothing from one and the identifier has to exist either way: it is what joins the
	// rows, and the rows are kept whether or not anybody is exporting spans.
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(id[:])
}

// Under is a context whose parent span is the one named by traceID and spanID, which is how a record
// that outlived the process that wrote it goes on being part of one trace.
//
// A trace identifier with no span identifier still joins: the span opened under it belongs to the
// trace and has no parent inside it, which is the honest shape for work nobody was inside when it
// was declared.
func Under(ctx context.Context, traceID, spanID string) context.Context {
	trace16, err := trace.TraceIDFromHex(traceID)
	if err != nil {
		return ctx
	}
	config := trace.SpanContextConfig{TraceID: trace16, TraceFlags: trace.FlagsSampled, Remote: true}
	if span8, err := trace.SpanIDFromHex(spanID); err == nil {
		config.SpanID = span8
	}
	return trace.ContextWithRemoteSpanContext(ctx, trace.NewSpanContext(config))
}

// Traceparent is the standard trace context header for whatever span the context is inside, and
// empty when there is none. A task carries this so anything inside the container that reads it joins
// the trace rather than starting a second one.
func Traceparent(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if !span.HasTraceID() || !span.HasSpanID() {
		return ""
	}
	// The flags are written as the byte they are. Formatted as the type itself they would go through
	// its own String method and be hex encoded twice, which reads as a valid header and is not one.
	return fmt.Sprintf("00-%s-%s-%02x", span.TraceID(), span.SpanID(),
		byte(span.TraceFlags()&trace.FlagsSampled))
}

// Record emits a span for something that already finished, between two moments the crew knows.
//
// A span is normally opened and closed by the code inside it. A piece of work cannot be: it outlives
// the process that declared it, it is picked up by whichever controller has the lease, and a span
// object held in memory across that would be lost with the first controller that died. So the crew
// records the span when it knows both ends, from the timestamps on the row.
//
// The cost is stated: these spans are siblings under the same parent rather than parents of one
// another, because a span identifier cannot be minted before the span exists. A reader sees one
// trace with the whole life of the work in it and the attempts beside it, joined by the trace
// identifier rather than nested inside it.
func Record(ctx context.Context, name string, started, ended time.Time, attributes ...attribute.KeyValue) {
	if started.IsZero() || ended.Before(started) {
		return
	}
	_, span := otel.Tracer(Tracer).Start(ctx, name,
		trace.WithTimestamp(started), trace.WithAttributes(attributes...))
	span.End(trace.WithTimestamp(ended))
}
