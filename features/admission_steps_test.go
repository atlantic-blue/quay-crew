package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/headroom"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/cucumber/godog"
)

// The steps for the scenarios about what the crew admits. The fault they close: on 30 August 2026
// the crew admitted nine jobs onto a runtime with room for fewer, and the runtime exited with the
// control plane, the database and eight running jobs inside it. See issue 466.
//
// The runtime is the one thing stood in for. Everything else here is the real crew: the controller
// decides, the ledger counts what has been promised, and the store holds what came of it.

// aSizedRuntime is a container runtime a scenario gave a size to. A scenario changes what it says
// and asks the crew to read it again, because a machine that fills and empties is the whole point.
type aSizedRuntime struct {
	memory     int64
	held       int64
	processors float64
}

func (r *aSizedRuntime) Sample(context.Context) (headroom.Sample, error) {
	return headroom.Sample{
		TakenAt:    time.Now(),
		Limit:      headroom.Measured(r.memory),
		Used:       headroom.Measured(r.held),
		Processors: headroom.MeasuredShare(r.processors),
		Held:       headroom.MeasuredShare(0),
	}, nil
}

type admissionKey struct{}

func runtimeFrom(ctx context.Context) *aSizedRuntime {
	r, _ := ctx.Value(admissionKey{}).(*aSizedRuntime)
	return r
}

func initializeAdmissionSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a runtime holding (\d+) megabytes of (\d+), with (\d+) processors?$`,
		func(ctx context.Context, held, total, processors int) (context.Context, error) {
			w := worldFrom(ctx)
			runtime := &aSizedRuntime{
				memory:     int64(total) * megabyte,
				held:       int64(held) * megabyte,
				processors: float64(processors) * 100,
			}
			w.machine = runtime
			return context.WithValue(ctx, admissionKey{}, runtime), w.restart()
		})

	sc.Step(`^the runtime frees memory, holding (\d+) megabytes$`,
		func(ctx context.Context, held int) error {
			runtime := runtimeFrom(ctx)
			if runtime == nil {
				return fmt.Errorf("no runtime was given to the crew yet")
			}
			// The crew's own containers let go. The reserve is measured rather than declared, so what
			// the crew keeps back falls with them and the room appears.
			runtime.held = int64(held) * megabyte
			return nil
		})

	sc.Step(`^(\d+) jobs titled "([^"]*)"$`, func(ctx context.Context, count int, title string) error {
		for range count {
			if err := declareJob(ctx, &quaycrewv1.CreateJobRequest{
				Title: title, Brief: "open the bill and say when it is due",
			}); err != nil {
				return err
			}
		}
		return nil
	})

	sc.Step(`^the job is waiting for room, and says which resource ran out$`,
		func(ctx context.Context) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetPhase() != job.PhasePending {
				return fmt.Errorf("the job is %q, want pending: a full machine has room again later",
					one.GetPhase())
			}
			if !namesAResource(one.GetReason()) {
				return fmt.Errorf("the job says %q, and it names no resource", one.GetReason())
			}
			return nil
		})

	sc.Step(`^the job is waiting because there is not enough "([^"]*)"$`,
		func(ctx context.Context, resource string) error {
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			if one.GetPhase() != job.PhasePending {
				return fmt.Errorf("the job is %q, want pending", one.GetPhase())
			}
			if !strings.Contains(one.GetReason(), "not enough "+resource) {
				return fmt.Errorf("the job says %q, want it to name %q", one.GetReason(), resource)
			}
			return nil
		})

	sc.Step(`^the job says nothing about waiting for room$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if namesAResource(one.GetReason()) {
			return fmt.Errorf("a job that is out of the wait still says %q", one.GetReason())
		}
		return nil
	})

	sc.Step(`^(\d+) jobs are waiting for room$`, func(ctx context.Context, want int) error {
		scenario := jobFrom(ctx)
		waiting := 0
		for index := range scenario.declared {
			one, err := readJob(ctx, index)
			if err != nil {
				return err
			}
			if one.GetPhase() == job.PhasePending && namesAResource(one.GetReason()) {
				waiting++
			}
		}
		if waiting != want {
			return fmt.Errorf("%d jobs are waiting for room, want %d", waiting, want)
		}
		return nil
	})
}

// namesAResource says whether this is the crew holding a job back for want of room, which is the one
// thing it writes on a job that is still pending.
func namesAResource(reason string) bool {
	return strings.Contains(reason, "not enough memory") ||
		strings.Contains(reason, "not enough processor")
}
