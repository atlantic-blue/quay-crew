package telemetry_test

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/krewe/internal/telemetry"
)

// The trace is what joins a row to a span to a log line, so the shape of these identifiers is a
// contract rather than an implementation detail: a value that is not 32 hexadecimal characters joins
// to nothing.
var (
	traceShape  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	parentShape = regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-0[01]$`)
)

func TestAContextNothingIsTracingCarriesNoTrace(t *testing.T) {
	ctx := context.Background()
	if got := telemetry.TraceIDFrom(ctx); got != "" {
		t.Fatalf("an untraced context says its trace is %q", got)
	}
	if got := telemetry.SpanIDFrom(ctx); got != "" {
		t.Fatalf("an untraced context says its span is %q", got)
	}
	// And it hands a task nothing rather than a header with zeros in it, which a reader would open
	// and find nothing behind.
	if got := telemetry.Traceparent(ctx); got != "" {
		t.Fatalf("an untraced context offers %q as a trace context", got)
	}
}

// A root that nothing was tracing still gets an identifier, because the identifier is what joins the
// tree together afterwards and a root with none leaves every descendant unjoined.
func TestATraceIdentifierIsMintedInTheShapeEverythingElseUses(t *testing.T) {
	minted := telemetry.NewTraceID()
	if !traceShape.MatchString(minted) {
		t.Fatalf("the minted trace is %q, which joins to nothing", minted)
	}
	if second := telemetry.NewTraceID(); second == minted {
		t.Fatal("two trees were given the same trace, so their records cannot be told apart")
	}
}

// The row is the trace context. A controller that picks up job reads both off it and goes on being
// part of the same trace, which is what makes a trace survive the process that started it.
func TestAContextUnderARecordedTraceCarriesItOn(t *testing.T) {
	const (
		trace  = "4bf92f3577b34da6a3ce929d0e0e4736"
		parent = "00f067aa0ba902b7"
	)
	ctx := telemetry.Under(context.Background(), trace, parent)
	if got := telemetry.TraceIDFrom(ctx); got != trace {
		t.Fatalf("the context traces %q, want the one on the row", got)
	}
	if got := telemetry.SpanIDFrom(ctx); got != parent {
		t.Fatalf("the context sits under span %q", got)
	}
	if got := telemetry.Traceparent(ctx); got != "00-"+trace+"-"+parent+"-01" {
		t.Fatalf("the task would be handed %q", got)
	}
	if !parentShape.MatchString(telemetry.Traceparent(ctx)) {
		t.Fatalf("%q is not a trace context header", telemetry.Traceparent(ctx))
	}
}

// A trace with no span still joins. Job declared by something nothing was inside belongs to the
// trace and has no parent within it, which is the honest shape rather than a made up parent.
func TestATraceWithNoSpanStillJoins(t *testing.T) {
	ctx := telemetry.Under(context.Background(), "4bf92f3577b34da6a3ce929d0e0e4736", "")
	if got := telemetry.TraceIDFrom(ctx); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("the context traces %q", got)
	}
	if got := telemetry.SpanIDFrom(ctx); got != "" {
		t.Fatalf("a span was invented: %q", got)
	}
	if got := telemetry.Traceparent(ctx); got != "" {
		t.Fatalf("a header was built with no span in it: %q", got)
	}
}

// A record the system cannot read leaves the context as it was. Half a trace context is worse than
// none: it sends a reader to a trace that does not exist.
func TestARecordedTraceTheSystemCannotReadIsIgnored(t *testing.T) {
	for _, bad := range []string{"", "not a trace", "4bf92f35", strings.Repeat("z", 32)} {
		ctx := telemetry.Under(context.Background(), bad, "00f067aa0ba902b7")
		if got := telemetry.TraceIDFrom(ctx); got != "" {
			t.Fatalf("%q was read as the trace %q", bad, got)
		}
	}
	// A trace that reads and a span that does not: the trace still joins, and no parent is invented.
	ctx := telemetry.Under(context.Background(), "4bf92f3577b34da6a3ce929d0e0e4736", "nonsense")
	if telemetry.TraceIDFrom(ctx) == "" {
		t.Fatal("a readable trace was thrown away because the span beside it was not")
	}
	if got := telemetry.SpanIDFrom(ctx); got != "" {
		t.Fatalf("a span was read out of nonsense: %q", got)
	}
}

// Recording a span that already happened must never panic on the times a row can actually hold: job
// that never started has no start, and a clock that moved backwards is not a duration.
func TestRecordingASpanWithNoHonestDurationDoesNothing(t *testing.T) {
	ctx := telemetry.Under(context.Background(), "4bf92f3577b34da6a3ce929d0e0e4736", "00f067aa0ba902b7")
	now := time.Now()
	telemetry.Record(ctx, "job", time.Time{}, now)
	telemetry.Record(ctx, "job", now, now.Add(-time.Hour))
	telemetry.Record(ctx, "job", now.Add(-time.Hour), now)
}
