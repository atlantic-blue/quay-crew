package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/role"
	"github.com/cucumber/godog"
)

// The roles a crew holds, driven over the control plane's real interface. What a session running as
// a role receives is a different question, and no session runs as one yet.
func initializeRoleSteps(sc *godog.ScenarioContext) {
	importRole := func(ctx context.Context, files []*quaycrewv1.RoleFile) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: files})
		return nil
	}

	sc.Step(`^the operator imports the "([^"]*)" role$`, func(ctx context.Context, name string) error {
		return importRole(ctx, roleFiles(name, 1, roleManifest{
			model: "opus", receives: []string{"work", "context"},
		}))
	})

	sc.Step(`^the operator imported the "([^"]*)" role$`, func(ctx context.Context, name string) error {
		if err := importRole(ctx, roleFiles(name, 1, roleManifest{
			model: "opus", receives: []string{"work", "context"},
		})); err != nil {
			return err
		}
		return worldFrom(ctx).lastErr
	})

	sc.Step(`^the operator imports version (\d+) of the "([^"]*)" role$`,
		func(ctx context.Context, version int, name string) error {
			return importRole(ctx, roleFiles(name, version, roleManifest{
				model: "opus", receives: []string{"work", "context"},
			}))
		})

	// The same name and version carrying a different role, which is the import that has to be
	// refused: a workspace pins a version, so changing one underneath it changes how a session
	// already running as it was told to work.
	sc.Step(`^the operator imports a different "([^"]*)" role at the same version$`,
		func(ctx context.Context, name string) error {
			return importRole(ctx, roleFiles(name, 1, roleManifest{
				model: "opus", receives: []string{"work"}, brief: "Write the code instead.",
			}))
		})

	sc.Step(`^the operator imports a role receiving "([^"]*)"$`,
		func(ctx context.Context, material string) error {
			return importRole(ctx, roleFiles("test-writer", 1, roleManifest{
				model: "opus", receives: []string{"work", material},
			}))
		})

	sc.Step(`^the operator imports a role naming no model$`, func(ctx context.Context) error {
		return importRole(ctx, roleFiles("test-writer", 1, roleManifest{
			receives: []string{"work"},
		}))
	})

	sc.Step(`^the operator imports a role declaring nothing it receives$`, func(ctx context.Context) error {
		return importRole(ctx, roleFiles("test-writer", 1, roleManifest{model: "opus"}))
	})

	sc.Step(`^the operator imports a role with no version$`, func(ctx context.Context) error {
		return importRole(ctx, roleFiles("test-writer", 0, roleManifest{
			model: "opus", receives: []string{"work"},
		}))
	})

	sc.Step(`^the operator (?:attaches|attached) the "([^"]*)" role to the workspace$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
				Workspace: w.workspaceID, Name: name,
			})
			return nil
		})

	sc.Step(`^the operator (?:attaches|attached) the "([^"]*)" role to the crew$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
				Scope: "crew", Name: name,
			})
			return w.lastErr
		})

	sc.Step(`^the operator detaches the "([^"]*)" role from the workspace$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.DetachRole(ctx, &quaycrewv1.DetachRoleRequest{
				Workspace: w.workspaceID, Name: name,
			})
			return w.lastErr
		})

	sc.Step(`^the operator detaches the "([^"]*)" role from the crew$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.DetachRole(ctx, &quaycrewv1.DetachRoleRequest{
				Scope: "crew", Name: name,
			})
			return w.lastErr
		})

	sc.Step(`^the operator lists the crew's roles$`, func(ctx context.Context) error {
		return listRoles(ctx, "")
	})

	sc.Step(`^the operator lists the workspace's roles$`, func(ctx context.Context) error {
		return listRoles(ctx, worldFrom(ctx).workspaceID)
	})

	sc.Step(`^the crew holds no roles$`, func(ctx context.Context) error {
		return rolesHeld(ctx, "", nil)
	})

	sc.Step(`^the crew holds the "([^"]*)" role$`, func(ctx context.Context, name string) error {
		return rolesHeld(ctx, "", []string{name})
	})

	sc.Step(`^the workspace holds no roles$`, func(ctx context.Context) error {
		return rolesHeld(ctx, worldFrom(ctx).workspaceID, nil)
	})

	sc.Step(`^the workspace holds the "([^"]*)" role$`, func(ctx context.Context, name string) error {
		return rolesHeld(ctx, worldFrom(ctx).workspaceID, []string{name})
	})

	sc.Step(`^the second workspace holds no roles$`, func(ctx context.Context) error {
		return rolesHeld(ctx, worldFrom(ctx).secondWorkspaceID, nil)
	})

	sc.Step(`^the second workspace holds the "([^"]*)" role$`, func(ctx context.Context, name string) error {
		return rolesHeld(ctx, worldFrom(ctx).secondWorkspaceID, []string{name})
	})

	sc.Step(`^the workspace (?:still holds|holds) version (\d+) of the "([^"]*)" role$`,
		func(ctx context.Context, version int, name string) error {
			held, err := roleNamed(ctx, worldFrom(ctx).workspaceID, name)
			if err != nil {
				return err
			}
			if got := int(held.GetVersion()); got != version {
				return fmt.Errorf("the workspace holds version %d of %s, want %d", got, name, version)
			}
			return nil
		})

	sc.Step(`^the listing says the "([^"]*)" role runs on "([^"]*)"$`,
		func(ctx context.Context, name, model string) error {
			held, err := roleNamed(ctx, "", name)
			if err != nil {
				return err
			}
			if held.GetModel() != model {
				return fmt.Errorf("the %s role runs on %q, want %q", name, held.GetModel(), model)
			}
			return nil
		})

	sc.Step(`^the listing says the "([^"]*)" role receives "([^"]*)"$`,
		func(ctx context.Context, name, receives string) error {
			held, err := roleNamed(ctx, "", name)
			if err != nil {
				return err
			}
			if got := strings.Join(held.GetReceives(), ", "); got != receives {
				return fmt.Errorf("the %s role receives %q, want %q", name, got, receives)
			}
			return nil
		})

	sc.Step(`^the listing says the "([^"]*)" role is held by the crew$`,
		func(ctx context.Context, name string) error {
			held, err := roleNamed(ctx, worldFrom(ctx).workspaceID, name)
			if err != nil {
				return err
			}
			if !held.GetCrew() {
				return fmt.Errorf("the listing does not say the %s role came from the crew", name)
			}
			return nil
		})

	sc.Step(`^the crew refuses the role saying "([^"]*)"$`, func(ctx context.Context, said string) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the crew accepted it, and it should have been refused")
		}
		if !strings.Contains(w.lastErr.Error(), said) {
			return fmt.Errorf("the refusal does not say %q: %v", said, w.lastErr)
		}
		return nil
	})
}

