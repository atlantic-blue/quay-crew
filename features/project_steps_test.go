package features_test

import (
	"context"
	"errors"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/workspace"
	"github.com/cucumber/godog"
)

// projectRefWorld is the state of one project reference resolution, kept beside the shared world.
type projectRefWorld struct {
	resolved string
	err      error
}

type projectRefKey struct{}

func projectRefFrom(ctx context.Context) *projectRefWorld {
	p, _ := ctx.Value(projectRefKey{}).(*projectRefWorld)
	return p
}

// initializeProjectSteps registers the steps for the project level.
func initializeProjectSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, projectRefKey{}, &projectRefWorld{}), nil
	})

	sc.Step(`^the operator creates a project named "([^"]*)"$`, func(ctx context.Context, name string) error {
		return worldFrom(ctx).createProject(ctx, name)
	})

	sc.Step(`^the operator creates a project named "([^"]*)" in workspace "([^"]*)"$`,
		func(ctx context.Context, name, target string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Workspace: target, Name: name})
			return nil
		})

	sc.Step(`^the project belongs to the workspace$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.lastErr != nil {
			return fmt.Errorf("the project was not created: %w", w.lastErr)
		}
		resp, err := w.client.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: w.projectID})
		if err != nil {
			return err
		}
		if got := resp.GetProject().GetWorkspace(); got != w.workspaceID {
			return fmt.Errorf("the project belongs to %q, want the workspace %q", got, w.workspaceID)
		}
		return nil
	})

	sc.Step(`^the workspace has (\d+) projects?$`, func(ctx context.Context, want int) error {
		w := worldFrom(ctx)
		resp, err := w.client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{Workspace: w.workspaceID})
		if err != nil {
			return err
		}
		if got := len(resp.GetProjects()); got != want {
			return fmt.Errorf("the workspace has %d projects, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^the operator refers to the project as "([^"]*)"$`, func(ctx context.Context, reference string) error {
		w, p := worldFrom(ctx), projectRefFrom(ctx)
		p.resolved, p.err = workspace.ResolveProject(ctx, w.client, w.workspaceName, reference)
		return nil
	})

	sc.Step(`^the project reference resolves to the project$`, func(ctx context.Context) error {
		w, p := worldFrom(ctx), projectRefFrom(ctx)
		if p.err != nil {
			return fmt.Errorf("the reference did not resolve: %w", p.err)
		}
		if p.resolved != w.projectID {
			return fmt.Errorf("resolved to %q, want the project %q", p.resolved, w.projectID)
		}
		return nil
	})

	sc.Step(`^the project reference is refused as not found$`, func(ctx context.Context) error {
		p := projectRefFrom(ctx)
		if p.err == nil {
			return fmt.Errorf("the reference resolved to %q, expected a refusal", p.resolved)
		}
		if !errors.Is(p.err, workspace.ErrNotFound) {
			return fmt.Errorf("refused with %v, want not found", p.err)
		}
		return nil
	})
}
