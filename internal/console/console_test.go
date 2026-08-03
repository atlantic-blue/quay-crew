package console

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeClient is a control plane double. It embeds the generated interface so unimplemented calls
// panic loudly rather than being silently satisfied.
type fakeClient struct {
	quaycrewv1.ControlPlaneServiceClient

	workspaces []*quaycrewv1.Workspace
	projects   []*quaycrewv1.Project
	sessions   []*quaycrewv1.Session

	attachErr       error
	listSessionsFor string
	stopped         []string
	listErr         error
}

func (f *fakeClient) ListWorkspaces(context.Context, *quaycrewv1.ListWorkspacesRequest, ...grpc.CallOption) (*quaycrewv1.ListWorkspacesResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &quaycrewv1.ListWorkspacesResponse{Workspaces: f.workspaces}, nil
}

func (f *fakeClient) AttachSession(context.Context, *quaycrewv1.AttachSessionRequest, ...grpc.CallOption) (*quaycrewv1.AttachSessionResponse, error) {
	if f.attachErr != nil {
		return nil, f.attachErr
	}
	return &quaycrewv1.AttachSessionResponse{Sandbox: "quaycrew-s1", Argv: []string{"claude", "--resume", "c1"}}, nil
}

func (f *fakeClient) ListProjects(_ context.Context, req *quaycrewv1.ListProjectsRequest, _ ...grpc.CallOption) (*quaycrewv1.ListProjectsResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if req.GetWorkspace() == "" {
		return &quaycrewv1.ListProjectsResponse{Projects: f.projects}, nil
	}
	matched := make([]*quaycrewv1.Project, 0, len(f.projects))
	for _, project := range f.projects {
		if project.GetWorkspace() == req.GetWorkspace() {
			matched = append(matched, project)
		}
	}
	return &quaycrewv1.ListProjectsResponse{Projects: matched}, nil
}

func (f *fakeClient) ListSessions(_ context.Context, req *quaycrewv1.ListSessionsRequest, _ ...grpc.CallOption) (*quaycrewv1.ListSessionsResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.listSessionsFor = req.GetProject()

	if req.GetProject() == "" {
		return &quaycrewv1.ListSessionsResponse{Sessions: f.sessions}, nil
	}
	matched := make([]*quaycrewv1.Session, 0, len(f.sessions))
	for _, session := range f.sessions {
		if session.GetProject() == req.GetProject() {
			matched = append(matched, session)
		}
	}
	return &quaycrewv1.ListSessionsResponse{Sessions: matched}, nil
}

func (f *fakeClient) StopSession(_ context.Context, req *quaycrewv1.StopSessionRequest, _ ...grpc.CallOption) (*quaycrewv1.StopSessionResponse, error) {
	f.stopped = append(f.stopped, req.GetId())
	return &quaycrewv1.StopSessionResponse{}, nil
}

// ---------- helpers ----------

func update(t *testing.T, model Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := model.Update(msg)
	got, isModel := next.(Model)
	if !isModel {
		t.Fatalf("Update returned %T, want console.Model", next)
	}
	return got, cmd
}

func runes(text string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)}
}

func typeAll(t *testing.T, model Model, text string) Model {
	t.Helper()
	for _, r := range text {
		model, _ = update(t, model, runes(string(r)))
	}
	return model
}

// rowsFor builds a listing message for the model's current view, which is what a completed refresh
// looks like arriving back.
func rowsFor(model Model, rows ...Row) rowsMsg {
	return rowsMsg{resource: model.active.Name, parent: model.parent, rows: rows}
}

func row(id string, cells ...string) Row {
	return Row{ID: id, Cells: cells}
}

