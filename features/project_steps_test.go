package features_test

import (
	"context"
	"errors"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/project"
	"github.com/cucumber/godog"
)

// projectRefWorld is the state of one reference resolution, kept beside the shared world so these
// scenarios do not widen what every other scenario carries.
type projectRefWorld struct {
	resolved string
	err      error
}

type projectRefKey struct{}

func projectRefFrom(ctx context.Context) *projectRefWorld {
	p, _ := ctx.Value(projectRefKey{}).(*projectRefWorld)
	return p
}

// initializeProjectSteps registers the steps for addressing a project by name or by id. Called from
// initializeScenario so these keep to their own file.
func initializeProjectSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, projectRefKey{}, &projectRefWorld{}), nil
	})

	sc.Step(`^a second project named after the first project's id$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.projectID == "" {
			return fmt.Errorf("there is no first project to copy the id of")
		}
		resp, err := w.client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Name: w.projectID})
		if err != nil {
			return err
		}
		w.secondProjectID = resp.GetProject().GetId()
		return nil
	})

	sc.Step(`^the operator refers to the project as "([^"]*)"$`, func(ctx context.Context, reference string) error {
		w, p := worldFrom(ctx), projectRefFrom(ctx)
		p.resolved, p.err = project.Resolve(ctx, w.client, reference)
		return nil
	})

	sc.Step(`^the operator refers to the project by its id$`, func(ctx context.Context) error {
		w, p := worldFrom(ctx), projectRefFrom(ctx)
		p.resolved, p.err = project.Resolve(ctx, w.client, w.projectID)
		return nil
	})

	sc.Step(`^the reference resolves to the project$`, func(ctx context.Context) error {
		w, p := worldFrom(ctx), projectRefFrom(ctx)
		if p.err != nil {
			return fmt.Errorf("the reference did not resolve: %w", p.err)
		}
		if p.resolved != w.projectID {
			return fmt.Errorf("resolved to %q, want the project %q", p.resolved, w.projectID)
		}
		return nil
	})

	sc.Step(`^the reference is refused as not found$`, func(ctx context.Context) error {
		p := projectRefFrom(ctx)
		if p.err == nil {
			return fmt.Errorf("the reference resolved to %q, expected a refusal", p.resolved)
		}
		if !errors.Is(p.err, project.ErrNotFound) {
			return fmt.Errorf("refused with %v, want not found", p.err)
		}
		return nil
	})

	sc.Step(`^the reference is refused as ambiguous$`, func(ctx context.Context) error {
		p := projectRefFrom(ctx)
		if p.err == nil {
			return fmt.Errorf("the reference resolved to %q, expected a refusal", p.resolved)
		}
		var ambiguous *project.AmbiguousError
		if !errors.As(p.err, &ambiguous) {
			return fmt.Errorf("refused with %v, want an ambiguous reference", p.err)
		}
		return nil
	})

	sc.Step(`^the refusal names both projects$`, func(ctx context.Context) error {
		w, p := worldFrom(ctx), projectRefFrom(ctx)
		var ambiguous *project.AmbiguousError
		if !errors.As(p.err, &ambiguous) {
			return fmt.Errorf("the refusal was %v, not an ambiguous reference", p.err)
		}
		// The operator has to be able to act on the message, so both candidates must be in it.
		for _, id := range []string{w.projectID, w.secondProjectID} {
			if id == "" {
				return fmt.Errorf("a project was not created, so the refusal cannot be checked")
			}
			if !strings.Contains(ambiguous.Error(), id) {
				return fmt.Errorf("the refusal %q does not name project %q", ambiguous.Error(), id)
			}
		}
		return nil
	})
}
