package features_test

import (
	"context"
	"errors"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/workspace"
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

// initializeAddressSteps registers the steps for addressing the crew by path.
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
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		return resolveAddress(ctx, fmt.Sprintf("%s/%s/%s", w.workspaceName, w.projectName, current.handle[:8]))
	})

	sc.Step(`^the session is called "([^"]*)"$`, func(ctx context.Context, label string) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		_, err = w.client.SetSessionLabel(ctx, &quaycrewv1.SetSessionLabelRequest{
			Id: current.sessionID, Label: label,
		})
		return err
	})

	// Read off the listing rather than off the session, because the whole point is that what the
	// operator has on screen is what the address takes. A step that read the handle from the crew
	// would pass against a listing that prints something else entirely.
	sc.Step(`^the operator addresses the session by the identifier the listing prints$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		cells, err := listedCells(ctx, w)
		if err != nil {
			return err
		}
		return resolveAddress(ctx, fmt.Sprintf("%s/%s/%s", w.workspaceName, w.projectName, cells[0]))
	})

	sc.Step(`^the operator addresses the session by its own id$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		return resolveAddress(ctx, fmt.Sprintf("%s/%s/%s",
			w.workspaceName, w.projectName, display.ShortID(current.sessionID)))
	})

	sc.Step(`^the listing still says what the session is called$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		cells, err := listedCells(ctx, w)
		if err != nil {
			return err
		}
		const nameColumn = 3
		if cells[nameColumn] != "the electricity bill" {
			return fmt.Errorf("the listing calls it %q, want the name the operator gave it", cells[nameColumn])
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
			return fmt.Errorf("it reached project %q, want none: a workspace is not somewhere a task runs", a.located.ProjectID)
		}
		return nil
	})

	sc.Step(`^the address reaches that session$`, func(ctx context.Context) error {
		w, a := worldFrom(ctx), addressFrom(ctx)
		if a.err != nil {
			return fmt.Errorf("the address did not resolve: %w", a.err)
		}
		current, err := w.lastTask()
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

// listedCells is the crew's one session as a listing draws it, which is the row the operator reads
// and copies from.
func listedCells(ctx context.Context, w *world) ([]string, error) {
	listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
	if err != nil {
		return nil, err
	}
	if len(listed.GetSessions()) != 1 {
		return nil, fmt.Errorf("the crew has %d sessions, so there is no single row to read",
			len(listed.GetSessions()))
	}
	return display.SessionCells(listed.GetSessions()[0], w.workspaceName, w.projectName), nil
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
