package console

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// treeClient answers every call the four levels make, so a scenario can walk the whole tree against
// one system rather than four doubles that cannot disagree with each other.
type treeClient struct {
	quaycrewv1.ControlPlaneServiceClient

	workspaces []*quaycrewv1.Workspace
	projects   []*quaycrewv1.Project
	jobs       []*quaycrewv1.Job
	sessions   []*quaycrewv1.Session
	tasks      []*quaycrewv1.Task
}

func (t *treeClient) ListWorkspaces(context.Context, *quaycrewv1.ListWorkspacesRequest, ...grpc.CallOption) (*quaycrewv1.ListWorkspacesResponse, error) {
	return &quaycrewv1.ListWorkspacesResponse{Workspaces: t.workspaces}, nil
}

func (t *treeClient) ListProjects(_ context.Context, req *quaycrewv1.ListProjectsRequest, _ ...grpc.CallOption) (*quaycrewv1.ListProjectsResponse, error) {
	matched := make([]*quaycrewv1.Project, 0, len(t.projects))
	for _, project := range t.projects {
		if req.GetWorkspace() == "" || project.GetWorkspace() == req.GetWorkspace() {
			matched = append(matched, project)
		}
	}
	return &quaycrewv1.ListProjectsResponse{Projects: matched}, nil
}

// Every narrowing the real control plane applies is applied here. A double that answers a narrowed
// request with everything is looser than the system it stands in for, and a view built against it
// passes while the real one is wrong.
func (t *treeClient) ListJobs(_ context.Context, req *quaycrewv1.ListJobsRequest, _ ...grpc.CallOption) (*quaycrewv1.ListJobsResponse, error) {
	matched := make([]*quaycrewv1.Job, 0, len(t.jobs))
	for _, one := range t.jobs {
		if req.GetProject() != "" && one.GetProject() != req.GetProject() {
			continue
		}
		if req.GetPhase() != "" && one.GetPhase() != req.GetPhase() {
			continue
		}
		matched = append(matched, one)
	}
	return &quaycrewv1.ListJobsResponse{Jobs: matched}, nil
}

func (t *treeClient) ListSessions(_ context.Context, req *quaycrewv1.ListSessionsRequest, _ ...grpc.CallOption) (*quaycrewv1.ListSessionsResponse, error) {
	matched := make([]*quaycrewv1.Session, 0, len(t.sessions))
	for _, session := range t.sessions {
		if req.GetProject() == "" || session.GetProject() == req.GetProject() {
			matched = append(matched, session)
		}
	}
	return &quaycrewv1.ListSessionsResponse{Sessions: matched}, nil
}

func (t *treeClient) ListTasks(_ context.Context, req *quaycrewv1.ListTasksRequest, _ ...grpc.CallOption) (*quaycrewv1.ListTasksResponse, error) {
	matched := make([]*quaycrewv1.Task, 0, len(t.tasks))
	for _, task := range t.tasks {
		if task.GetSession() == req.GetSession() {
			matched = append(matched, task)
		}
	}
	return &quaycrewv1.ListTasksResponse{Tasks: matched}, nil
}

func (t *treeClient) AttachSession(context.Context, *quaycrewv1.AttachSessionRequest, ...grpc.CallOption) (*quaycrewv1.AttachSessionResponse, error) {
	return &quaycrewv1.AttachSessionResponse{Sandbox: "krewe-s1", Argv: []string{"claude", "--resume", "c1"}}, nil
}

// aSystemWithOneOfEverything is one workspace holding one project holding one job that ran one task,
// which is the shortest path from the top of the tree to the bottom of it.
func aSystemWithOneOfEverything() *treeClient {
	made := timestamppb.New(time.Now().Add(-90 * time.Second))
	return &treeClient{
		workspaces: []*quaycrewv1.Workspace{{Id: "1111111111111111aaaaaaaa", Name: "acme", CreatedAt: made}},
		projects: []*quaycrewv1.Project{{
			Id: "2222222222222222bbbbbbbb", Workspace: "1111111111111111aaaaaaaa",
			Name: "house-bills", CreatedAt: made,
		}},
		jobs: []*quaycrewv1.Job{{
			Id: "3333333333333333cccccccc", Workspace: "1111111111111111aaaaaaaa",
			Project: "2222222222222222bbbbbbbb", Title: "read the electricity bill",
			Role: "backlog-clearer", Phase: job.PhaseRunning, Session: "4444444444444444dddddddd",
			CreatedAt: made,
		}},
		sessions: []*quaycrewv1.Session{{
			Id: "4444444444444444dddddddd", Workspace: "1111111111111111aaaaaaaa",
			Project: "2222222222222222bbbbbbbb", Status: "idle",
		}},
		tasks: []*quaycrewv1.Task{{
			Id: "5555555555555555eeeeeeee", Session: "4444444444444444dddddddd",
			Status: "done", Prompt: "read the electricity bill", Reply: "it is due on the 14th",
			OccurredAt: timestamppb.New(time.Now()),
		}},
	}
}

