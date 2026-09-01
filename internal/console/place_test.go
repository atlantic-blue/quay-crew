package console

import (
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	tea "github.com/charmbracelet/bubbletea"
)

// heldPlace is a place store kept in memory, standing in for the file the command line writes.
type heldPlace struct {
	where   Place
	writes  int
	refuses bool
}

func (h *heldPlace) store() PlaceStore {
	return PlaceStore{
		Load: func() (Place, error) { return h.where, nil },
		Save: func(where Place) error {
			h.writes++
			if h.refuses {
				return errCannotWrite
			}
			h.where = where
			return nil
		},
	}
}

var errCannotWrite = &writeRefused{}

type writeRefused struct{}

func (*writeRefused) Error() string { return "nowhere to write it" }

// runAll drives a command the way the runtime does, including a batch: bubbletea runs every command
// in a tea.BatchMsg, and a helper that stops at the first one never reaches the write that records
// where the console is standing. That is the whole feature here, so it cannot be skipped.
func runAll(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	return drain(t, model, cmd, 0)
}

func drain(t *testing.T, model Model, cmd tea.Cmd, depth int) Model {
	t.Helper()
	if cmd == nil || depth > 8 {
		return model
	}
	msg := cmd()
	if msg == nil {
		return model
	}
	if batch, isBatch := msg.(tea.BatchMsg); isBatch {
		for _, one := range batch {
			model = drain(t, model, one, depth+1)
		}
		return model
	}
	next, produced := update(t, model, msg)
	return drain(t, next, produced, depth+1)
}

// step drives one key and runs everything it asked for.
func step(t *testing.T, model Model, key tea.KeyMsg) Model {
	t.Helper()
	next, cmd := update(t, model, key)
	return runAll(t, next, cmd)
}

