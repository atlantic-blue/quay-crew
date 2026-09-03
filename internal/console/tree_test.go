package console

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// treeClient answers every call the three levels make, so a scenario can walk the whole tree against
// one system rather than three doubles that cannot disagree with each other.
type treeClient struct {
	quaycrewv1.ControlPlaneServiceClient

	workspaces []*quaycrewv1.Workspace
	projects   []*quaycrewv1.Project
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

// aSystemWithOneOfEverything is one workspace holding one project holding one session that ran one
// task, which is the shortest path from the top of the tree to the bottom of it.
func aSystemWithOneOfEverything() *treeClient {
	made := timestamppb.New(time.Now().Add(-90 * time.Second))
	return &treeClient{
		workspaces: []*quaycrewv1.Workspace{{Id: "1111111111111111aaaaaaaa", Name: "acme", CreatedAt: made}},
		projects: []*quaycrewv1.Project{{
			Id: "2222222222222222bbbbbbbb", Workspace: "1111111111111111aaaaaaaa",
			Name: "house-bills", CreatedAt: made,
		}},
		sessions: []*quaycrewv1.Session{{
			Id: "4444444444444444dddddddd", Workspace: "1111111111111111aaaaaaaa",
			Project: "2222222222222222bbbbbbbb", Label: "bills", Status: "idle",
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

// The whole shape in one scenario: in at the top, down all three levels, and back up all three. Each
// step asserts the screen the operator is left looking at, after the listing has landed, rather than
// the command the key produced.
func TestTheConsoleOpensAtTheTopAndGoesDownThreeLevelsAndBackUp(t *testing.T) {
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

	// Level three. The sessions of the project, which is where the conversations are.
	model = walk(t, model, enter())
	if model.active.Name != "sessions" {
		t.Fatalf("enter on a project opens %q, want its sessions", model.active.Name)
	}
	screenSays(t, model, "bills")

	// And back up, one key at a time, all three.
	model = walk(t, model, escape())
	if model.active.Name != "projects" {
		t.Fatalf("escape from sessions lands on %q, want the projects it came from", model.active.Name)
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
	model = walk(t, walk(t, model, enter()), enter())
	model = walk(t, model, runes("t"))
	// Move, then refresh, then come back.
	model = walk(t, model, runes("j"))
	model = walk(t, model, runes("r"))
	screenSays(t, model, "check the meter reading")

	model = walk(t, model, escape())

	if model.active.Name != "sessions" {
		t.Fatalf("escape landed on %q, want the sessions level", model.active.Name)
	}
	screenSays(t, model, "bills")
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

func TestAProjectWithNoSessionsSaysSoRatherThanFailing(t *testing.T) {
	client := aSystemWithOneOfEverything()
	client.sessions = nil

	model := openedOnTheTree(t, client)
	model = walk(t, walk(t, model, enter()), enter())

	if model.active.Name != "sessions" {
		t.Fatalf("enter on a project with no sessions opened %q, want its sessions", model.active.Name)
	}
	if model.err != nil {
		t.Fatalf("a project with no sessions reported %v, and having none is not a fault", model.err)
	}
	screenSays(t, model, "nothing here")
}

// A task is a paragraph, and the panel is about a hundred characters wide. A line left whole is a
// line cut at the border, which is the fault this key exists to answer, one order of magnitude along.
func TestALongAskIsReadWholeRatherThanCutAtTheBorder(t *testing.T) {
	const ask = "read the electricity bill for the flat in the north of the city, work out what " +
		"the standing charge came to over the quarter, and say whether the supplier moved it " +
		"without telling anybody"
	client := aSystemWithOneOfEverything()
	client.tasks[0].Prompt = ask

	model := openedOnTheTree(t, client)
	model = walk(t, walk(t, walk(t, model, enter()), enter()), runes("t"))

	// Every word of it, in the order it was written, across however many rows the panel needed.
	if drawn := drawnText(model); !strings.Contains(drawn, ask) {
		t.Fatalf("the screen does not carry the whole ask:\n%s", model.View())
	}
}

// drawnText is what the screen says with the frame taken off and the rows run together, so a sentence
// wrapped over several rows reads as the sentence it is.
func drawnText(model Model) string {
	drawn := strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ").Replace(model.View())
	return strings.Join(strings.Fields(drawn), " ")
}

// Wrapping is where a reading of any length is kept, so the pieces are checked on their own too: a
// word too long for the panel is broken rather than dropped, and nothing is lost between two pieces.
func TestWrappingKeepsEveryWord(t *testing.T) {
	for _, one := range []struct {
		name string
		line string
		wide int
		want []string
	}{
		{"a line that already fits", "pay the bill", 20, []string{"pay the bill"}},
		{"broken on its spaces", "pay the water bill today", 10, []string{"pay the", "water bill", "today"}},
		{"a word longer than the panel", "aaaaaaaa bb", 4, []string{"aaaa", "aaaa", "bb"}},
		{"nothing at all", "", 10, []string{""}},
	} {
		t.Run(one.name, func(t *testing.T) {
			got := wrapTo(one.line, one.wide)
			if strings.Join(got, "|") != strings.Join(one.want, "|") {
				t.Fatalf("wrapping %q at %d gives %q, want %q", one.line, one.wide, got, one.want)
			}
			for _, piece := range got {
				if len([]rune(piece)) > one.wide {
					t.Fatalf("the piece %q is wider than the %d the panel has", piece, one.wide)
				}
			}
		})
	}
}

// The fault this level had: every row opened the same shell, so the one key that means "this one"
// could not reach the task under the cursor. The column holds 34 characters, so what a row shows is a
// fragment of a sentence, and the whole of it was only at the command line.
func TestEnterOnATaskOpensTheTaskUnderTheCursor(t *testing.T) {
	const second = "pay the water bill before the fourteenth or the supply is cut off"
	client := aSystemWithOneOfEverything()
	client.tasks = append(client.tasks, &quaycrewv1.Task{
		Id: "6666666666666666ffffffff", Session: "4444444444444444dddddddd",
		Status: "done", Prompt: second, Reply: "it is paid",
		OccurredAt: timestamppb.New(time.Now().Add(time.Minute)),
	})

	model := openedOnTheTree(t, client)
	model = walk(t, walk(t, walk(t, model, enter()), enter()), runes("t"))
	if len(model.Listed()) != 2 {
		t.Fatalf("the running work lists %d rows, want the two tasks this is about", len(model.Listed()))
	}
	model = walk(t, model, tea.KeyMsg{Type: tea.KeyDown})
	if row, found := model.Selected(); !found || row.ID != "6666666666666666ffffffff" {
		t.Fatalf("the cursor is on %+v, want the second task", row)
	}

	model = walk(t, model, enter())

	// The whole sentence, which no row on this level could ever have drawn, and the answer under it.
	screenSays(t, model, second, "it is paid")
	// The first task's own answer, which is what a key that opened the row above this one would show.
	screenDoesNotSay(t, model, "it is due on the 14th")
	// And the way out: any other key puts the rows back, so the reading is somewhere a person leaves.
	model = walk(t, model, escape())
	screenSays(t, model, "it is due on the 14th")
}

// Enter used to hand the terminal to a shell in the session's container. It reads the task now, and a
// key that suspends the console into somebody else's container is not a thing to do by accident.
func TestEnterOnATaskOpensNoShell(t *testing.T) {
	client := aSystemWithOneOfEverything()
	model := openedOnTheTree(t, client)
	model = walk(t, walk(t, walk(t, model, enter()), enter()), runes("t"))

	var handed []string
	model = model.WithTerminal(func(command *exec.Cmd, done func(error) tea.Msg) tea.Cmd {
		handed = command.Args
		return func() tea.Msg { return done(nil) }
	})
	model = walk(t, model, enter())

	if handed != nil {
		t.Fatalf("enter handed the terminal to %q, want the task on the screen instead", strings.Join(handed, " "))
	}
	if model.err != nil {
		t.Fatalf("enter on a task refused: %v", model.err)
	}
	screenSays(t, model, "read the electricity bill", "it is due on the 14th")
}

// The other half of the case a row could never answer. Enter needs a row and this session has none,
// so the conversation keeps a key that acts on the session the level is scoped to.
func TestTheConversationIsStillReachableWhenNoTaskHasAnswered(t *testing.T) {
	client := aSystemWithOneOfEverything()
	client.tasks = nil

	model := openedOnTheTree(t, client)
	model = walk(t, walk(t, walk(t, model, enter()), enter()), runes("t"))
	if len(model.Listed()) != 0 {
		t.Fatalf("the running work lists %d rows, want none, which is the case this is about", len(model.Listed()))
	}

	var handed []string
	model = model.WithTerminal(func(command *exec.Cmd, done func(error) tea.Msg) tea.Cmd {
		handed = command.Args
		return func() tea.Msg { return done(nil) }
	})
	model = walk(t, model, runes("a"))

	if model.err != nil {
		t.Fatalf("opening the conversation refused: %v", model.err)
	}
	line := strings.Join(handed, " ")
	if !strings.Contains(line, "krewe-s1") || !strings.Contains(line, "--resume") {
		t.Fatalf("the command is %q, want the conversation in the job's own sandbox", line)
	}
}

// The key that was the way to a project's conversations while enter went elsewhere. Enter reaches
// them again, and the key is kept because it is in fingers.
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
	client.sessions = append(client.sessions, &quaycrewv1.Session{
		Id: "8888888888888888bbbbbbbb", Workspace: "1111111111111111aaaaaaaa",
		Project: "2222222222222222bbbbbbbb", Label: "mow lawn", Status: "idle",
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
	if model.active.Name != "sessions" {
		t.Fatalf("after clearing the filter, enter opened %q, want sessions", model.active.Name)
	}
	model, _ = update(t, model, runes("/"))
	model = typeAll(t, model, "lawn")
	if len(model.Listed()) != 1 {
		t.Fatalf("filtering the sessions level left %d rows, want the one that matched", len(model.Listed()))
	}
	screenSays(t, model, "mow lawn")
}

// A window too narrow for the widest row still draws every level, and never draws a line wider than
// the window it is in.
func TestEveryLevelFitsAWindowTooNarrowForItsWidestRow(t *testing.T) {
	client := aSystemWithOneOfEverything()
	model := openedOnTheTree(t, client)
	model.width, model.height = 40, 20
	model = settle(t, model, listCmd(model.active, model.parent))

	for _, level := range []string{"workspaces", "projects", "sessions"} {
		if model.active.Name != level {
			t.Fatalf("the walk is on %q, want %q", model.active.Name, level)
		}
		for _, line := range strings.Split(model.View(), "\n") {
			if width := lipgloss.Width(line); width > model.width {
				t.Fatalf("on %s a line is %d wide in a window of %d: %q", level, width, model.width, line)
			}
		}
		if level != "sessions" {
			model = walk(t, model, enter())
		}
	}
}