func newTestModel(t *testing.T, resources ...Resource) Model {
	t.Helper()
	registry, err := NewRegistry(resources...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	model, err := New(registry, resources[0].Name, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model.width, model.height = 120, 20
	return model
}

func staticResource(name string, aliases ...string) Resource {
	return Resource{
		Name:    name,
		Aliases: aliases,
		Columns: []Column{{Title: "id", Width: 20}, {Title: "name", Width: 0}},
		List:    func(context.Context, string) ([]Row, error) { return nil, nil },
	}
}

// ---------- registry ----------

func TestRegistryResolvesNameAndAliases(t *testing.T) {
	registry, err := NewRegistry(staticResource("sessions", "s", "sess"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	for _, token := range []string{"sessions", "s", "sess", ":sessions", "  Sessions  "} {
		resource, found := registry.Resolve(token)
		if !found {
			t.Fatalf("Resolve(%q): not found", token)
		}
		if resource.Name != "sessions" {
			t.Fatalf("Resolve(%q) = %q, want sessions", token, resource.Name)
		}
	}
	if _, found := registry.Resolve("containers"); found {
		t.Fatal("Resolve(containers): want not found")
	}
}

func TestRegistryRejectsShadowing(t *testing.T) {
	tests := []struct {
		name      string
		resources []Resource
	}{
		{"duplicate name", []Resource{staticResource("sessions"), staticResource("sessions")}},
		{"alias shadows a name", []Resource{staticResource("sessions"), staticResource("pods", "sessions")}},
		{"duplicate alias", []Resource{staticResource("sessions", "s"), staticResource("streams", "s")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewRegistry(test.resources...); err == nil {
				t.Fatal("NewRegistry: want an error, got none")
			}
		})
	}
}

func TestRegistryRejectsResourceWithoutLister(t *testing.T) {
	if _, err := NewRegistry(Resource{Name: "sessions"}); err == nil {
		t.Fatal("NewRegistry: want an error for a resource with no lister")
	}
}

// ---------- the console shell ----------

func TestCommandBarSwitchesResource(t *testing.T) {
	model := newTestModel(t, staticResource("sessions", "s"), staticResource("workspaces", "p"))

	model, _ = update(t, model, runes(":"))
	if model.mode != modeCommand {
		t.Fatalf("mode = %v, want modeCommand", model.mode)
	}
	model = typeAll(t, model, "p")
	model, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if model.active.Name != "workspaces" {
		t.Fatalf("active = %q, want workspaces", model.active.Name)
	}
	if model.mode != modeBrowse {
		t.Fatal("command bar stayed open after enter")
	}
	if cmd == nil {
		t.Fatal("switching resource did not trigger a listing")
	}
}

func TestCommandBarReportsAnUnknownResource(t *testing.T) {
	model := newTestModel(t, staticResource("sessions", "s"))

	model, _ = update(t, model, runes(":"))
	model = typeAll(t, model, "containers")
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if model.err == nil {
		t.Fatal("want an error for an unknown resource")
	}
	if model.active.Name != "sessions" {
		t.Fatalf("active = %q, want the view to stay on sessions", model.active.Name)
	}
	if !strings.Contains(model.err.Error(), "containers") {
		t.Fatalf("error %q does not name what was typed", model.err)
	}
}

func TestFailedRefreshKeepsThePreviousRows(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model, _ = update(t, model, rowsFor(model, row("a", "a", "one"), row("b", "b", "two")))

	model, _ = update(t, model, errMsg{err: errors.New("control plane unreachable")})

	if len(model.rows) != 2 {
		t.Fatalf("rows = %d, want the previous 2 to stay on screen", len(model.rows))
	}
	if model.err == nil {
		t.Fatal("want the error recorded")
	}
	if !strings.Contains(model.View(), "control plane unreachable") {
		t.Fatal("the error is not shown in the view")
	}
}

func TestListingForALeftViewIsIgnored(t *testing.T) {
	model := newTestModel(t, staticResource("sessions", "s"), staticResource("workspaces", "p"))
	model, _ = update(t, model, rowsFor(model, row("a", "a", "one")))

	// Switch away, then let the earlier view's slow listing land.
	model, _ = update(t, model, runes(":"))
	model = typeAll(t, model, "p")
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	stale := rowsMsg{resource: "sessions", parent: "", rows: []Row{row("z", "z", "stale")}}
	model, _ = update(t, model, stale)

	if len(model.rows) != 0 {
		t.Fatalf("rows = %d, want the stale listing dropped", len(model.rows))
	}
}

func TestFilterNarrowsRowsAndKeepsTheSelectionInRange(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model, _ = update(t, model, rowsFor(model,
		row("alpha", "alpha", "one"), row("beta", "beta", "two"), row("gamma", "gamma", "three")))

	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyDown})
	if model.selected != 2 {
		t.Fatalf("selected = %d, want 2 before filtering", model.selected)
	}

	model, _ = update(t, model, runes("/"))
	model = typeAll(t, model, "alph")

	visible := model.visibleRows()
	if len(visible) != 1 || visible[0].ID != "alpha" {
		t.Fatalf("visible rows = %v, want just alpha", visible)
	}
	if model.selected != 0 {
		t.Fatalf("selected = %d, want the cursor pulled back into range", model.selected)
	}
}

func TestEscapeClearsTheFilterBeforeLeavingTheView(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model, _ = update(t, model, rowsFor(model, row("alpha", "alpha", "one"), row("beta", "beta", "two")))

	model, _ = update(t, model, runes("/"))
	model = typeAll(t, model, "alph")
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEsc})

	if model.filter != "" {
		t.Fatalf("filter = %q, want it cleared", model.filter)
	}
	if len(model.visibleRows()) != 2 {
		t.Fatalf("visible = %d, want both rows back", len(model.visibleRows()))
	}
}

