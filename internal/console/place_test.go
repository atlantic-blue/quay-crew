package console

import (
	"errors"
	"testing"
)

// aNotebook is a place store held in memory, so a scenario can close a console and open another one
// over the same system and read what carried across.
type aNotebook struct {
	held    Place
	written int
	loadErr error
	saveErr error
}

func (n *aNotebook) store() PlaceStore {
	return PlaceStore{
		Load: func() (Place, error) {
			if n.loadErr != nil {
				return Place{}, n.loadErr
			}
			return n.held, nil
		},
		Save: func(where Place) error {
			if n.saveErr != nil {
				return n.saveErr
			}
			n.held, n.written = where, n.written+1
			return nil
		},
	}
}

// openedRemembering is the console as the tool opens it: over one system, with somewhere to write
// down where it ends up, and resuming whatever is already written there.
func openedRemembering(t *testing.T, client *treeClient, notebook *aNotebook) Model {
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
	model = model.Remembering(notebook.store()).Resuming(resume(notebook.store()))
	return openRun(t, model)
}

// openRun is the console on the way up, without its refresh clock. Init also starts a three second
// timer, and a table test should not wait for it.
func openRun(t *testing.T, model Model) Model {
	t.Helper()
	return settle(t, model, model.Opening())
}

// The whole of what this is for: close the console three levels down, open it again, and land back
// where you were rather than at the top.
func TestTheConsoleOpensWhereItWasLastLeft(t *testing.T) {
	client := aSystemWithOneOfEverything()
	notebook := &aNotebook{}

	first := openedRemembering(t, client, notebook)
	first = walk(t, walk(t, first, enter()), enter())
	if first.active.Name != "jobs" {
		t.Fatalf("the first console ended on %q, want the jobs of a project", first.active.Name)
	}

	second := openedRemembering(t, client, notebook)

	if second.active.Name != "jobs" {
		t.Fatalf("the next console opened on %q, want where the last one was left", second.active.Name)
	}
	if where := second.Position(); where != "acme/house-bills" {
		t.Fatalf("the next console says it is at %q, want acme/house-bills", where)
	}
	// The screen, not the model: a console that remembers the level and draws nothing on it has
	// remembered nothing worth having.
	screenSays(t, second, "read the electricity bill", "jobs(acme/house-bills)")
	// And the way back still works from a level nobody drilled to in this run.
	second = walk(t, second, escape())
	if second.active.Name != "projects" {
		t.Fatalf("escape from a resumed level lands on %q, want the projects above it", second.active.Name)
	}
}

// Nothing remembered is the top, which is what a console being opened for the first time does.
func TestAConsoleWithNothingRememberedOpensAtTheTop(t *testing.T) {
	model := openedRemembering(t, aSystemWithOneOfEverything(), &aNotebook{})

	if model.active.Name != "workspaces" {
		t.Fatalf("a console with nothing remembered opened on %q, want the top", model.active.Name)
	}
	if where := model.Position(); where != "" {
		t.Fatalf("it says it is at %q, and the top is nowhere", where)
	}
}

// A remembered place naming a project somebody removed opens at the level above it rather than on a
// listing that promises rows and has none.
func TestAPlaceThatIsGoneOpensAtTheDeepestLevelStillThere(t *testing.T) {
	client := aSystemWithOneOfEverything()
	notebook := &aNotebook{}
	first := openedRemembering(t, client, notebook)
	first = walk(t, walk(t, first, enter()), enter())
	if first.active.Name != "jobs" {
		t.Fatalf("the first console ended on %q, want the jobs of a project", first.active.Name)
	}

	// The project is removed between the two runs, which is the case this is about.
	client.projects = nil

	second := openedRemembering(t, client, notebook)

	// The workspace is still there, so the walk gets that far and stops: the operator lands on the
	// projects of acme, which is empty, rather than on the jobs of a project that is gone.
	if second.active.Name != "projects" {
		t.Fatalf("with the project gone the console opened on %q, want the deepest level still there",
			second.active.Name)
	}
	if where := second.Position(); where != "acme" {
		t.Fatalf("it says it is at %q, want acme", where)
	}
	if second.err != nil {
		t.Fatalf("opening reported %v, and a project somebody removed is not a fault", second.err)
	}
	screenSays(t, second, "projects(acme)", "nothing here")

	// And escape still comes back to the workspaces from a level nobody drilled to in this run.
	second = walk(t, second, escape())
	if second.active.Name != "workspaces" {
		t.Fatalf("escape landed on %q, want the workspaces", second.active.Name)
	}
}

