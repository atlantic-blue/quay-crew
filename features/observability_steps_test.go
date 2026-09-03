package features_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/logging"
	"github.com/cucumber/godog"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// spans holds every span the system ended during the suite. The provider is installed at package
// initialisation rather than in a hook, because the gRPC instrumentation reads the global provider
// once when the server is built, and a server built before the provider is set records nothing.
//
// WithSyncer rather than a batcher, so a span is readable the moment it ends and no scenario has to
// flush anything.
var spans = tracetest.NewInMemoryExporter()

func init() {
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans)))
}

// observabilityWorld is where one scenario's log output is captured, beside the shared world.
type observabilityWorld struct {
	logs *bytes.Buffer
	// line is the log line a step found, so the next step can assert on the same one.
	line map[string]any
}

type observabilityKey struct{}

func observabilityFrom(ctx context.Context) *observabilityWorld {
	o, _ := ctx.Value(observabilityKey{}).(*observabilityWorld)
	return o
}

// loggedLines decodes what the system wrote during this scenario, oldest first.
func loggedLines(o *observabilityWorld) ([]map[string]any, error) {
	read := make([]map[string]any, 0)
	for _, raw := range bytes.Split(bytes.TrimSpace(o.logs.Bytes()), []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal(raw, &line); err != nil {
			return nil, fmt.Errorf("the system wrote a line that is not JSON: %w: %s", err, raw)
		}
		read = append(read, line)
	}
	return read, nil
}

// endedSpanNames is what the system recorded, so a failure says what was there instead of nothing.
func endedSpanNames() []string {
	ended := spans.GetSpans()
	names := make([]string, 0, len(ended))
	for _, span := range ended {
		names = append(names, span.Name)
	}
	return names
}

// initializeObservabilitySteps registers the steps for following a call after it happened.
func initializeObservabilitySteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		spans.Reset()
		o := &observabilityWorld{logs: &bytes.Buffer{}}
		logging.Init("controlplane", o.logs)
		return context.WithValue(ctx, observabilityKey{}, o), nil
	})

	// The span is ended by a gRPC stats handler on the server, and a stats handler sees the end of a
	// call after the status has gone back to the caller. So a client that has its answer is not
	// evidence that the span has landed, and reading once asserts on a set that may still be filling.
	// The same shape as the exported task below, which waits for the same reason.
	sc.Step(`^the system records a span named "([^"]*)"$`, func(ctx context.Context, name string) error {
		deadline := time.Now().Add(2 * time.Second)
		for {
			ended := endedSpanNames()
			for _, one := range ended {
				if one == name {
					return nil
				}
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("no span named %q was recorded; the system recorded %v", name, ended)
			}
			time.Sleep(10 * time.Millisecond)
		}
	})

	// The span is waited for, for the reason the step above gives: a stats handler sees the end of a
	// call after the status has gone back to the caller, so a line that exists is not evidence that
	// the span carrying its id has landed. Reading the set once fails whenever the machine is busy.
	sc.Step(`^that line carries the correlation id of the call it happened under$`, func(ctx context.Context) error {
		o := observabilityFrom(ctx)
		if o.line == nil {
			return fmt.Errorf("no line was found to assert on")
		}
		id, _ := o.line[logging.CorrelationKey].(string)
		if id == "" {
			return fmt.Errorf("the line carries no correlation id: %v", o.line)
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			for _, ended := range spans.GetSpans() {
				if ended.SpanContext.TraceID().String() == id {
					return nil
				}
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("the line's correlation id %q belongs to no recorded call, so a log line and a trace cannot be joined", id)
			}
			time.Sleep(10 * time.Millisecond)
		}
	})

	sc.Step(`^the system logs on its way up$`, func(ctx context.Context) error {
		slog.Info("control plane serving")
		return nil
	})

	sc.Step(`^that line names the service and carries no correlation id$`, func(ctx context.Context) error {
		lines, err := loggedLines(observabilityFrom(ctx))
		if err != nil {
			return err
		}
		for _, line := range lines {
			message, _ := line["msg"].(string)
			if message != "control plane serving" {
				continue
			}
			if service, _ := line[logging.ServiceKey].(string); service != "controlplane" {
				return fmt.Errorf("the line names the service as %q, wanted controlplane", service)
			}
			if _, present := line[logging.CorrelationKey]; present {
				return fmt.Errorf("a line written before any call arrived carries a correlation id: %v", line)
			}
			return nil
		}
		return fmt.Errorf("the system wrote no line on its way up")
	})
}
