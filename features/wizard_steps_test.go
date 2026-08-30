package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/console"
	"github.com/atlantic-blue/krewe/internal/display"
	"github.com/atlantic-blue/krewe/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/cucumber/godog"
)

// The wizard's scenarios drive the console's own reducer against the real control plane, the same way
// the destructive keys do. That is the only way to say "making a project made a project and nothing
// else": against a double it would be a claim about the double.

// initializeWizardSteps registers them.
func initializeWizardSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator answers the wizard with:$`, func(ctx context.Context, answers *godog.Table) error {
		c := consoleFrom(ctx)
		if err := c.openWizard(worldFrom(ctx).client); err != nil {
			return err
		}
		return c.answerWizard(answers)
	})

	// The last row is typed and never accepted, so escape arrives on a half answered question. Pressing
	// escape on a finished wizard would prove nothing: there would be nothing left to forget.
	sc.Step(`^the operator abandons the wizard after typing:$`, func(ctx context.Context, answers *godog.Table) error {
		c := consoleFrom(ctx)
		if err := c.openWizard(worldFrom(ctx).client); err != nil {
			return err
		}
		rows := answers.Rows
		if len(rows) == 0 {
			return fmt.Errorf("no answers to type")
		}
		if err := c.answerWizard(&godog.Table{Rows: rows[:len(rows)-1]}); err != nil {
			return err
		}
		for _, cell := range rows[len(rows)-1].Cells {
			if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(cell.Value)}); err != nil {
				return err
			}
		}
		return c.press(tea.KeyMsg{Type: tea.KeyEsc})
	})

	// Tab is the way onto an option without spelling it out. Three presses land on "dangerous" because
	// the modes are offered narrowest first, so counting presses in the scenario is naming the same
	// thing an operator would see land under the cursor.
	sc.Step(`^the operator presses tab (\d+) times? to choose the mode, then sends "([^"]*)"$`,
		func(ctx context.Context, times int, message string) error {
			c := consoleFrom(ctx)
			for i := 0; i < times; i++ {
				if err := c.press(tea.KeyMsg{Type: tea.KeyTab}); err != nil {
					return err
				}
			}
			if err := c.press(tea.KeyMsg{Type: tea.KeyEnter}); err != nil {
				return err
			}
			if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(message)}); err != nil {
				return err
			}
			return c.press(tea.KeyMsg{Type: tea.KeyEnter})
		})

	sc.Step(`^the system has (\d+) workspaces?$`, func(ctx context.Context, want int) error {
		listed, err := worldFrom(ctx).client.ListWorkspaces(ctx, &quaycrewv1.ListWorkspacesRequest{})
		if err != nil {
			return err
		}
		if got := len(listed.GetWorkspaces()); got != want {
			return fmt.Errorf("the system has %d workspaces, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^the system has (\d+) projects?$`, func(ctx context.Context, want int) error {
		listed, err := worldFrom(ctx).client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{})
		if err != nil {
			return err
		}
		if got := len(listed.GetProjects()); got != want {
			return fmt.Errorf("the system has %d projects, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^the system has (\d+) sessions?$`, func(ctx context.Context, want int) error {
		listed, err := worldFrom(ctx).client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
		if err != nil {
			return err
		}
		if got := len(listed.GetSessions()); got != want {
			return fmt.Errorf("the system has %d sessions, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^the console is asking nothing$`, func(ctx context.Context) error {
		view := consoleFrom(ctx).model.View()
		// "enter accepts" is common to the wizard's hint and the guided setup's, so an open
		// question from either fails here rather than only the standalone one.
		if strings.Contains(view, "enter accepts") {
			return fmt.Errorf("the console is still asking a question:\n%s", view)
		}
		if strings.Contains(view, "making ") {
			return fmt.Errorf("the console is still drawn on the wizard working:\n%s", view)
		}
		return nil
	})

	sc.Step(`^the console lists the session the wizard started$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		// Asked of the control plane rather than of the world, because this task was dispatched by
		// the console rather than by a step.
		listed, err := w.client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
		if err != nil {
			return err
		}
		if len(listed.GetSessions()) != 1 {
			return fmt.Errorf("the system has %d sessions, want the one the wizard started", len(listed.GetSessions()))
		}
		view := c.model.View()
		if !strings.Contains(view, display.ShortID(listed.GetSessions()[0].GetId())) {
			return fmt.Errorf("the console does not list the session the wizard made:\n%s", view)
		}
		return nil
	})

	sc.Step(`^the secrets backend holds nothing for that workspace$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		got, err := w.secrets.Get(ctx, w.workspaceID, model.ClaudeCodeOAuthTokenEnv)
		if err == nil && got != "" {
			return fmt.Errorf("the secrets backend holds a token for the workspace, want nothing")
		}
		return nil
	})

	sc.Step(`^the wizard says there is nothing called "([^"]*)" to make$`, func(ctx context.Context, typed string) error {
		view := consoleFrom(ctx).model.View()
		want := fmt.Sprintf("there is nothing called %q to make", typed)
		if !strings.Contains(view, want) {
			return fmt.Errorf("the console does not say %q:\n%s", want, view)
		}
		return nil
	})
}

// openWizard opens the real console over the live control plane and presses the key that makes
// something. The console needs the system handed to it separately, because listing goes through each
// view's own lister and the wizard is the one thing no view owns.
func (c *consoleWorld) openWizard(client quaycrewv1.ControlPlaneServiceClient) error {
	registry, err := console.NewDefaultRegistry(client)
	if err != nil {
		return err
	}
	opened, err := console.New(registry, console.Default, nil)
	if err != nil {
		return err
	}
	c.model = opened.WithClient(client)
	if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")}); err != nil {
		return err
	}
	return c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
}

// answerWizard types each row of the table and presses enter, which is what answering a question is.
// Every command the console asks for is run on the way, so a step that reads what the system already
// has has an answer by the time the next key arrives.
func (c *consoleWorld) answerWizard(answers *godog.Table) error {
	for _, row := range answers.Rows {
		for _, cell := range row.Cells {
			if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(cell.Value)}); err != nil {
				return err
			}
			if err := c.press(tea.KeyMsg{Type: tea.KeyEnter}); err != nil {
				return err
			}
		}
	}
	return nil
}

// The mode a session was born in, read off the system rather than off the wizard, because what matters
// is what the session holds and not what was typed at it.
func initializeWizardModeSteps(sc *godog.ScenarioContext) {
	sc.Step(`^that session's mode is "([^"]*)"$`, func(ctx context.Context, want string) error {
		listed, err := worldFrom(ctx).client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
		if err != nil {
			return err
		}
		if len(listed.GetSessions()) != 1 {
			return fmt.Errorf("the system has %d sessions, so there is no single one to ask about",
				len(listed.GetSessions()))
		}
		if got := listed.GetSessions()[0].GetPermissionMode(); got != want {
			return fmt.Errorf("the session was born in %q, want %q", got, want)
		}
		return nil
	})

	sc.Step(`^the console says "([^"]*)"$`, func(ctx context.Context, want string) error {
		view := consoleFrom(ctx).model.View()
		if !strings.Contains(view, want) {
			return fmt.Errorf("the console does not say %q:\n%s", want, view)
		}
		return nil
	})
}
