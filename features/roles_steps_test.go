package features_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/role"
	"github.com/cucumber/godog"
)

// The roles a system holds, driven over the control plane's real interface. What a session running as
// a role receives is a different question, and no session runs as one yet.
func initializeRoleSteps(sc *godog.ScenarioContext) {
	importRole := func(ctx context.Context, files []*quaycrewv1.RoleFile) error {
		w := worldFrom(ctx)
		_, w.lastErr = w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: files})
		return nil
	}

	sc.Step(`^the operator imports the "([^"]*)" role$`, func(ctx context.Context, name string) error {
		return importRole(ctx, roleFiles(name, 1, roleManifest{
			model: "opus", receives: []string{"job", "context"},
		}))
	})

	sc.Step(`^the operator imported the "([^"]*)" role$`, func(ctx context.Context, name string) error {
		if err := importRole(ctx, roleFiles(name, 1, roleManifest{
			model: "opus", receives: []string{"job", "context"},
		})); err != nil {
			return err
		}
		return worldFrom(ctx).lastErr
	})

	sc.Step(`^the operator imports version (\d+) of the "([^"]*)" role$`,
		func(ctx context.Context, version int, name string) error {
			return importRole(ctx, roleFiles(name, version, roleManifest{
				model: "opus", receives: []string{"job", "context"},
			}))
		})

	// The same name and version carrying a different role, which is the import that has to be
	// refused: a workspace pins a version, so changing one underneath it changes how a session
	// already running as it was told to work.
	sc.Step(`^the operator imports a different "([^"]*)" role at the same version$`,
		func(ctx context.Context, name string) error {
			return importRole(ctx, roleFiles(name, 1, roleManifest{
				model: "opus", receives: []string{"job"}, brief: "Write the code instead.",
			}))
		})

	sc.Step(`^the operator imported a role that may create and read jobs$`, func(ctx context.Context) error {
		if err := importRole(ctx, roleFiles("test-writer", 1, roleManifest{
			model: "opus", receives: []string{"job", "context"},
			verbs: []string{role.VerbJobCreate, role.VerbJobRead},
		})); err != nil {
			return err
		}
		return worldFrom(ctx).lastErr
	})

	// The way off the retired key. A role file lives in somebody's repository, so the system meets the
	// old spelling long after it stopped using it.
	sc.Step(`^the operator imports a role saying "may" where it should say "verbs"$`, func(ctx context.Context) error {
		return importRole(ctx, roleFiles("test-writer", 1, roleManifest{
			model: "opus", receives: []string{"job"},
			verbs: []string{role.VerbJobCreate}, verbsKey: "may",
		}))
	})

	sc.Step(`^the operator imports a role receiving "([^"]*)"$`,
		func(ctx context.Context, material string) error {
			return importRole(ctx, roleFiles("test-writer", 1, roleManifest{
				model: "opus", receives: []string{"job", material},
			}))
		})

	sc.Step(`^the operator imports a role naming no model$`, func(ctx context.Context) error {
		return importRole(ctx, roleFiles("test-writer", 1, roleManifest{
			receives: []string{"job"},
		}))
	})

	sc.Step(`^the operator imports a role declaring nothing it receives$`, func(ctx context.Context) error {
		return importRole(ctx, roleFiles("test-writer", 1, roleManifest{model: "opus"}))
	})

	sc.Step(`^the operator imports a role with no version$`, func(ctx context.Context) error {
		return importRole(ctx, roleFiles("test-writer", 0, roleManifest{
			model: "opus", receives: []string{"job"},
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

	sc.Step(`^the operator (?:attaches|attached) the "([^"]*)" role to the system$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
				Scope: "system", Name: name,
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

	sc.Step(`^the operator detaches the "([^"]*)" role from the system$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			_, w.lastErr = w.client.DetachRole(ctx, &quaycrewv1.DetachRoleRequest{
				Scope: "system", Name: name,
			})
			return w.lastErr
		})

	sc.Step(`^the operator lists the system's roles$`, func(ctx context.Context) error {
		return listRoles(ctx, "")
	})

	sc.Step(`^the operator lists the workspace's roles$`, func(ctx context.Context) error {
		return listRoles(ctx, worldFrom(ctx).workspaceID)
	})

	sc.Step(`^the system holds no roles$`, func(ctx context.Context) error {
		return rolesHeld(ctx, "", nil)
	})

	sc.Step(`^the system holds the "([^"]*)" role$`, func(ctx context.Context, name string) error {
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

	sc.Step(`^the listing says the "([^"]*)" role is held by the system$`,
		func(ctx context.Context, name string) error {
			held, err := roleNamed(ctx, worldFrom(ctx).workspaceID, name)
			if err != nil {
				return err
			}
			if !held.GetSystem() {
				return fmt.Errorf("the listing does not say the %s role came from the system", name)
			}
			return nil
		})

	// Reading a role back whole. The brief is the role, so a system that cannot hand it back is a system
	// nobody can audit: there is no way to diff what it holds against the file it came from.
	sc.Step(`^the operator reads the "([^"]*)" role back$`, func(ctx context.Context, name string) error {
		return readRole(ctx, "", name)
	})

	sc.Step(`^the operator reads the workspace's "([^"]*)" role back$`, func(ctx context.Context, name string) error {
		return readRole(ctx, worldFrom(ctx).workspaceID, name)
	})

	sc.Step(`^the role comes back with its brief$`, func(ctx context.Context) error {
		read := worldFrom(ctx).lastRole
		if read == nil {
			return fmt.Errorf("no role came back at all")
		}
		// Held to the text the import carried rather than to "not empty", because an empty brief
		// passes every check that only asks whether something is there.
		if read.GetBrief() != roleBrief {
			return fmt.Errorf("the brief came back as %q, and it went in as %q", read.GetBrief(), roleBrief)
		}
		return nil
	})

	sc.Step(`^the role comes back saying what it receives$`, func(ctx context.Context) error {
		read := worldFrom(ctx).lastRole
		if read == nil {
			return fmt.Errorf("no role came back at all")
		}
		if got := strings.Join(read.GetRole().GetReceives(), ", "); got != "context, job" {
			return fmt.Errorf("it came back receiving %q, and it declared %q", got, "context, job")
		}
		return nil
	})

	sc.Step(`^the role comes back saying it may call "([^"]*)"$`, func(ctx context.Context, want string) error {
		read := worldFrom(ctx).lastRole
		if read == nil {
			return fmt.Errorf("no role came back at all")
		}
		if got := strings.Join(read.GetVerbs(), ", "); got != want {
			return fmt.Errorf("it came back able to call %q, and it declared %q", got, want)
		}
		return nil
	})

	sc.Step(`^the role comes back at version (\d+)$`, func(ctx context.Context, version int) error {
		read := worldFrom(ctx).lastRole
		if read == nil {
			return fmt.Errorf("no role came back at all")
		}
		if got := int(read.GetRole().GetVersion()); got != version {
			return fmt.Errorf("version %d came back, want %d", got, version)
		}
		return nil
	})
	sc.Step(`^the system refuses the role saying "([^"]*)"$`, func(ctx context.Context, said string) error {
		w := worldFrom(ctx)
		if w.lastErr == nil {
			return fmt.Errorf("the system accepted it, and it should have been refused")
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
	verbs    []string
	// verbsKey is the key the verbs are written under. Empty writes the one the system takes, and a
	// scenario naming the retired one is how the way off the old spelling is held to a test.
	verbsKey string
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
	manifest.WriteString("summary: writes the tests for a job, from the job alone\n")
	if said.model != "" {
		fmt.Fprintf(&manifest, "model: %s\n", said.model)
	}
	if len(said.receives) > 0 {
		manifest.WriteString("receives:\n")
		for _, one := range said.receives {
			fmt.Fprintf(&manifest, "  - %s\n", one)
		}
	}
	if len(said.verbs) > 0 {
		key := said.verbsKey
		if key == "" {
			key = "verbs"
		}
		fmt.Fprintf(&manifest, "%s:\n", key)
		for _, one := range said.verbs {
			fmt.Fprintf(&manifest, "  - %s\n", one)
		}
	}
	brief := said.brief
	if brief == "" {
		brief = roleBrief
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

// The roles this build ships, read from roles/ at the root of the repository rather than from a list
// written here. A role added tomorrow is imported by these steps without anybody remembering, and a
// roles/ that lost its contents fails the scenario rather than passing over nothing: role.All
// refuses a directory holding none.
const shippedRoles = "../roles"

func initializeShippedRoleSteps(sc *godog.ScenarioContext) {
	// One shipped role rather than all twelve, so a scenario whose background already holds a role
	// under a name this build also ships does not collide with it at import.
	sc.Step(`^the operator imports the "([^"]*)" role this build ships$`,
		func(ctx context.Context, name string) error {
			files, err := roleFilesFrom(filepath.Join(shippedRoles, name))
			if err != nil {
				return err
			}
			_, err = worldFrom(ctx).client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: files})
			if err != nil {
				return fmt.Errorf("the system refused the %s role, which ships with it: %w", name, err)
			}
			return nil
		})

	sc.Step(`^the operator imports every role this build ships$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		ships, err := role.All(shippedRoles)
		if err != nil {
			return err
		}
		for _, one := range ships {
			files, err := roleFilesFrom(one.Dir)
			if err != nil {
				return err
			}
			if _, err := w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: files}); err != nil {
				return fmt.Errorf("the system refused the %s role, which ships with it: %w", one.Name, err)
			}
		}
		return nil
	})

	sc.Step(`^the system holds every role this build ships$`, func(ctx context.Context) error {
		ships, err := role.All(shippedRoles)
		if err != nil {
			return err
		}
		held, err := worldFrom(ctx).client.ListRoles(ctx, &quaycrewv1.ListRolesRequest{})
		if err != nil {
			return err
		}
		names := make([]string, 0, len(held.GetRoles()))
		for _, one := range held.GetRoles() {
			names = append(names, one.GetName())
		}
		sort.Strings(names)
		wanted := make([]string, 0, len(ships))
		for _, one := range ships {
			wanted = append(wanted, one.Name)
		}
		sort.Strings(wanted)
		if strings.Join(names, ", ") != strings.Join(wanted, ", ") {
			return fmt.Errorf("the system holds %q and roles/ ships %q",
				strings.Join(names, ", "), strings.Join(wanted, ", "))
		}
		return nil
	})

	// A ported brief carrying a word the system does not hand out is the failure this scenario exists
	// to catch, so it is a shipped role with one word changed rather than a role invented here.
	sc.Step(`^the operator imports a shipped role receiving "([^"]*)"$`,
		func(ctx context.Context, material string) error {
			w := worldFrom(ctx)
			files, err := roleFilesFrom(filepath.Join(shippedRoles, "test-writer"))
			if err != nil {
				return err
			}
			for _, file := range files {
				if file.GetPath() == role.ManifestFile {
					file.Body = []byte(strings.Replace(string(file.GetBody()),
						"  - skills", "  - "+material, 1))
				}
			}
			_, w.lastErr = w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: files})
			return nil
		})
}