// openedOnTheTree is the console as an operator meets it: the resources the tree is made of, opened
// on whichever one the tool opens on, with the first listing already landed.
func openedOnTheTree(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) Model {
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
	return settle(t, model, listCmd(model.active, model.parent))
}

// settle runs the command a key produced and feeds what came back in, the way the runtime does, so a
// scenario reads the screen the operator is left with rather than the intent of the keystroke. It
// keeps going while each message produces another, which is how a drill that lists then clamps lands.
func settle(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	for range 8 {
		if cmd == nil {
			return model
		}
		msg := cmd()
		if msg == nil {
			return model
		}
		model, cmd = update(t, model, msg)
	}
	t.Fatal("the console kept producing commands, so it never settled on a screen")
	return model
}

// press drives one key and settles whatever it asked for.
func walk(t *testing.T, model Model, key tea.KeyMsg) Model {
	t.Helper()
	next, cmd := update(t, model, key)
	return settle(t, next, cmd)
}

func enter() tea.KeyMsg  { return tea.KeyMsg{Type: tea.KeyEnter} }
func escape() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEsc} }

// screenSays fails when the drawn console does not carry the text, naming what it drew instead. It
// reads View rather than the rows, because what the rows hold and what a person sees are two
// different claims and only the second one is the feature.
func screenSays(t *testing.T, model Model, want ...string) {
	t.Helper()
	drawn := model.View()
	for _, one := range want {
		if !strings.Contains(drawn, one) {
			t.Fatalf("the screen does not say %q:\n%s", one, drawn)
		}
	}
}

func screenDoesNotSay(t *testing.T, model Model, unwanted string) {
	t.Helper()
	if drawn := model.View(); strings.Contains(drawn, unwanted) {
		t.Fatalf("the screen still says %q, which belongs to the level that was left:\n%s", unwanted, drawn)
	}
}

// The whole shape in one scenario: in at the top, down all four levels, and back up all four. Each
// step asserts the screen the operator is left looking at, after the listing has landed, rather than
// the command the key produced.
func TestTheConsoleOpensAtTheTopAndGoesDownFourLevelsAndBackUp(t *testing.T) {
	client := aSystemWithOneOfEverything()
	model := openedOnTheTree(t, client)

	// Level one. The console opens on workspaces, not on a flat list of every session in the system.
	if model.active.Name != "workspaces" {
		t.Fatalf("the console opens on %q, want workspaces", model.active.Name)
	}
	screenSays(t, model, "acme")

	// Level two.
	model = walk(t, model, enter())
	if model.active.Name != "projects" {
		t.Fatalf("enter on a workspace opens %q, want its projects", model.active.Name)
	}
	screenSays(t, model, "house-bills", "acme")

	// Level three. Jobs, not sessions: a job is what a person declares.
	model = walk(t, model, enter())
	if model.active.Name != "jobs" {
		t.Fatalf("enter on a project opens %q, want its jobs", model.active.Name)
	}
	screenSays(t, model, "read the electricity bill", job.PhaseRunning)

	// Level four. The running work: what the job's session was asked, and what came back.
	model = walk(t, model, enter())
	if model.active.Name != "tasks" {
		t.Fatalf("enter on a job opens %q, want the work running under it", model.active.Name)
	}
	if model.parent != "4444444444444444dddddddd" {
		t.Fatalf("the running work is scoped to %q, want the job's session", model.parent)
	}
	screenSays(t, model, "read the electricity bill", "it is due on the 14th")

	// And back up, one key at a time, all four.
	model = walk(t, model, escape())
	if model.active.Name != "jobs" {
		t.Fatalf("escape from the running work lands on %q, want the jobs it came from", model.active.Name)
	}
	screenSays(t, model, "read the electricity bill")
	screenDoesNotSay(t, model, "it is due on the 14th")

	model = walk(t, model, escape())
	if model.active.Name != "projects" {
		t.Fatalf("escape from jobs lands on %q, want the projects it came from", model.active.Name)
	}
	screenSays(t, model, "house-bills")

	model = walk(t, model, escape())
	if model.active.Name != "workspaces" {
		t.Fatalf("escape from projects lands on %q, want the workspaces it came from", model.active.Name)
	}
	screenSays(t, model, "acme")

	// The fourth escape is the one at the top. It has nowhere to go and must not take the console
	// with it.
	model = walk(t, model, escape())
	if model.active.Name != "workspaces" || model.quitting {
		t.Fatalf("escape at the top left the console on %q, quitting=%v", model.active.Name, model.quitting)
	}
}