// The place is written whenever the level changes, including drilling from one workspace into
// another's projects, which does not change the view at all.
func TestThePlaceIsWrittenWheneverTheLevelChanges(t *testing.T) {
	client := aSystemWithOneOfEverything()
	notebook := &aNotebook{}
	model := openedRemembering(t, client, notebook)

	model = walk(t, model, enter())
	if notebook.written == 0 {
		t.Fatal("drilling wrote nothing down, so the next console opens at the top")
	}
	if notebook.held.View != "projects" {
		t.Fatalf("what was written down says %q, want the projects", notebook.held.View)
	}
	if len(notebook.held.Levels) != 1 || notebook.held.Levels[0].Typed != "acme" {
		t.Fatalf("what was written down carries %v, want the one workspace it drilled through",
			notebook.held.Levels)
	}

	// Coming back is a level change too, and one that has to shorten what was written.
	walk(t, model, escape())
	if notebook.held.View != "workspaces" || len(notebook.held.Levels) != 0 {
		t.Fatalf("after coming back the place says %q with %d levels, want the top",
			notebook.held.View, len(notebook.held.Levels))
	}
}

// Switching resource through the command bar is a level change as much as a drill is, so the next
// console opens on the listing somebody switched to.
func TestSwitchingResourceIsRememberedToo(t *testing.T) {
	client := aSystemWithOneOfEverything()
	notebook := &aNotebook{}
	model := openedRemembering(t, client, notebook)

	model, _ = update(t, model, runes(":"))
	model = typeAll(t, model, "sessions")
	walk(t, model, enter())

	if notebook.held.View != "sessions" {
		t.Fatalf("switching to the sessions listing wrote down %q", notebook.held.View)
	}
	second := openedRemembering(t, client, notebook)
	if second.active.Name != "sessions" {
		t.Fatalf("the next console opened on %q, want the listing that was switched to", second.active.Name)
	}
}

// A console that cannot read or write where it was is a console, not an error. Losing where somebody
// was costs one keystroke; refusing to open costs them the tool.
func TestAConsoleThatCannotRememberStillOpensAndStillWorks(t *testing.T) {
	client := aSystemWithOneOfEverything()

	broken := &aNotebook{loadErr: errors.New("unreadable"), saveErr: errors.New("read only")}
	model := openedRemembering(t, client, broken)
	if model.active.Name != "workspaces" {
		t.Fatalf("a console that cannot read its place opened on %q, want the top", model.active.Name)
	}
	model = walk(t, model, enter())
	if model.err != nil {
		t.Fatalf("drilling with an unwritable place reported %v, and it is not the operator's problem", model.err)
	}
	if model.active.Name != "projects" {
		t.Fatalf("drilling landed on %q, want the projects", model.active.Name)
	}

	// And a console given no store at all behaves the same way.
	none := openedOnTheTree(t, client)
	none = walk(t, none, enter())
	if none.active.Name != "projects" {
		t.Fatalf("a console with no place store landed on %q, want the projects", none.active.Name)
	}
}

// A place written by a build whose views have since been renamed opens at the top rather than
// refusing to open. It is read from a file an upgrade never rewrites.
func TestAPlaceNamingAViewThatIsGoneOpensAtTheTop(t *testing.T) {
	client := aSystemWithOneOfEverything()
	notebook := &aNotebook{held: Place{
		View:   "reticulations",
		Levels: []Level{{Resource: "workspaces", Row: "1111111111111111aaaaaaaa", Into: "acme", Typed: "acme"}},
	}}

	model := openedRemembering(t, client, notebook)

	if model.active.Name != "workspaces" {
		t.Fatalf("a place naming a view that is gone opened on %q, want the top", model.active.Name)
	}
	screenSays(t, model, "acme")
}

// The panel's header is told which view the resumed console landed on, or it names the keys of the
// view the console opened on and every hint on the screen is for somewhere else.
func TestAResumedConsoleTellsTheHeaderWhereItLanded(t *testing.T) {
	client := aSystemWithOneOfEverything()
	notebook := &aNotebook{}
	first := openedRemembering(t, client, notebook)
	if first = walk(t, walk(t, first, enter()), enter()); first.active.Name != "jobs" {
		t.Fatalf("the first console ended on %q, want the jobs of a project", first.active.Name)
	}

	var told []string
	registry, err := NewDefaultRegistry(client)
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	model, err := New(registry, Default, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model.width, model.height = 120, 30
	model = model.Remembering(notebook.store()).Resuming(resume(notebook.store())).
		WithViewPublisher(func(view string) error {
			told = append(told, view)
			return nil
		})
	model = openRun(t, model)

	if len(told) == 0 || told[len(told)-1] != "jobs" {
		t.Fatalf("the header was told %v, want the jobs level the console resumed to", told)
	}
}
