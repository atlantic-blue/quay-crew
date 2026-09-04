package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/logging"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// underACall returns a context inside a recorded span, and the trace id that span was given.
func underACall(t *testing.T) (context.Context, string) {
	t.Helper()
	provider := trace.NewTracerProvider(trace.WithSyncer(tracetest.NewNoopExporter()))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer("test").Start(context.Background(), "inbound")
	t.Cleanup(func() { span.End() })
	return ctx, span.SpanContext().TraceID().String()
}

// lines reads back what was written as one decoded JSON object per line.
func lines(t *testing.T, out *bytes.Buffer) []map[string]any {
	t.Helper()
	var read []map[string]any
	for _, raw := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("a log line is not JSON: %v: %s", err, raw)
		}
		read = append(read, line)
	}
	return read
}

func TestALineUnderACallCarriesTheTraceIDAsItsCorrelationID(t *testing.T) {
	var out bytes.Buffer
	logger := logging.Init("controlplane", &out)
	ctx, traceID := underACall(t)

	logger.WarnContext(ctx, "an exec could not be exported")

	read := lines(t, &out)
	if len(read) != 1 {
		t.Fatalf("wanted one line, got %d", len(read))
	}
	if got := read[0][logging.CorrelationKey]; got != traceID {
		t.Errorf("correlation id is %v, wanted the trace id %s", got, traceID)
	}
	if got := read[0][logging.ServiceKey]; got != "controlplane" {
		t.Errorf("service is %v, wanted controlplane", got)
	}
}

// The bug this package exists to fix: the system logs through the package level slog, so a logger that
// is built and not made the default leaves every line inside internal/ unstructured and uncorrelated.
func TestThePackageLevelSlogIsTheSystemsLogger(t *testing.T) {
	var out bytes.Buffer
	logging.Init("controlplane", &out)
	ctx, traceID := underACall(t)

	slog.WarnContext(ctx, "a secret could not be mounted", "secret", "GH_TOKEN")

	read := lines(t, &out)
	if len(read) != 1 {
		t.Fatalf("wanted one line, got %d", len(read))
	}
	if got := read[0][logging.CorrelationKey]; got != traceID {
		t.Errorf("correlation id is %v, wanted the trace id %s", got, traceID)
	}
	if got := read[0]["secret"]; got != "GH_TOKEN" {
		t.Errorf("the call site's own attributes are lost: secret is %v", got)
	}
}

// Exec and flow job is detached from the request that started it, so the id has to survive the
// detaching or the interesting half of an exec is uncorrelated.
func TestTheCorrelationIDSurvivesADetachedContext(t *testing.T) {
	var out bytes.Buffer
	logger := logging.Init("controlplane", &out)
	ctx, traceID := underACall(t)

	logger.WarnContext(context.WithoutCancel(ctx), "an exec could not be written to history")

	read := lines(t, &out)
	if got := read[0][logging.CorrelationKey]; got != traceID {
		t.Errorf("correlation id is %v, wanted the trace id %s", got, traceID)
	}
}

// A handler that forgets to re-wrap itself in WithAttrs loses the correlation id the moment anything
// calls logger.With, which is the shape most of these wrappers get wrong.
func TestWithKeepsTheCorrelationID(t *testing.T) {
	var out bytes.Buffer
	logger := logging.Init("controlplane", &out)
	ctx, traceID := underACall(t)

	logger.With("session", "3cb04bf5").WarnContext(ctx, "a session could not be described")

	read := lines(t, &out)
	if got := read[0][logging.CorrelationKey]; got != traceID {
		t.Errorf("correlation id is %v, wanted the trace id %s", got, traceID)
	}
	if got := read[0]["session"]; got != "3cb04bf5" {
		t.Errorf("session is %v, wanted 3cb04bf5", got)
	}
}

func TestALineOutsideAnyCallCarriesNoCorrelationID(t *testing.T) {
	var out bytes.Buffer
	logger := logging.Init("controlplane", &out)

	logger.Info("control plane serving")

	read := lines(t, &out)
	if _, present := read[0][logging.CorrelationKey]; present {
		t.Errorf("a line written before any call arrived carries a correlation id: %v", read[0])
	}
	if got := read[0][logging.ServiceKey]; got != "controlplane" {
		t.Errorf("service is %v, wanted controlplane", got)
	}
}

// Exporting is a copy, not a move. A container's stdout is what an operator reads when the collector
// is the broken thing, so it has to keep carrying every line after the export is switched on.
func TestExportingKeepsWritingToStdout(t *testing.T) {
	var out bytes.Buffer
	logger := logging.AlsoExport("controlplane", &out)
	ctx, traceID := underACall(t)

	logger.WarnContext(ctx, "an exec could not be exported", "session", "3cb04bf5")

	read := lines(t, &out)
	if len(read) != 1 {
		t.Fatalf("wanted one line on stdout, got %d", len(read))
	}
	if got := read[0][logging.CorrelationKey]; got != traceID {
		t.Errorf("correlation id is %v, wanted the trace id %s", got, traceID)
	}
	if got := read[0]["session"]; got != "3cb04bf5" {
		t.Errorf("session is %v, wanted 3cb04bf5", got)
	}
	if got := read[0][logging.ServiceKey]; got != "controlplane" {
		t.Errorf("service is %v, wanted controlplane", got)
	}
}

// The exporting logger is the default one too, since almost everything the system logs goes through
// the package level slog rather than through a logger it was handed.
func TestExportingLoggerBecomesTheDefault(t *testing.T) {
	var out bytes.Buffer
	logging.AlsoExport("gateway", &out)

	slog.Info("service started")

	read := lines(t, &out)
	if len(read) != 1 {
		t.Fatalf("wanted one line, got %d", len(read))
	}
	if got := read[0][logging.ServiceKey]; got != "gateway" {
		t.Errorf("service is %v, wanted gateway", got)
	}
}

func TestCorrelationIDIsEmptyWithoutASpan(t *testing.T) {
	if got := logging.CorrelationID(context.Background()); got != "" {
		t.Errorf("correlation id is %q on a bare context, wanted empty", got)
	}
}
