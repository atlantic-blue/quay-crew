package features_test

import (
	"context"
	"fmt"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/job"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/cucumber/godog"
)

// The console's view of jobs. These steps sit beside the other console steps rather than with the job
// steps, because what they drive is the console over the real control plane: the rows are the system's
// actual jobs, and where a key lands is read off the screen the operator is left looking at.

// sessionCell is where a job's session sits in its row. Named so a step reads as the cell it is about
// rather than as a number in a slice.
const sessionCell = 4

func initializeConsoleJobsSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator opens the console on jobs$`, func(ctx context.Context) error {
		return consoleFrom(ctx).open(ctx, worldFrom(ctx).client, "jobs")
	})

	sc.Step(`^the console lists (\d+) jobs?$`, func(ctx context.Context, want int) error {
		return expectRows(consoleFrom(ctx), "jobs", want)
	})

	sc.Step(`^the console is showing jobs$`, func(ctx context.Context) error {
		if got := consoleFrom(ctx).active.Name; got != "jobs" {
			return fmt.Errorf("the console is showing %q, want jobs", got)
		}
		return nil
	})

	sc.Step(`^the job's row says it has no session yet$`, func(ctx context.Context) error {
		row, err := onlyRow(consoleFrom(ctx))
		if err != nil {
			return err
		}
		if row.Parent != "" {
			return fmt.Errorf("the job carries session %q, so this is not a job without one", row.Parent)
		}
		// The words rather than an empty cell. Nothing there reads as a hole in the row rather than
		// as a job waiting its turn.
		if got := row.Cells[sessionCell]; got != "not yet" {
			return fmt.Errorf(`the session cell says %q, want "not yet"`, got)
		}
		return nil
	})

	sc.Step(`^the job's row names the session doing it$`, func(ctx context.Context) error {
		row, err := onlyRow(consoleFrom(ctx))
		if err != nil {
			return err
		}
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetSession() == "" {
			return fmt.Errorf("the system gave the job no session, so there is nothing for the row to name")
		}
		// The whole identifier is what descending and acting use; the short one is what is read.
		if row.Parent != one.GetSession() {
			return fmt.Errorf("the row carries session %q and the system says %q", row.Parent, one.GetSession())
		}
		if got := row.Cells[sessionCell]; got != display.ShortID(one.GetSession()) {
			return fmt.Errorf("the session cell says %q, want %q", got, display.ShortID(one.GetSession()))
		}
		return nil
	})

	// Enter through the real console, so what the later steps read is the screen it left the operator
	// on rather than the call the key produced.
	sc.Step(`^the operator presses enter on the selected job$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if err := c.openModelOnJobs(worldFrom(ctx)); err != nil {
			return err
		}
		return c.press(tea.KeyMsg{Type: tea.KeyEnter})
	})

	// What the job's session was asked, which is the first thing the tasks view puts on screen and
	// the reason enter goes there rather than to the session row.
	sc.Step(`^the console shows what the job's session was asked$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		view := consoleFrom(ctx).model.View()
		// The opening of the brief rather than the whole of it: the column holds what it can and cuts
		// the rest, which is what any listing of a paragraph does.
		opening := strings.Join(strings.Fields(one.GetBrief())[:4], " ")
		if !strings.Contains(view, opening) {
			return fmt.Errorf("the screen does not carry %q, so enter did not open what the job did:\n%s",
				opening, view)
		}
		// The breadcrumb says where the operator is: the tasks of the job they came from.
		if !strings.Contains(view, "tasks("+one.GetTitle()+")") {
			return fmt.Errorf("the screen does not say it is showing that job's tasks:\n%s", view)
		}
		return nil
	})

	// The way back off the key: a job with nothing behind it leaves the operator where they were,
	// rather than on an empty listing under a heading that promised one.
	sc.Step(`^the console is still showing the job$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		view := consoleFrom(ctx).model.View()
		if !strings.Contains(view, one.GetTitle()) {
			return fmt.Errorf("the screen no longer carries the job %q:\n%s", one.GetTitle(), view)
		}
		if strings.Contains(view, one.GetBrief()) {
			return fmt.Errorf("the screen carries the brief, so enter opened a listing after all:\n%s", view)
		}
		return nil
	})

	sc.Step(`^the operator opens the console on jobs and presses backspace on the job$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if err := c.openModelOnJobs(worldFrom(ctx)); err != nil {
			return err
		}
		return c.press(tea.KeyMsg{Type: tea.KeyBackspace})
	})

	sc.Step(`^the console asks whether to stop that job$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		view := consoleFrom(ctx).model.View()
		// The console names the row the way the listing does, and a job is called by its title. An
		// operator confirming an identifier they cannot read is confirming nothing.
		want := "stop job " + one.GetTitle() + "?"
		if !strings.Contains(view, want) {
			return fmt.Errorf("the console does not ask %q:\n%s", want, view)
		}
		return nil
	})

	sc.Step(`^the job is stopped, and the reason says a person did it$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() != job.PhaseStopped {
			return fmt.Errorf("the job is %q, want stopped", one.GetPhase())
		}
		// A job that went quiet and a job somebody halted must never read the same, so the reason is
		// on the record rather than the phase alone.
		if !strings.Contains(one.GetReason(), "operator") {
			return fmt.Errorf("the job was stopped saying %q, which does not say a person did it", one.GetReason())
		}
		return nil
	})
}

// openModelOnJobs stands the real console up and walks it to the jobs view.
func (c *consoleWorld) openModelOnJobs(w *world) error {
	return c.openModelOn(w, "jobs")
}