func TestBackspaceNarrowsTheFilterBack(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model, _ = update(t, model, rowsFor(model, row("alpha", "alpha", "one"), row("beta", "beta", "two")))

	model, _ = update(t, model, runes("/"))
	model = typeAll(t, model, "alph")
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyBackspace})

	if model.filter != "alp" {
		t.Fatalf("filter = %q, want alp", model.filter)
	}
}

func TestQuitStopsTheProgram(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model, cmd := update(t, model, runes("q"))

	if !model.quitting {
		t.Fatal("want the model marked as quitting")
	}
	if cmd == nil {
		t.Fatal("want a quit command")
	}
	if model.View() != "" {
		t.Fatal("a quitting console should draw nothing, so it does not fight the restored screen")
	}
}

// ---------- drilling ----------

func TestEnterDrillsIntoTheChildResourceScopedToTheRow(t *testing.T) {
	client := &fakeClient{
		projects: []*quaycrewv1.Project{
			{Id: "p1", Workspace: "acme", Name: "house bills"},
			{Id: "p2", Workspace: "other", Name: "gardening"},
		},
		sessions: []*quaycrewv1.Session{
			{Id: "s1", Workspace: "acme", Project: "p1", Status: "idle"},
			{Id: "s2", Workspace: "other", Project: "p2", Status: "idle"},
		},
	}
	model := newTestModel(t, Workspaces(client), Projects(client), Sessions(client))
	model, _ = update(t, model, rowsFor(model, row("acme", "acme", "Acme"), row("other", "other", "Other")))

	model, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	// A workspace drills into its projects now, not straight into sessions.
	if model.active.Name != "projects" {
		t.Fatalf("active = %q, want projects", model.active.Name)
	}
	if model.parent != "acme" {
		t.Fatalf("parent = %q, want the selected workspace", model.parent)
	}
	if cmd == nil {
		t.Fatal("drilling did not trigger a listing")
	}

	msg, isRows := cmd().(rowsMsg)
	if !isRows {
		t.Fatalf("listing returned %T, want rowsMsg", cmd())
	}
	if len(msg.rows) != 1 || msg.rows[0].ID != "p1" {
		t.Fatalf("rows = %v, want only acme's project", msg.rows)
	}
}

func TestEscapeReturnsToTheParentViewWithItsSelection(t *testing.T) {
	client := &fakeClient{}
	model := newTestModel(t, Workspaces(client), Projects(client), Sessions(client))
	model, _ = update(t, model, rowsFor(model,
		row("acme", "acme", "Acme"), row("other", "other", "Other"), row("third", "third", "Third")))

	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.active.Name != "projects" {
		t.Fatalf("active = %q, want projects after drilling", model.active.Name)
	}

	model, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEsc})

	if model.active.Name != "workspaces" {
		t.Fatalf("active = %q, want workspaces after escape", model.active.Name)
	}
	if model.selected != 1 {
		t.Fatalf("selected = %d, want the previous selection of 1 restored", model.selected)
	}
	if model.parent != "" {
		t.Fatalf("parent = %q, want the scope cleared", model.parent)
	}
	if cmd == nil {
		t.Fatal("going back did not reload the parent view")
	}
}

func TestSwitchingResourceByNameResetsTheBreadcrumb(t *testing.T) {
	client := &fakeClient{}
	model := newTestModel(t, Workspaces(client), Projects(client), Sessions(client))
	model, _ = update(t, model, rowsFor(model, row("acme", "acme", "Acme")))
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	model, _ = update(t, model, runes(":"))
	model = typeAll(t, model, "workspaces")
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(model.stack) != 0 {
		t.Fatalf("stack depth = %d, want a jump to clear the trail", len(model.stack))
	}
}

// ---------- actions ----------

