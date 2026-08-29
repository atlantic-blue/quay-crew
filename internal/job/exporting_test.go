package job_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/telemetry"
)

// theLog is what the crew offered to the event log, in the order it offered it. It records rather
// than publishes, because what this proves is when the crew hands a record over: after the write
// that put it in the store, and never instead of it.
type theLog struct {
	mu      sync.Mutex
	offered []*job.Event
}

func (l *theLog) ExportJob(_ context.Context, events ...*job.Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, event := range events {
		if event != nil {
			l.offered = append(l.offered, event)
		}
	}
}

func (l *theLog) kinds() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.offered))
	for _, event := range l.offered {
		out = append(out, event.Kind)
	}
	return out
}

func (l *theLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.offered)
}

const aTrace = "4bf92f3577b34da6a3ce929d0e0e4736"

// Every movement reaches the log, and every one of them carries the job's own trace.
func TestEveryMovementIsOfferedToTheLogAndCarriesTheTrace(t *testing.T) {
	kept, plane := newRows(), newCrew()
	log := &theLog{}
	controller := job.NewController(kept, plane, nil, nil, nil).Exporting(log)

	declared := declaredJob("read the electricity bill")
	declared.TraceID, declared.ParentSpanID = aTrace, "00f067aa0ba902b7"
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q", got.Phase)
	}
	// The claim, the start and the answer. The store holds the same three, which is the point: the
	// log is a copy of what landed rather than a second account of it.
	want := []string{job.EventClaimed, job.EventStarted, job.EventAnswered}
	if got := log.kinds(); !sameKinds(got, want) {
		t.Fatalf("the log was offered %v, want %v", got, want)
	}
	if got := kept.kinds(one.ID); !sameKinds(got, want) {
		t.Fatalf("the store holds %v, want %v", got, want)
	}
	for _, event := range log.offered {
		if event.TraceID != aTrace {
			t.Fatalf("a %s record traces %q, and one job is one trace", event.Kind, event.TraceID)
		}
	}
}

// The export follows the write. A write that did not apply must leave nothing on the log, or a
// consumer learns about a movement the store never made.
func TestAWriteThatDidNotApplyIsNotExported(t *testing.T) {
	kept, plane := newRows(), newCrew()
	log := &theLog{}
	controller := job.NewController(kept, plane, nil, nil, nil).Exporting(log)

	one := kept.add(declaredJob("read the electricity bill"))
	kept.refuseStart = errors.New("another controller claimed it first")

	controller.Tick(context.Background())

	if log.count() != 0 {
		t.Fatalf("%d records were offered to the log for a claim that never applied: %v",
			log.count(), log.kinds())
	}
	if got := kept.get(one.ID); got.Phase != job.PhasePending {
		t.Fatalf("the job is %q, so this test is not about a claim that failed", got.Phase)
	}
}

// A crew with no broker configured is the default, and it is the one every scenario runs on. The
// job has to run exactly the same on it.
func TestAControllerWithNoExporterStillRunsTheJob(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.Phase != job.PhaseDone || got.Answer == "" {
		t.Fatalf("the job is %q with answer %q on a crew with no broker", got.Phase, got.Answer)
	}
	if len(kept.recorded(one.ID)) != 3 {
		t.Fatalf("%d records are in the store on a crew with no broker", len(kept.recorded(one.ID)))
	}
}

// The task the controller sends belongs to the job's trace. That is what lets a reader join the row
// to the conversation that ran it, and it comes off the row rather than out of this process.
func TestTheTaskAControllerSendsRunsUnderTheWorksOwnTrace(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := job.NewController(kept, plane, nil, nil, nil)

	declared := declaredJob("read the electricity bill")
	declared.TraceID, declared.ParentSpanID = aTrace, "00f067aa0ba902b7"
	kept.add(declared)

	controller.Tick(context.Background())

	if got := telemetry.TraceIDFrom(plane.lastContext()); got != aTrace {
		t.Fatalf("the task was sent under trace %q, want the job's own", got)
	}
}

// Job nothing was tracing is dispatched all the same. A missing trace is a record with less in it,
// never a job that does not run.
func TestJobWithNoTraceStillRuns(t *testing.T) {
	kept, plane := newRows(), newCrew()
	log := &theLog{}
	controller := job.NewController(kept, plane, nil, nil, nil).Exporting(log)
	one := kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks", plane.sent())
	}
	if got := kept.get(one.ID); got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q", got.Phase)
	}
	for _, event := range log.offered {
		if event.TraceID != "" {
			t.Fatalf("a trace was invented for untraced job: %q", event.TraceID)
		}
	}
}

func sameKinds(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
