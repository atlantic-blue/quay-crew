package features_test

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/console"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/model"
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

	sc.Step(`^the crew has (\d+) workspaces?$`, func(ctx context.Context, want int) error {
		listed, err := worldFrom(ctx).client.ListWorkspaces(ctx, &quaycrewv1.ListWorkspacesRequest{})
		if err != nil {
			return err
		}
		if got := len(listed.GetWorkspaces()); got != want {
			return fmt.Errorf("the crew has %d workspaces, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^the crew has (\d+) projects?$`, func(ctx context.Context, want int) error {
		listed, err := worldFrom(ctx).client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{})
		if err != nil {
			return err
		}
		if got := len(listed.GetProjects()); got != want {
			return fmt.Errorf("the crew has %d projects, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^the crew has (\d+) sessions?$`, func(ctx context.Context, want int) error {
		listed, err := worldFrom(ctx).client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{})
		if err != nil {
			return err
		}
		if got := len(listed.GetThreads()); got != want {
			return fmt.Errorf("the crew has %d sessions, want %d", got, want)
		}
		return nil
	})

	sc.Step(`^the console is asking nothing$`, func(ctx context.Context) error {
		view := consoleFrom(ctx).model.View()
		if strings.Contains(view, "esc cancels and makes nothing") {
			return fmt.Errorf("the wizard is still asking a question:\n%s", view)
		}
		if strings.Contains(view, "making ") {
			return fmt.Errorf("the console is still drawn on the wizard working:\n%s", view)
		}
		return nil
	})

	sc.Step(`^the console lists the session the wizard started$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		// Asked of the control plane rather than of the world, because this turn was dispatched by
		// the console rather than by a step.
		listed, err := w.client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{})
		if err != nil {
			return err
		}
		if len(listed.GetThreads()) != 1 {
			return fmt.Errorf("the crew has %d sessions, want the one the wizard started", len(listed.GetThreads()))
		}
		view := c.model.View()
		if !strings.Contains(view, display.ShortID(listed.GetThreads()[0].GetId())) {
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
// something. The console needs the crew handed to it separately, because listing goes through each
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
	if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}); err != nil {
		return err
	}
	return c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
}

// answerWizard types each row of the table and presses enter, which is what answering a question is.
// Every command the console asks for is run on the way, so a step that reads what the crew already
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