func TestStopActionStopsTheSelectedSession(t *testing.T) {
	client := &fakeClient{sessions: []*quaycrewv1.Session{{Id: "s1", Workspace: "acme", Status: "idle"}}}
	model := newTestModel(t, Sessions(client))
	model, _ = update(t, model, rowsFor(model, Row{ID: "s1", Cells: []string{"s1", "acme", "", "idle", "1m"}}))

	_, cmd := update(t, model, runes("x"))
	if cmd == nil {
		t.Fatal("stop produced no command")
	}
	if msg, isDone := cmd().(actionDoneMsg); !isDone || msg.err != nil {
		t.Fatalf("stop returned %#v, want a clean actionDoneMsg", cmd())
	}
	if len(client.stopped) != 1 || client.stopped[0] != "s1" {
		t.Fatalf("stopped = %v, want [s1]", client.stopped)
	}
}

func TestShellActionExecsIntoTheSessionContainer(t *testing.T) {
	client := &fakeClient{}
	resource := Sessions(client)

	var shell *Action
	for index := range resource.Actions {
		if resource.Actions[index].Key == "s" {
			shell = &resource.Actions[index]
		}
	}
	if shell == nil {
		t.Fatal("sessions has no shell action")
	}

	command, err := shell.Shell(Row{ID: "s1"})
	if err != nil {
		t.Fatalf("shell action: %v", err)
	}
	want := []string{"docker", "exec", "-it", "quaycrew-s1", "sh"}
	if strings.Join(command.Args, " ") != strings.Join(want, " ") {
		t.Fatalf("command = %v, want %v", command.Args, want)
	}
	if _, err := shell.Shell(Row{}); err == nil {
		t.Fatal("want a reason for a row with no session id")
	}
}

// TestAttachTellsTheOperatorWhyItCannot covers the thing that made this worthless before: the console
// swallowed the control plane's reason and said "nothing to run", which is not something anyone can
// act on. A session with no conversation yet is fixed by dispatching a turn, and the operator has to
// be told that.
func TestAttachTellsTheOperatorWhyItCannot(t *testing.T) {
	client := &fakeClient{attachErr: fmt.Errorf("session s1 has no conversation yet: dispatch a turn to it first")}
	resource := Sessions(client)

	var attach *Action
	for index := range resource.Actions {
		if resource.Actions[index].Key == "a" {
			attach = &resource.Actions[index]
		}
	}
	if attach == nil {
		t.Fatal("sessions has no attach action")
	}

	_, err := attach.Shell(Row{ID: "s1"})
	if err == nil {
		t.Fatal("attaching to a session with no conversation succeeded")
	}
	if !strings.Contains(err.Error(), "no conversation yet") {
		t.Fatalf("the reason did not reach the operator: %v", err)
	}
}

func TestAnActionOnAnEmptyViewDoesNothing(t *testing.T) {
	client := &fakeClient{}
	model := newTestModel(t, Sessions(client))

	_, cmd := update(t, model, runes("x"))
	if cmd != nil {
		t.Fatal("want no command when there is no row selected")
	}
	if len(client.stopped) != 0 {
		t.Fatalf("stopped = %v, want nothing stopped", client.stopped)
	}
}

// ---------- resources ----------

func TestSessionListingMapsStatusOntoState(t *testing.T) {
	tests := map[string]State{
		"idle":        StateReady,
		"running":     StateBusy,
		"dispatching": StateBusy,
		"stopped":     StateStopped,
		"failed":      StateFailed,
		"something":   StateUnknown,
	}
	for status, want := range tests {
		t.Run(status, func(t *testing.T) {
			if got := stateFromStatus(status); got != want {
				t.Fatalf("stateFromStatus(%q) = %v, want %v", status, got, want)
			}
		})
	}
}

func TestSessionListingSurfacesTheControlPlaneError(t *testing.T) {
	client := &fakeClient{listErr: errors.New("unavailable")}
	resource := Sessions(client)

	if _, err := resource.List(context.Background(), ""); err == nil {
		t.Fatal("want the control plane error surfaced, not swallowed")
	}
}

func TestPlainOutputListsSessionsWithoutEscapeCodes(t *testing.T) {
	client := &fakeClient{sessions: []*quaycrewv1.Session{{Id: "s1", Workspace: "acme", Status: "idle"}}}

	var out strings.Builder
	if err := Plain(context.Background(), client, &out); err != nil {
		t.Fatalf("Plain: %v", err)
	}
	if !strings.Contains(out.String(), "s1") {
		t.Fatalf("output %q does not list the session", out.String())
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("output %q carries escape codes, which defeats piping", out.String())
	}
}

