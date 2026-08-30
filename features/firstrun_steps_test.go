package features_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/console"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/cucumber/godog"
)

// The guided setup's scenarios drive the console's own reducer against the real control plane, the
// same way the wizard's do: what has to be true is that a person who answered the questions is left
// with a system that works, and only the real one can say so.

func initializeFirstRunSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator opens the console on the empty system$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		registry, err := console.NewDefaultRegistry(w.client)
		if err != nil {
			return err
		}
		opened, err := console.New(registry, console.Default, nil)
		if err != nil {
			return err
		}
		// The client is what the guided setup asks whether any workspaces exist, and what the
		// wizard's own makes go through.
		c.model = opened.WithClient(w.client)
		return c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	})

	sc.Step(`^the guided setup is asking for a workspace name$`, func(ctx context.Context) error {
		view := consoleFrom(ctx).model.View()
		if !strings.Contains(view, "no workspaces yet") {
			return fmt.Errorf("the console does not offer the setup:\n%s", view)
		}
		if !strings.Contains(view, "new workspace") {
			return fmt.Errorf("the setup is not asking for a workspace name:\n%s", view)
		}
		return nil
	})

	sc.Step(`^the guided setup is asking for a first message$`, func(ctx context.Context) error {
		view := consoleFrom(ctx).model.View()
		if !strings.Contains(view, "first message") {
			return fmt.Errorf("the setup is not asking for a first message:\n%s", view)
		}
		return nil
	})

	sc.Step(`^the guided setup mentions "([^"]*)"$`, func(ctx context.Context, want string) error {
		view := consoleFrom(ctx).model.View()
		if !strings.Contains(view, want) {
			return fmt.Errorf("the setup does not mention %q:\n%s", want, view)
		}
		return nil
	})

	sc.Step(`^the operator answers the guided setup with:$`, func(ctx context.Context, answers *godog.Table) error {
		return consoleFrom(ctx).answerWizard(answers)
	})

	sc.Step(`^the operator skips a stage of the guided setup$`, func(ctx context.Context) error {
		return consoleFrom(ctx).press(tea.KeyMsg{Type: tea.KeyEnter})
	})

	sc.Step(`^the operator leaves the guided setup$`, func(ctx context.Context) error {
		return consoleFrom(ctx).press(tea.KeyMsg{Type: tea.KeyEsc})
	})

	sc.Step(`^a file "([^"]*)" saying "([^"]*)"$`, func(ctx context.Context, name, body string) error {
		dir, err := os.MkdirTemp("", "quaycrew-firstrun")
		if err != nil {
			return err
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return err
		}
		consoleFrom(ctx).contextFile = path
		return nil
	})

	sc.Step(`^the operator answers the guided setup with the path to "([^"]*)"$`, func(ctx context.Context, name string) error {
		c := consoleFrom(ctx)
		if c.contextFile == "" || filepath.Base(c.contextFile) != name {
			return fmt.Errorf("no file %q was set up", name)
		}
		if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(c.contextFile)}); err != nil {
			return err
		}
		return c.press(tea.KeyMsg{Type: tea.KeyEnter})
	})

	sc.Step(`^the secrets backend holds a token for the workspace named "([^"]*)"$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		id, err := workspaceIDNamed(ctx, w, name)
		if err != nil {
			return err
		}
		got, err := w.secrets.Get(ctx, id, model.ClaudeCodeOAuthTokenEnv)
		if err != nil || got == "" {
			return fmt.Errorf("the secrets backend holds no token for %s: %v", name, err)
		}
		return nil
	})

	sc.Step(`^the secrets backend holds nothing for the workspace named "([^"]*)"$`, func(ctx context.Context, name string) error {
		w := worldFrom(ctx)
		id, err := workspaceIDNamed(ctx, w, name)
		if err != nil {
			return err
		}
		if got, err := w.secrets.Get(ctx, id, model.ClaudeCodeOAuthTokenEnv); err == nil && got != "" {
			return fmt.Errorf("the secrets backend holds a token for %s, want nothing", name)
		}
		return nil
	})

	sc.Step(`^the context of the project named "([^"]*)" says "([^"]*)"$`, func(ctx context.Context, name, want string) error {
		w := worldFrom(ctx)
		listed, err := w.client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{})
		if err != nil {
			return err
		}
		for _, project := range listed.GetProjects() {
			if project.GetName() != name {
				continue
			}
			got, err := w.store.GetContext(ctx, store.ContextProject, project.GetId())
			if err != nil {
				return err
			}
			if !strings.Contains(got, want) {
				return fmt.Errorf("the context of %s says %q, want it to say %q", name, got, want)
			}
			return nil
		}
		return fmt.Errorf("there is no project named %q", name)
	})

	sc.Step(`^the workspace named "([^"]*)" holds the skill "([^"]*)"$`, func(ctx context.Context, name, skillName string) error {
		w := worldFrom(ctx)
		id, err := workspaceIDNamed(ctx, w, name)
		if err != nil {
			return err
		}
		listed, err := w.client.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{Workspace: id})
		if err != nil {
			return err
		}
		for _, one := range listed.GetSkills() {
			if one.GetName() == skillName {
				return nil
			}
		}
		return fmt.Errorf("workspace %s holds %d skills and none is %q", name, len(listed.GetSkills()), skillName)
	})
}

// workspaceIDNamed resolves a workspace the console made, so a step can ask about it without the
// world having created it.
func workspaceIDNamed(ctx context.Context, w *world, name string) (string, error) {
	listed, err := w.client.ListWorkspaces(ctx, &quaycrewv1.ListWorkspacesRequest{})
	if err != nil {
		return "", err
	}
	for _, workspace := range listed.GetWorkspaces() {
		if workspace.GetName() == name {
			return workspace.GetId(), nil
		}
	}
	return "", fmt.Errorf("there is no workspace named %q", name)
}
