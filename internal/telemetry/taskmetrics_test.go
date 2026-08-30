package telemetry_test

import (
	"context"
	"testing"

	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// collected installs a meter provider that keeps what is recorded, and returns a function that reads
// it back.
func collected(t *testing.T) func() metricdata.ResourceMetrics {
	t.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetMeterProvider(nil)
	})
	return func() metricdata.ResourceMetrics {
		var got metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &got); err != nil {
			t.Fatalf("collecting: %v", err)
		}
		return got
	}
}

// sumFor is the total recorded on a metric, added across every attribute set, and whether the metric
// was published at all.
func sumFor(got metricdata.ResourceMetrics, name string) (float64, bool) {
	var total float64
	var found bool
	for _, scope := range got.ScopeMetrics {
		for _, published := range scope.Metrics {
			if published.Name != name {
				continue
			}
			found = true
			switch data := published.Data.(type) {
			case metricdata.Sum[int64]:
				for _, point := range data.DataPoints {
					total += float64(point.Value)
				}
			case metricdata.Sum[float64]:
				for _, point := range data.DataPoints {
					total += point.Value
				}
			}
		}
	}
	return total, found
}

// attributesOn is the attribute sets a metric was recorded with, as maps.
func attributesOn(got metricdata.ResourceMetrics, name string) []map[string]string {
	var sets []map[string]string
	for _, scope := range got.ScopeMetrics {
		for _, published := range scope.Metrics {
			if published.Name != name {
				continue
			}
			if data, ok := published.Data.(metricdata.Sum[int64]); ok {
				for _, point := range data.DataPoints {
					set := map[string]string{}
					for _, attribute := range point.Attributes.ToSlice() {
						set[string(attribute.Key)] = attribute.Value.String()
					}
					sets = append(sets, set)
				}
			}
		}
	}
	return sets
}

func aTask() telemetry.TaskMeasurement {
	return telemetry.TaskMeasurement{
		Workspace: "me", Project: "house-bills", Model: "claude-code", Status: "idle",
		Usage:   sandbox.Usage{Input: 1200, Output: 340, CacheRead: 9000, CacheWritten: 500},
		CostUSD: 0.0241, Reported: true,
	}
}

func TestATaskPublishesItsTokensAndItsCost(t *testing.T) {
	read := collected(t)
	metrics, err := telemetry.NewTaskMetrics()
	if err != nil {
		t.Fatalf("creating the instruments: %v", err)
	}

	metrics.Record(context.Background(), aTask())

	got := read()
	if total, found := sumFor(got, telemetry.TokensMetric); !found || total != 11040 {
		t.Errorf("tokens published %v (found %v), wanted 11040 across the four kinds", total, found)
	}
	if total, found := sumFor(got, telemetry.CostMetric); !found || total != 0.0241 {
		t.Errorf("cost published %v (found %v), wanted 0.0241", total, found)
	}
	if total, found := sumFor(got, telemetry.TasksMetric); !found || total != 1 {
		t.Errorf("tasks published %v (found %v), wanted 1", total, found)
	}
}

// A total nobody can break down answers the only cheap question and none of the expensive ones.
func TestEveryTokenCountSaysWhoseItIsAndWhatKindItIs(t *testing.T) {
	read := collected(t)
	metrics, _ := telemetry.NewTaskMetrics()

	metrics.Record(context.Background(), aTask())

	sets := attributesOn(read(), telemetry.TokensMetric)
	if len(sets) != 4 {
		t.Fatalf("tokens were recorded under %d attribute sets, wanted one per kind", len(sets))
	}
	kinds := map[string]bool{}
	for _, set := range sets {
		for _, key := range []string{
			telemetry.WorkspaceAttribute, telemetry.ProjectAttribute,
			telemetry.ModelAttribute, telemetry.StatusAttribute, telemetry.TokenKindAttribute,
		} {
			if set[key] == "" {
				t.Errorf("a token count carries no %s: %v", key, set)
			}
		}
		kinds[set[telemetry.TokenKindAttribute]] = true
	}
	for _, kind := range []string{"input", "output", "cache_read", "cache_written"} {
		if !kinds[kind] {
			t.Errorf("no token count was published for %s", kind)
		}
	}
}

// A zero that means "the backend never said" must not read as "this task was free". The task is
// still counted, so the task rate stays right.
func TestATaskThatReportsNothingCountsAsATaskAndNotAsFree(t *testing.T) {
	read := collected(t)
	metrics, _ := telemetry.NewTaskMetrics()

	metrics.Record(context.Background(), telemetry.TaskMeasurement{
		Workspace: "me", Project: "house-bills", Model: "echo", Status: "idle", Reported: false,
	})

	got := read()
	if total, found := sumFor(got, telemetry.TasksMetric); !found || total != 1 {
		t.Errorf("tasks published %v (found %v), wanted the task counted", total, found)
	}
	if _, found := sumFor(got, telemetry.TokensMetric); found {
		t.Error("a task whose backend reported no usage published a token count, so an unknown reads as a zero")
	}
	if _, found := sumFor(got, telemetry.CostMetric); found {
		t.Error("a task whose backend reported no usage published a cost, so an unknown reads as free")
	}
}

// A failed task spent what it spent. Counting only the tasks that worked understates the bill in
// exactly the situation somebody is investigating.
func TestAFailedTaskIsMeasuredToo(t *testing.T) {
	read := collected(t)
	metrics, _ := telemetry.NewTaskMetrics()

	failed := aTask()
	failed.Status = "failed"
	metrics.Record(context.Background(), failed)

	for _, set := range attributesOn(read(), telemetry.TokensMetric) {
		if set[telemetry.StatusAttribute] != "failed" {
			t.Errorf("a failed task was published as %q", set[telemetry.StatusAttribute])
		}
	}
	if total, _ := sumFor(read(), telemetry.TokensMetric); total == 0 {
		t.Error("a failed task published no tokens, so what it spent is invisible")
	}
}

// Recording on a system with no telemetry must not be a crash. NewTaskMetrics returning nil is the
// shape a caller gets when it ignored the error, and every call site would otherwise need a guard.
func TestRecordingOnNothingIsSafe(t *testing.T) {
	var absent *telemetry.TaskMetrics
	absent.Record(context.Background(), aTask())
}
