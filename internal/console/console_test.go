package console

import (
	"context"
	"errors"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/grpc"
)

// fakeClient is a control plane double. It embeds the generated interface so unimplemented calls
// panic loudly rather than being silently satisfied.
type fakeClient struct {
	quaycrewv1.ControlPlaneServiceClient

	projects []*quaycrewv1.Project
	sessions []*quaycrewv1.Session

	listSessionsFor string
	stopped         []string
	listErr         error
}

func (f *fakeClient) ListProjects(context.Context, *quaycrewv1.ListProjectsRequest, ...grpc.CallOption) (*quaycrewv1.ListProjectsResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &quaycrewv1.ListProjectsResponse{Projects: f.projects}, nil
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
	model, err := New(registry, resources[0].Name)
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
	model := newTestModel(t, staticResource("sessions", "s"), staticResource("projects", "p"))

	model, _ = update(t, model, runes(":"))
	if model.mode != modeCommand {
		t.Fatalf("mode = %v, want modeCommand", model.mode)
	}
	model = typeAll(t, model, "p")
	model, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if model.active.Name != "projects" {
		t.Fatalf("active = %q, want projects", model.active.Name)
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
	model := newTestModel(t, staticResource("sessions", "s"), staticResource("projects", "p"))
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
		sessions: []*quaycrewv1.Session{
			{Id: "s1", Project: "acme", Status: "idle"},
			{Id: "s2", Project: "other", Status: "idle"},
		},
	}
	model := newTestModel(t, Projects(client), Sessions(client))
	model, _ = update(t, model, rowsFor(model, row("acme", "acme", "Acme"), row("other", "other", "Other")))

	model, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if model.active.Name != "sessions" {
		t.Fatalf("active = %q, want sessions", model.active.Name)
	}
	if model.parent != "acme" {
		t.Fatalf("parent = %q, want the selected project", model.parent)
	}
	if cmd == nil {
		t.Fatal("drilling did not trigger a listing")
	}

	msg, isRows := cmd().(rowsMsg)
	if !isRows {
		t.Fatalf("listing returned %T, want rowsMsg", cmd())
	}
	if len(msg.rows) != 1 || msg.rows[0].ID != "s1" {
		t.Fatalf("rows = %v, want only acme's session", msg.rows)
	}
	if client.listSessionsFor != "acme" {
		t.Fatalf("ListSessions asked for %q, want acme", client.listSessionsFor)
	}
}

func TestEscapeReturnsToTheParentViewWithItsSelection(t *testing.T) {
	client := &fakeClient{}
	model := newTestModel(t, Projects(client), Sessions(client))
	model, _ = update(t, model, rowsFor(model,
		row("acme", "acme", "Acme"), row("other", "other", "Other"), row("third", "third", "Third")))

	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.active.Name != "sessions" {
		t.Fatalf("active = %q, want sessions after drilling", model.active.Name)
	}

	model, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEsc})

	if model.active.Name != "projects" {
		t.Fatalf("active = %q, want projects after escape", model.active.Name)
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
	model := newTestModel(t, Projects(client), Sessions(client))
	model, _ = update(t, model, rowsFor(model, row("acme", "acme", "Acme")))
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	model, _ = update(t, model, runes(":"))
	model = typeAll(t, model, "projects")
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if len(model.stack) != 0 {
		t.Fatalf("stack depth = %d, want a jump to clear the trail", len(model.stack))
	}
}

// ---------- actions ----------

func TestStopActionStopsTheSelectedSession(t *testing.T) {
	client := &fakeClient{sessions: []*quaycrewv1.Session{{Id: "s1", Project: "acme", Status: "idle"}}}
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

	command := shell.Shell(Row{ID: "s1"})
	if command == nil {
		t.Fatal("shell action produced no command")
	}
	want := []string{"docker", "exec", "-it", "quaycrew-s1", "sh"}
	if strings.Join(command.Args, " ") != strings.Join(want, " ") {
		t.Fatalf("command = %v, want %v", command.Args, want)
	}
	if shell.Shell(Row{}) != nil {
		t.Fatal("want no command for a row with no session id")
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
	client := &fakeClient{sessions: []*quaycrewv1.Session{{Id: "s1", Project: "acme", Status: "idle"}}}

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

func TestViewShowsTheBreadcrumbCountAndKeyHints(t *testing.T) {
	client := &fakeClient{}
	model := newTestModel(t, Sessions(client), Projects(client))
	model, _ = update(t, model, rowsFor(model, Row{ID: "s1", Cells: []string{"s1", "acme", "", "idle", "1m"}}))

	view := model.View()
	for _, want := range []string{"sessions", "(1)", "Shell", "Stop", "Filter", "Quit"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not mention %q:\n%s", want, view)
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
	if _, err := New(registry, "containers"); err == nil {
		t.Fatal("want an error opening on a resource that is not registered")
	}
}

// ---------- batched keys ----------

// A terminal can hand several runes over in one read, which is what pasting looks like. Before this
// was handled the whole message matched no binding and the keystrokes vanished, which showed up as
// the command bar simply not opening.

func TestABatchedKeyReadIsFoldedIntoSeparateKeypresses(t *testing.T) {
	model := newTestModel(t, staticResource("sessions", "s"), staticResource("projects", "p"))

	model, _ = update(t, model, runes(":p"))
	if model.mode != modeCommand {
		t.Fatalf("mode = %v, want the colon to have opened the command bar", model.mode)
	}
	if model.input != "p" {
		t.Fatalf("input = %q, want the p to have landed in the command bar", model.input)
	}

	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.active.Name != "projects" {
		t.Fatalf("active = %q, want projects", model.active.Name)
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
