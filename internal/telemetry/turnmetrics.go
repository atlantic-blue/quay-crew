package telemetry

import (
	"context"
	"fmt"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// The names the crew publishes. Spelled once here, because a dashboard and an alert are written
// against these strings and renaming one quietly empties both.
const (
	TurnsMetric  = "quaycrew.turns"
	TokensMetric = "quaycrew.tokens"
	CostMetric   = "quaycrew.cost.usd"
)

// The attributes every turn measurement carries. A total with no way to say whose it is answers the
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

// TurnMeasurement is what one finished turn spent, and where.
type TurnMeasurement struct {
	Workspace string
	Project   string
	Model     string
	// Status is "idle" for a turn that worked and "failed" for one that did not. A failed turn still
	// spends tokens, and a cost dashboard that counts only the turns that worked understates the
	// bill in exactly the situation somebody is investigating.
	Status string
	// Usage is the four numbers a turn spends, in the crew's existing vocabulary for them.
	Usage   sandbox.Usage
	CostUSD float64
	// Reported says the backend gave numbers. A turn that reports nothing is still counted as a
	// turn, and contributes no tokens and no cost, so an unknown never reads as a zero.
	Reported bool
}

// TurnMetrics is the crew's spending, published as OpenTelemetry instruments.
type TurnMetrics struct {
	turns  metric.Int64Counter
	tokens metric.Int64Counter
	cost   metric.Float64Counter
}

// NewTurnMetrics creates the instruments. It reads the global meter provider, so Init has to have
// run; with no provider installed the instruments are the no operation ones and recording costs
// nothing, which is what the tests and a crew with telemetry off both want.
func NewTurnMetrics() (*TurnMetrics, error) {
	meter := otel.Meter("github.com/atlantic-blue/quay-crew")

	turns, err := meter.Int64Counter(TurnsMetric,
		metric.WithDescription("turns run, by workspace, project, model and status"))
	if err != nil {
		return nil, fmt.Errorf("telemetry: turns counter: %w", err)
	}
	tokens, err := meter.Int64Counter(TokensMetric,
		metric.WithDescription("tokens spent, by kind: input, output, cache read and cache written"))
	if err != nil {
		return nil, fmt.Errorf("telemetry: tokens counter: %w", err)
	}
	cost, err := meter.Float64Counter(CostMetric,
		metric.WithDescription("what the turns would cost at published prices; the crew runs under a subscription, so this is not a charge"))
	if err != nil {
		return nil, fmt.Errorf("telemetry: cost counter: %w", err)
	}
	return &TurnMetrics{turns: turns, tokens: tokens, cost: cost}, nil
}

// Record publishes one finished turn. A nil TurnMetrics records nothing, so a caller built without
// telemetry does not have to guard every call site.
func (m *TurnMetrics) Record(ctx context.Context, measurement TurnMeasurement) {
	if m == nil {
		return
	}
	where := metric.WithAttributes(
		attribute.String(WorkspaceAttribute, measurement.Workspace),
		attribute.String(ProjectAttribute, measurement.Project),
		attribute.String(ModelAttribute, measurement.Model),
		attribute.String(StatusAttribute, measurement.Status),
	)
	m.turns.Add(ctx, 1, where)
	if !measurement.Reported {
		return
	}
	// Reported is the only gate. A reported zero is published as a zero, because "this workspace ran
	// turns and wrote nothing to the cache" is a fact worth having a series for, and skipping it
	// would leave a gap that reads the same as a turn nobody measured.
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