// openedRemembering is the console with somewhere to keep its address, opened the way the runtime
// opens it: the first listing landed, and anything it was told to resume to walked back down.
func openedRemembering(t *testing.T, client *treeClient, held *heldPlace, resume Place) Model {
	t.Helper()
	registry, err := NewDefaultRegistry(client)
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	model, err := New(registry, Default, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model.width, model.height = 120, 30
	model = model.Remembering(held.store()).Resuming(resume)
	return runAll(t, model, model.Opening())
}

// The whole of it: walk down, and the next console opens where the last one was left. Asserted on the
// screen the second console draws, not on what was written to the store, because a place written and
// never walked back is a place nobody comes back to.
func TestTheConsoleOpensWhereItWasLeft(t *testing.T) {
	client := aSystemWithOneOfEverything()
	held := &heldPlace{}

	first := openedRemembering(t, client, held, Place{})
	first = step(t, first, enter())
	first = step(t, first, enter())
	if got := first.Position(); got != "acme/house-bills" {
		t.Fatalf("the first console ended at %q, want the jobs of house-bills", got)
	}

	next := openedRemembering(t, client, held, held.where)
	if got := next.Position(); got != "acme/house-bills" {
		t.Fatalf("the next console opened at %q, want where the last one was left", got)
	}
	screenSays(t, next, "<jobs>", "read the electricity bill", "esc to go back")
}

// Opening at the top is the default, so a console that has never been opened is not a console that
// refuses to open.
func TestAConsoleWithNothingRememberedOpensAtTheTop(t *testing.T) {
	model := openedRemembering(t, aSystemWithOneOfEverything(), &heldPlace{}, Place{})

	if got := model.Position(); got != "" {
		t.Fatalf("a console with nothing remembered opened at %q", got)
	}
	screenSays(t, model, "<workspaces>", "acme")
	// Nothing to go back to, so nothing is offered.
	if strings.Contains(model.View(), "esc to go back") {
		t.Fatalf("the top of the tree offers a way back:\n%s", model.View())
	}
}

// The way back has to work from a resumed place too. A console that walked four levels down on the
// way up, and then cannot come back, has stranded whoever opened it.
func TestTheWayBackWorksFromAPlaceTheConsoleResumedInto(t *testing.T) {
	client := aSystemWithOneOfEverything()
	held := &heldPlace{}

	deep := openedRemembering(t, client, held, Place{})
	for range 3 {
		deep = step(t, deep, enter())
	}
	if got := deep.Position(); got != "acme/house-bills/33333333" {
		t.Fatalf("the first console ended at %q", got)
	}

	resumed := openedRemembering(t, client, held, held.where)
	screenSays(t, resumed, "<tasks>")
	for _, want := range []string{"acme/house-bills", "acme", ""} {
		resumed = step(t, resumed, escape())
		if got := resumed.Position(); got != want {
			t.Fatalf("escape out of a resumed place left the address at %q, want %q", got, want)
		}
	}
	screenSays(t, resumed, "<workspaces>")
}

// The project somebody was standing in may have been removed since. Opening on it would list nothing
// under a heading that promises something, which reads as a broken console rather than as a project
// that is gone, so the walk stops at the last level still there.
func TestAPlaceWhoseLevelIsGoneStopsAtTheLastOneStillThere(t *testing.T) {
	client := aSystemWithOneOfEverything()
	held := &heldPlace{}

	deep := openedRemembering(t, client, held, Place{})
	deep = step(t, deep, enter())
	// The second drill is what puts the project on the place, which is the level this removes.
	if deep = step(t, deep, enter()); deep.Position() != "acme/house-bills" {
		t.Fatalf("the first console ended at %q", deep.Position())
	}

	// The project goes, and with it the job under it.
	client.projects = nil
	client.jobs = nil

	resumed := openedRemembering(t, client, held, held.where)
	if got := resumed.Position(); got != "acme" {
		t.Fatalf("the walk stopped at %q, want the workspace that is still there", got)
	}
	screenSays(t, resumed, "<projects>", "esc to go back")
}

// A whole place that names nothing this build has leaves the console at the top rather than empty.
func TestAPlaceNamingAViewThatIsGoneOpensAtTheTop(t *testing.T) {
	model := openedRemembering(t, aSystemWithOneOfEverything(), &heldPlace{},
		Place{View: "gantt-charts"})

	if got := model.ViewName(); got != Default {
		t.Fatalf("the console opened on %q, want %q", got, Default)
	}
	screenSays(t, model, "<workspaces>")
}

// Drilling from one workspace into another's projects leaves the view alone and moves the operator, so
// a console that wrote its place down only when the view changed would never write this one.
func TestMovingWithinOneViewIsStillWrittenDown(t *testing.T) {
	client := aSystemWithOneOfEverything()
	client.workspaces = append(client.workspaces, &quaycrewv1.Workspace{
		Id: "6666666666666666ffffffff", Name: "beta", CreatedAt: client.workspaces[0].GetCreatedAt(),
	})
	client.projects = append(client.projects, &quaycrewv1.Project{
		Id: "7777777777777777gggggggg", Workspace: "6666666666666666ffffffff",
		Name: "other-thing", CreatedAt: client.workspaces[0].GetCreatedAt(),
	})
	held := &heldPlace{}

	model := openedRemembering(t, client, held, Place{})
	model = step(t, model, enter())
	first := held.where

	model = step(t, model, escape())
	model = step(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	model = step(t, model, enter())

	if held.where.Same(first) {
		t.Fatalf("the console wrote down the same place for two different workspaces: %+v", held.where)
	}
	if got := model.Position(); got != "beta" {
		t.Fatalf("the second drill landed at %q, want the other workspace", got)
	}
}

// A place that cannot be written is dropped. The console is usable without one, and an error screen
// over a console that works is worse than losing where somebody was.
func TestAConsoleThatCannotWriteItsPlaceStillWorks(t *testing.T) {
	held := &heldPlace{refuses: true}
	model := openedRemembering(t, aSystemWithOneOfEverything(), held, Place{})

	model = step(t, model, enter())
	if held.writes == 0 {
		t.Fatal("the console never tried to write its place down")
	}
	if model.err != nil {
		t.Fatalf("a place that could not be written put an error on the screen: %v", model.err)
	}
	screenSays(t, model, "<projects>", "house-bills")
}
