package features_test

import (
	"context"
	"fmt"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/telemetry"
	"github.com/cucumber/godog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// measured is the meter this suite collects from. Installed at package initialisation for the same
// reason the tracer provider is: the control plane creates its instruments when the server is built,
// and instruments made against a provider installed later measure nothing.
var measured = sdkmetric.NewManualReader()

func init() {
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(measured)))
}

// metricsWorld is one scenario's measurement state, beside the shared world.
type metricsWorld struct {
	// before is everything that had been recorded when the scenario started. The reader is process
	// wide and counters only go up, so a scenario reads the difference rather than the total. The
	// whole snapshot is kept rather than a total, because a step filters by attributes and both
	// sides of the subtraction have to be filtered the same way.
	before metricdata.ResourceMetrics
}

type metricsKey struct{}

func metricsFrom(ctx context.Context) *metricsWorld {
	m, _ := ctx.Value(metricsKey{}).(*metricsWorld)
	return m
}

// recorded is everything the crew has measured so far.
func recorded() (metricdata.ResourceMetrics, error) {
	var collected metricdata.ResourceMetrics
	if err := measured.Collect(context.Background(), &collected); err != nil {
		return collected, fmt.Errorf("collecting what was measured: %w", err)
	}
	return collected, nil
}

// totalOf sums one metric across a snapshot, keeping only the points whose attributes match want.
func totalOf(snapshot metricdata.ResourceMetrics, name string, want map[string]string) float64 {
	matches := func(set attribute.Set) bool {
		for key, value := range want {
			found, present := set.Value(attribute.Key(key))
			if !present || found.String() != value {
				return false
			}
		}
		return true
	}
	var total float64
	for _, scope := range snapshot.ScopeMetrics {
		for _, published := range scope.Metrics {
			if published.Name != name {
				continue
			}
			switch data := published.Data.(type) {
			case metricdata.Sum[int64]:
				for _, point := range data.DataPoints {
					if matches(point.Attributes) {
						total += float64(point.Value)
					}
				}
			case metricdata.Sum[float64]:
				for _, point := range data.DataPoints {
					if matches(point.Attributes) {
						total += point.Value
					}
				}
			}
		}
	}
	return total
}

// measuredDuring is how much of a metric this scenario added, for the attributes given. Both sides of
// the subtraction are filtered the same way, or the difference is between two different questions.
func measuredDuring(ctx context.Context, name string, want map[string]string) (float64, error) {
	now, err := recorded()
	if err != nil {
		return 0, err
	}
	return totalOf(now, name, want) - totalOf(metricsFrom(ctx).before, name, want), nil
}

// initializeMetricsSteps registers the steps for what a turn spent.
func initializeMetricsSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		before, err := recorded()
		if err != nil {
			return ctx, err
		}
		return context.WithValue(ctx, metricsKey{}, &metricsWorld{before: before}), nil
	})

	sc.Step(`^the model reports spending (\d+) in and (\d+) out, costing ([0-9.]+)$`,
		func(ctx context.Context, in, out int, cost float64) error {
			w := worldFrom(ctx)
			w.runner.usage = sandbox.Usage{Input: int64(in), Output: int64(out)}
			w.runner.cost = cost
			w.runner.usageReported = true
			return nil
		})

	sc.Step(`^the crew measures (\d+) tokens spent on "([^"]*)" and "([^"]*)"$`,
		func(ctx context.Context, want int, workspace, project string) error {
			if err := worldFrom(ctx).settled(ctx); err != nil {
				return err
			}
			got, err := measuredDuring(ctx, telemetry.TokensMetric, map[string]string{
				telemetry.WorkspaceAttribute: workspace,
				telemetry.ProjectAttribute:   project,
			})
			if err != nil {
				return err
			}
			if got != float64(want) {
				anywhere, _ := measuredDuring(ctx, telemetry.TokensMetric, nil)
				return fmt.Errorf("the crew measured %v tokens on %s and %s, want %d (%v were measured anywhere)",
					got, workspace, project, want, anywhere)
			}
			return nil
		})

	sc.Step(`^the crew measures ([0-9.]+) of cost$`, func(ctx context.Context, want float64) error {
		got, err := measuredDuring(ctx, telemetry.CostMetric, nil)
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("the crew measured %v of cost, want %v", got, want)
		}
		return nil
	})

	sc.Step(`^the crew counts one turn, which failed$`, func(ctx context.Context) error {
		if err := worldFrom(ctx).settled(ctx); err != nil {
			return err
		}
		got, err := measuredDuring(ctx, telemetry.TurnsMetric, map[string]string{
			telemetry.StatusAttribute: "failed",
		})
		if err != nil {
			return err
		}
		if got != 1 {
			return fmt.Errorf("the crew counted %v failed turns, want 1", got)
		}
		return nil
	})

	sc.Step(`^the crew counts one turn$`, func(ctx context.Context) error {
		if err := worldFrom(ctx).settled(ctx); err != nil {
			return err
		}
		got, err := measuredDuring(ctx, telemetry.TurnsMetric, nil)
		if err != nil {
			return err
		}
		if got != 1 {
			return fmt.Errorf("the crew counted %v turns, want 1", got)
		}
		return nil
	})

	sc.Step(`^the crew measures no tokens and no cost$`, func(ctx context.Context) error {
		tokens, err := measuredDuring(ctx, telemetry.TokensMetric, nil)
		if err != nil {
			return err
		}
		if tokens != 0 {
			return fmt.Errorf("the crew measured %v tokens for a turn that reported none, want none", tokens)
		}
		cost, err := measuredDuring(ctx, telemetry.CostMetric, nil)
		if err != nil {
			return err
		}
		if cost != 0 {
			return fmt.Errorf("the crew measured %v of cost for a turn that reported none, want none", cost)
		}
		return nil
	})
}
