package features_test

import (
	"context"
	"fmt"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/cucumber/godog"
)

// The console's view of jobs. These steps sit beside the other console steps rather than with the job
// steps, because what they drive is the console over the real control plane: the rows are the system's
// actual jobs, and where a key lands is read off the screen the operator is left looking at.

// sessionCell is where a job's session sits in its row. Named so a step reads as the cell it is about
// rather than as a number in a slice.
const sessionCell = 6

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

	// What the job's session was asked, which is the first thing the exec view puts on screen and
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
		// The breadcrumb says where the operator is: what the job they came from ran.
		if !strings.Contains(view, "exec("+one.GetTitle()+")") {
			return fmt.Errorf("the screen does not say it is showing what that job ran:\n%s", view)
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
		// The opening of the title rather than the whole of it, the way the step above reads a brief:
		// the column holds what it can and cuts the rest, and what this step is about is whether the
		// operator is still on the listing at all.
		opening := strings.Join(strings.Fields(one.GetTitle())[:3], " ")
		if !strings.Contains(view, opening) {
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

// titleCell is where a job's title sits in its row, and the cell the tree is drawn in: how many parts
// are under a job, and an indent on each part.
const titleCell = 5

func initializeConsolePartsSteps(sc *godog.ScenarioContext) {
	// The fan out this view was rebuilt for: a job in its test stage runs one session for each
	// requirement. The runs are written the way a controller writes them, because standing the whole
	// test stage up would make this a scenario about the test stage.
	sc.Step(`^its test stage fans out into (\d+) runs$`, func(ctx context.Context, count int) error {
		w := worldFrom(ctx)
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		for at := 1; at <= count; at++ {
			run := &job.Execution{
				ID: store.NewID(), Job: one.GetId(), Stage: job.StageTest, Number: at,
				Claim: job.ClaimOnRequirement(one.GetId(), job.Requirement{Number: at}),
				Phase: job.PhaseRunning, Session: store.NewID(),
			}
			if err := w.store.CreateExecution(ctx, run, &job.Event{
				ID: store.NewID(), Kind: job.EventDeclared, Job: one.GetId(), Execution: run.ID,
				Workspace: w.workspaceID, Project: w.projectID,
				Detail:     job.RunCalled(run.Stage, run.Number),
				OccurredAt: time.Now().UTC(),
			}); err != nil {
				return err
			}
		}
		return nil
	})

	sc.Step(`^the operator opens the console on jobs and presses tab on the job$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if err := c.openModelOnJobs(worldFrom(ctx)); err != nil {
			return err
		}
		return c.press(tea.KeyMsg{Type: tea.KeyTab})
	})

	sc.Step(`^the operator presses tab again$`, func(ctx context.Context) error {
		return consoleFrom(ctx).press(tea.KeyMsg{Type: tea.KeyTab})
	})

	sc.Step(`^the screen carries the job and not its parts$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		c := consoleFrom(ctx)
		rows := c.model.Listed()
		if len(rows) != 1 {
			return fmt.Errorf("the console draws %d rows, want the one job a person declared", len(rows))
		}
		view := c.model.View()
		// The cell rather than the screen, because the column cuts a title it cannot hold and this
		// step is about which row is there rather than about how wide the window is.
		if !strings.Contains(rows[0].Cells[titleCell], one.GetTitle()) {
			return fmt.Errorf("the row reads %q, want the job %q:\n%s",
				rows[0].Cells[titleCell], one.GetTitle(), view)
		}
		if strings.Contains(view, "tests for requirement") {
			return fmt.Errorf("the screen carries the parts, so the declared work is still buried:\n%s", view)
		}
		return nil
	})

	sc.Step(`^the job's row says it has (\d+) parts$`, func(ctx context.Context, count int) error {
		rows := consoleFrom(ctx).model.Listed()
		if len(rows) == 0 {
			return fmt.Errorf("the console draws nothing")
		}
		want := fmt.Sprintf("%d ", count)
		if !strings.HasPrefix(rows[0].Cells[titleCell], "▸"+want) {
			return fmt.Errorf("the row reads %q, want it to say it has %d parts", rows[0].Cells[titleCell], count)
		}
		return nil
	})

	sc.Step(`^the console draws the job and its (\d+) parts under it$`, func(ctx context.Context, count int) error {
		rows := consoleFrom(ctx).model.Listed()
		if len(rows) != count+1 {
			return fmt.Errorf("the console draws %d rows, want the job and its %d parts", len(rows), count)
		}
		view := consoleFrom(ctx).model.View()
		// Every part is on the screen. The column cuts a title it cannot hold, and at this width the
		// five read alike once cut, so which is which is read off the cells underneath.
		if drawn := strings.Count(view, "tests for requi"); drawn != count {
			return fmt.Errorf("the screen draws %d parts, want %d:\n%s", drawn, count, view)
		}
		// Read as a set. The store answers newest first, so which part is on which row is not
		// anything this view promises, and a step that indexed into the order would pass by accident.
		// The cells rather than the screen for the indent too: every cell is padded out to its
		// column, so a screen full of spaces carries an indent that was never drawn.
		drawn := map[string]bool{}
		for _, part := range rows[1:] {
			drawn[part.Cells[titleCell]] = true
		}
		for at := 1; at <= count; at++ {
			want := fmt.Sprintf("  tests for requirement %d", at)
			if !drawn[want] {
				return fmt.Errorf("the parts are drawn as %v, want %q indented under the job above it",
					drawn, want)
			}
		}
		return nil
	})
}

// initializeConsoleViewSteps registers the steps for the views this console gained: the stage on a
// job's row, the key that answers a job, and the listings of what the system holds.
func initializeConsoleViewSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator types "([^"]*)" into the command bar$`, func(ctx context.Context, typed string) error {
		return consoleFrom(ctx).openModelOn(worldFrom(ctx), typed)
	})

	sc.Step(`^the job's row says it is in the "([^"]*)" stage$`, func(ctx context.Context, stage string) error {
		row, err := onlyRow(consoleFrom(ctx))
		if err != nil {
			return err
		}
		if got := row.Cells[stageCell]; got != stage {
			return fmt.Errorf("the stage cell says %q, want %q", got, stage)
		}
		return nil
	})

	// The key an operator presses, on the console standing over the real control plane, and then the
	// answer and the listing that follows it fed back the way the runtime feeds them.
	sc.Step(`^the operator answers the job under the cursor with "([^"]*)"$`,
		func(ctx context.Context, said string) error {
			c := consoleFrom(ctx)
			if err := c.openModelOnJobs(worldFrom(ctx)); err != nil {
				return err
			}
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

	sc.Step(`^the console shows that job is no longer asking$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		view := consoleFrom(ctx).model.View()
		opening := strings.Join(strings.Fields(one.GetTitle())[:2], " ")
		for _, line := range strings.Split(view, "\n") {
			if !strings.Contains(line, opening) {
				continue
			}
			if strings.Contains(line, job.PhaseAsking) {
				return fmt.Errorf("the row still reads asking:\n%s", line)
			}
			return nil
		}
		return fmt.Errorf("the job is not on the screen at all:\n%s", view)
	})

	sc.Step(`^the operator opens the console on the "([^"]*)" view$`, func(ctx context.Context, view string) error {
		return consoleFrom(ctx).open(ctx, worldFrom(ctx).client, view)
	})

	// What the console lists against what the system says it holds, read a second time from the
	// control plane. A count written into the scenario would pass against a console listing the wrong
	// thing entirely.
	sc.Step(`^the console lists every (skill|role|hook) the system holds$`, func(ctx context.Context, held string) error {
		c := consoleFrom(ctx)
		names, err := whatTheSystemHolds(ctx, held)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return fmt.Errorf("the system holds no %ss, so this scenario proves nothing", held)
		}
		if len(c.rows) != len(names) {
			return fmt.Errorf("the console lists %d %ss and the system holds %d", len(c.rows), held, len(names))
		}
		for _, name := range names {
			if _, found := rowNamed(c.rows, name); !found {
				return fmt.Errorf("the console does not list the %s %q", held, name)
			}
		}
		return nil
	})

	// What the row says, read off the screen the operator has. The summary is the line the manifest
	// carries, so a cell holding anything else is a cell built from something the system did not say.
	sc.Step(`^the skill's row says what the skill is for$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		row, err := onlyRow(c)
		if err != nil {
			return err
		}
		held, err := worldFrom(ctx).client.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{})
		if err != nil {
			return err
		}
		if len(held.GetSkills()) != 1 {
			return fmt.Errorf("the system holds %d skills, want the one this scenario imported", len(held.GetSkills()))
		}
		summary := held.GetSkills()[0].GetSummary()
		if summary == "" {
			return fmt.Errorf("the system says nothing about what %q is for, so this proves nothing", row.ID)
		}
		if err := c.openModelOn(worldFrom(ctx), c.active.Name); err != nil {
			return err
		}
		if !strings.Contains(c.model.View(), strings.Join(strings.Fields(summary)[:4], " ")) {
			return fmt.Errorf("the screen does not say what the skill is for:\n%s", c.model.View())
		}
		return nil
	})

	// The other half, and the reason the row says only that: why a skill is held and not given belongs
	// to a workspace or to a session, and this listing is the system's own.
	sc.Step(`^no row says a skill is held and not given$`, func(ctx context.Context) error {
		held, err := worldFrom(ctx).client.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{})
		if err != nil {
			return err
		}
		for _, one := range held.GetSkills() {
			if one.GetLeftOut() != "" {
				return fmt.Errorf("the system's own listing says %q is left out, so the console could say why",
					one.GetName())
			}
		}
		for _, row := range consoleFrom(ctx).rows {
			for _, cell := range row.Cells {
				if strings.Contains(cell, "left out") {
					return fmt.Errorf("the row for %q claims a reason the system never sent: %q", row.ID, cell)
				}
			}
		}
		return nil
	})

	sc.Step(`^the console lists nothing, and says so$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if len(c.rows) != 0 {
			return fmt.Errorf("the console lists %d rows, want none", len(c.rows))
		}
		if err := c.openModelOn(worldFrom(ctx), c.active.Name); err != nil {
			return err
		}
		if !strings.Contains(c.model.View(), "nothing here") {
			return fmt.Errorf("an empty listing says nothing about being empty:\n%s", c.model.View())
		}
		return nil
	})
}

