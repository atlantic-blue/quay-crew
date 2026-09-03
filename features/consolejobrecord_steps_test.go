package features_test

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cucumber/godog"
)

// What one job holds, read in the console rather than at the command line.
//
// A row of the jobs listing is nine cells, and a brief is a paragraph. The answer is not in the
// listing at all: a listing keeps every answer out, because a hundred answers is a listing nobody
// can read. So the page reads the job itself, and this scenario is what says so: a page built out of
// the row it was opened from would draw no answer.

func initializeConsoleJobRecordSteps(sc *godog.ScenarioContext) {
	// The console is built the way `krewe` builds it, with the system it is connected to, because the
	// page asks that system for the job.
	sc.Step(`^the operator opens that job in the console$`, func(ctx context.Context) error {
		c, w := consoleFrom(ctx), worldFrom(ctx)
		if err := c.openModel(w); err != nil {
			return err
		}
		c.model = c.model.WithClient(w.client)
		if err := pressKeys(c, ":jobs"); err != nil {
			return err
		}
		if err := c.press(tea.KeyMsg{Type: tea.KeyEnter}); err != nil {
			return err
		}
		return c.press(tea.KeyMsg{Type: tea.KeyEnter})
	})

	sc.Step(`^the console shows that job's brief and answer whole$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		drawn := screenProse(consoleFrom(ctx).model.View())
		for what, want := range map[string]string{"brief": one.GetBrief(), "answer": one.GetAnswer()} {
			if want == "" {
				return fmt.Errorf("the job carries no %s, so this scenario proves nothing", what)
			}
			if !strings.Contains(drawn, strings.Join(strings.Fields(want), " ")) {
				return fmt.Errorf("the screen does not carry the %s whole:\n%s", what, drawn)
			}
		}
		return nil
	})
}

// screenProse is what the screen says with the frame taken off and the rows run together, so a
// sentence the panel wrapped over three rows reads as the sentence it is.
func screenProse(view string) string {
	drawn := strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ").Replace(view)
	return strings.Join(strings.Fields(drawn), " ")
}
