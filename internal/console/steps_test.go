package console

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// aRunWithSteps is one project holding a run and the three jobs it declared, which is what a flow run
// looks like on the row: a root with children under it.
func aRunWithSteps() *treeClient {
	client := aSystemWithOneOfEverything()
	client.jobs[0].Title = "ship the reader"
	for at, title := range []string{"read the design", "write the code", "review it"} {
		client.jobs = append(client.jobs, &quaycrewv1.Job{
			Id:        strings.Repeat("d", 16) + "0000000" + string(rune('1'+at)),
			Workspace: "1111111111111111aaaaaaaa", Project: "2222222222222222bbbbbbbb",
			Parent: "3333333333333333cccccccc", Depth: 1,
			Title: title, Phase: job.PhaseDone,
		})
	}
	return client
}

// Inside a project, a run and the jobs it declared side by side read as unrelated work. The project
// lists what was declared, and the steps are under the run.
func TestAProjectListsTheJobsThatWereDeclaredAndNotTheStepsOfARun(t *testing.T) {
	client := aRunWithSteps()
	model := openedOnTheTree(t, client)
	model = walk(t, walk(t, model, enter()), enter())

	if got := len(model.Listed()); got != 1 {
		t.Fatalf("the project lists %d jobs, want the one that was declared: %v", got, model.Listed())
	}
	screenSays(t, model, "ship the reader")
	screenDoesNotSay(t, model, "write the code")
}

// The count is what says a row is a run rather than a single piece of work, so the steps are worth
// pressing a key for.
func TestARunSaysHowManyStepsItDeclared(t *testing.T) {
	client := aRunWithSteps()
	model := openedOnTheTree(t, client)
	model = walk(t, walk(t, model, enter()), enter())

	run, hasRow := model.Selected()
	if !hasRow {
		t.Fatal("the project lists nothing, so there is no run to read a count off")
	}
	// The mark, job, phase, outcome, role, title, session, steps, attempts, age.
	if run.Cells[7] != "3" {
		t.Fatalf("the run says it declared %q steps, want 3", run.Cells[7])
	}
	// A job that declared nothing says so rather than leaving a hole in the row. The literal rather
	// than the constant, because a case reading the constant passes against it emptied out.
	alone := jobRow(aJob("1111111111111111aaaaaaaa", job.PhaseDone, nil), 0)
	if alone.Cells[7] != "-" {
		t.Fatalf(`a job that declared nothing says %q in the steps cell, want "-"`, alone.Cells[7])
	}
}

// The key, carried through to the screen it leaves the operator on: the three jobs the run declared,
// listed, with the way back still working.
func TestTheStepsOfARunAreOneKeyAwayAndComeBackOnEscape(t *testing.T) {
	client := aRunWithSteps()
	model := openedOnTheTree(t, client)
	model = walk(t, walk(t, model, enter()), enter())

	model = walk(t, model, runes("S"))

	if model.active.Name != "steps" {
		t.Fatalf("S on a run opened %q, want the jobs it declared", model.active.Name)
	}
	if model.parent != "3333333333333333cccccccc" {
		t.Fatalf("the steps are scoped to %q, want the run", model.parent)
	}
	if got := len(model.Listed()); got != 3 {
		t.Fatalf("the steps listing shows %d rows, want the three the run declared", got)
	}
	screenSays(t, model, "read the design", "write the code", "review it")
	// The run itself is not one of its own steps. It is still named in the breadcrumb, which is what
	// says where escape goes back to, so this reads the rows rather than the whole screen.
	for _, one := range model.Listed() {
		if one.Name() == "ship the reader" {
			t.Fatal("the run is listed among its own steps")
		}
	}

	model = walk(t, model, escape())
	if model.active.Name != "jobs" {
		t.Fatalf("escape from the steps lands on %q, want the jobs it came from", model.active.Name)
	}
	screenSays(t, model, "ship the reader")
}

// A step is a job, so everything a job row can do a step row can do: enter still opens the work
// running under it.
func TestAStepIsStillAJobAndOpensItsOwnWork(t *testing.T) {
	client := aRunWithSteps()
	client.jobs[1].Session = "4444444444444444dddddddd"
	model := openedOnTheTree(t, client)
	model = walk(t, walk(t, model, enter()), enter())
	model = walk(t, model, runes("S"))

	model = walk(t, model, enter())

	if model.active.Name != "tasks" {
		t.Fatalf("enter on a step opened %q, want the work running under it", model.active.Name)
	}
	if model.parent != "4444444444444444dddddddd" {
		t.Fatalf("the work is scoped to %q, want the step's session", model.parent)
	}
}

// The flat listing is the flat one. A jobs listing that hid every step would be answering a question
// nobody asked it.
func TestTheFlatJobsListingStillHoldsEveryJob(t *testing.T) {
	client := aRunWithSteps()

	rows, err := Jobs(client).List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing every job: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("the flat listing holds %d jobs, want the run and its three steps", len(rows))
	}
}

// The steps view opened on its own has no job to be the steps of, and says so rather than listing
// nothing under a heading that promises something.
func TestTheStepsViewOpenedOnItsOwnSaysWhatItNeeds(t *testing.T) {
	_, err := Steps(aRunWithSteps()).List(context.Background(), "")
	if err == nil {
		t.Fatal("the steps view listed with no job to be the steps of, which is an empty screen with no reason on it")
	}
	if !strings.Contains(err.Error(), "jobs listing") {
		t.Fatalf("the refusal says %q, want it to name where steps are opened from", err)
	}
}

func TestTheStepsViewIsRegisteredAndAnswersToWhatFingersType(t *testing.T) {
	registry, err := NewDefaultRegistry(aRunWithSteps())
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	for _, typed := range []string{"steps", "step", "children"} {
		resource, found := registry.Resolve(typed)
		if !found {
			t.Fatalf("typing %q opens nothing", typed)
		}
		if resource.Name != "steps" {
			t.Fatalf("typing %q opens %q, want steps", typed, resource.Name)
		}
	}
}

// A run with steps under it is still a job, so the whole walk down and back has to survive one more
// level in the middle of it.
func TestTheWayBackWorksFromInsideARunsSteps(t *testing.T) {
	client := aRunWithSteps()
	client.jobs[1].Session = "4444444444444444dddddddd"
	model := openedOnTheTree(t, client)
	model = walk(t, walk(t, model, enter()), enter())
	model = walk(t, model, runes("S"))
	model = walk(t, model, enter())

	for _, want := range []string{"steps", "jobs", "projects", "workspaces"} {
		model = walk(t, model, escape())
		if model.active.Name != want {
			t.Fatalf("coming back landed on %q, want %q", model.active.Name, want)
		}
	}
	if model.quitting {
		t.Fatal("coming back to the top quit the console")
	}
}

// A window too narrow for the widest row still draws the steps level, and never wider than it is.
func TestTheStepsLevelFitsANarrowWindow(t *testing.T) {
	client := aRunWithSteps()
	model := openedOnTheTree(t, client)
	model = walk(t, walk(t, model, enter()), enter())
	model = walk(t, model, runes("S"))
	sized, _ := update(t, model, tea.WindowSizeMsg{Width: 40, Height: 20})

	for _, line := range strings.Split(sized.View(), "\n") {
		if width := lipgloss.Width(line); width > 40 {
			t.Fatalf("a line is %d wide in a window of 40: %q", width, line)
		}
	}
}