// roleFilesFrom reads a role off disk into the shape the wire carries, which is what the command
// line does before it sends one.
func roleFilesFrom(dir string) ([]*quaycrewv1.RoleFile, error) {
	read, err := role.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]*quaycrewv1.RoleFile, 0, len(read))
	for _, file := range read {
		files = append(files, &quaycrewv1.RoleFile{Path: file.Path, Body: file.Body})
	}
	return files, nil
}

// What a role this build ships may actually do, driven through the credential a job running as it
// carries. A listing cannot answer this: it says what a role receives and not what it may call, and
// what the system holds a session to is the credential.
func initializeShippedRoleVerbSteps(sc *godog.ScenarioContext) {
	sc.Step(`^a job running as the "([^"]*)" role this build ships$`,
		func(ctx context.Context, name string) error {
			w := worldFrom(ctx)
			if err := importAndAttachShipped(ctx, name); err != nil {
				return err
			}
			declared, err := w.client.CreateJob(ctx, &quaycrewv1.CreateJobRequest{
				Project: w.projectID, Title: "deliver the page",
				Brief: "turn this brief into the tree that delivers it", Role: name,
			})
			if err != nil {
				return err
			}
			scenario := capabilityFrom(ctx)
			scenario.running = declared.GetJob().GetId()
			token, minted := w.server.JobCredentialForTest(ctx, scenario.running)
			if !minted {
				return fmt.Errorf("the system minted no credential for the job running as %s", name)
			}
			scenario.token = token
			return nil
		})

	sc.Step(`^that session declares a job running as the "([^"]*)" role$`,
		func(ctx context.Context, name string) error {
			w, scenario := worldFrom(ctx), capabilityFrom(ctx)
			if err := importAndAttachShipped(ctx, name); err != nil {
				return err
			}
			declared, err := w.dialAs(scenario.token).CreateJob(ctx, &quaycrewv1.CreateJobRequest{
				Project: w.projectID, Title: "write the tests",
				Brief: "from the contract alone", Role: name,
			})
			w.lastErr = err
			if err != nil {
				return nil
			}
			scenario.declared = append(scenario.declared, declared.GetJob())
			return nil
		})

	// The verb the orchestrator holds and this role does not, asked the way a session asks: through
	// the credential. Reading it off the manifest would prove only what the file says.
	sc.Step(`^that session may not stop a job$`, func(ctx context.Context) error {
		w, scenario := worldFrom(ctx), capabilityFrom(ctx)
		_, err := w.dialAs(scenario.token).StopJob(ctx, &quaycrewv1.StopJobRequest{
			Id: scenario.running, Reason: "changed my mind",
		})
		if err == nil {
			return fmt.Errorf("the system let it stop a job, and its role grants no %s", role.VerbJobStop)
		}
		if !strings.Contains(err.Error(), role.VerbJobStop) {
			return fmt.Errorf("the refusal does not name %s: %v", role.VerbJobStop, err)
		}
		return nil
	})
}

// importAndAttachShipped puts a role from roles/ in front of the workspace, importing it if the
// scenario has not already. Importing the same revision twice is harmless, which is what lets a
// scenario name two roles without caring which was named first.
func importAndAttachShipped(ctx context.Context, name string) error {
	w := worldFrom(ctx)
	files, err := roleFilesFrom(filepath.Join(shippedRoles, name))
	if err != nil {
		return err
	}
	if _, err := w.client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: files}); err != nil {
		return fmt.Errorf("the system refused the %s role, which ships with it: %w", name, err)
	}
	if _, err := w.client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
		Workspace: w.workspaceID, Name: name,
	}); err != nil {
		return err
	}
	return nil
}

// roleBrief is the instruction every role a scenario imports carries, so a step reading one back
// compares against the text that went in rather than against "something came back".
const roleBrief = "Write the tests. Do not write the code."

// readRole asks the system for one role whole, at the system's level or at a workspace's.
func readRole(ctx context.Context, workspace, name string) error {
	w := worldFrom(ctx)
	read, err := w.client.GetRole(ctx, &quaycrewv1.GetRoleRequest{Workspace: workspace, Name: name})
	w.lastErr = err
	w.lastRole = read
	return nil
}