func TestPlainOutputSaysSoWhenThereIsNothing(t *testing.T) {
	var out strings.Builder
	if err := Plain(context.Background(), &fakeClient{}, &out); err != nil {
		t.Fatalf("Plain: %v", err)
	}
	if !strings.Contains(out.String(), "no sessions") {
		t.Fatalf("output %q, want it to say there are none", out.String())
	}
}

// ---------- view ----------

// TestTheHeaderShowsThisViewsOwnCommands: the header carries the verbs for what is on screen, and
// the key that lists the rest. A header that lists every key teaches the operator to stop reading it.
func TestTheHeaderShowsThisViewsOwnCommands(t *testing.T) {
	client := &fakeClient{}
	model := newTestModel(t, Sessions(client), Workspaces(client))
	model, _ = update(t, model, rowsFor(model, Row{ID: "s1", Cells: []string{"s1", "acme", "", "idle", "1m"}}))

	view := model.View()
	for _, want := range []string{"<a> Attach", "<s> Shell", "<x> Stop", "<?> Help"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the header does not offer %q:\n%s", want, view)
		}
	}
	for _, unwanted := range []string{"Quit", "Refresh", "Resource"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("the header lists %q, which belongs behind the question mark:\n%s", unwanted, view)
		}
	}
}

// TestTheQuestionMarkListsEveryKey is where the keys the header does not show have to live.
func TestTheQuestionMarkListsEveryKey(t *testing.T) {
	client := &fakeClient{}
	model := newTestModel(t, Sessions(client), Workspaces(client))
	model, _ = update(t, model, rowsFor(model, Row{ID: "s1", Cells: []string{"s1", "acme", "", "idle", "1m"}}))

	model, _ = update(t, model, runes("?"))
	view := model.View()
	for _, want := range []string{"help(sessions)", "Quit", "Refresh now", "Filter these rows", "<a> Attach"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the key list does not mention %q:\n%s", want, view)
		}
	}

	// Any key closes it, because nothing in there acts on anything.
	model, _ = update(t, model, runes("z"))
	if model.mode != modeBrowse {
		t.Fatalf("mode is %v after a keypress, want back to browsing", model.mode)
	}
	if strings.Contains(model.View(), "help(") {
		t.Fatalf("the key list is still on screen:\n%s", model.View())
	}
}

// TestTheStatusBlockNamesTheBuildAndWhereYouAreStanding covers the two things only the tool knows
// about itself: which build it is, and the context the operator set.
func TestTheStatusBlockNamesTheBuildAndWhereYouAreStanding(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model, _ = update(t, model, infoMsg{info: Info{Version: "5fd7bee", Address: "localhost:50051", Context: "me/house-bills"}})

	view := model.View()
	for _, want := range []string{"Version:", "5fd7bee", "Context:", "me/house-bills"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the status block does not say %q:\n%s", want, view)
		}
	}
}

// TestTheWordmarkIsThereBeforeTheCrewAnswers is how it went missing: against a control plane too old
// to say what it is running, the status block is three lines, and the mark is six.
func TestTheWordmarkIsThereBeforeTheCrewAnswers(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model, _ = update(t, model, tea.WindowSizeMsg{Width: 150, Height: 30})
	model, _ = update(t, model, infoMsg{info: Info{Version: "709b79e", Address: "localhost:50051", Context: "demo/default"}})

	if !strings.Contains(model.View(), logo[0]) {
		t.Fatalf("the wordmark is missing when the status block is short:\n%s", model.View())
	}
}

// TestTheWordmarkGivesWayToASmallWindow: branding is the first thing to drop when there is no room.
func TestTheWordmarkGivesWayToASmallWindow(t *testing.T) {
	full := newTestModel(t, staticResource("sessions"))
	full, _ = update(t, full, tea.WindowSizeMsg{Width: 140, Height: 30})
	full, _ = update(t, full, infoMsg{info: Info{
		Version: "5fd7bee", Address: "localhost:50051", Context: "me", Model: "echo", Sandbox: "docker", Store: "memory",
	}})
	if !strings.Contains(full.View(), logo[0]) {
		t.Fatalf("a wide window does not carry the wordmark:\n%s", full.View())
	}

	narrow, _ := update(t, full, tea.WindowSizeMsg{Width: 70, Height: 30})
	if strings.Contains(narrow.View(), logo[0]) {
		t.Fatalf("a narrow window still carries the wordmark:\n%s", narrow.View())
	}

	// And a short one keeps its rows rather than its branding.
	short, _ := update(t, full, tea.WindowSizeMsg{Width: 140, Height: 12})
	if strings.Contains(short.View(), logo[0]) {
		t.Fatalf("a short window spends its rows on the wordmark:\n%s", short.View())
	}
}

