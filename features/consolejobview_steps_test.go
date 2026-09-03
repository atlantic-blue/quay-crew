package features_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/atlantic-blue/quay-krewe/internal/display"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/cucumber/godog"
)

// A person answering a job on the job's own view, driven through the real console over the real
// control plane. Every step here is a keypress, and what is asserted is the screen the person is
// left looking at rather than the call the key produced.

func initializeConsoleJobViewSteps(sc *godog.ScenarioContext) {
	// Enter, on the job under the cursor, which is how a person opens one.
	sc.Step(`^the operator opens that job in the console$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if err := c.openModelOnJobs(worldFrom(ctx)); err != nil {
			return err
		}
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		on, standing := c.model.Selected()
		if !standing || on.ID != one.GetId() {
			return fmt.Errorf("the cursor is on %q, want the job %q", on.ID, one.GetId())
		}
		return c.press(tea.KeyMsg{Type: tea.KeyEnter})
	})

	// The key, the words, and enter. The same letter the jobs listing answers with.
	sc.Step(`^the operator answers it there with "([^"]*)"$`, func(ctx context.Context, said string) error {
		c := consoleFrom(ctx)
		if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}); err != nil {
			return err
		}
		for _, letter := range said {
			if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{letter}}); err != nil {
				return err
			}
		}
		return c.press(tea.KeyMsg{Type: tea.KeyEnter})
	})

	// Where the person is standing afterwards, read off the screen: the same job, with what it was
	// told drawn on it. A console that went back to the listing took them off the job they opened.
	sc.Step(`^the console still shows that job, saying it was told "([^"]*)"$`,
		func(ctx context.Context, said string) error {
			c := consoleFrom(ctx)
			one, err := readJob(ctx, 0)
			if err != nil {
				return err
			}
			view := c.model.View()
			if !strings.Contains(view, display.ShortID(one.GetId())) {
				return fmt.Errorf("the screen is not about the job that was opened:\n%s", view)
			}
			if !strings.Contains(view, said) {
				return fmt.Errorf("the screen does not say what the job was told:\n%s", view)
			}
			return nil
		})
}
