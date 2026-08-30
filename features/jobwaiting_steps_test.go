package features_test

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// A job cannot wait, so a brief that asks it to is refused where it is written.
//
// The three scenarios are one behaviour. The rule reads English, so what it leaves alone matters as
// much as what it stops: a refusal that fires on ordinary work is the rule everybody words around.

func initializeJobWaitingSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the caller declares a job briefed to "([^"]*)"$`, func(ctx context.Context, brief string) error {
		return declareJob(ctx, &quaycrewv1.CreateJobRequest{
			Title: "land the defect fix", Brief: brief,
		})
	})

	sc.Step(`^the crew refuses it and says a job cannot wait, and names the flow$`,
		func(ctx context.Context) error {
			for _, phrase := range []string{"cannot wait", "flow", "quay flow import"} {
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
