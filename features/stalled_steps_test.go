package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/capacity"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/cucumber/godog"
)

// Steps for the scenarios about a system that has stopped moving. The fault they close: on
// 31 August 2026 twenty five jobs were declared, fifteen finished, and then five sat held while
// twelve idle sandboxes held the whole machine. Nothing in the system was looking for that state,
// and an operator drained thirty three sessions by hand. See issue 575.
//
// The runtime is the one thing stood in for, the same way the admission scenarios stand it in.
// Everything else is the real system: the controller decides, the ledger counts, the control plane
// builds and closes containers, and the store holds what came of it.

// theStandstill is how many passes a scenario gives the controller before it says the queue is not
// going to move. A pass is a tick, every detached task landed, and another tick, so the number is
// passes and not ticks.
//
// It is a ceiling rather than a wait: the failure these scenarios are about is a system that never
// moves again, so a scenario that looped for ever would report that failure as a hang.
const theStandstill = 60

func initializeStalledSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the controller works until nothing moves$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		was := ""
		for range theStandstill {
			w.server.TickJob(ctx)
			if err := w.settled(ctx); err != nil {
				return err
			}
			w.server.TickJob(ctx)
			now, err := phaseCensus(ctx)
			if err != nil {
				return err
			}
			if now == was {
				return nil
			}
			was = now
		}
		return fmt.Errorf("the queue was still moving after %d passes, and it reads %s",
			theStandstill, was)
	})

	// The plural of the step next door in jobcontroller_steps_test.go. A scenario about a queue lands
	// several tasks at once, and one wait covers all of them: settled waits for every detached task
	// every control plane in this scenario started.
	sc.Step(`^the tasks the controller sent land$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.release != nil {
			w.release()
			w.release = nil
		}
		return w.settled(ctx)
	})

	sc.Step(`^some jobs are waiting for room$`, func(ctx context.Context) error {
		listed, err := everyJob(ctx)
		if err != nil {
			return err
		}
		for _, one := range listed {
			if one.GetPhase() == job.PhasePending && one.GetReason() != "" {
				return nil
			}
		}
		return fmt.Errorf("no job is waiting for room, so this machine had space for all of them")
	})

	sc.Step(`^the machine turned some of those jobs away$`, func(ctx context.Context) error {
		return recordSaying(ctx, job.EventHeld,
			"no job was ever held, so this machine had room for all of them and the scenario is "+
				"not standing in front of the fault")
	})

	sc.Step(`^a container was taken back to make room$`, func(ctx context.Context) error {
		return recordSaying(ctx, job.EventUnstuck,
			"nothing took a container back, so the queue was never stopped or was never started again")
	})

	sc.Step(`^no container was taken back$`, func(ctx context.Context) error {
		records, err := jobRecords(ctx)
		if err != nil {
			return err
		}
		for _, record := range records {
			if record.GetKind() == job.EventUnstuck {
				return fmt.Errorf("a container was taken back: %q", record.GetDetail())
			}
		}
		return nil
	})

	sc.Step(`^every job is done$`, func(ctx context.Context) error {
		listed, err := everyJob(ctx)
		if err != nil {
			return err
		}
		for _, one := range listed {
			if one.GetPhase() != job.PhaseDone {
				return fmt.Errorf("job %q is %q saying %q", one.GetTitle(), one.GetPhase(), one.GetReason())
			}
		}
		return nil
	})

	sc.Step(`^every job waiting for room says the system is stopped rather than busy$`,
		func(ctx context.Context) error {
			listed, err := everyJob(ctx)
			if err != nil {
				return err
			}
			waiting := 0
			for _, one := range listed {
				if one.GetPhase() != job.PhasePending || one.GetReason() == "" {
					continue
				}
				waiting++
				if !strings.Contains(one.GetReason(), "stopped rather than busy") {
					return fmt.Errorf("a job waiting for room says %q, and nothing in it says this "+
						"system is running nothing at all", one.GetReason())
				}
			}
			if waiting == 0 {
				return fmt.Errorf("no job is waiting for room, so this queue never stopped")
			}
			return nil
		})

	sc.Step(`^the job "([^"]*)" says nothing about the system being stopped$`,
		func(ctx context.Context, title string) error {
			listed, err := everyJob(ctx)
			if err != nil {
				return err
			}
			for _, one := range listed {
				if one.GetTitle() != title {
					continue
				}
				if strings.Contains(one.GetReason(), "stopped rather than busy") {
					return fmt.Errorf("%q says %q while a job was running on this machine",
						title, one.GetReason())
				}
				return nil
			}
			return fmt.Errorf("no job here is titled %q", title)
		})

	// Asked of the arithmetic rather than of a listing, because what decides whether the next job
	// starts is the arithmetic. The probe takes nothing: whatever it reserved is given straight back.
	sc.Step(`^the machine has no room left$`, func(ctx context.Context) error {
		if verdict := probeForRoom(ctx); verdict.OK {
			return fmt.Errorf("the machine admitted another sandbox, so it is not full")
		}
		return nil
	})

	sc.Step(`^every sandbox on the machine is idle$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
		if err != nil {
			return err
		}
		holding := 0
		for _, session := range listed.GetSessions() {
			if session.GetStatus() == "running" {
				return fmt.Errorf("session %s is still running a task", session.GetHandle())
			}
			if session.GetStatus() == "idle" {
				holding++
			}
		}
		if holding == 0 {
			return fmt.Errorf("no session holds a container, so nothing is holding this machine")
		}
		return nil
	})

	// The one thing that legitimately stops the system taking a container back. It is set before the
	// containers exist, because a scenario that watched them afterwards would be racing the tick that
	// is about to reclaim one.
	sc.Step(`^an operator is in every container$`, func(ctx context.Context) error {
		worldFrom(ctx).provider.WatchEverything()
		return nil
	})
}

// probeForRoom asks the machine whether one more sandbox of the standard size would fit, and gives
// back whatever the question reserved.
func probeForRoom(ctx context.Context) capacity.Verdict {
	w := worldFrom(ctx)
	const key = "is-there-room/for-one-more"
	verdict := w.server.Admit(ctx, key, capacity.DefaultRequest())
	w.server.Release(key)
	return verdict
}

// everyJob is what this scenario's project holds, read back rather than remembered: what a scenario
// declared says nothing about where those jobs got to.
func everyJob(ctx context.Context) ([]*quaycrewv1.Job, error) {
	w := worldFrom(ctx)
	listed, err := w.client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Project: w.projectID})
	if err != nil {
		return nil, err
	}
	if len(listed.GetJobs()) == 0 {
		return nil, fmt.Errorf("this project holds no job at all")
	}
	return listed.GetJobs(), nil
}

// phaseCensus is what the queue looks like now, as one comparable line, so a caller can tell a pass
// that changed something from a pass that did not.
func phaseCensus(ctx context.Context) (string, error) {
	listed, err := everyJob(ctx)
	if err != nil {
		return "", err
	}
	census := map[string]int{}
	for _, one := range listed {
		census[one.GetPhase()]++
	}
	return fmt.Sprintf("%v", census), nil
}

// recordSaying passes when the log carries a record of this kind for any job in the scenario.
func recordSaying(ctx context.Context, kind, missing string) error {
	records, err := jobRecords(ctx)
	if err != nil {
		return err
	}
	for _, record := range records {
		if record.GetKind() == kind {
			return nil
		}
	}
	return fmt.Errorf("%s. The log carries %v", missing, jobKindsOf(records))
}