// Coming back has to work from the deepest level after the cursor has been moved and the view
// refreshed under it, because that is the state an operator is actually in when they press escape.
func TestTheWayBackWorksFromTheDeepestLevelAfterAnythingElseHappened(t *testing.T) {
	client := aSystemWithOneOfEverything()
	client.tasks = append(client.tasks, &quaycrewv1.Task{
		Id: "6666666666666666ffffffff", Session: "4444444444444444dddddddd",
		Status: "done", Prompt: "check the meter reading", Reply: "it matches",
		OccurredAt: timestamppb.New(time.Now()),
	})
	model := openedOnTheTree(t, client)
	for range 3 {
		model = walk(t, model, enter())
	}
	// Move, then refresh, then come back.
	model = walk(t, model, runes("j"))
	model = walk(t, model, runes("r"))
	screenSays(t, model, "check the meter reading")

	model = walk(t, model, escape())

	if model.active.Name != "jobs" {
		t.Fatalf("escape landed on %q, want the jobs level", model.active.Name)
	}
	screenSays(t, model, "read the electricity bill")
}

// A workspace with no projects in it. Nothing to drill into is a state a new system is in on its
// first day, and the level below has to say so rather than draw a heading over nothing.
func TestAWorkspaceWithNoProjectsSaysSoRatherThanFailing(t *testing.T) {
	client := aSystemWithOneOfEverything()
	client.projects = nil

	model := walk(t, openedOnTheTree(t, client), enter())

	if model.active.Name != "projects" {
		t.Fatalf("enter on an empty workspace opened %q, want its projects", model.active.Name)
	}
	if model.err != nil {
		t.Fatalf("an empty workspace reported %v, and having no projects is not a fault", model.err)
	}
	screenSays(t, model, "nothing here")
}

func TestAProjectWithNoJobsSaysSoRatherThanFailing(t *testing.T) {
	client := aSystemWithOneOfEverything()
	client.jobs = nil

	model := openedOnTheTree(t, client)
	model = walk(t, walk(t, model, enter()), enter())

	if model.active.Name != "jobs" {
		t.Fatalf("enter on a project with no jobs opened %q, want its jobs", model.active.Name)
	}
	if model.err != nil {
		t.Fatalf("a project with no jobs reported %v, and having no jobs is not a fault", model.err)
	}
	screenSays(t, model, "nothing here")
}

// A job that has not reached a session is the normal state of a pending job. Enter has to refuse and
// say why, rather than opening a level that promises work and lists none.
func TestAJobWithNoSessionYetRefusesTheLastLevelAndSaysWhy(t *testing.T) {
	client := aSystemWithOneOfEverything()
	client.jobs[0].Phase, client.jobs[0].Session = job.PhasePending, ""

	model := openedOnTheTree(t, client)
	model = walk(t, walk(t, walk(t, model, enter()), enter()), enter())

	if model.active.Name != "jobs" {
		t.Fatalf("enter on a job with no session opened %q, want to stay on the jobs level", model.active.Name)
	}
	if model.err == nil {
		t.Fatal("enter did nothing and said nothing, which reads as a console that stopped answering")
	}
	screenSays(t, model, job.PhasePending)
}

// The deepest level is where a person watches something happen, so the two keys that reach the
// machine live there. Both act on the session the level is scoped to.
func TestTheRunningWorkOpensTheConversationAndAShell(t *testing.T) {
	client := aSystemWithOneOfEverything()
	work := Tasks(client)

	open, found := actionNamed(work, "Open")
	if !found {
		t.Fatal("the running work has no key that opens the conversation")
	}
	if !open.Bound("enter") || !open.OnScope {
		t.Fatalf("Open answers to %v and OnScope=%v, want enter acting on the session", open.Keys(), open.OnScope)
	}
	shell, found := actionNamed(work, "Shell")
	if !found {
		t.Fatal("the running work has no key that opens a shell, so shelling in has nowhere to live")
	}
	if !shell.Bound("s") || !shell.OnScope {
		t.Fatalf("Shell answers to %v and OnScope=%v, want s acting on the session", shell.Keys(), shell.OnScope)
	}
}

