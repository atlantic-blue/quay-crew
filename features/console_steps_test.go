package features_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/console"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/cucumber/godog"
)

// consoleWorld is the console's own scenario state, kept beside the shared world rather than inside
// it so the console scenarios do not widen what every other scenario carries.
type consoleWorld struct {
	secondProject string
	registry      *console.Registry
	active        console.Resource
	rows          []console.Row
	// opened is the command the console would hand the terminal, and openErr the reason it would not.
	opened  *exec.Cmd
	openErr error
	// model is the real console, driven by keys, for the scenarios about what a key does.
	model console.Model
	// handedOver is every command the console gave the terminal, and terminalErr is what the terminal
	// answered with. Together they carry a key through to what the operator is left looking at, rather
	// than stopping at the command the key produced.
	handedOver  []*exec.Cmd
	terminalErr error
	// contextFile is a file a scenario wrote for the guided setup.s context stage to read.
	contextFile string
	// rung is how many times the console rang the bell, which is the part of the telling that reaches
	// somebody looking at another tab. A count rather than a flag: the whole rule is one ring for each
	// rise rather than one for each redraw.
	rung int
	// panes is the panel the console is in, and besideEach is the conversation that was open beside it
	// after each key, which is what the operator was looking at each time.
	panes      *panelPanes
	besideEach []string
}

type consoleKey struct{}

func consoleFrom(ctx context.Context) *consoleWorld {
	c, _ := ctx.Value(consoleKey{}).(*consoleWorld)
	return c
}

// lastLine is the bottom row of a rendered console, which is where the footer is. Asserting on the
// whole view would pass on a version string that turned up in a listing.
func lastLine(view string) string {
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	return lines[len(lines)-1]
}

// leftOf is the half of the footer that says where the operator is: everything before the build, with
// any mark left by a cut taken off, so a row that was truncated still compares against the whole one.
func leftOf(row string) string {
	if cut := strings.Index(row, "Version:"); cut >= 0 {
		row = row[:cut]
	}
	return strings.TrimRight(strings.TrimSuffix(strings.TrimRight(row, " "), "…"), " ")
}

// plain drops the escape sequences, so an assertion about what a row says is not defeated by how it
// is coloured.
func plain(line string) string {
	var out strings.Builder
	for at := 0; at < len(line); at++ {
		if line[at] == 0x1b {
			for at < len(line) && line[at] != 'm' {
				at++
			}
			continue
		}
		out.WriteByte(line[at])
	}
	return out.String()
}

// open builds the console.s registry against the live control plane and lists one resource.
func (c *consoleWorld) open(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, name string) error {
	registry, err := console.NewDefaultRegistry(client)
	if err != nil {
		return err
	}
	resource, found := registry.Get(name)
	if !found {
		return fmt.Errorf("the console has no resource named %q", name)
	}
	c.registry, c.active = registry, resource
	return c.list(ctx, "")
}

func (c *consoleWorld) list(ctx context.Context, parent string) error {
	rows, err := c.active.List(ctx, parent)
	if err != nil {
		return fmt.Errorf("list %s: %w", c.active.Name, err)
	}
	c.rows = rows
	return nil
}

// initializeConsoleSteps registers the console scenarios' steps. It is called from
// initializeScenario so the console keeps its steps in its own file.
// theWordmark is KREWE, the word a person types, in the block letters the console draws it in.
var theWordmark = []string{
	" ██  ▄█▀ ██▀▀▀█▄ ██▀▀▀▀▀ ██     ██ ██▀▀▀▀▀ ",
	" ██▀▀█▄  ██▀▀██  ██▀▀▀   ██ ▄█▄ ██ ██▀▀▀   ",
	" ▀▀   ▀▀ ▀▀   ▀▀ ▀▀▀▀▀▀▀  ▀▀   ▀▀  ▀▀▀▀▀▀▀ ",
}

func initializeConsoleSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, consoleKey{}, &consoleWorld{}), nil
	})

	sc.Step(`^the operator dispatches "([^"]*)" to the second workspace$`, func(ctx context.Context, text string) error {
		w := worldFrom(ctx)
		if w.secondWorkspaceID == "" {
			return fmt.Errorf("no second workspace was created")
		}
		return w.dispatch(ctx, w.secondWorkspaceID, "", text)
	})

	sc.Step(`^a second project named "([^"]*)"$`, func(ctx context.Context, name string) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		resp, err := w.client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Workspace: w.workspaceID, Name: name})
		if err != nil {
			return err
		}
		// The background's project is the one the other steps mean, so record this one separately.
		c.secondProject = resp.GetProject().GetId()
		return nil
	})

	sc.Step(`^the operator dispatches "([^"]*)" to the second project$`, func(ctx context.Context, text string) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		if c.secondProject == "" {
			return fmt.Errorf("no second project was created")
		}
		return w.dispatch(ctx, c.secondProject, "", text)
	})

	sc.Step(`^the operator drills into project "([^"]*)"$`, func(ctx context.Context, name string) error {
		return consoleFrom(ctx).drillInto(ctx, worldFrom(ctx).client, "projects", name)
	})

	// The header the operator is actually looking at, asserted whole. The console renders it from what
	// the control plane says about itself, so these drive the real thing rather than a description of
	// it.
	sc.Step(`^the operator looks at the console$`, func(ctx context.Context) error {
		return consoleFrom(ctx).openModel(worldFrom(ctx))
	})

	// The sessions listing, driven as the real console. It is where the scenarios about how a row is
	// drawn belong, because a workspace row has three cells and a session row has twelve.
	sc.Step(`^the operator looks at the sessions listing$`, func(ctx context.Context) error {
		return consoleFrom(ctx).openModelOn(worldFrom(ctx), "sessions")
	})

	sc.Step(`^the operator opens the console with a conversation beside it$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if err := c.openModel(worldFrom(ctx)); err != nil {
			return err
		}
		// Half the width, which is what a conversation on the right leaves the console.
		return c.press(tea.WindowSizeMsg{Width: 84, Height: 41})
	})

	sc.Step(`^the operator looks at the console (\d+) columns wide$`, func(ctx context.Context, columns int) error {
		c := consoleFrom(ctx)
		if err := c.openModel(worldFrom(ctx)); err != nil {
			return err
		}
		return c.press(tea.WindowSizeMsg{Width: columns, Height: 41})
	})

	// On the sessions listing, because the claim is that the panel carries this view's own keys, and
	// the workspaces view the console opens on binds none.
	sc.Step(`^the operator looks at the console and asks for help$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if err := c.openModelOn(worldFrom(ctx), "sessions"); err != nil {
			return err
		}
		// Tall enough for the whole panel, which is what a real terminal usually is. A shorter one
		// scrolls, and that is a table test in internal/console.
		if err := c.press(tea.WindowSizeMsg{Width: 170, Height: 60}); err != nil {
			return err
		}
		return c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	})

	sc.Step(`^the operator drills into a workspace$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if err := c.openModel(worldFrom(ctx)); err != nil {
			return err
		}
		if err := c.press(tea.WindowSizeMsg{Width: 170, Height: 41}); err != nil {
			return err
		}
		// The console opens on the workspaces, so one enter is one level down.
		return c.press(tea.KeyMsg{Type: tea.KeyEnter})
	})

	// The footer is the last line of the screen, so it is read as the last line rather than searched
	// for anywhere in the view. A version string that turned up in a row would satisfy a Contains over
	// the whole thing and say nothing about the footer.
	sc.Step(`^the footer (says where the operator is standing|says how to go back|says which build this is|says how to reach everything else|names the product)$`,
		func(ctx context.Context, claim string) error {
			c := consoleFrom(ctx)
			row := lastLine(c.model.View())
			want := map[string]string{
				"says where the operator is standing": c.model.Position(),
				"says how to go back":                 "esc to go back",
				"says which build this is":            "Version:",
				"says how to reach everything else":   "Help",
				"names the product":                   "Krewe",
			}[claim]
			if want == "" {
				return fmt.Errorf("the console says nothing for %q", claim)
			}
			if !strings.Contains(row, want) {
				return fmt.Errorf("the footer does not carry %q:\n%s", want, row)
			}
			return nil
		})

	// The left of the row is drawn first and whole, so a narrow window cuts it from the end like any
	// other line. What must never happen is the right half taking the front of the row: the assertion
	// is that the narrow row still begins the way the wide one does.
	sc.Step(`^the footer still says where the operator is standing$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		wide, _ := c.model.Update(tea.WindowSizeMsg{Width: 200, Height: 41})
		full := leftOf(plain(lastLine(wide.View())))
		narrow := leftOf(plain(lastLine(c.model.View())))
		if narrow == "" {
			return fmt.Errorf("the footer says nothing about where the operator is")
		}
		if !strings.HasPrefix(full, narrow) {
			return fmt.Errorf("the footer begins %q, and at full width it begins %q", narrow, full)
		}
		return nil
	})

	sc.Step(`^the footer carries (the build, help and the product|the build|nothing on the right)$`,
		func(ctx context.Context, expected string) error {
			row := lastLine(consoleFrom(ctx).model.View())
			has := func(text string) bool { return strings.Contains(row, text) }
			switch expected {
			case "the build, help and the product":
				if !has("Version:") || !has("Help") || !has("Krewe") {
					return fmt.Errorf("the footer dropped something it had room for:\n%s", row)
				}
			case "the build":
				if !has("Version:") {
					return fmt.Errorf("the footer dropped the build before the rest:\n%s", row)
				}
				if has("Krewe") || has("Help") {
					return fmt.Errorf("the footer kept more than the build:\n%s", row)
				}
			case "nothing on the right":
				for _, gone := range []string{"Version:", "Help", "Krewe"} {
					if has(gone) {
						return fmt.Errorf("the footer still carries %q at this width:\n%s", gone, row)
					}
				}
			}
			return nil
		})

	sc.Step(`^no wordmark is drawn anywhere on the screen$`, func(ctx context.Context) error {
		view := consoleFrom(ctx).model.View()
		for _, row := range theWordmark {
			if strings.Contains(view, row) {
				return fmt.Errorf("a row of the wordmark is still drawn:\n%s", view)
			}
		}
		return nil
	})

	// The whole point of taking the header out: three rows across the top became one row underneath.
	sc.Step(`^the console draws one row under the list$`, func(ctx context.Context) error {
		lines := strings.Split(consoleFrom(ctx).model.View(), "\n")
		if len(lines) < 3 {
			return fmt.Errorf("the console drew %d lines", len(lines))
		}
		// The panel's bottom edge, then the hairline, then the footer. Anything under the footer is a
		// second row the list is paying for.
		if !strings.Contains(lines[len(lines)-1], "Version:") {
			return fmt.Errorf("the last line is not the footer:\n%s", lines[len(lines)-1])
		}
		return nil
	})

	sc.Step(`^the help panel names the system it is pointed at$`, func(ctx context.Context) error {
		view := consoleFrom(ctx).model.View()
		for _, want := range []string{"this system", "Workspace", "Address"} {
			if !strings.Contains(view, want) {
				return fmt.Errorf("the help panel does not name %q:\n%s", want, view)
			}
		}
		return nil
	})

	sc.Step(`^the help panel names what the system is running$`, func(ctx context.Context) error {
		view := consoleFrom(ctx).model.View()
		for _, want := range []string{"Sandbox engine", "Store engine"} {
			if !strings.Contains(view, want) {
				return fmt.Errorf("the help panel does not name %q:\n%s", want, view)
			}
		}
		return nil
	})

	sc.Step(`^the help panel says what the keys on this view do$`, func(ctx context.Context) error {
		if !strings.Contains(consoleFrom(ctx).model.View(), "Open") {
			return fmt.Errorf("the help panel does not say what enter does")
		}
		return nil
	})

	sc.Step(`^the help panel never asks a question it has already answered$`, func(ctx context.Context) error {
		view := consoleFrom(ctx).model.View()
		if strings.Contains(view, "still asking what this control plane is running") {
			return fmt.Errorf("the help panel asks what the control plane is running, and it has been told:\n%s", view)
		}
		return nil
	})

	sc.Step(`^the operator opens the console$`, func(ctx context.Context) error {
		return consoleFrom(ctx).open(ctx, worldFrom(ctx).client, console.Default)
	})

	sc.Step(`^the operator opens the console on workspaces$`, func(ctx context.Context) error {
		return consoleFrom(ctx).open(ctx, worldFrom(ctx).client, "workspaces")
	})

	// The console opens on the tree now, so a scenario about the flat listing of every session says so.
	// That listing did not go anywhere: it is one word in the command bar.
	sc.Step(`^the operator opens the console on sessions$`, func(ctx context.Context) error {
		return consoleFrom(ctx).open(ctx, worldFrom(ctx).client, "sessions")
	})

	sc.Step(`^the operator drills into workspace "([^"]*)"$`, func(ctx context.Context, name string) error {
		return consoleFrom(ctx).drillInto(ctx, worldFrom(ctx).client, "workspaces", name)
	})

	sc.Step(`^the console lists (\d+) sessions?$`, func(ctx context.Context, want int) error {
		return expectRows(consoleFrom(ctx), "sessions", want)
	})

	// The command bar resolves what was typed, so this drives the same path a keystroke does rather
	// than asking the registry a question the operator never asks it.
	sc.Step(`^the operator opens the console by typing "([^"]*)"$`, func(ctx context.Context, typed string) error {
		c := consoleFrom(ctx)
		registry, err := console.NewDefaultRegistry(worldFrom(ctx).client)
		if err != nil {
			return err
		}
		resource, found := registry.Resolve(typed)
		if !found {
			return fmt.Errorf("the command bar does not know %q", typed)
		}
		c.registry, c.active = registry, resource
		return c.list(ctx, "")
	})

	sc.Step(`^typing "([^"]*)" in the console opens nothing$`, func(ctx context.Context, typed string) error {
		registry, err := console.NewDefaultRegistry(worldFrom(ctx).client)
		if err != nil {
			return err
		}
		if resource, found := registry.Resolve(typed); found {
			return fmt.Errorf("typing %q still opens %q, and that word is gone", typed, resource.Name)
		}
		return nil
	})

	sc.Step(`^the console is showing sessions$`, func(ctx context.Context) error {
		if got := consoleFrom(ctx).active.Name; got != "sessions" {
			return fmt.Errorf("the console is showing %q, want sessions", got)
		}
		return nil
	})

	// A session exists only once a task creates it, so a failing runner is how you get one with no
	// conversation behind it.
	sc.Step(`^a session whose first task failed$`, func(ctx context.Context) error {
		w := worldFrom(ctx)
		w.runner.failNext = true
		_ = w.dispatch(ctx, w.projectID, "", "this task fails")
		return nil
	})

	sc.Step(`^the operator presses enter on the selected session$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		row, err := onlyRow(c)
		if err != nil {
			return err
		}
		c.pressEnter(row)
		return nil
	})

	sc.Step(`^the console opens that session's conversation$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		if c.openErr != nil {
			return fmt.Errorf("enter was refused: %w", c.openErr)
		}
		if c.opened == nil {
			return fmt.Errorf("enter produced no command")
		}
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		// The conversation the task ran in, read back rather than written down: the system names one
		// before the task starts, and the name is a fresh identifier every time.
		ran, err := w.conversationOfFirstTask()
		if err != nil {
			return err
		}
		line := strings.Join(c.opened.Args, " ")
		for _, want := range []string{sandbox.ContainerName(current.sessionID), sandbox.OpenConversation, ran} {
			if !strings.Contains(line, want) {
				return fmt.Errorf("the command is %q, want it to carry %q", line, want)
			}
		}
		return nil
	})

	sc.Step(`^the operator opens the console and presses backspace on the session$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if err := c.openModelOn(worldFrom(ctx), "sessions"); err != nil {
			return err
		}
		return c.press(tea.KeyMsg{Type: tea.KeyBackspace})
	})

	sc.Step(`^the operator answers "([^"]*)"$`, func(ctx context.Context, answer string) error {
		return consoleFrom(ctx).press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(answer)})
	})

	sc.Step(`^the console asks whether to stop that session$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		current, err := w.lastTask()
		if err != nil {
			return err
		}
		view := c.model.View()
		want := "stop session " + display.ShortID(current.sessionID) + "?"
		if !strings.Contains(view, want) {
			return fmt.Errorf("the console does not ask %q:\n%s", want, view)
		}
		return nil
	})

	sc.Step(`^the operator opens the console and archives the session$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if err := c.openModelOn(worldFrom(ctx), "sessions"); err != nil {
			return err
		}
		if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")}); err != nil {
			return err
		}
		if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}); err != nil {
			return err
		}
		// The console asked; whichever view a later step reads is listed fresh from the control plane.
		return c.open(ctx, worldFrom(ctx).client, "sessions")
	})

	sc.Step(`^the archived view lists (\d+) sessions?$`, func(ctx context.Context, want int) error {
		c := consoleFrom(ctx)
		if err := c.open(ctx, worldFrom(ctx).client, "archived"); err != nil {
			return err
		}
		return expectRows(c, "archived", want)
	})

	sc.Step(`^the archived session still holds its conversation$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		row, err := onlyRow(c)
		if err != nil {
			return err
		}
		session, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: row.ID})
		if err != nil {
			return err
		}
		if session.GetSession().GetModelSessionId() == "" {
			return fmt.Errorf("the archived session has no conversation handle left")
		}
		return nil
	})

	// What the operator is left with, rather than what the key returned: the command opens the one
	// session on their screen, and the system can name the conversation it opens, so the history and
	// the cost of it belong to that session afterwards.
	sc.Step(`^the console opens a conversation the system can name$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		if c.openErr != nil {
			return fmt.Errorf("enter was refused: %w", c.openErr)
		}
		if c.opened == nil {
			return fmt.Errorf("enter produced no command")
		}
		row, err := onlyRow(c)
		if err != nil {
			return err
		}
		resp, err := w.client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: row.ID})
		if err != nil {
			return err
		}
		named := resp.GetSession().GetModelSessionId()
		if named == "" {
			return fmt.Errorf("the conversation that opened has no name, so nothing can be attributed to it")
		}
		line := strings.Join(c.opened.Args, " ")
		for _, want := range []string{sandbox.ContainerName(row.ID), sandbox.OpenConversation, named} {
			if !strings.Contains(line, want) {
				return fmt.Errorf("the command is %q, want it to carry %q", line, want)
			}
		}
		return nil
	})

	sc.Step(`^the console lists (\d+) projects?$`, func(ctx context.Context, want int) error {
		return expectRows(consoleFrom(ctx), "projects", want)
	})

	sc.Step(`^the console lists (\d+) workspaces?$`, func(ctx context.Context, want int) error {
		return expectRows(consoleFrom(ctx), "workspaces", want)
	})

	sc.Step(`^the console can drill from workspaces into projects$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if c.active.DrillTo != "projects" {
			return fmt.Errorf("workspaces drills into %q, want projects", c.active.DrillTo)
		}
		if _, found := c.registry.Get("projects"); !found {
			return fmt.Errorf("projects is not a registered resource, so the drill would dead end")
		}
		return nil
	})

	sc.Step(`^the console shows the session's workspace as "([^"]*)"$`, func(ctx context.Context, want string) error {
		c := consoleFrom(ctx)
		row, err := onlyRow(c)
		if err != nil {
			return err
		}
		// The workspace column is the second one the sessions resource declares.
		const workspaceColumn = 1
		if got := row.Cells[workspaceColumn]; got != want {
			return fmt.Errorf("the console shows the workspace as %q, want the name %q", got, want)
		}
		return nil
	})

	sc.Step(`^the console shows the session identifier shortened$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		row, err := onlyRow(c)
		if err != nil {
			return err
		}
		full, err := w.lastTask()
		if err != nil {
			return err
		}
		shown := row.Cells[0]
		if shown == full.sessionID {
			return fmt.Errorf("the console shows the whole identifier %q, want it shortened", shown)
		}
		if !strings.HasPrefix(full.sessionID, shown) {
			return fmt.Errorf("the shortened identifier %q is not a prefix of %q", shown, full.sessionID)
		}
		// The row must still carry the whole thing, or every action on it breaks.
		if row.ID != full.sessionID {
			return fmt.Errorf("the row carries %q, want the whole identifier %q", row.ID, full.sessionID)
		}
		return nil
	})

	sc.Step(`^a session's row carries more than one colour$`, func(ctx context.Context) error {
		view := consoleFrom(ctx).model.View()
		if colouredRow(view) != "" {
			return nil
		}
		return fmt.Errorf("no row on the screen carries more than one colour, so the whole listing has "+
			"to be read one row at a time:\n%s", view)
	})

	sc.Step(`^the row says how the session is doing in its status cell$`, func(ctx context.Context) error {
		view := consoleFrom(ctx).model.View()
		row := colouredRow(view)
		if row == "" {
			return fmt.Errorf("no coloured row to read a status from:\n%s", view)
		}
		// Whichever of them the task left behind. What is being said is that the word carries the
		// colour, not which word it is.
		for _, coloured := range []string{"\x1b[32midle", "\x1b[33mrunning", "\x1b[33mdispatching"} {
			if strings.Contains(row, coloured) {
				return nil
			}
		}
		return fmt.Errorf("the status cell carries no colour, so the row says nothing about how the "+
			"session is doing:\n%q", row)
	})

	sc.Step(`^the operator stops the selected session from the console$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		row, err := onlyRow(c)
		if err != nil {
			return err
		}
		for _, action := range c.active.Actions {
			if action.Label != "Stop" {
				continue
			}
			if action.Run == nil {
				return fmt.Errorf("the Stop action has nothing to run")
			}
			return action.Run(ctx, row)
		}
		return fmt.Errorf("the sessions view has no Stop action")
	})
}

// colouredRow is the first drawn line carrying two different cell colours, or empty when there is
// none. Two rather than one, because a line drawn entirely in its state has exactly one and would
// pass a case looking for any colour at all.
//
// The cursor line is skipped by this without being asked to: colour comes off the selected row on
// purpose, so it carries none.
func colouredRow(view string) string {
	for _, line := range strings.Split(view, "\n") {
		seen := map[string]bool{}
		for _, part := range strings.Split(line, "\x1b[38;5;")[1:] {
			code, _, found := strings.Cut(part, "m")
			if found {
				seen[code] = true
			}
		}
		if len(seen) > 1 {
			return line
		}
	}
	return ""
}

// drive runs a console command and feeds everything it produces back into the model, which is what
// the bubbletea runtime does with no terminal in the way. It is how a scenario can press a key
// against the real control plane and then assert on the store rather than on a double.
//
// A batch is unpacked rather than run whole, because one of the console's own commands is the three
// second refresh clock and a scenario should not wait for it.
func drive(model console.Model, cmd tea.Cmd) (console.Model, error) {
	if cmd == nil {
		return model, nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, inner := range msg {
			next, err := drive(model, inner)
			if err != nil {
				return model, err
			}
			model = next
		}
		return model, nil
	case nil:
		return model, nil
	default:
		updated, next := model.Update(msg)
		typed, isModel := updated.(console.Model)
		if !isModel {
			return model, fmt.Errorf("the console returned %T, want a console.Model", updated)
		}
		return drive(typed, next)
	}
}

// press sends one key through the console and runs whatever it asks for.
func (c *consoleWorld) press(key tea.Msg) error {
	updated, cmd := c.model.Update(key)
	typed, isModel := updated.(console.Model)
	if !isModel {
		return fmt.Errorf("the console returned %T, want a console.Model", updated)
	}
	next, err := drive(typed, cmd)
	if err != nil {
		return err
	}
	c.model = next
	return nil
}

// openModel builds the real console over the live control plane and loads its opening view. It asks
// for a refresh rather than calling Init, because Init also starts the refresh clock.
func (c *consoleWorld) openModel(w *world) error {
	client := w.client
	registry, err := console.NewDefaultRegistry(client)
	if err != nil {
		return err
	}
	// Where the operator is standing, which is what the console reads from its own context file and
	// what the header names.
	known := console.Info{
		Version: "test", Address: "bufconn",
		Workspace: w.workspaceName, Project: w.projectName,
	}
	model, err := console.New(registry, console.Default, console.InfoFrom(client, known))
	if err != nil {
		return err
	}
	// Ask the system what it is running, the same way the console does on the way up, so the header a
	// scenario reads is filled in from the control plane rather than being the empty one a console
	// shows before its first answer. Asked here rather than by running Init, because Init also starts
	// the three second refresh clock and a scenario should not wait for it.
	described, err := console.InfoFrom(client, known)(context.Background())
	if err != nil {
		return err
	}
	c.model = model.WithInfo(described).WithTerminal(recordingTerminal(c))
	return c.refresh()
}

// refresh reloads the active view, which is what the key for it does and what the clock does on its
// own every few seconds.
func (c *consoleWorld) refresh() error {
	return c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
}

// pressEnter runs whatever the active view has bound to enter on the single listed row, which is the
// same path a keypress takes. It records the command and the refusal rather than asserting on either,
// so the two scenarios can each say what they expect.
func (c *consoleWorld) pressEnter(row console.Row) {
	for _, action := range c.active.Actions {
		if !action.Bound("enter") || action.Shell == nil {
			continue
		}
		c.opened, c.openErr = action.Shell(row)
		return
	}
	c.openErr = fmt.Errorf("the %s view has nothing bound to enter", c.active.Name)
}

// onlyRow returns the single listed row, so a scenario asserting on "the session" cannot quietly
// pass by looking at the first of several.
func onlyRow(c *consoleWorld) (console.Row, error) {
	if c.registry == nil {
		return console.Row{}, fmt.Errorf("the console was not opened")
	}
	if len(c.rows) != 1 {
		return console.Row{}, fmt.Errorf("the console lists %d rows, want exactly 1", len(c.rows))
	}
	return c.rows[0], nil
}

// drillInto opens a resource, finds the named row, and descends into whatever that resource drills
// to, which is how the operator moves from a workspace to its projects and on to their sessions.
func (c *consoleWorld) drillInto(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, resource, name string) error {
	if err := c.open(ctx, client, resource); err != nil {
		return err
	}
	target, found := rowNamed(c.rows, name)
	if !found {
		return fmt.Errorf("the console does not list a %s row called %q", resource, name)
	}
	child, known := c.registry.Get(c.active.DrillTo)
	if !known {
		return fmt.Errorf("%s drills into %q, which is not registered", resource, c.active.DrillTo)
	}
	c.active = child
	return c.list(ctx, target.ID)
}

func expectRows(c *consoleWorld, resource string, want int) error {
	if c.registry == nil {
		return fmt.Errorf("the console was not opened")
	}
	if c.active.Name != resource {
		return fmt.Errorf("the console is showing %q, not %q", c.active.Name, resource)
	}
	if len(c.rows) != want {
		return fmt.Errorf("the console lists %d %s, want %d", len(c.rows), resource, want)
	}
	return nil
}

func rowNamed(rows []console.Row, name string) (console.Row, bool) {
	for _, row := range rows {
		for _, cell := range row.Cells {
			if strings.EqualFold(cell, name) {
				return row, true
			}
		}
	}
	return console.Row{}, false
}

// initializeTasksViewSteps registers the steps for the console's history view. They live here rather
// than with the other tasks steps because they drive the console's own reducer.
func initializeTasksViewSteps(sc *godog.ScenarioContext) {
	sc.Step(`^the operator asks for the selected session's history$`, func(ctx context.Context) error {
		return askForHistory(ctx, consoleFrom(ctx))
	})

	// The same key, from the view a finished run's thread ends up in. A run archives its own thread,
	// so this is the view the history of an automation is actually read from.
	sc.Step(`^the operator asks for the archived session's history$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		if err := c.open(ctx, worldFrom(ctx).client, "archived"); err != nil {
			return err
		}
		return askForHistory(ctx, c)
	})

	sc.Step(`^the console is showing tasks$`, func(ctx context.Context) error {
		if got := consoleFrom(ctx).active.Name; got != "tasks" {
			return fmt.Errorf("the console is showing %q, want tasks", got)
		}
		return nil
	})

	sc.Step(`^the history lists (\d+) tasks? saying "([^"]*)"$`, func(ctx context.Context, want int, asked string) error {
		c := consoleFrom(ctx)
		if len(c.rows) != want {
			return fmt.Errorf("the history lists %d tasks, want %d", len(c.rows), want)
		}
		if len(c.rows) == 0 {
			return nil
		}
		// Column 2 is what was asked; column 3 is what came back.
		if got := c.rows[0].Cells[2]; got != asked {
			return fmt.Errorf("the first task says %q was asked, want %q", got, asked)
		}
		if c.rows[0].Cells[3] == "" {
			return fmt.Errorf("the first task shows nothing as the answer")
		}
		return nil
	})

	// Rule 46 in this repository: when a key gains a neighbour, test that the neighbour did not take
	// its place. Enter opening the conversation is the thing this view is for.
	sc.Step(`^enter on a session still opens its conversation rather than its history$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		opens := false
		for _, action := range c.active.Actions {
			if !action.Bound("enter") {
				continue
			}
			// Every action bound to enter, not the first one: an action added ahead of the opener
			// would take the key, and one added behind it would be dead. Both are wrong.
			if action.Descend != "" {
				return fmt.Errorf("an action bound to enter descends into %q, so enter no longer means open", action.Descend)
			}
			if action.Shell != nil {
				opens = true
			}
		}
		if !opens {
			return fmt.Errorf("the %s view no longer opens anything on enter", c.active.Name)
		}
		return nil
	})
}

// askForHistory presses the key the current view binds to a history and descends into whatever it
// names, which is how an operator reaches the tasks of the row they are sitting on.
func askForHistory(ctx context.Context, c *consoleWorld) error {
	row, err := onlyRow(c)
	if err != nil {
		return err
	}
	for _, action := range c.active.Actions {
		if !action.Bound("t") {
			continue
		}
		if action.Descend == "" {
			return fmt.Errorf("the key bound to t on %s descends into nothing", c.active.Name)
		}
		resource, found := c.registry.Get(action.Descend)
		if !found {
			return fmt.Errorf("%s descends into %q, which is not registered", c.active.Name, action.Descend)
		}
		c.active = resource
		return c.list(ctx, row.ID)
	}
	return fmt.Errorf("the %s view has nothing bound to t", c.active.Name)
}

// openModelOn stands the real console up and walks it to a view the way an operator does: colon, the
// name, enter. Typed rather than opened there, because the command bar is how anybody reaches a view
// that is not the one the console opens on.
func (c *consoleWorld) openModelOn(w *world, view string) error {
	if err := c.openModel(w); err != nil {
		return err
	}
	if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")}); err != nil {
		return err
	}
	for _, letter := range view {
		if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{letter}}); err != nil {
			return err
		}
	}
	return c.press(tea.KeyMsg{Type: tea.KeyEnter})
}

// panelPanes is the console in a panel: its own pane, and a conversation beside it. It records what
// each pane runs, so a scenario reads the conversation the operator is looking at rather than the
// command the key produced. That distinction is the whole of this defect.
type panelPanes struct {
	made    int
	running map[string][]string
	beside  string
}

func aConversationBesideTheConsole(what []string) *panelPanes {
	panes := &panelPanes{made: 1, running: map[string][]string{"%1": what}, beside: "%1"}
	return panes
}

func (p *panelPanes) Beside(string) (string, bool) { return p.beside, p.beside != "" }

func (p *panelPanes) Open(_ string, argv []string) (string, error) {
	p.made++
	pane := fmt.Sprintf("%%%d", p.made)
	p.running[pane] = argv
	p.beside = pane
	return pane, nil
}

func (p *panelPanes) Close(pane string) error {
	delete(p.running, pane)
	p.beside = ""
	return nil
}

// showing is the conversation open beside the console right now.
func (p *panelPanes) showing() string {
	if p.beside == "" {
		return "no conversation is open beside the console"
	}
	return strings.Join(p.running[p.beside], " ")
}

// conversationOfSession is what the command line makes of the session the console hands over: the
// conversation that session holds, asked of the system rather than written down. It is the shape
// `krewe` builds for the pane, and handing over nothing asks for the driver.
func conversationOfSession(w *world) func(string) ([]string, error) {
	return func(selected string) ([]string, error) {
		if selected == "" {
			return []string{"krewe", "attach", "the-driver"}, nil
		}
		spec, err := w.client.AttachSession(context.Background(), &quaycrewv1.AttachSessionRequest{Id: selected})
		if err != nil {
			return nil, err
		}
		return append([]string{"krewe", "attach", selected}, spec.GetArgv()...), nil
	}
}

func initializeConversationBesideSteps(sc *godog.ScenarioContext) {
	// tmux names the pane every command it starts is in, and that is how the console knows it is in a
	// panel at all. Put back afterwards, because a scenario that leaves it set would tell every later
	// one it is in a panel it is not in.
	sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
		return ctx, os.Unsetenv("TMUX_PANE")
	})

	sc.Step(`^the operator opens the console beside a conversation$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		if err := os.Setenv("TMUX_PANE", "%0"); err != nil {
			return err
		}
		c.panes = aConversationBesideTheConsole([]string{"krewe", "attach", "the-driver"})
		if err := c.openModelOn(w, "sessions"); err != nil {
			return err
		}
		c.model = c.model.Beside(conversationOfSession(w)).WithPanes(c.panes)
		return nil
	})

	sc.Step(`^the operator presses enter on each session in turn$`, func(ctx context.Context) error {
		c := consoleFrom(ctx)
		listed := c.model.Listed()
		if len(listed) != 3 {
			return fmt.Errorf("the console lists %d sessions, and this is about telling three apart", len(listed))
		}
		c.opened, c.openErr = nil, nil
		for at := range listed {
			if err := c.pressAt(at, tea.KeyMsg{Type: tea.KeyEnter}); err != nil {
				return err
			}
			c.besideEach = append(c.besideEach, c.panes.showing())
		}
		return nil
	})

	sc.Step(`^the conversation beside the console was that session's own each time$`, func(ctx context.Context) error {
		w, c := worldFrom(ctx), consoleFrom(ctx)
		listed := c.model.Listed()
		if len(c.besideEach) != len(listed) {
			return fmt.Errorf("%d sessions were opened and %d rows were listed", len(c.besideEach), len(listed))
		}
		for at, row := range listed {
			spec, err := w.client.AttachSession(context.Background(), &quaycrewv1.AttachSessionRequest{Id: row.ID})
			if err != nil {
				return fmt.Errorf("asking the system for %s's conversation: %w", row.ID, err)
			}
			held := strings.Join(spec.GetArgv(), " ")
			if !strings.Contains(c.besideEach[at], held) {
				return fmt.Errorf("the cursor was on %s, whose conversation is %q, and the console was "+
					"beside %q", row.ID, held, c.besideEach[at])
			}
		}
		return nil
	})
}

// pressAt puts the cursor on one row and sends a key, which is what an operator does when they move
// down a listing and act on what they land on.
func (c *consoleWorld) pressAt(at int, key tea.Msg) error {
	if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}); err != nil {
		return err
	}
	if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")}); err != nil {
		return err
	}
	for range at {
		if err := c.press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}); err != nil {
			return err
		}
	}
	return c.press(key)
}