// stageCell is where a job's stage sits in its row, named so a step reads as the cell it is about
// rather than as a number in a slice.
const stageCell = 2

// whatTheSystemHolds asks the control plane again, rather than trusting the console's own answer, so
// the comparison is between two readings of one system.
func whatTheSystemHolds(ctx context.Context, held string) ([]string, error) {
	client := worldFrom(ctx).client
	switch held {
	case "skill":
		resp, err := client.ListSkills(ctx, &quaycrewv1.ListSkillsRequest{})
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(resp.GetSkills()))
		for _, one := range resp.GetSkills() {
			names = append(names, one.GetName())
		}
		return names, nil
	case "role":
		resp, err := client.ListRoles(ctx, &quaycrewv1.ListRolesRequest{})
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(resp.GetRoles()))
		for _, one := range resp.GetRoles() {
			names = append(names, one.GetName())
		}
		return names, nil
	case "hook":
		resp, err := client.ListHooks(ctx, &quaycrewv1.ListHooksRequest{})
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(resp.GetHooks()))
		for _, one := range resp.GetHooks() {
			names = append(names, one.GetName())
		}
		return names, nil
	}
	return nil, fmt.Errorf("nothing reads what the system holds as %q", held)
}

// initializeConsoleAnswerSteps reads the record the console's answer key wrote. It is a separate
// reading from the screen: one says the operator can see what happened, and this says the system
// holds it.
func initializeConsoleAnswerSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the system keeps "([^"]*)" as what the person wrote$`, func(ctx context.Context, said string) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		if one.GetPhase() == job.PhaseAsking {
			return fmt.Errorf("the job is still asking after being answered")
		}
		events, err := worldFrom(ctx).store.ListJobEvents(ctx, one.GetId())
		if err != nil {
			return err
		}
		for _, event := range events {
			if event.Kind == job.EventTold && strings.Contains(event.Detail, said) {
				return nil
			}
		}
		return fmt.Errorf("no record of that job says a person answered %q: the records read %v",
			said, eventKinds(events))
	})
}