// The case a row could never answer: a job whose session has produced no task yet lists nothing, and
// that is exactly the job somebody is watching. The key has to reach the session anyway.
func TestShellingInWorksOnAJobWhoseSessionHasAnsweredNothingYet(t *testing.T) {
	client := aSystemWithOneOfEverything()
	client.tasks = nil

	model := openedOnTheTree(t, client)
	model = walk(t, walk(t, walk(t, model, enter()), enter()), enter())
	if len(model.Listed()) != 0 {
		t.Fatalf("the running work lists %d rows, want none, which is the case this is about", len(model.Listed()))
	}

	var handed []string
	model = model.WithTerminal(func(command *exec.Cmd, done func(error) tea.Msg) tea.Cmd {
		handed = command.Args
		return func() tea.Msg { return done(nil) }
	})
	model = walk(t, model, runes("s"))

	if model.err != nil {
		t.Fatalf("shelling in refused: %v", model.err)
	}
	line := strings.Join(handed, " ")
	if !strings.Contains(line, "4444444444444444dddddddd") {
		t.Fatalf("the shell command is %q, want the job's session in it", line)
	}
}

// The way off the old destination. Enter on a project used to open its sessions and now opens its
// jobs, so the sessions of one project keep a key of their own rather than becoming a command bar
// round trip.
func TestAProjectStillReachesItsSessionsInOneKey(t *testing.T) {
	client := aSystemWithOneOfEverything()
	model := walk(t, openedOnTheTree(t, client), enter())

	model = walk(t, model, runes("s"))

	if model.active.Name != "sessions" {
		t.Fatalf("s on a project opened %q, want the sessions of that project", model.active.Name)
	}
	if model.parent != "2222222222222222bbbbbbbb" {
		t.Fatalf("the sessions are scoped to %q, want the project", model.parent)
	}
	// And escape still comes back, so the second path out of a project is not a dead end.
	model = walk(t, model, escape())
	if model.active.Name != "projects" {
		t.Fatalf("escape from a project's sessions lands on %q, want the projects", model.active.Name)
	}
}

// Every flat listing stays one word away. The tree is the organised way in, not a replacement for
// asking the system for everything of one kind.
func TestEveryFlatListingIsStillOneWordAway(t *testing.T) {
	client := aSystemWithOneOfEverything()
	model := openedOnTheTree(t, client)

	model, _ = update(t, model, runes(":"))
	model = typeAll(t, model, "sessions")
	model = walk(t, model, enter())

	if model.active.Name != "sessions" {
		t.Fatalf("typing :sessions opened %q", model.active.Name)
	}
	if model.parent != "" {
		t.Fatalf("the sessions view is scoped to %q, want every session in the system", model.parent)
	}
	screenSays(t, model, "sessions")
}

// Filtering narrows what the level lists, at every level, and it stays one key. The tree must not
// have cost the quick way anything.
func TestFilterNarrowsEveryLevelOfTheTree(t *testing.T) {
	client := aSystemWithOneOfEverything()
	client.projects = append(client.projects, &quaycrewv1.Project{
		Id: "7777777777777777aaaaaaaa", Workspace: "1111111111111111aaaaaaaa", Name: "gardening",
	})
	client.jobs = append(client.jobs, &quaycrewv1.Job{
		Id: "8888888888888888bbbbbbbb", Workspace: "1111111111111111aaaaaaaa",
		Project: "2222222222222222bbbbbbbb", Title: "mow the lawn", Phase: job.PhaseDone,
	})
	model := walk(t, openedOnTheTree(t, client), enter())

	model, _ = update(t, model, runes("/"))
	model = typeAll(t, model, "garden")
	if len(model.Listed()) != 1 {
		t.Fatalf("filtering the projects level left %d rows, want the one that matched", len(model.Listed()))
	}
	screenSays(t, model, "gardening")
	screenDoesNotSay(t, model, "house-bills")

	// Escape out of the filter bar puts every row back without costing a level, then on to the level
	// below and filter that one too.
	model = walk(t, model, escape())
	if model.active.Name != "projects" {
		t.Fatalf("clearing the filter left the operator on %q, want the projects it was filtering", model.active.Name)
	}
	if len(model.Listed()) != 2 {
		t.Fatalf("clearing the filter left %d rows, want both projects back", len(model.Listed()))
	}
	model = walk(t, model, runes("j"))
	model = walk(t, model, enter())
	if model.active.Name != "jobs" {
		t.Fatalf("after clearing the filter, enter opened %q, want jobs", model.active.Name)
	}
	model, _ = update(t, model, runes("/"))
	model = typeAll(t, model, "lawn")
	if len(model.Listed()) != 1 {
		t.Fatalf("filtering the jobs level left %d rows, want the one that matched", len(model.Listed()))
	}
	screenSays(t, model, "mow the lawn")
}