// TestStatusBlockNamesTheCrewItIsConnectedTo covers the question the block exists to answer: which
// crew am I about to act on. Two stacks list identical sessions and behave nothing alike.
func TestStatusBlockNamesTheCrewItIsConnectedTo(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model, _ = update(t, model, infoMsg{info: Info{
		Address: "localhost:50051", Model: "claude-code", Sandbox: "docker", Store: "postgres", StateKept: true,
	}})

	view := model.View()
	for _, want := range []string{"localhost:50051", "claude-code", "docker", "postgres", "kept on the host"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the status block does not say %q:\n%s", want, view)
		}
	}
}

// TestStatusBlockSaysWhenAConversationWouldBeLost is the reason state is in words rather than a
// boolean: "false" is not something anyone reads as "your conversation dies with its container".
func TestStatusBlockSaysWhenAConversationWouldBeLost(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model, _ = update(t, model, infoMsg{info: Info{Address: "here", Store: "memory", StateKept: false}})

	if !strings.Contains(model.View(), "lost with the container") {
		t.Fatalf("the status block does not warn that state is lost:\n%s", model.View())
	}
}

// TestStatusBlockSaysNothingItWasNotTold guards against the console inventing a reassuring answer
// when the control plane never replied.
func TestStatusBlockSaysNothingItWasNotTold(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	view := model.View()
	for _, unwanted := range []string{"kept on the host", "lost with the container", "postgres", "docker"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("the status block claims %q without being told:\n%s", unwanted, view)
		}
	}
}

