package features_test

import (
	"context"
	"errors"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/workspace"
	"github.com/cucumber/godog"
)

// addressWorld is one scenario's addressing state, kept beside the shared world.
type addressWorld struct {
	located workspace.Location
	err     error
}

type addressKey struct{}

func addressFrom(ctx context.Context) *addressWorld {
	a, _ := ctx.Value(addressKey{}).(*addressWorld)
	return a
}

// initializeAddressSteps registers the steps for addressing the system by path.
func initializeAddressSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, addressKey{}, &addressWorld{}), nil
	})

	sc.Step(`^a project named "([^"]*)" in the second workspace$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		_, err := w.client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
			Workspace: w.secondWorkspaceID, Name: name,
		})
		return err
	})

	sc.Step(`^the operator addresses "([^"]*)"$`, func(ctx context.Context, address string) error {
		return resolveAddress(ctx, address)
	})

	sc.Step(`^the operator addresses the session by its first eight characters$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastExec()
		if err != nil {
			return err
		}
		return resolveAddress(ctx, fmt.Sprintf("%s/%s/%s", w.workspaceName, w.projectName, current.handle[:8]))
	})

	// The id, taken off the listing the way the operator takes it: shortened to what the column
	// shows, and pasted back into an address.
	sc.Step(`^the operator addresses the session by the id in the listing$`, func(ctx context.Context) error {
		typed, err := addressAtTheListedID(ctx)
		if err != nil {
			return err
		}
		return resolveAddress(ctx, typed)
	})

	sc.Step(`^the operator labels the session "([^"]*)"$`, func(ctx context.Context, label string) error {
		w := worldFrom(ctx)
		current, err := w.lastExec()
		if err != nil {
			return err
		}
		_, err = w.client.SetSessionLabel(ctx, &quaycrewv1.SetSessionLabelRequest{
			Id: current.sessionID, Label: label,
		})
		return err
	})

	// The next thing the operator does with an address, rather than the resolving on its own: a
	// resolver that answers and a dispatch that lands are two different claims.
	sc.Step(`^the operator dispatches "([^"]*)" to the session at the id in the listing$`,
		func(ctx context.Context, text string) error {
			w := worldFrom(ctx)
			typed, err := addressAtTheListedID(ctx)
			if err != nil {
				return err
			}
			path, err := workspace.ParsePath(typed)
			if err != nil {
				return err
			}
			located, err := workspace.ResolvePath(ctx, w.client, path)
			if err != nil {
				return fmt.Errorf("the address %q was refused: %w", typed, err)
			}
			return w.dispatch(ctx, located.ProjectID, located.SessionID, text)
		})

	sc.Step(`^the refusal offers the identifier the listing prints$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), addressFrom(ctx)
		if a.err == nil {
			return fmt.Errorf("the address resolved, expected a refusal")
		}
		current, err := w.lastExec()
		if err != nil {
			return err
		}
		said := a.err.Error()
		if !strings.Contains(said, current.sessionID[:8]) {
			return fmt.Errorf("the refusal is %q, want it to offer %q", said, current.sessionID[:8])
		}
		// And not the handle, which no column of the listing carries. Offering it sent the operator
		// looking for a value they cannot see.
		if strings.Contains(said, current.handle[:8]) {
			return fmt.Errorf("the refusal is %q, and it offers the handle %q, which is nowhere on the screen",
				said, current.handle[:8])
		}
		return nil
	})

	sc.Step(`^the address reaches the project$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), addressFrom(ctx)
		if a.err != nil {
			return fmt.Errorf("the address did not resolve: %w", a.err)
		}
		if a.located.WorkspaceID != w.workspaceID {
			return fmt.Errorf("it reached workspace %q, want %q", a.located.WorkspaceID, w.workspaceID)
		}
		if a.located.ProjectID != w.projectID {
			return fmt.Errorf("it reached project %q, want %q", a.located.ProjectID, w.projectID)
		}
		return nil
	})

	sc.Step(`^the address reaches the workspace but no project$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), addressFrom(ctx)
		if a.err != nil {
			return fmt.Errorf("the address did not resolve: %w", a.err)
		}
		if a.located.WorkspaceID != w.workspaceID {
			return fmt.Errorf("it reached workspace %q, want %q", a.located.WorkspaceID, w.workspaceID)
		}
		if a.located.HasProject() {
			return fmt.Errorf("it reached project %q, want none: a workspace is not somewhere an exec runs", a.located.ProjectID)
		}
		return nil
	})

	sc.Step(`^the address reaches that session$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), addressFrom(ctx)
		if a.err != nil {
			return fmt.Errorf("the address did not resolve: %w", a.err)
		}
		current, err := w.lastExec()
		if err != nil {
			return err
		}
		if a.located.SessionID != current.handle {
			return fmt.Errorf("it reached session %q, want %q", a.located.SessionID, current.handle)
		}
		return nil
	})

	sc.Step(`^the address is refused as not found$`, func(ctx context.Context) error {
		a := addressFrom(ctx)
		if a.err == nil {
			return fmt.Errorf("the address resolved to %+v, expected a refusal", a.located)
		}
		if !errors.Is(a.err, workspace.ErrNotFound) {
			return fmt.Errorf("refused with %v, want not found", a.err)
		}
		return nil
	})

	sc.Step(`^the address is refused as malformed$`, func(ctx context.Context) error {
		a := addressFrom(ctx)
		if a.err == nil {
			return fmt.Errorf("the address resolved to %+v, expected a refusal", a.located)
		}
		// Malformed is refused before anything is looked up, so it is not a not found.
		if errors.Is(a.err, workspace.ErrNotFound) {
			return fmt.Errorf("refused as not found, want it refused for its shape: %v", a.err)
		}
		return nil
	})
}

// addressAtTheListedID is the session written the way the operator reads it off the listing: the
// workspace, the project, and the shortened id from the first column.
func addressAtTheListedID(ctx context.Context) (string, error) {
	w := worldFrom(ctx)
	current, err := w.lastExec()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s/%s", w.workspaceName, w.projectName, current.sessionID[:8]), nil
}

// resolveAddress parses and resolves an address, recording whichever of the two failed.
func resolveAddress(ctx context.Context, address string) error {
	w, a := worldFrom(ctx), addressFrom(ctx)
	path, err := workspace.ParsePath(address)
	if err != nil {
		a.err = err
		return nil
	}
	a.located, a.err = workspace.ResolvePath(ctx, w.client, path)
	return nil
}
