package features_test

import (
	"context"
	"errors"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/workspace"
	"github.com/cucumber/godog"
)

// workspaceRefWorld is the state of one reference resolution, kept beside the shared world so these
// scenarios do not widen what every other scenario carries.
type workspaceRefWorld struct {
	resolved string
	err      error
}

type workspaceRefKey struct{}

func workspaceRefFrom(ctx context.Context) *workspaceRefWorld {
	p, _ := ctx.Value(workspaceRefKey{}).(*workspaceRefWorld)
	return p
}

// initializeWorkspaceSteps registers the steps for addressing a workspace by name or by id. Called from
// initializeScenario so these keep to their own file.
func initializeWorkspaceSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator asks which secrets the workspace has$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		resp, err := w.client.ListSecrets(ctx, &quaycrewv1.ListSecretsRequest{Workspace: w.workspaceID})
		if err != nil {
			return err
		}
		w.lastSecrets = resp
		return nil
	})

	sc.Step(`^it names "([^"]*)"$`, func(ctx context.Context, want string) error {
		w := worldFrom(ctx)
		for _, secret := range w.lastSecrets.GetSecrets() {
			if secret.GetName() == want {
				return nil
			}
		}
		return fmt.Errorf("the listing does not name %q: %v", want, w.lastSecrets.GetSecrets())
	})

	sc.Step(`^it names nothing$`, func(ctx context.Context) error {
		if got := len(worldFrom(ctx).lastSecrets.GetSecrets()); got != 0 {
			return fmt.Errorf("%d secrets listed, want none", got)
		}
		return nil
	})

	sc.Step(`^the answer carries no value$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		// Whatever the workspace's token is, it must not appear anywhere in the answer.
		token, err := w.secrets.Get(ctx, w.workspaceID, "CLAUDE_CODE_OAUTH_TOKEN")
		if err == nil && token != "" && strings.Contains(w.lastSecrets.String(), token) {
			return fmt.Errorf("the listing leaks a value")
		}
		return nil
	})

	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, workspaceRefKey{}, &workspaceRefWorld{}), nil
	})

	sc.Step(`^a second workspace named after the first workspace's id$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		if w.workspaceID == "" {
			return fmt.Errorf("there is no first workspace to copy the id of")
		}
		resp, err := w.client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: w.workspaceID})
		if err != nil {
			return err
		}
		w.secondWorkspaceID = resp.GetWorkspace().GetId()
		return nil
	})

	sc.Step(`^the operator refers to the workspace as "([^"]*)"$`, func(ctx context.Context, reference string) error {
		w, p := worldFrom(ctx), workspaceRefFrom(ctx)
		p.resolved, p.err = workspace.Resolve(ctx, w.client, reference)
		return nil
	})

	sc.Step(`^the operator refers to the workspace by its id$`, func(ctx context.Context) error {
		w, p := worldFrom(ctx), workspaceRefFrom(ctx)
		p.resolved, p.err = workspace.Resolve(ctx, w.client, w.workspaceID)
		return nil
	})

	sc.Step(`^the reference resolves to the workspace$`, func(ctx context.Context) error {
		w, p := worldFrom(ctx), workspaceRefFrom(ctx)
		if p.err != nil {
			return fmt.Errorf("the reference did not resolve: %w", p.err)
		}
		if p.resolved != w.workspaceID {
			return fmt.Errorf("resolved to %q, want the workspace %q", p.resolved, w.workspaceID)
		}
		return nil
	})

	sc.Step(`^the reference is refused as not found$`, func(ctx context.Context) error {
		p := workspaceRefFrom(ctx)
		if p.err == nil {
			return fmt.Errorf("the reference resolved to %q, expected a refusal", p.resolved)
		}
		if !errors.Is(p.err, workspace.ErrNotFound) {
			return fmt.Errorf("refused with %v, want not found", p.err)
		}
		return nil
	})

	sc.Step(`^the reference is refused as ambiguous$`, func(ctx context.Context) error {
		p := workspaceRefFrom(ctx)
		if p.err == nil {
			return fmt.Errorf("the reference resolved to %q, expected a refusal", p.resolved)
		}
		var ambiguous *workspace.AmbiguousError
		if !errors.As(p.err, &ambiguous) {
			return fmt.Errorf("refused with %v, want an ambiguous reference", p.err)
		}
		return nil
	})

	sc.Step(`^the refusal names both workspaces$`, func(ctx context.Context) error {
		w, p := worldFrom(ctx), workspaceRefFrom(ctx)
		var ambiguous *workspace.AmbiguousError
		if !errors.As(p.err, &ambiguous) {
			return fmt.Errorf("the refusal was %v, not an ambiguous reference", p.err)
		}
		// The operator has to be able to act on the message, so both candidates must be in it.
		for _, id := range []string{w.workspaceID, w.secondWorkspaceID} {
			if id == "" {
				return fmt.Errorf("a workspace was not created, so the refusal cannot be checked")
			}
			if !strings.Contains(ambiguous.Error(), id) {
				return fmt.Errorf("the refusal %q does not name workspace %q", ambiguous.Error(), id)
			}
		}
		return nil
	})
}