// roleManifest is what a scenario wants a role's manifest to say, so a step can leave one field out
// and say nothing about the rest.
type roleManifest struct {
	model    string
	receives []string
	brief    string
}

// roleFiles builds the two files an import carries. A version of zero writes no version line, which
// is how a scenario says the manifest is missing it.
func roleFiles(name string, version int, said roleManifest) []*quaycrewv1.RoleFile {
	var manifest strings.Builder
	fmt.Fprintf(&manifest, "name: %s\n", name)
	if version > 0 {
		fmt.Fprintf(&manifest, "version: %d\n", version)
	}
	manifest.WriteString("summary: writes the tests for a piece of work, from the work alone\n")
	if said.model != "" {
		fmt.Fprintf(&manifest, "model: %s\n", said.model)
	}
	if len(said.receives) > 0 {
		manifest.WriteString("receives:\n")
		for _, one := range said.receives {
			fmt.Fprintf(&manifest, "  - %s\n", one)
		}
	}
	brief := said.brief
	if brief == "" {
		brief = "Write the tests. Do not write the code."
	}
	return []*quaycrewv1.RoleFile{
		{Path: role.ManifestFile, Body: []byte(manifest.String())},
		{Path: role.BriefFile, Body: []byte(brief)},
	}
}

func listRoles(ctx context.Context, workspace string) error {
	w := worldFrom(ctx)
	listed, err := w.client.ListRoles(ctx, &quaycrewv1.ListRolesRequest{Workspace: workspace})
	w.lastErr = err
	if err != nil {
		return err
	}
	w.lastRoles = listed
	return nil
}

// rolesHeld reads a level's listing and says exactly which roles it holds, so a scenario that means
// "none" cannot pass against a listing holding one.
func rolesHeld(ctx context.Context, workspace string, want []string) error {
	if err := listRoles(ctx, workspace); err != nil {
		return err
	}
	var got []string
	for _, one := range worldFrom(ctx).lastRoles.GetRoles() {
		got = append(got, one.GetName())
	}
	if len(got) != len(want) {
		return fmt.Errorf("it holds %v, want %v", got, want)
	}
	for at, name := range want {
		if got[at] != name {
			return fmt.Errorf("it holds %v, want %v", got, want)
		}
	}
	return nil
}

func roleNamed(ctx context.Context, workspace, name string) (*quaycrewv1.Role, error) {
	if err := listRoles(ctx, workspace); err != nil {
		return nil, err
	}
	for _, one := range worldFrom(ctx).lastRoles.GetRoles() {
		if one.GetName() == name {
			return one, nil
		}
	}
	return nil, fmt.Errorf("the listing does not hold the %s role", name)
}
