package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/cucumber/godog"
)

// initializeDeployTargetSteps registers the steps for where a project ships.
func initializeDeployTargetSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the project is declared to deploy to account "([^"]*)" in "([^"]*)" as "([^"]*)"$`,
		func(ctx context.Context, account, region, identity string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.SetDeployTarget(ctx, &quaycrewv1.SetDeployTargetRequest{
				Project: w.projectID,
				Target: &quaycrewv1.DeployTarget{
					Account: account, Region: region, Identity: identity,
				},
			})
			return nil
		})

	sc.Step(`^a project that does not exist is declared to deploy to account "([^"]*)" in "([^"]*)" as "([^"]*)"$`,
		func(ctx context.Context, account, region, identity string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.SetDeployTarget(ctx, &quaycrewv1.SetDeployTargetRequest{
				Project: "ghost",
				Target: &quaycrewv1.DeployTarget{
					Account: account, Region: region, Identity: identity,
				},
			})
			return nil
		})

	sc.Step(`^the project is declared to deploy nowhere$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.SetDeployTarget(ctx, &quaycrewv1.SetDeployTargetRequest{Project: w.projectID})
		return nil
	})

	sc.Step(`^the project deploys nowhere$`, func(ctx context.Context) error {
		return deploysNowhere(ctx, worldFrom(ctx).projectID, "the project")
	})

	sc.Step(`^the second project deploys nowhere$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if c.secondProject == "" {
			return fmt.Errorf("no second project was created")
		}
		return deploysNowhere(ctx, c.secondProject, "the second project")
	})

	sc.Step(`^the project deploys to account "([^"]*)" in "([^"]*)"$`,
		func(ctx context.Context, account, region string) error {
			w := worldFrom(ctx)
			if w.lastErr != nil {
				return fmt.Errorf("the target was not declared: %w", w.lastErr)
			}
			resp, err := w.client.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: w.projectID})
			if err != nil {
				return err
			}
			target := resp.GetProject().GetDeployTarget()
			if target.GetAccount() != account || target.GetRegion() != region {
				return fmt.Errorf("the project deploys to %q in %q, want %q in %q",
					target.GetAccount(), target.GetRegion(), account, region)
			}
			return nil
		})

	// The row the record exists for. A target only a fetch of one project can reach is a target
	// nobody reads.
	sc.Step(`^the listing of projects says the project deploys to account "([^"]*)"$`,
		func(ctx context.Context, account string) error {
			w := worldFrom(ctx)
			resp, err := w.client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{Workspace: w.workspaceID})
			if err != nil {
				return err
			}
			for _, project := range resp.GetProjects() {
				if project.GetId() != w.projectID {
					continue
				}
				if got := project.GetDeployTarget().GetAccount(); got != account {
					return fmt.Errorf("the listed project deploys to account %q, want %q", got, account)
				}
				return nil
			}
			return fmt.Errorf("the project is not in the listing at all")
		})

	// Two long numbers that differ in the middle are not something anybody spots by eye, so the
	// refusal has to say both.
	sc.Step(`^the refusal names both accounts$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("nothing was refused")
		}
		for _, want := range []string{"123456789012", "999999999999"} {
			if !strings.Contains(w.lastErr.Error(), want) {
				return fmt.Errorf("the refusal %q does not name %s", w.lastErr.Error(), want)
			}
		}
		return nil
	})
}

// deploysNowhere reads a project back and requires it to carry no target.
func deploysNowhere(ctx context.Context, project, called string) error {
	resp, err := worldFrom(ctx).client.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: project})
	if err != nil {
		return err
	}
	if target := resp.GetProject().GetDeployTarget(); target != nil {
		return fmt.Errorf("%s deploys to account %q in %q, want nowhere",
			called, target.GetAccount(), target.GetRegion())
	}
	return nil
}
