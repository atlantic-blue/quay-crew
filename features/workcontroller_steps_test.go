package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/work"
	"github.com/cucumber/godog"
)

// The controller that makes declared work happen, driven one tick at a time.
//
// A tick rather than a ticker: what is specified is what one pass over the rows does, and waiting
// out a real interval would be slow when it passed and flaky when it did not.

func initializeWorkControllerSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a piece of work titled "([^"]*)" that claims the answer carries "([^"]*)"$`,
		func(ctx context.Context, title, carries string) error {
			return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
				Title: title, Brief: "open the bill and say when it is due", ExpectContains: carries,
			})
		})

	sc.Step(`^a piece of work titled "([^"]*)" after the first$`, func(ctx context.Context, title string) error {
		first, err := firstWork(ctx)
		if err != nil {
			return err
		}
		return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
			Title: title, Brief: "pay it", After: []string{first.GetId()},
		})
	})

	sc.Step(`^a piece of work titled "([^"]*)" in the role "([^"]*)"$`,
		func(ctx context.Context, title, role string) error {
			return declareWork(ctx, &quaycrewv1.CreateWorkRequest{
				Title: title, Brief: "read the open pull requests", Role: role,
			})
		})

	sc.Step(`^the controller ticks$`, func(ctx context.Context) error {
		worldFrom(ctx).server.TickWork(ctx)
		return nil
	})

	sc.Step(`^the controller ticks again$`, func(ctx context.Context) error {
		worldFrom(ctx).server.TickWork(ctx)
		return nil
	})

	sc.Step(`^the controller ticks (\d+) times$`, func(ctx context.Context, times int) error {
		for range times {
			worldFrom(ctx).server.TickWork(ctx)
		}
		return nil
	})

	// The caller's own context is cancelled, which is what a closed terminal does to the call it was
	// holding. What runs afterwards runs for nobody.
	sc.Step(`^the caller goes away and the controller ticks$`, func(ctx context.Context) error {
		calling, hangUp := context.WithCancel(ctx)
		hangUp()
		if _, err := worldFrom(ctx).client.ListWork(calling, &quaycrewv1.ListWorkRequest{}); err == nil {
			return fmt.Errorf("the call the caller hung up on answered anyway, so the caller never went away")
		}
		worldFrom(ctx).server.TickWork(ctx)
		return nil
	})

	sc.Step(`^the task the controller sent lands$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.release != nil {
			w.release()
			w.release = nil
		}
		return w.settled(ctx)
	})

	// A dispatch that lets go answers before the model is called, so the count is waited for rather
	// than read once: reading immediately would pass or fail on how fast the machine is.
	sc.Step(`^the crew was asked to run (\d+) tasks?$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		deadline := time.Now().Add(5 * time.Second)
		for w.runner.count() < want && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if got := w.runner.count(); got != want {
			return fmt.Errorf("the crew was asked to run %d tasks, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^the work is done, and its answer is what the model said$`, func(ctx context.Context) error {
		one, err := readWork(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != work.PhaseDone {
			return fmt.Errorf("the work is %q saying %q, want done", one.GetPhase(), one.GetReason())
		}
		// The double answers with what it was asked, so the answer names the brief the work carried.
		if !strings.Contains(one.GetAnswer(), one.GetBrief()) {
			return fmt.Errorf("the answer is %q, want what the model said about %q", one.GetAnswer(), one.GetBrief())
		}
		return nil
	})

	sc.Step(`^the work says which session did it$`, func(ctx context.Context) error {
		one, err := readWork(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetSession() == "" {
			return fmt.Errorf("the work does not say which session did it")
		}
		// And that session is one the crew holds, so a person can open the conversation.
		if _, err := worldFrom(ctx).client.GetSession(ctx, &quaycrewv1.GetSessionRequest{
			Id: one.GetSession(),
		}); err != nil {
			return fmt.Errorf("the session on the work is not one the crew holds: %w", err)
		}
		return nil
	})

	sc.Step(`^the work carries the moment it started and the moment it finished$`, func(ctx context.Context) error {
		one, err := readWork(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetStartedAt() == nil || one.GetFinishedAt() == nil {
			return fmt.Errorf("the work started at %v and finished at %v", one.GetStartedAt(), one.GetFinishedAt())
		}
		return nil
	})

	// Counted on the record rather than on the model, because a model that has not answered yet has
	// still been asked, and asking twice is paying twice.
	sc.Step(`^one task is recorded against that work$`, func(ctx context.Context) error {
		one, err := readWork(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetSession() == "" {
			return fmt.Errorf("the work says no session, so nothing was asked of the crew")
		}
		tasks, err := worldFrom(ctx).client.ListTasks(ctx, &quaycrewv1.ListTasksRequest{Session: one.GetSession()})
		if err != nil {
			return err
		}
		if len(tasks.GetTasks()) != 1 {
			return fmt.Errorf("%d tasks are recorded against the work, want 1", len(tasks.GetTasks()))
		}
		return nil
	})

	sc.Step(`^the work is running$`, func(ctx context.Context) error {
		return workIs(ctx, 0, work.PhaseRunning)
	})

	sc.Step(`^the work titled "([^"]*)" is pending$`, func(ctx context.Context, title string) error {
		one, err := workTitled(ctx, title)
		if err != nil {
			return err
		}
		if one.GetPhase() != work.PhasePending {
			return fmt.Errorf("%q is %q, want pending", title, one.GetPhase())
		}
		return nil
	})

	sc.Step(`^the work is failed, and the reason says what the model said$`, func(ctx context.Context) error {
		one, err := readWork(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != work.PhaseFailed {
			return fmt.Errorf("the work is %q, want failed", one.GetPhase())
		}
		if one.GetReason() == "" {
			return fmt.Errorf("the work failed and says nothing about why")
		}
		return nil
	})

	sc.Step(`^the work is stopped, and the reason names what was claimed$`, func(ctx context.Context) error {
		one, err := readWork(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != work.PhaseStopped {
			return fmt.Errorf("the work is %q, want stopped", one.GetPhase())
		}
		if !strings.Contains(one.GetReason(), one.GetExpectContains()) {
			return fmt.Errorf("the reason is %q, want it to name what the work claimed", one.GetReason())
		}
		return nil
	})

	sc.Step(`^the answer is still on the record$`, func(ctx context.Context) error {
		one, err := readWork(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetAnswer() == "" {
			return fmt.Errorf("the work carries no answer, and what the model said is how somebody works out why the claim failed")
		}
		return nil
	})

	sc.Step(`^the records for that work read "([^"]*)", "([^"]*)", "([^"]*)"$`,
		func(ctx context.Context, first, second, third string) error {
			one, err := readWork(ctx, 0)
			if err != nil {
				return err
			}
			events, err := worldFrom(ctx).store.ListWorkEvents(ctx, one.GetId())
			if err != nil {
				return err
			}
			got := eventKinds(events)
			if strings.Join(got, ",") != strings.Join([]string{first, second, third}, ",") {
				return fmt.Errorf("the records read %v, want %v", got, []string{first, second, third})
			}
			return nil
		})
}

// readWork reads one of the scenario's pieces of work back off the crew, so an assertion is about
// what the crew holds rather than about what a call answered.
func readWork(ctx context.Context, which int) (*quaycrewv1.Work, error) {
	scenario := workFrom(ctx)
	if len(scenario.declared) <= which {
		return nil, fmt.Errorf("%d pieces of work were declared in this scenario", len(scenario.declared))
	}
	found, err := worldFrom(ctx).client.GetWork(ctx, &quaycrewv1.GetWorkRequest{
		Id: scenario.declared[which].GetId(),
	})
	if err != nil {
		return nil, err
	}
	return found.GetWork(), nil
}

func workIs(ctx context.Context, which int, phase string) error {
	one, err := readWork(ctx, which)
	if err != nil {
		return err
	}
	if one.GetPhase() != phase {
		return fmt.Errorf("the work is %q saying %q, want %q", one.GetPhase(), one.GetReason(), phase)
	}
	return nil
}

func workTitled(ctx context.Context, title string) (*quaycrewv1.Work, error) {
	scenario := workFrom(ctx)
	for i, declared := range scenario.declared {
		if declared.GetTitle() == title {
			return readWork(ctx, i)
		}
	}
	return nil, fmt.Errorf("this scenario declared no work titled %q", title)
}