func TestTheConsoleAsksWhatItIsConnectedTo(t *testing.T) {
	asked := false
	registry, err := NewRegistry(staticResource("sessions"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	model, err := New(registry, "sessions", func(context.Context) (Info, error) {
		asked = true
		return Info{Address: "somewhere"}, nil
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Init batches the listing, the question and the clock. Running the batch is the runtime's job,
	// so drive the one command this test is about.
	msg := infoCmd(model.source)()
	if !asked {
		t.Fatal("the console never asked what it is connected to")
	}
	info, isInfo := msg.(infoMsg)
	if !isInfo {
		t.Fatalf("asking returned %T, want infoMsg", msg)
	}
	if info.info.Address != "somewhere" {
		t.Fatalf("it recorded %q, want the address it was given", info.info.Address)
	}
}

// TestAControlPlaneTooOldToAnswerSaysSo is the case that cost an afternoon: the tool was installed,
// the stack was not rebuilt, the call did not exist yet, and the console silently showed four fewer
// lines. Silence reads as the console being broken.
func TestAControlPlaneTooOldToAnswerSaysSo(t *testing.T) {
	msg := infoCmd(func(context.Context) (Info, error) {
		return Info{}, status.Error(codes.Unimplemented, "unknown method GetInfo")
	})()
	if _, isBehind := msg.(behindMsg); !isBehind {
		t.Fatalf("asking an old control plane produced %#v, want it reported as behind", msg)
	}

	model := newTestModel(t, staticResource("sessions"))
	model, _ = update(t, model, behindMsg{})
	view := model.View()
	if !strings.Contains(view, "Quay:") || !strings.Contains(view, "older than the tool") || !strings.Contains(view, "make upgrade") {
		t.Fatalf("the status block does not say the crew is behind, or how to fix it:\n%s", view)
	}
}

// TestAFailedQuestionLeavesTheConsoleUsable: the operator came to look at sessions, and a status
// block that could not be filled in is not a reason to show them an error instead.
func TestAFailedQuestionLeavesTheConsoleUsable(t *testing.T) {
	msg := infoCmd(func(context.Context) (Info, error) { return Info{}, errors.New("no answer") })()
	if msg != nil {
		t.Fatalf("a failed question produced %v, want nothing", msg)
	}
}

// TestPanelTitleCarriesScopeAndCount is k9s's contexts(all)[1] in our own words: what am I looking
// at, narrowed to what, and how many, without counting rows.
func TestPanelTitleCarriesScopeAndCount(t *testing.T) {
	client := &fakeClient{
		workspaces: []*quaycrewv1.Workspace{{Id: "w1", Name: "me"}},
		projects:   []*quaycrewv1.Project{{Id: "p1", Workspace: "w1", Name: "house-bills"}},
	}
	model := newTestModel(t, Workspaces(client), Projects(client), Sessions(client))
	model, _ = update(t, model, rowsFor(model, Row{ID: "w1", Label: "me", Cells: []string{"w1", "me", "1m"}}))
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = update(t, model, rowsFor(model,
		Row{ID: "p1", Label: "house-bills", Cells: []string{"p1", "house-bills", "me", "1m"}}))

	if got := model.View(); !strings.Contains(got, "projects(me)[1]") {
		t.Fatalf("the panel is not titled with its scope and count:\n%s", got)
	}
}

func TestPanelTitleWithNoScopeIsJustTheCount(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model, _ = update(t, model, rowsFor(model, row("s1", "s1", "one"), row("s2", "s2", "two")))

	if got := model.View(); !strings.Contains(got, "sessions[2]") {
		t.Fatalf("the panel is not titled with its count:\n%s", got)
	}
}

// TestRowsAreSortedByTheMarkedColumn: an order you cannot see is an order you cannot trust, so the
// arrow and the ordering have to be the same thing.
func TestRowsAreSortedByTheMarkedColumn(t *testing.T) {
	resource := staticResource("sessions")
	resource.SortBy = 1
	model := newTestModel(t, resource)
	model, _ = update(t, model, rowsFor(model, row("c", "c", "cherry"), row("a", "a", "apple"), row("b", "b", "banana")))

	visible := model.visibleRows()
	if visible[0].ID != "a" || visible[2].ID != "c" {
		t.Fatalf("rows are in the order %v, want them sorted by the second column",
			[]string{visible[0].ID, visible[1].ID, visible[2].ID})
	}
	view := model.View()
	if !strings.Contains(view, "NAME↑") {
		t.Fatalf("the sorted column is not marked:\n%s", view)
	}
	if strings.Contains(view, "ID↑") {
		t.Fatalf("a column that is not sorted is marked:\n%s", view)
	}
}

// TestSortingTiesKeepTheControlPlanesOrder: a stable sort is what stops rows shuffling under the
// cursor on every refresh.
func TestSortingTiesKeepTheControlPlanesOrder(t *testing.T) {
	resource := staticResource("sessions")
	resource.SortBy = 1
	model := newTestModel(t, resource)
	model, _ = update(t, model, rowsFor(model, row("second", "x", "same"), row("first", "y", "same")))

	visible := model.visibleRows()
	if visible[0].ID != "second" {
		t.Fatalf("rows that tie were reordered: %q came first", visible[0].ID)
	}
}

// TestTheSelectedRowIsHighlightedAcrossTheWholeRow: a cursor that only covers the text is one you
// lose in a wide window.
func TestTheSelectedRowIsHighlightedAcrossTheWholeRow(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model, _ = update(t, model, rowsFor(model, row("s1", "s1", "one")))

	line := model.rowLine(model.rows[0], true)
	if lipgloss.Width(line) != model.innerWidth() {
		t.Fatalf("the selected row is %d wide, want the full %d inside the panel", lipgloss.Width(line), model.innerWidth())
	}
	// The highlight itself is a lipgloss style, and lipgloss renders no escape codes with no
	// terminal attached, so what is asserted here is the width the highlight covers. That it is
	// drawn at all is what the live run shows.
	if lipgloss.Width(model.rowLine(model.rows[0], false)) != model.innerWidth() {
		t.Fatalf("an unselected row is not padded to the same width")
	}
}

// TestTheBreadcrumbNamesWhatWasDrilledThrough: "me > house-bills > sessions" says what escape goes
// back to, which a trail of resource names does not.
func TestTheBreadcrumbNamesWhatWasDrilledThrough(t *testing.T) {
	client := &fakeClient{
		workspaces: []*quaycrewv1.Workspace{{Id: "w1", Name: "me"}},
		projects:   []*quaycrewv1.Project{{Id: "p1", Workspace: "w1", Name: "house-bills"}},
		sessions:   []*quaycrewv1.Session{{Id: "s1", Workspace: "w1", Project: "p1", Status: "idle"}},
	}
	model := newTestModel(t, Workspaces(client), Projects(client), Sessions(client))
	model, _ = update(t, model, rowsFor(model, Row{ID: "w1", Label: "me", Cells: []string{"w1", "me", "1m"}}))
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = update(t, model, rowsFor(model,
		Row{ID: "p1", Label: "house-bills", Cells: []string{"p1", "house-bills", "me", "1m"}}))
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	view := model.View()
	for _, want := range []string{"me", "house-bills", "sessions", "esc to go back"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the breadcrumb does not name %q:\n%s", want, view)
		}
	}
}

func TestViewSaysWhenAViewIsEmpty(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	if !strings.Contains(model.View(), "nothing here") {
		t.Fatalf("an empty view should say so:\n%s", model.View())
	}
}

func TestTruncateMarksWhatItCut(t *testing.T) {
	if got := truncate("abcdefgh", 4); got != "abc…" {
		t.Fatalf("truncate = %q, want abc…", got)
	}
	if got := truncate("abc", 10); got != "abc" {
		t.Fatalf("truncate should leave short text alone, got %q", got)
	}
}

func TestNewRejectsAnUnknownStartingResource(t *testing.T) {
	registry, err := NewRegistry(staticResource("sessions"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := New(registry, "containers", nil); err == nil {
		t.Fatal("want an error opening on a resource that is not registered")
	}
}

// ---------- batched keys ----------

// A terminal can hand several runes over in one read, which is what pasting looks like. Before this
// was handled the whole message matched no binding and the keystrokes vanished, which showed up as
// the command bar simply not opening.

func TestABatchedKeyReadIsFoldedIntoSeparateKeypresses(t *testing.T) {
	model := newTestModel(t, staticResource("sessions", "s"), staticResource("workspaces", "p"))

	model, _ = update(t, model, runes(":p"))
	if model.mode != modeCommand {
		t.Fatalf("mode = %v, want the colon to have opened the command bar", model.mode)
	}
	if model.input != "p" {
		t.Fatalf("input = %q, want the p to have landed in the command bar", model.input)
	}

	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.active.Name != "workspaces" {
		t.Fatalf("active = %q, want workspaces", model.active.Name)
	}
}

func TestPastingIntoTheFilterKeepsEveryRune(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model, _ = update(t, model, rowsFor(model, row("alpha", "alpha", "one"), row("beta", "beta", "two")))

	model, _ = update(t, model, runes("/alph"))

	if model.filter != "alph" {
		t.Fatalf("filter = %q, want alph", model.filter)
	}
	if len(model.visibleRows()) != 1 {
		t.Fatalf("visible = %d, want the pasted filter applied", len(model.visibleRows()))
	}
}

// TestTheViewIsExactlyTheHeightOfTheWindow is the invariant the layout has to hold: draw one line
// too many and the terminal scrolls, which pushes the status block and the key hints off the top of
// the screen. That is what happened against a control plane too old to answer what it is running:
// the status block was one line, the key hints were three, and the body was sized off the status
// block alone.
func TestTheViewIsExactlyTheHeightOfTheWindow(t *testing.T) {
	client := &fakeClient{}
	cases := []struct {
		name string
		info Info
		rows int
		err  error
		size [2]int
	}{
		{name: "nothing known yet", size: [2]int{120, 24}},
		{name: "the crew answered", info: Info{
			Address: "localhost:50051", Model: "claude-code", Sandbox: "docker", Store: "postgres", StateKept: true,
		}, size: [2]int{120, 24}},
		{name: "more rows than fit", info: Info{Address: "here"}, rows: 40, size: [2]int{120, 24}},
		{name: "an error to show", err: errors.New("list sessions: nope"), size: [2]int{120, 24}},
		{name: "a short window", info: Info{Address: "here", Store: "memory"}, size: [2]int{120, 12}},
		{name: "a narrow window", info: Info{Address: "here"}, size: [2]int{60, 20}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := newTestModel(t, Sessions(client), Workspaces(client))
			model, _ = update(t, model, tea.WindowSizeMsg{Width: tc.size[0], Height: tc.size[1]})
			if tc.info != (Info{}) {
				model, _ = update(t, model, infoMsg{info: tc.info})
			}
			rows := make([]Row, 0, tc.rows)
			for i := 0; i < tc.rows; i++ {
				rows = append(rows, row(fmt.Sprintf("s%d", i), fmt.Sprintf("s%d", i), "me", "bills", "t", "idle", "1m"))
			}
			model, _ = update(t, model, rowsFor(model, rows...))
			if tc.err != nil {
				model, _ = update(t, model, errMsg{err: tc.err})
			}

			lines := strings.Split(model.View(), "\n")
			if len(lines) != tc.size[1] {
				t.Fatalf("the view is %d lines in a window %d high, so the top scrolls off:\n%s",
					len(lines), tc.size[1], model.View())
			}
			for index, line := range lines {
				if width := lipgloss.Width(line); width > tc.size[0] {
					t.Fatalf("line %d is %d wide in a window %d wide, so it wraps: %q", index, width, tc.size[0], line)
				}
			}
		})
	}
}
