package telemetry

import (
	"context"
	"fmt"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// The names the system publishes. Spelled once here, because a dashboard and an alert are written
// against these strings and renaming one quietly empties both.
const (
	TasksMetric  = "quaycrew.tasks"
	TokensMetric = "quaycrew.tokens"
	CostMetric   = "quaycrew.cost.usd"
)

// The attributes every task measurement carries. A total with no way to say whose it is answers the
// only cheap question and none of the expensive ones.
const (
	WorkspaceAttribute = "workspace"
	ProjectAttribute   = "project"
	ModelAttribute     = "model"
	StatusAttribute    = "status"
	// TokenKindAttribute separates input, output and the two cache figures on one counter, rather
	// than spending four metric names on one quantity.
	TokenKindAttribute = "kind"
)

// TaskMeasurement is what one finished task spent, and where.
type TaskMeasurement struct {
	Workspace string
	Project   string
	Model     string
	// Status is "idle" for a task that worked and "failed" for one that did not. A failed task still
	// spends tokens, and a cost dashboard that counts only the tasks that worked understates the
	// bill in exactly the situation somebody is investigating.
	Status string
	// Usage is the four numbers a task spends, in the system's existing vocabulary for them.
	Usage   sandbox.Usage
	CostUSD float64
	// Reported says the backend gave numbers. A task that reports nothing is still counted as a
	// task, and contributes no tokens and no cost, so an unknown never reads as a zero.
	Reported bool
}

// TaskMetrics is the system's spending, published as OpenTelemetry instruments.
type TaskMetrics struct {
	tasks  metric.Int64Counter
	tokens metric.Int64Counter
	cost   metric.Float64Counter
}

// NewTaskMetrics creates the instruments. It reads the global meter provider, so Init has to have
// run; with no provider installed the instruments are the no operation ones and recording costs
// nothing, which is what the tests and a system with telemetry off both want.
func NewTaskMetrics() (*TaskMetrics, error) {
	meter := otel.Meter("github.com/atlantic-blue/quay-crew")

	tasks, err := meter.Int64Counter(TasksMetric,
		metric.WithDescription("tasks run, by workspace, project, model and status"))
	if err != nil {
		return nil, fmt.Errorf("telemetry: tasks counter: %w", err)
	}
	tokens, err := meter.Int64Counter(TokensMetric,
		metric.WithDescription("tokens spent, by kind: input, output, cache read and cache written"))
	if err != nil {
		return nil, fmt.Errorf("telemetry: tokens counter: %w", err)
	}
	cost, err := meter.Float64Counter(CostMetric,
		metric.WithDescription("what the tasks would cost at published prices; the system runs under a subscription, so this is not a charge"))
	if err != nil {
		return nil, fmt.Errorf("telemetry: cost counter: %w", err)
	}
	return &TaskMetrics{tasks: tasks, tokens: tokens, cost: cost}, nil
}

// Record publishes one finished task. A nil TaskMetrics records nothing, so a caller built without
// telemetry does not have to guard every call site.
func (m *TaskMetrics) Record(ctx context.Context, measurement TaskMeasurement) {
	if m == nil {
		return
	}
	where := metric.WithAttributes(
		attribute.String(WorkspaceAttribute, measurement.Workspace),
		attribute.String(ProjectAttribute, measurement.Project),
		attribute.String(ModelAttribute, measurement.Model),
		attribute.String(StatusAttribute, measurement.Status),
	)
	m.tasks.Add(ctx, 1, where)
	if !measurement.Reported {
		return
	}
	// Reported is the only gate. A reported zero is published as a zero, because "this workspace ran
	// tasks and wrote nothing to the cache" is a fact worth having a series for, and skipping it
	// would leave a gap that reads the same as a task nobody measured.
	for kind, count := range map[string]int64{
		"input":         measurement.Usage.Input,
		"output":        measurement.Usage.Output,
		"cache_read":    measurement.Usage.CacheRead,
		"cache_written": measurement.Usage.CacheWritten,
	} {
		m.tokens.Add(ctx, count, where, metric.WithAttributes(attribute.String(TokenKindAttribute, kind)))
	}
	m.cost.Add(ctx, measurement.CostUSD, where)
}