// A window too narrow for the widest row still draws every level, and never draws a line wider than
// the window it is in.
func TestEveryLevelFitsAWindowTooNarrowForItsWidestRow(t *testing.T) {
	client := aSystemWithOneOfEverything()
	model := openedOnTheTree(t, client)
	model.width, model.height = 40, 20
	model = settle(t, model, listCmd(model.active, model.parent))

	for _, level := range []string{"workspaces", "projects", "jobs", "tasks"} {
		if model.active.Name != level {
			t.Fatalf("the walk is on %q, want %q", model.active.Name, level)
		}
		for _, line := range strings.Split(model.View(), "\n") {
			if width := lipgloss.Width(line); width > model.width {
				t.Fatalf("on %s a line is %d wide in a window of %d: %q", level, width, model.width, line)
			}
		}
		if level != "tasks" {
			model = walk(t, model, enter())
		}
	}
}

// The position is on screen at every level, written the way a person would type it: the workspace,
// then the project, then the job. The job is its identifier rather than its title, because that is
// what a job command takes.
func TestTheConsoleSaysWhereItIsAsAnAddress(t *testing.T) {
	client := aSystemWithOneOfEverything()
	model := openedOnTheTree(t, client)

	// At the top there is nothing to address: the operator is above every workspace.
	if where := model.Position(); where != "" {
		t.Fatalf("the console says it is at %q, and at the top there is nowhere to be", where)
	}

	model = walk(t, model, enter())
	if where := model.Position(); where != "acme" {
		t.Fatalf("inside a workspace the console says %q, want acme", where)
	}
	screenSays(t, model, "projects(acme)")

	model = walk(t, model, enter())
	if where := model.Position(); where != "acme/house-bills" {
		t.Fatalf("inside a project the console says %q, want acme/house-bills", where)
	}
	screenSays(t, model, "jobs(acme/house-bills)")

	model = walk(t, model, enter())
	if where := model.Position(); where != "acme/house-bills/33333333" {
		t.Fatalf("inside a job the console says %q, want acme/house-bills/33333333", where)
	}
	screenSays(t, model, "acme/house-bills/33333333")
}

// A job is addressed by the shortened identifier a listing prints, never by its title, because no
// command takes the title. The breadcrumb still reads the title: one is to read and one is to type.
func TestAJobIsAddressedByItsIdentifierAndReadByItsTitle(t *testing.T) {
	one := jobRow(aJob("3333333333333333cccccccc", job.PhaseRunning, func(j *quaycrewv1.Job) {
		j.Title = "read the electricity bill"
	}))

	if one.Typed() != "33333333" {
		t.Fatalf("a job is typed as %q, want the shortened identifier a listing prints", one.Typed())
	}
	if one.Name() != "read the electricity bill" {
		t.Fatalf("a job reads as %q, want its title", one.Name())
	}
	// A row that says nothing about how it is typed falls back to what it reads as, which is what
	// every workspace and project row does.
	plain := Row{ID: "w1", Label: "acme"}
	if plain.Typed() != "acme" {
		t.Fatalf("a row with no address of its own is typed as %q, want its name", plain.Typed())
	}
}

// The position has to survive the two bars that draw over the footer, because those are exactly the
// moments a person is typing and needs to know what they are typing at.
func TestThePositionStaysOnScreenWhileABarIsOpen(t *testing.T) {
	deep := openedOnTheTree(t, aSystemWithOneOfEverything())
	deep = walk(t, walk(t, deep, enter()), enter())
	screenSays(t, deep, "acme/house-bills")

	for _, bar := range []struct {
		name string
		key  tea.KeyMsg
	}{
		{"the command bar", runes(":")},
		{"the filter bar", runes("/")},
	} {
		t.Run(bar.name, func(t *testing.T) {
			opened, _ := update(t, deep, bar.key)
			if !strings.Contains(opened.View(), "acme/house-bills") {
				t.Fatalf("with %s open the console no longer says where it is:\n%s", bar.name, opened.View())
			}
		})
	}
}

// Coming back up shortens the address, so the line is never one level behind where the operator is.
func TestTheAddressShortensOnTheWayBack(t *testing.T) {
	client := aSystemWithOneOfEverything()
	model := openedOnTheTree(t, client)
	for range 3 {
		model = walk(t, model, enter())
	}
	if where := model.Position(); where != "acme/house-bills/33333333" {
		t.Fatalf("at the deepest level the console says %q", where)
	}
	for _, want := range []string{"acme/house-bills", "acme", ""} {
		model = walk(t, model, escape())
		if where := model.Position(); where != want {
			t.Fatalf("after coming back the console says %q, want %q", where, want)
		}
	}
}