// initializeConsoleJobSessionsSteps registers the steps for the conversations running under one job.
// A job in its test stage holds its own and one for each requirement its stage fanned out into, and
// this is the view that draws them.
func initializeConsoleJobSessionsSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the console draws one line for each session running under the job$`, func(ctx context.Context) error {
		one, err := readJob(ctx, 0)
		if err != nil {
			return err
		}
		runs, err := worldFrom(ctx).client.ListExecutions(ctx, &quaycrewv1.ListExecutionsRequest{
			Job: one.GetId(),
		})
		if err != nil {
			return err
		}
		running := []string{one.GetSession()}
		for _, run := range runs.GetExecutions() {
			running = append(running, run.GetSession())
		}

		rows := consoleFrom(ctx).model.Listed()
		view := consoleFrom(ctx).model.View()
		if len(rows) != len(running) {
			return fmt.Errorf("the console draws %d lines, want one for each of the %d sessions running "+
				"under the job:\n%s", len(rows), len(running), view)
		}
		// Read as a set, and by the identifier rather than by the row it landed on: which session is
		// drawn on which line is not something this view promises, so a step that indexed into the
		// order would pass by accident.
		for _, session := range running {
			on := 0
			for _, row := range rows {
				if row.ID == session {
					on++
				}
			}
			if on != 1 {
				return fmt.Errorf("session %s is on %d lines of the job, want one:\n%s",
					display.ShortID(session), on, view)
			}
			if !strings.Contains(view, display.ShortID(session)) {
				return fmt.Errorf("the screen does not name session %s:\n%s", display.ShortID(session), view)
			}
		}
		return nil
	})
}
