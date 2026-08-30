package features_test

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// A job cannot wait, so a brief that asks it to is refused where a caller declares it.
//
// The scenarios are one behaviour. The rule reads English, so what it leaves alone matters as much
// as what it stops: a refusal that fires on ordinary work is the rule everybody words around.

func initializeJobWaitingSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the caller declares a job briefed to "([^"]*)"$`, func(ctx context.Context, brief string) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "land the defect fix", Brief: brief,
		})
	})

	sc.Step(`^the system refuses it and says a job cannot wait, and names the flow$`,
		func(ctx context.Context) error {
			for _, phrase := range []string{"cannot wait", "flow", "krewe flow import"} {
				if err := theRefusalSays(phrase)(ctx); err != nil {
					return err
				}
			}
			return nil
		})

	sc.Step(`^the job is declared$`, func(ctx context.Context) error {
		if err := worldFrom(ctx).lastErr; err != nil {
			return fmt.Errorf("the declaration was refused: %v", err)
		}
		_, err := lastJob(ctx)
		return err
	})
}

// initializeFlowStepSteps says what a run's steps were asked.
//
// The rule is on what a caller declared and not on a step, so the one thing to prove about a flow is
// that the node after its wait really did merge.
func initializeFlowStepSteps(sc *godog.ScenarioContext) {
	sc.Step(`^one of the run's steps was asked "([^"]*)"$`, func(ctx context.Context, want string) error {
		tasks, err := flowRunTasks(ctx, worldFrom(ctx))
		if err != nil {
			return err
		}
		asked := make([]string, 0, len(tasks))
		for _, task := range tasks {
			if task.GetPrompt() == want {
				return nil
			}
			asked = append(asked, task.GetPrompt())
		}
		return fmt.Errorf("the run's steps were asked %q, and none was asked %q", asked, want)
	})
}
