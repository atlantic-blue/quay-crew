package console

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// fakeClient is a control plane double. It embeds the generated interface so unimplemented calls
// panic loudly rather than being silently satisfied.
type fakeClient struct {
	quaycrewv1.ControlPlaneServiceClient

	workspaces []*quaycrewv1.Workspace
	projects   []*quaycrewv1.Project
	sessions   []*quaycrewv1.Session

	attachErr        error
	restartErr       error
	restarted        []string
	listSessionsFor  string
	listArchivedOnly bool
	stopped          []string
	archived         []string
	restored         []string
	modesSet         []string
	contexts         []*quaycrewv1.ContextDir
	secrets          []*quaycrewv1.SecretRef
	listErr          error
}

// GetInfo describes the crew the stats view reads. The double answers with something for every field,
// because a field the control plane does not answer for is a different scenario.
func (f *fakeClient) GetInfo(context.Context, *quaycrewv1.GetInfoRequest, ...grpc.CallOption) (*quaycrewv1.GetInfoResponse, error) {
	return &quaycrewv1.GetInfoResponse{
		Model: "claude-code", Sandbox: "docker", Store: "postgres",
		Secrets: "postgres", Events: "kafka", State: "host",
	}, nil
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
	f.listSessionsFor, f.listArchivedOnly = req.GetProject(), req.GetArchived()

	matched := make([]*quaycrewv1.Session, 0, len(f.sessions))
	for _, session := range f.sessions {
		if (session.GetArchivedAt() != nil) != req.GetArchived() {
			continue
		}
		if req.GetProject() != "" && session.GetProject() != req.GetProject() {
			continue
		}
		matched = append(matched, session)
	}
	return &quaycrewv1.ListSessionsResponse{Sessions: matched}, nil
}

func (f *fakeClient) ArchiveSession(_ context.Context, req *quaycrewv1.ArchiveSessionRequest, _ ...grpc.CallOption) (*quaycrewv1.ArchiveSessionResponse, error) {
	f.archived = append(f.archived, req.GetId())
	return &quaycrewv1.ArchiveSessionResponse{}, nil
}

func (f *fakeClient) ListSecrets(_ context.Context, _ *quaycrewv1.ListSecretsRequest, _ ...grpc.CallOption) (*quaycrewv1.ListSecretsResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &quaycrewv1.ListSecretsResponse{Secrets: f.secrets}, nil
}

func (f *fakeClient) ListContexts(_ context.Context, _ *quaycrewv1.ListContextsRequest, _ ...grpc.CallOption) (*quaycrewv1.ListContextsResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &quaycrewv1.ListContextsResponse{Dirs: f.contexts}, nil
}

func (f *fakeClient) SetSessionPermissionMode(_ context.Context, req *quaycrewv1.SetSessionPermissionModeRequest, _ ...grpc.CallOption) (*quaycrewv1.SetSessionPermissionModeResponse, error) {
	f.modesSet = append(f.modesSet, req.GetMode())
	return &quaycrewv1.SetSessionPermissionModeResponse{}, nil
}

func (f *fakeClient) RestoreSession(_ context.Context, req *quaycrewv1.RestoreSessionRequest, _ ...grpc.CallOption) (*quaycrewv1.RestoreSessionResponse, error) {
	f.restored = append(f.restored, req.GetId())
	return &quaycrewv1.RestoreSessionResponse{}, nil
}

func (f *fakeClient) StopSession(_ context.Context, req *quaycrewv1.StopSessionRequest, _ ...grpc.CallOption) (*quaycrewv1.StopSessionResponse, error) {
	f.stopped = append(f.stopped, req.GetId())
	return &quaycrewv1.StopSessionResponse{}, nil
}

func (f *fakeClient) RestartSession(_ context.Context, req *quaycrewv1.RestartSessionRequest, _ ...grpc.CallOption) (*quaycrewv1.RestartSessionResponse, error) {
	if f.restartErr != nil {
		return nil, f.restartErr
	}
	f.restarted = append(f.restarted, req.GetId())
	return &quaycrewv1.RestartSessionResponse{}, nil
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

// tallTestModel is a console with room for the whole help panel, which is taller than the default
// window now that it carries what the header used to.
func tallTestModel(t *testing.T, resources ...Resource) Model {
	t.Helper()
	model := newTestModel(t, resources...)
	model.height = 60
	return model
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

	model, _ = update(t, model, runes("x"))
	_, cmd := update(t, model, runes("y"))
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
	attach := actionBoundTo(t, Sessions(client), "a")

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

// TestEnterAttachesToTheSelectedThread is the point of the key: a thread has nothing to drill into,
// so enter did nothing at all, on the one view where the obvious key has an obvious meaning.
func TestEnterAttachesToTheSelectedThread(t *testing.T) {
	client := &fakeClient{}
	model := newTestModel(t, Sessions(client))
	model, _ = update(t, model, rowsFor(model, Row{ID: "s1", Cells: []string{"s1", "acme", "bills", "t1", "idle", "1m"}}))

	_, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on a thread produced no command")
	}
	if msg, isErr := cmd().(errMsg); isErr {
		t.Fatalf("enter on a thread failed: %v", msg.err)
	}
}

// TestEnterAndAOpenTheSameConversation: the old key keeps working, so muscle memory is not punished.
func TestEnterAndAOpenTheSameConversation(t *testing.T) {
	client := &fakeClient{}
	attach := actionBoundTo(t, Sessions(client), "enter")
	if !attach.Bound("a") {
		t.Fatal("the attach action no longer answers to a")
	}
	if attach.Label != "Open" {
		t.Fatalf("enter runs %q, want Open", attach.Label)
	}

	command, err := attach.Shell(Row{ID: "s1"})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	want := "docker exec --interactive --tty quaycrew-s1 claude --resume c1"
	if got := strings.Join(command.Args, " "); got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

// TestEnterOnAThreadWithNoConversationSaysWhy: opening something that errors is worse than being
// told to dispatch a turn first.
func TestEnterOnAThreadWithNoConversationSaysWhy(t *testing.T) {
	client := &fakeClient{attachErr: fmt.Errorf("session s1 has no conversation yet: dispatch a turn to it first")}
	model := newTestModel(t, Sessions(client))
	model, _ = update(t, model, rowsFor(model, Row{ID: "s1", Cells: []string{"s1", "acme", "bills", "t1", "idle", "1m"}}))

	_, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	msg, isErr := cmd().(errMsg)
	if !isErr {
		t.Fatalf("enter returned %#v, want the control plane's reason", cmd())
	}
	if !strings.Contains(msg.err.Error(), "no conversation yet") {
		t.Fatalf("the reason did not reach the operator: %v", msg.err)
	}
}

// TestEnterStillDrillsWhereThereIsSomewhereToGo: attaching must not cost the console its navigation.
func TestEnterStillDrillsWhereThereIsSomewhereToGo(t *testing.T) {
	client := &fakeClient{}
	model := newTestModel(t, Workspaces(client), Projects(client), Sessions(client))
	model, _ = update(t, model, rowsFor(model, Row{ID: "w1", Label: "me", Cells: []string{"w1", "me", "1m"}}))

	model, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.active.Name != "projects" {
		t.Fatalf("enter left the console on %q, want it drilled into projects", model.active.Name)
	}
	if cmd == nil {
		t.Fatal("drilling did not trigger a listing")
	}
}

// threadsAt builds a threads view with one thread listed and the cursor on it.
func threadsAt(t *testing.T, client *fakeClient) Model {
	t.Helper()
	model := newTestModel(t, Sessions(client))
	model, _ = update(t, model, rowsFor(model,
		Row{ID: "s1", Label: "d754610f", Cells: []string{"5d013d07", "acme", "bills", "d754610f", "idle", "1m"}}))
	return model
}

// TestBackspaceAsksBeforeItStops is the whole point of the confirmation: the list is full of
// conversations and there is no way back from stopping the wrong one.
func TestBackspaceAsksBeforeItStops(t *testing.T) {
	client := &fakeClient{}
	model, cmd := update(t, threadsAt(t, client), tea.KeyMsg{Type: tea.KeyBackspace})

	if model.mode != modeConfirm {
		t.Fatalf("mode = %v, want the console waiting for a yes", model.mode)
	}
	if cmd != nil {
		t.Fatal("backspace produced a command, want nothing to happen until yes")
	}
	if len(client.stopped) != 0 {
		t.Fatalf("stopped = %v, want nothing stopped yet", client.stopped)
	}
	if view := model.View(); !strings.Contains(view, "stop session d754610f?") {
		t.Fatalf("the console does not name what it is about to stop:\n%s", view)
	}
}

func TestYesStopsTheThreadItNamed(t *testing.T) {
	client := &fakeClient{}
	model, _ := update(t, threadsAt(t, client), tea.KeyMsg{Type: tea.KeyBackspace})
	model, cmd := update(t, model, runes("y"))

	if model.mode != modeBrowse {
		t.Fatalf("mode = %v, want back to browsing", model.mode)
	}
	if cmd == nil {
		t.Fatal("yes produced no command")
	}
	if msg, isDone := cmd().(actionDoneMsg); !isDone || msg.err != nil {
		t.Fatalf("yes returned %#v, want a clean actionDoneMsg", cmd())
	}
	if len(client.stopped) != 1 || client.stopped[0] != "s1" {
		t.Fatalf("stopped = %v, want [s1]", client.stopped)
	}
}

// TestAnythingOtherThanYesCancels: cancelling is the default, because an accidental cancel costs one
// keypress and an accidental yes costs a conversation.
func TestAnythingOtherThanYesCancels(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyEsc},
		runes("n"),
		runes("Y"),
		{Type: tea.KeyEnter},
	} {
		t.Run(key.String(), func(t *testing.T) {
			client := &fakeClient{}
			model, _ := update(t, threadsAt(t, client), tea.KeyMsg{Type: tea.KeyBackspace})
			model, cmd := update(t, model, key)

			if model.mode != modeBrowse {
				t.Fatalf("mode = %v, want back to browsing", model.mode)
			}
			if cmd != nil {
				t.Fatalf("%s produced a command, want the action cancelled", key.String())
			}
			if len(client.stopped) != 0 {
				t.Fatalf("stopped = %v, want nothing stopped", client.stopped)
			}
		})
	}
}

// TestXStopsThroughTheSameConfirmation: the old key is not a way around the question.
func TestXStopsThroughTheSameConfirmation(t *testing.T) {
	client := &fakeClient{}
	model, _ := update(t, threadsAt(t, client), runes("x"))

	if model.mode != modeConfirm {
		t.Fatalf("mode = %v, want x to ask too", model.mode)
	}
	if len(client.stopped) != 0 {
		t.Fatalf("stopped = %v, want nothing stopped yet", client.stopped)
	}
}

// TestAnUnconfirmedActionStillActsAtOnce: attaching is not destructive and must not grow a question.
func TestAnUnconfirmedActionStillActsAtOnce(t *testing.T) {
	model, cmd := update(t, threadsAt(t, &fakeClient{}), tea.KeyMsg{Type: tea.KeyEnter})

	if model.mode != modeBrowse {
		t.Fatalf("mode = %v, want attaching to act at once", model.mode)
	}
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
}

// TestTheConfirmationSurvivesARefreshUnderneathIt: a listing arriving between the question and the
// answer must not turn a yes into a yes to a different conversation.
func TestTheConfirmationSurvivesARefreshUnderneathIt(t *testing.T) {
	client := &fakeClient{}
	model, _ := update(t, threadsAt(t, client), tea.KeyMsg{Type: tea.KeyBackspace})

	model, _ = update(t, model, rowsFor(model,
		Row{ID: "s2", Label: "aaaa1111", Cells: []string{"other", "acme", "bills", "aaaa1111", "idle", "1m"}},
		Row{ID: "s1", Label: "d754610f", Cells: []string{"5d013d07", "acme", "bills", "d754610f", "idle", "2m"}}))
	_, cmd := update(t, model, runes("y"))
	if cmd != nil {
		cmd()
	}

	if len(client.stopped) != 1 || client.stopped[0] != "s1" {
		t.Fatalf("stopped = %v, want the thread the console named, [s1]", client.stopped)
	}
}

// TestRefreshIsBoundToRAndG: refreshing is the key reached for constantly, so it holds the short
// obvious letter.
func TestRefreshIsBoundToRAndG(t *testing.T) {
	for _, key := range []string{"r", "g"} {
		t.Run(key, func(t *testing.T) {
			client := &fakeClient{}
			_, cmd := update(t, threadsAt(t, client), runes(key))
			if cmd == nil {
				t.Fatalf("%s produced no command, want a listing", key)
			}
			if _, isRows := cmd().(rowsMsg); !isRows {
				t.Fatalf("%s returned %#v, want a listing", key, cmd())
			}
		})
	}
}

// TestRNoLongerRestarts is the way off the old spelling. The tests for `R` all pass while `r` quietly
// keeps restarting, which is how a key that moved carries on doing the old thing.
func TestRNoLongerRestarts(t *testing.T) {
	client := &fakeClient{}
	_, cmd := update(t, threadsAt(t, client), runes("r"))
	if cmd != nil {
		cmd()
	}
	if len(client.restarted) != 0 {
		t.Fatalf("r restarted %v, want it to refresh instead", client.restarted)
	}
}

// TestRestartBringsAThreadBackWithoutAsking: restarting is not destructive, so it acts at once.
func TestRestartBringsAThreadBackWithoutAsking(t *testing.T) {
	client := &fakeClient{}
	model, cmd := update(t, threadsAt(t, client), runes("R"))

	if model.mode != modeBrowse {
		t.Fatalf("mode = %v, want restarting to act at once", model.mode)
	}
	if cmd == nil {
		t.Fatal("restart produced no command")
	}
	if msg, isDone := cmd().(actionDoneMsg); !isDone || msg.err != nil {
		t.Fatalf("restart returned %#v, want a clean actionDoneMsg", cmd())
	}
	if len(client.restarted) != 1 || client.restarted[0] != "s1" {
		t.Fatalf("restarted = %v, want [s1]", client.restarted)
	}
}

// TestRestartingARunningThreadShowsTheRefusal: the control plane decides whether there is anything to
// restart, and its reason is what the operator has to see.
func TestRestartingARunningThreadShowsTheRefusal(t *testing.T) {
	client := &fakeClient{restartErr: fmt.Errorf("session s1 is idle, not stopped, so there is nothing to restart")}
	_, cmd := update(t, threadsAt(t, client), runes("R"))
	if cmd == nil {
		t.Fatal("restart produced no command")
	}
	msg, isDone := cmd().(actionDoneMsg)
	if !isDone || msg.err == nil {
		t.Fatalf("restart returned %#v, want the refusal", cmd())
	}
	if !strings.Contains(msg.err.Error(), "nothing to restart") {
		t.Fatalf("the reason did not reach the operator: %v", msg.err)
	}
}

// TestArchiveAsksBeforePuttingAThreadAway: a thread that vanishes from the list under an accidental
// keypress reads exactly like one that was deleted, and this one is not.
func TestArchiveAsksBeforePuttingAThreadAway(t *testing.T) {
	client := &fakeClient{}
	model, _ := update(t, threadsAt(t, client), runes("A"))

	if model.mode != modeConfirm {
		t.Fatalf("mode = %v, want the console waiting for a yes", model.mode)
	}
	if view := model.View(); !strings.Contains(view, "archive session d754610f?") {
		t.Fatalf("the console does not name what it is about to archive:\n%s", view)
	}
	if len(client.archived) != 0 {
		t.Fatalf("archived = %v, want nothing archived yet", client.archived)
	}

	_, cmd := update(t, model, runes("y"))
	if cmd == nil {
		t.Fatal("yes produced no command")
	}
	if msg, isDone := cmd().(actionDoneMsg); !isDone || msg.err != nil {
		t.Fatalf("archiving returned %#v, want a clean actionDoneMsg", cmd())
	}
	if len(client.archived) != 1 || client.archived[0] != "s1" {
		t.Fatalf("archived = %v, want [s1]", client.archived)
	}
}

// TestTheTwoListingsNeverMix is what makes archiving worth having: a thread the operator put away
// must not come back into the view they put it away from.
func TestTheTwoListingsNeverMix(t *testing.T) {
	client := &fakeClient{sessions: []*quaycrewv1.Session{
		{Id: "live", Workspace: "acme", ThreadId: "t1", Status: "idle"},
		{Id: "away", Workspace: "acme", ThreadId: "t2", Status: "stopped", ArchivedAt: timestamppb.Now()},
	}}

	threads, err := Sessions(client).List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing threads: %v", err)
	}
	if len(threads) != 1 || threads[0].ID != "live" {
		t.Fatalf("the threads view lists %v, want only the live one", rowIDs(threads))
	}
	if client.listArchivedOnly {
		t.Fatal("the threads view asked the control plane for archived threads")
	}

	archived, err := Archived(client).List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing archived: %v", err)
	}
	if len(archived) != 1 || archived[0].ID != "away" {
		t.Fatalf("the archived view lists %v, want only the one put away", rowIDs(archived))
	}
	if !client.listArchivedOnly {
		t.Fatal("the archived view asked the control plane for live threads")
	}
}

// TestRestoreBringsAThreadBack: nothing was deleted, so the only thing to do in there is undo it.
func TestRestoreBringsAThreadBack(t *testing.T) {
	client := &fakeClient{}
	model := newTestModel(t, Archived(client))
	model, _ = update(t, model, rowsFor(model,
		Row{ID: "s1", Label: "d754610f", Cells: []string{"5d013d07", "acme", "bills", "d754610f", "stopped", "2h"}}))

	_, cmd := update(t, model, runes("u"))
	if cmd == nil {
		t.Fatal("restore produced no command")
	}
	if msg, isDone := cmd().(actionDoneMsg); !isDone || msg.err != nil {
		t.Fatalf("restore returned %#v, want a clean actionDoneMsg", cmd())
	}
	if len(client.restored) != 1 || client.restored[0] != "s1" {
		t.Fatalf("restored = %v, want [s1]", client.restored)
	}
}

// TestTheArchivedViewSaysWhenAThreadWasPutAway: its last column is the stamp, not the last touch,
// because "two hours ago" about a thread nobody has touched since is the useful number.
func TestTheArchivedViewSaysWhenAThreadWasPutAway(t *testing.T) {
	client := &fakeClient{sessions: []*quaycrewv1.Session{{
		Id: "away", Workspace: "acme", ThreadId: "t2", Status: "stopped",
		UpdatedAt:  timestamppb.New(time.Now().Add(-72 * time.Hour)),
		ArchivedAt: timestamppb.New(time.Now().Add(-2 * time.Hour)),
	}}}

	rows, err := Archived(client).List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing archived: %v", err)
	}
	if got := rows[0].Cells[6]; got != "2h" {
		t.Fatalf("the last column reads %q, want 2h, when it was put away", got)
	}
}

func rowIDs(rows []Row) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.ID)
	}
	return out
}

// TestTheDangerousToggleAsksAndFlipsBothWays. Arming a thread asks
// first, like every key that changes what it is allowed to do, and pressing it again puts it back.
func TestTheDangerousToggleAsksAndFlipsBothWays(t *testing.T) {
	client := &fakeClient{}
	model, _ := update(t, threadsAt(t, client), runes("D"))

	if model.mode != modeConfirm {
		t.Fatalf("mode = %v, want the console waiting for a yes", model.mode)
	}
	if view := model.View(); !strings.Contains(view, "dangerous session d754610f?") {
		t.Fatalf("the console does not name the thread it is about to arm:\n%s", view)
	}
	if len(client.modesSet) != 0 {
		t.Fatalf("modes set = %v, want nothing armed yet", client.modesSet)
	}

	_, cmd := update(t, model, runes("y"))
	if cmd == nil {
		t.Fatal("yes produced no command")
	}
	cmd()
	if len(client.modesSet) != 1 || client.modesSet[0] != "bypassPermissions" {
		t.Fatalf("modes set = %v, want [bypassPermissions]", client.modesSet)
	}

	// An armed thread goes back to asking, rather than being armed a second time.
	armed := newTestModel(t, Sessions(client))
	armed, _ = update(t, armed, rowsFor(armed,
		Row{ID: "s1", Label: "d754610f", Cells: []string{"5d013d07", "acme", "bills", "d754610f", "idle", "dangerous", "1m"}}))
	armed, _ = update(t, armed, runes("D"))
	_, cmd = update(t, armed, runes("y"))
	cmd()
	if len(client.modesSet) != 2 || client.modesSet[1] != "acceptEdits" {
		t.Fatalf("modes set = %v, want the second one to disarm it", client.modesSet)
	}
}

// TestTheModeIsInTheListing: a thread that skips every permission must not look like one that asks.
func TestTheModeIsInTheListing(t *testing.T) {
	tests := map[string]string{
		"bypassPermissions": "dangerous",
		"plan":              "plan",
		"acceptEdits":       "edits",
		// A thread from before the mode was written down has none, and every one of those runs
		// acceptEdits. An empty cell here would read as "asks first", which is the opposite.
		"": "edits",
	}
	for mode, want := range tests {
		t.Run(mode, func(t *testing.T) {
			client := &fakeClient{sessions: []*quaycrewv1.Session{
				{Id: "s1", Workspace: "acme", ThreadId: "t1", Status: "idle", PermissionMode: mode},
			}}
			rows, err := Sessions(client).List(context.Background(), "")
			if err != nil {
				t.Fatalf("listing threads: %v", err)
			}
			if got := rows[0].Cells[permissionColumn]; got != want {
				t.Fatalf("mode %q reads as %q, want %q", mode, got, want)
			}
		})
	}
}

// TestTheContextViewSaysWhereToEditAndWhetherAnythingIsThere: an empty directory is the normal state
// and says nothing, so whether the memory file exists is a column rather than something to infer.
func TestTheContextViewSaysWhereToEditAndWhetherAnythingIsThere(t *testing.T) {
	client := &fakeClient{contexts: []*quaycrewv1.ContextDir{
		{Scope: "workspace", Name: "demo", Host: "/data/workspaces/w1/claude",
			Sandbox: "/home/agent/.claude", Memory: "/data/workspaces/w1/claude/CLAUDE.md"},
		{Scope: "project", Name: "default", Owner: "p1", Host: "/data/workspaces/w1/projects/p1/workspace",
			Sandbox: "/home/agent/workspace", Memory: "/data/workspaces/w1/projects/p1/workspace/CLAUDE.md",
			Written: true, Body: "pay the water bill first"},
	}}

	rows, err := Contexts(client).List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing contexts: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the context view lists %d rows, want the workspace and the project", len(rows))
	}
	if got := rows[0].Cells[2]; got != "not written yet" {
		t.Fatalf("an unwritten memory file reads as %q, want it said out loud", got)
	}
	if got := rows[1].Cells[2]; got != "written" {
		t.Fatalf("a written memory file reads as %q", got)
	}
	// The row carries the level, because that is what an action on it acts on.
	if rows[1].ID != "project/p1" {
		t.Fatalf("the row carries %q, want the level to set", rows[1].ID)
	}
	if rows[1].Detail != "pay the water bill first" {
		t.Fatalf("the row carries %q, want the whole of what the level says", rows[1].Detail)
	}
}

// TestEditingContextOpensTheOperatorsEditor: the console suspends itself to run a command, which is
// how opening a thread works, so an editor is the same mechanism. The file is created by the editor
// saving it, but the directory has to be there first or an editor writing into a sandbox that has
// never run fails on a path nobody made.
func TestEditingContextOpensTheOperatorsEditor(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "never", "made", "CLAUDE.md")
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "vi -u NONE")

	edit := actionBoundTo(t, Contexts(&fakeClient{}), "enter")
	if edit.Label != "Edit" {
		t.Fatalf("enter runs %q, want Edit", edit.Label)
	}
	row := Row{Cells: []string{"project", "bills", "written", "pay the water bill"}, Parent: "p1",
		Detail: "pay the water bill first"}
	command, err := edit.Shell(row)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	draft := command.Args[len(command.Args)-1]
	if !strings.HasPrefix(strings.Join(command.Args, " "), "vi -u NONE ") {
		t.Fatalf("command = %q, want the editor and a file", command.Args)
	}
	// Seeded with what the level already says, or editing would be starting again every time.
	seeded, err := os.ReadFile(draft)
	if err != nil {
		t.Fatalf("the draft was not written: %v", err)
	}
	if string(seeded) != row.Detail {
		t.Fatalf("the draft reads %q, want what the level already says", seeded)
	}
	_ = file
}

// TestTheEditorIsTheirsThenVi is the order git and crontab use. Refusing when neither is set made the
// whole feature dead on a machine with no EDITOR exported, which is most machines, including the one
// this was built on.
func TestTheEditorIsTheirsThenVi(t *testing.T) {
	tests := []struct {
		name           string
		visual, editor string
		want           string
	}{
		{"neither set", "", "", "vi"},
		{"EDITOR set", "", "nano", "nano"},
		{"VISUAL wins", "code -w", "nano", "code -w"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("VISUAL", test.visual)
			t.Setenv("EDITOR", test.editor)
			if got := Editor(); got != test.want {
				t.Fatalf("Editor() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestEditingOpensSomethingWithNoEditorSet: the operator presses the key and something opens.
func TestEditingOpensSomethingWithNoEditorSet(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	file := filepath.Join(t.TempDir(), "CLAUDE.md")

	edit := actionBoundTo(t, Contexts(&fakeClient{}), "e")
	command, err := edit.Shell(Row{Cells: []string{"crew", "crew", "", ""}})
	if err != nil {
		t.Fatalf("editing with nothing set: %v", err)
	}
	if got := command.Args[0]; got != "vi" {
		t.Fatalf("command = %q, want vi", command.Args)
	}
	_ = file
}

// TestTheContextViewIsRegistered: it is no use if the command bar cannot reach it.
func TestTheContextViewIsRegistered(t *testing.T) {
	registry, err := NewDefaultRegistry(&fakeClient{})
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	for _, token := range []string{"context", "contexts", "ctx", "c"} {
		resource, found := registry.Resolve(token)
		if !found {
			t.Fatalf("Resolve(%q): not found", token)
		}
		if resource.Name != "context" {
			t.Fatalf("Resolve(%q) = %q, want context", token, resource.Name)
		}
	}
}

// TestTheCommandBarOffersWhatItCanOpen: pressing colon asked a question and gave nothing to answer it
// with, so the only way to learn a view's name was to know it already.
func TestTheCommandBarOffersWhatItCanOpen(t *testing.T) {
	client := &fakeClient{}
	model := newTestModel(t, Sessions(client), Archived(client), Projects(client), Contexts(client))

	model, _ = update(t, model, runes(":"))
	view := model.View()
	for _, want := range []string{"sessions", "archived", "projects", "context"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the command bar does not offer %q:\n%s", want, view)
		}
	}

	// Typing narrows it, so a long list becomes the one you meant.
	model = typeAll(t, model, "a")
	view = model.View()
	if !strings.Contains(view, "archived") {
		t.Fatalf("typing a does not offer archived:\n%s", view)
	}
	if strings.Contains(view, "projects") {
		t.Fatalf("typing a still offers projects:\n%s", view)
	}

	model = typeAll(t, model, "zzz")
	if view := model.View(); !strings.Contains(view, "nothing called that") {
		t.Fatalf("a prefix matching nothing says nothing about it:\n%s", view)
	}
}

// TestTheQuestionMarkListsTheViews: the keys were all in there and the views were not, so the command
// bar was the one thing the help could not help with.
func TestTheQuestionMarkListsTheViews(t *testing.T) {
	client := &fakeClient{}
	// Tall enough to show the whole panel at once: everything the header used to carry lives in it
	// now, and a shorter window scrolls rather than dropping the end.
	model := tallTestModel(t, Sessions(client), Archived(client), Contexts(client))
	model, _ = update(t, model, runes("?"))

	view := model.View()
	for _, want := range []string{"views, with :", "sessions", "archived", "context"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the key list does not name %q:\n%s", want, view)
		}
	}
	// And the briefest way to type one, which is the point of listing them.
	if !strings.Contains(view, "<context c>") {
		t.Fatalf("the key list does not say what to type for the context view:\n%s", view)
	}
}

// actionBoundTo returns the action a key runs, failing the test when nothing is bound to it.
func actionBoundTo(t *testing.T, resource Resource, key string) Action {
	t.Helper()
	for _, action := range resource.Actions {
		if action.Bound(key) {
			return action
		}
	}
	t.Fatalf("%s has nothing bound to %q", resource.Name, key)
	return Action{}
}

// ---------- resources ----------

// TestTheConsoleCallsThemSessions: one name across the whole system. The database, the API and the
// console all say session, so nobody has to translate between a listing and a query.
func TestTheConsoleCallsThemSessions(t *testing.T) {
	client := &fakeClient{}
	if got := Sessions(client).Name; got != "sessions" {
		t.Fatalf("the view is called %q, want sessions", got)
	}
	if Default != "sessions" {
		t.Fatalf("the console opens on %q, want sessions", Default)
	}

	model := newTestModel(t, Sessions(client), Projects(client))
	model, _ = update(t, model, rowsFor(model, Row{ID: "s1", Cells: []string{"s1", "acme", "bills", "t1", "idle", "1m"}}))

	view := model.View()
	if !strings.Contains(view, "sessions[1]") {
		t.Fatalf("the panel is not titled sessions:\n%s", view)
	}
	if !strings.Contains(view, "<sessions>") {
		t.Fatalf("the breadcrumb does not say sessions:\n%s", view)
	}
	// The word it had for a day is gone from the chrome, and lives on only as something to type.
	if strings.Contains(view, "threads") {
		t.Fatalf("the console still says threads somewhere:\n%s", view)
	}
}

// TestThreadsStillOpensTheSessionsView keeps the muscle memory working, in both directions: this view
// has been called both things inside a day.
func TestThreadsStillOpensTheSessionsView(t *testing.T) {
	client := &fakeClient{}
	registry, err := NewDefaultRegistry(client)
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	for _, token := range []string{"sessions", "session", "sess", "s", "threads", "thread", "t"} {
		resource, found := registry.Resolve(token)
		if !found {
			t.Fatalf("Resolve(%q): not found", token)
		}
		if resource.Name != "sessions" {
			t.Fatalf("Resolve(%q) = %q, want sessions", token, resource.Name)
		}
	}
}

// TestDrillingIntoAProjectLandsOnItsSessions: the rename has to carry the drill target with it, or
// enter on a project dead ends on a resource nobody registers.
func TestDrillingIntoAProjectLandsOnItsSessions(t *testing.T) {
	client := &fakeClient{}
	registry, err := NewDefaultRegistry(client)
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	projects, _ := registry.Get("projects")
	if projects.DrillTo != "sessions" {
		t.Fatalf("projects drills into %q, want sessions", projects.DrillTo)
	}
	if _, found := registry.Get(projects.DrillTo); !found {
		t.Fatalf("projects drills into %q, which nothing registers", projects.DrillTo)
	}
}

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

// TestTheFeaturesViewAsksTheControlPlaneNothing is the point of that view: it is what an operator
// opens before they have a stack, and a capability belongs to the build rather than to a running one.
func TestTheFeaturesViewAsksTheControlPlaneNothing(t *testing.T) {
	rows, err := Features().List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing features: %v", err)
	}
	if len(rows) < 10 {
		t.Fatalf("the features view lists %d rows, want every scenario in the build", len(rows))
	}

	var titled int
	for _, row := range rows {
		if row.Cells[0] != "" {
			titled++
		}
		if row.Cells[1] == "" {
			t.Fatalf("a row names no scenario: %+v", row)
		}
	}
	// One title per feature, then its scenarios beneath it, rather than the title repeated down the
	// column.
	if titled < 5 || titled >= len(rows) {
		t.Fatalf("%d of %d rows carry a feature title, want one per feature", titled, len(rows))
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

// TestTheHeaderIsTheWordmarkTheBuildAndTheWayToEverythingElse.
//
// The header is as wide as the console, and the console is half the window once a conversation is
// beside it. A column of key hints and ten lines of status left no room for the wordmark, so both
// moved into the help panel and the header keeps what cannot be looked up.
func TestTheHeaderIsTheWordmarkTheBuildAndTheWayToEverythingElse(t *testing.T) {
	client := &fakeClient{}
	model := newTestModel(t, Sessions(client))
	model.info = Info{
		Version: "21fca40", Address: "localhost:50051", Workspace: "juliantellez",
		Project: "quay-crew", Model: "claude-code", Sandbox: "docker", Store: "postgres",
	}
	shown := strings.Join(model.headerLines(), "\n")

	if !strings.Contains(shown, "21fca40") {
		t.Fatalf("the header does not say which build this is:\n%s", shown)
	}
	if !strings.Contains(shown, "Help") {
		t.Fatalf("the header does not say how to reach everything else:\n%s", shown)
	}
	if !strings.Contains(shown, logo[0]) {
		t.Fatalf("the wordmark is missing from the header:\n%s", shown)
	}
	for _, moved := range []string{"localhost:50051", "Sandbox engine", "Open", "Restart"} {
		if strings.Contains(shown, moved) {
			t.Fatalf("the header still carries %q, which belongs in the help panel:\n%s", moved, shown)
		}
	}
}

// TestTheWordmarkSurvivesAConversationBesideIt, which is the whole reason the rest moved out: the
// console is half the window then, and the wordmark was the thing that lost.
func TestTheWordmarkSurvivesAConversationBesideIt(t *testing.T) {
	// Down to half of a 168 column window, which is what a conversation beside the console leaves.
	// The wordmark is 35 columns wide, so below roughly 80 it genuinely does not fit and is dropped
	// rather than drawn over the top of something.
	for _, width := range []int{170, 99, 84} {
		model := newTestModel(t, Sessions(&fakeClient{}))
		model.width = width
		model.info = Info{Version: "21fca40", Address: "localhost:50051", Workspace: "juliantellez"}

		if !strings.Contains(strings.Join(model.headerLines(), "\n"), logo[0]) {
			t.Fatalf("at %d columns the wordmark is gone:\n%s", width, strings.Join(model.headerLines(), "\n"))
		}
	}
}

// TestTheQuestionMarkListsEveryKey is where the keys the header does not show have to live.
func TestTheQuestionMarkListsEveryKey(t *testing.T) {
	client := &fakeClient{}
	model := tallTestModel(t, Sessions(client), Workspaces(client))
	model, _ = update(t, model, rowsFor(model, Row{ID: "s1", Cells: []string{"s1", "acme", "", "idle", "1m"}}))

	model, _ = update(t, model, runes("?"))
	view := model.View()
	for _, want := range []string{"help(sessions)", "Quit", "Refresh now", "Filter these rows", "<enter a> Open", "<ctrl-q> Leave a conversation running"} {
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

// TestTheHelpPanelCarriesWhatTheHeaderDropped. The information was moved, not deleted, and the help
// panel is where it went.
func TestTheHelpPanelCarriesWhatTheHeaderDropped(t *testing.T) {
	model := tallTestModel(t, Sessions(&fakeClient{}))
	model.info = Info{
		Version: "21fca40", Address: "localhost:50051", Workspace: "juliantellez",
		Project: "quay-crew", Model: "claude-code", Sandbox: "docker", Store: "postgres",
		Secrets: "postgres", Events: "kafka", State: "host",
	}
	model, _ = update(t, model, runes("?"))
	shown := model.View()

	for _, want := range []string{
		"this crew", "localhost:50051", "juliantellez", "quay-crew", "claude-code",
		"Sandbox engine", "Store engine", "Secrets", "Events engine", "State",
	} {
		if !strings.Contains(shown, want) {
			t.Fatalf("the help panel does not carry %q, so moving it out of the header lost it:\n%s", want, shown)
		}
	}
	// And this view's own keys, which also left the header.
	if !strings.Contains(shown, "Open") {
		t.Fatalf("the help panel does not say what enter does:\n%s", shown)
	}
}

// TestTheWordmarkIsThereBeforeTheCrewAnswers is how it went missing: against a control plane too old
// to say what it is running, the status block is three lines, and the mark is six.
func TestTheWordmarkIsThereBeforeTheCrewAnswers(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model, _ = update(t, model, tea.WindowSizeMsg{Width: 150, Height: 30})
	model, _ = update(t, model, infoMsg{info: Info{Version: "709b79e", Address: "localhost:50051", Workspace: "demo", Project: "default"}})

	if !strings.Contains(model.View(), logo[0]) {
		t.Fatalf("the wordmark is missing when the status block is short:\n%s", model.View())
	}
}

// TestTheWordmarkFitsWhereverTheHeaderDoes. It used to be six lines of block letters, so it was the
// first thing dropped when the window was small and the whole header cost six rows to say the name.
// One line fits beside the version at every width worth drawing a console in.
func TestTheWordmarkFitsWhereverTheHeaderDoes(t *testing.T) {
	for _, size := range [][2]int{{140, 30}, {84, 24}, {70, 30}, {140, 12}} {
		model := newTestModel(t, staticResource("sessions"))
		model, _ = update(t, model, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		model, _ = update(t, model, infoMsg{info: Info{Version: "5fd7bee", Address: "localhost:50051"}})

		if !strings.Contains(model.View(), logo[0]) {
			t.Fatalf("at %dx%d the wordmark is gone:\n%s", size[0], size[1], model.View())
		}
	}
}

// TestTheHeaderCostsOneRow. It sits above both halves of the panel, so every row it takes is a row the
// list and the conversation lose.
func TestTheHeaderCostsOneRow(t *testing.T) {
	model := newTestModel(t, Sessions(&fakeClient{}))
	model.info = Info{
		Version: "21fca40", Address: "localhost:50051", Workspace: "juliantellez",
		Model: "claude-code", Sandbox: "docker", Store: "postgres",
	}
	if got := len(model.headerLines()); got != 1 {
		t.Fatalf("the header is %d rows:\n%s", got, strings.Join(model.headerLines(), "\n"))
	}
	// One row, and it still carries all three things.
	only := model.headerLines()[0]
	for _, want := range []string{"21fca40", "Help", logo[0]} {
		if !strings.Contains(only, want) {
			t.Fatalf("the header's one row does not carry %q:\n%s", want, only)
		}
	}
}

// TestTheHelpPanelSaysWhichCrewItIsConnectedTo, so an operator with two of them can tell which one
// they are about to act on.
func TestTheHelpPanelSaysWhichCrewItIsConnectedTo(t *testing.T) {
	model := tallTestModel(t, Sessions(&fakeClient{}))
	model.info = Info{Version: "dev", Address: "localhost:50051", Store: "postgres"}
	model, _ = update(t, model, runes("?"))

	if !strings.Contains(model.View(), "localhost:50051") {
		t.Fatalf("the help panel does not say which crew this is:\n%s", model.View())
	}
}

// TestTheHelpPanelSaysWhenAConversationWouldBeLost, which is the one line in there that is a warning
// rather than a fact.
func TestTheHelpPanelSaysWhenAConversationWouldBeLost(t *testing.T) {
	model := tallTestModel(t, Sessions(&fakeClient{}))
	model.info = Info{Version: "dev", Store: "postgres", State: ""}
	model, _ = update(t, model, runes("?"))

	if !strings.Contains(model.View(), "lost when it is replaced") {
		t.Fatalf("the help panel does not warn that state is lost:\n%s", model.View())
	}
}

// TestStatusBlockSaysNothingItWasNotTold guards against the console inventing a reassuring answer
// when the control plane never replied.
func TestStatusBlockSaysNothingItWasNotTold(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	view := model.View()
	for _, unwanted := range []string{"host directory", "lost when it is replaced", "postgres", "docker"} {
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
			Address: "localhost:50051", Model: "claude-code", Sandbox: "docker", Store: "postgres",
			State: "host directory /Users/x/.quaycrew/data",
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

// TestTheSecretsViewNeverShowsAValue is the whole point of the view: an operator needs to know the
// token is there, and a value on a screen is a value in a terminal's scrollback.
func TestTheSecretsViewNeverShowsAValue(t *testing.T) {
	client := &fakeClient{secrets: []*quaycrewv1.SecretRef{
		{Workspace: "w1", WorkspaceName: "demo", Name: "CLAUDE_CODE_OAUTH_TOKEN"},
	}}

	rows, err := Secrets(client).List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing secrets: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the secrets view lists %d rows, want the one that is set", len(rows))
	}
	if rows[0].Cells[0] != "demo" || rows[0].Cells[1] != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Fatalf("the row reads %v, want the workspace's name and the secret's", rows[0].Cells)
	}
	// The value column says the thing is set and nothing else. Said out loud rather than left blank,
	// so nobody wonders whether the column is empty because the value is.
	if rows[0].Cells[2] != "set, and not shown anywhere" {
		t.Fatalf("the value column reads %q, and the only safe thing for it to read is that it is set",
			rows[0].Cells[2])
	}
	// There is nowhere for a value to be, by construction: the reference carries none.
	if len(Secrets(client).Actions) != 0 {
		t.Fatal("the secrets view has an action on a row, which is where a value would end up")
	}
}

// wizardClient records everything the wizard asks the crew to make.
type wizardClient struct {
	fakeClient
	workspaces, projects, secrets, contexts, dispatched []string
	// projectsIn is the workspace each project was made in, so a scenario can say the parent was the
	// one that already existed rather than one made on the way.
	projectsIn []string
}

func (w *wizardClient) CreateWorkspace(_ context.Context, req *quaycrewv1.CreateWorkspaceRequest, _ ...grpc.CallOption) (*quaycrewv1.CreateWorkspaceResponse, error) {
	w.workspaces = append(w.workspaces, req.GetName())
	return &quaycrewv1.CreateWorkspaceResponse{Workspace: &quaycrewv1.Workspace{Id: "w1", Name: req.GetName()}}, nil
}

func (w *wizardClient) CreateProject(_ context.Context, req *quaycrewv1.CreateProjectRequest, _ ...grpc.CallOption) (*quaycrewv1.CreateProjectResponse, error) {
	w.projects = append(w.projects, req.GetName())
	w.projectsIn = append(w.projectsIn, req.GetWorkspace())
	return &quaycrewv1.CreateProjectResponse{Project: &quaycrewv1.Project{Id: "p1", Name: req.GetName()}}, nil
}

func (w *wizardClient) SetSecret(_ context.Context, req *quaycrewv1.SetSecretRequest, _ ...grpc.CallOption) (*quaycrewv1.SetSecretResponse, error) {
	w.secrets = append(w.secrets, req.GetKey())
	return &quaycrewv1.SetSecretResponse{}, nil
}

func (w *wizardClient) SetContext(_ context.Context, req *quaycrewv1.SetContextRequest, _ ...grpc.CallOption) (*quaycrewv1.SetContextResponse, error) {
	w.contexts = append(w.contexts, req.GetBody())
	return &quaycrewv1.SetContextResponse{}, nil
}

func (w *wizardClient) Dispatch(_ context.Context, req *quaycrewv1.DispatchRequest, _ ...grpc.CallOption) (*quaycrewv1.DispatchResponse, error) {
	w.dispatched = append(w.dispatched, req.GetText())
	return &quaycrewv1.DispatchResponse{}, nil
}

// made is everything the wizard asked the crew for, in one number. It is what "and nothing else" is
// asserted against.
func (w *wizardClient) made() int {
	return len(w.workspaces) + len(w.projects) + len(w.secrets) + len(w.contexts) + len(w.dispatched)
}

// wizardAt opens the console on a crew that already has a workspace and a project in it, which is the
// situation the wizard could do nothing with: everything it made, it made from nothing.
func wizardAt(t *testing.T, client *wizardClient) Model {
	t.Helper()
	client.fakeClient.workspaces = []*quaycrewv1.Workspace{{Id: "w-acme", Name: "acme"}}
	client.fakeClient.projects = []*quaycrewv1.Project{{Id: "p-bills", Workspace: "w-acme", Name: "house-bills"}}
	model := newTestModel(t, Sessions(client)).WithClient(client)
	model, _ = update(t, model, runes("n"))
	return model
}

// answer types one answer and accepts it, running whatever the console asks for and feeding the
// result back, which is what the bubbletea runtime does with no terminal in the way. A step that
// offers what already exists loads it that way, so the next answer has something to resolve against.
func answer(t *testing.T, model Model, text string) (Model, tea.Cmd) {
	t.Helper()
	model = typeAll(t, model, text)
	model, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !model.making.picking() {
		// Anything else is the command that makes something, and it is the caller's to run once.
		return model, cmd
	}
	model, _ = update(t, model, cmd())
	return model, nil
}

// answerAll walks a whole wizard and returns the command the last answer produced, which is the one
// that makes something.
func answerAll(t *testing.T, model Model, answers ...string) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, text := range answers {
		model, cmd = answer(t, model, text)
	}
	return model, cmd
}

// TestTheWizardMakesOneThing is the whole point of it: each kind can be made on its own, against a
// crew that already has a workspace and a project, and each touches exactly one call.
func TestTheWizardMakesOneThing(t *testing.T) {
	for _, test := range []struct {
		name    string
		answers []string
		want    func(*wizardClient) []string
	}{
		{"workspace", []string{"workspace", "other"}, func(c *wizardClient) []string { return c.workspaces }},
		{"project", []string{"project", "acme", "gardening"}, func(c *wizardClient) []string { return c.projects }},
		{"secret", []string{"secret", "acme", "tok-xyz"}, func(c *wizardClient) []string { return c.secrets }},
		{"context", []string{"context", "acme", "house-bills", "the water bill"}, func(c *wizardClient) []string { return c.contexts }},
		{"session", []string{"session", "acme", "house-bills", "hello"}, func(c *wizardClient) []string { return c.dispatched }},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &wizardClient{}
			model := wizardAt(t, client)
			if model.mode != modeWizard {
				t.Fatalf("mode = %v, want the wizard open", model.mode)
			}

			model, cmd := answerAll(t, model, test.answers...)
			if cmd == nil {
				t.Fatal("the last answer produced no command, so nothing was ever made")
			}
			done := cmd()
			if msg, isDone := done.(actionDoneMsg); !isDone || msg.err != nil {
				t.Fatalf("making it returned %#v, want a clean actionDoneMsg", done)
			}
			// Carried through to what the operator is left with, not stopped at the call: the answer
			// goes back in the way the runtime feeds it, and the console has to come back to its list.
			model, _ = update(t, model, done)
			if model.mode != modeBrowse {
				t.Fatalf("the wizard is still open after making a %s, mode = %v", test.name, model.mode)
			}

			if got := test.want(client); len(got) != 1 {
				t.Fatalf("the %s call was asked for %v, want exactly one", test.name, got)
			}
			// The one that matters: a wizard opened to add a project must not leave a workspace
			// behind it, which is what it did when every path started by making one.
			if client.made() != 1 {
				t.Fatalf("making a %s touched %d calls, want only its own: workspaces %v, projects %v, "+
					"secrets %v, contexts %v, dispatched %v", test.name, client.made(),
					client.workspaces, client.projects, client.secrets, client.contexts, client.dispatched)
			}
		})
	}
}

// TestPickingAnExistingParentMakesNoSecondOne: the pick step resolves against what the crew has, so
// answering "acme" where there is an "acme" reuses it. The identifier proves it, because a workspace
// made on the way would carry the double's own new one.
func TestPickingAnExistingParentMakesNoSecondOne(t *testing.T) {
	client := &wizardClient{}
	model := wizardAt(t, client)

	_, cmd := answerAll(t, model, "project", "acme", "gardening")
	if cmd == nil {
		t.Fatal("no command, so nothing was made")
	}
	cmd()

	if len(client.workspaces) != 0 {
		t.Fatalf("picking an existing workspace made %v", client.workspaces)
	}
	if len(client.projectsIn) != 1 || client.projectsIn[0] != "w-acme" {
		t.Fatalf("the project was made in %v, want the workspace that already existed", client.projectsIn)
	}
}

// TestTheWizardOffersWhatExists: a step that needs a parent shows what there is. Naming a workspace
// the operator cannot see is how it ended up only able to make new ones.
func TestTheWizardOffersWhatExists(t *testing.T) {
	client := &wizardClient{}
	model := wizardAt(t, client)
	model, _ = answer(t, model, "project")

	if view := model.View(); !strings.Contains(view, "acme") {
		t.Fatalf("the workspace step does not offer the workspace that exists:\n%s", view)
	}
	if !strings.Contains(model.View(), "which workspace") {
		t.Fatalf("the step does not say what it is asking:\n%s", model.View())
	}
}

// TestTheWizardRefusesAParentThatDoesNotExist: the step picks, it never creates, so a name matching
// nothing is refused rather than quietly made.
func TestTheWizardRefusesAParentThatDoesNotExist(t *testing.T) {
	client := &wizardClient{}
	model := wizardAt(t, client)
	model, _ = answer(t, model, "project")
	model, cmd := answer(t, model, "ghost")

	if model.err == nil {
		t.Fatal("a workspace that does not exist was accepted")
	}
	if !strings.Contains(model.err.Error(), "ghost") {
		t.Fatalf("the refusal is %q, want it to name what was typed", model.err)
	}
	if cmd != nil {
		t.Fatal("the refusal still produced a command")
	}
	if client.made() != 0 {
		t.Fatal("a refused parent made something anyway")
	}
}

// TestTheWizardRefusesAnAmbiguousKind: a secret and a session are one keystroke apart, so "s" has to
// name both rather than pick the first.
func TestTheWizardRefusesAnAmbiguousKind(t *testing.T) {
	client := &wizardClient{}
	model, _ := answer(t, wizardAt(t, client), "s")

	if model.err == nil {
		t.Fatal("\"s\" was accepted as one of secret and session")
	}
	for _, want := range []string{"secret", "session"} {
		if !strings.Contains(model.err.Error(), want) {
			t.Fatalf("the refusal is %q, want it to name %q", model.err, want)
		}
	}
	if model.making.kind != kindUnchosen {
		t.Fatalf("the wizard chose %v anyway", model.making.kind)
	}
}

// TestTheWizardTakesAKindByItsPrefix: one keystroke where only one kind starts with it.
func TestTheWizardTakesAKindByItsPrefix(t *testing.T) {
	client := &wizardClient{}
	model, _ := answer(t, wizardAt(t, client), "w")

	if model.err != nil {
		t.Fatalf("\"w\" was refused: %v", model.err)
	}
	if model.making.kind != kindWorkspace {
		t.Fatalf("\"w\" made %v, want a workspace", model.making.kind)
	}
}

// TestTheOldWizardIsRefusedRatherThanSwallowed is rule 46 in this repository: when an interface is
// replaced, test the way off it, not only the way onto it. The wizard used to open by asking for a
// new workspace name, so the first thing anybody types is still a name, and it must be refused
// loudly and name what to type instead rather than becoming the answer to a different question.
func TestTheOldWizardIsRefusedRatherThanSwallowed(t *testing.T) {
	client := &wizardClient{}
	model, cmd := answer(t, wizardAt(t, client), "acme-two")

	if model.err == nil {
		t.Fatal("a workspace name typed at the first question was accepted")
	}
	if !strings.Contains(model.err.Error(), "acme-two") {
		t.Fatalf("the refusal is %q, want it to name what was typed", model.err)
	}
	// Naming what to type instead is the difference between a refusal and a dead end.
	for _, want := range []string{"workspace", "project", "secret", "context", "session"} {
		if !strings.Contains(model.err.Error(), want) {
			t.Fatalf("the refusal is %q, want it to name %q as something that can be made", model.err, want)
		}
	}
	if cmd != nil || client.made() != 0 {
		t.Fatalf("the old form made %d things", client.made())
	}
}

// TestEscapeInTheWizardMakesNothing is the one behaviour here worth being certain of: a wizard that
// half creates is worse than no wizard.
//
// It walks every kind, escaping at every point in each, including partway through typing the answer
// that is never accepted. That last one is the case that matters: a secret typed and abandoned.
func TestEscapeInTheWizardMakesNothing(t *testing.T) {
	for _, walk := range [][]string{
		{"workspace", "other"},
		{"project", "acme", "gardening"},
		{"secret", "acme", "tok-xyz"},
		{"context", "acme", "house-bills", "the water bill"},
		{"session", "acme", "house-bills", "hello"},
	} {
		for answered := 0; answered < len(walk); answered++ {
			name := fmt.Sprintf("%s after %d", walk[0], answered)
			t.Run(name, func(t *testing.T) {
				client := &wizardClient{}
				model := wizardAt(t, client)
				for i := 0; i < answered; i++ {
					model, _ = answer(t, model, walk[i])
				}
				// Typed but never accepted, so escape lands on a half answered question.
				model = typeAll(t, model, walk[answered])
				model, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEsc})

				if model.mode != modeBrowse {
					t.Fatalf("escape left the wizard open, mode = %v", model.mode)
				}
				if cmd != nil {
					t.Fatal("escape produced a command")
				}
				if client.made() != 0 {
					t.Fatalf("escape made %d things", client.made())
				}

				// And it forgets everything the moment it closes, asserted here rather than through
				// the view, because a closed wizard is not drawn: a console still holding the token
				// looks exactly like one that dropped it. A cancelled wizard that kept a half typed
				// token would be carrying one around for the rest of the session.
				if !reflect.DeepEqual(model.making, wizard{}) {
					t.Fatalf("escape left %+v behind", model.making)
				}
			})
		}
	}
}

// TestEveryWizardQuestionIsNeeded: the wizard makes one thing now, so a question it asks is part of
// that thing rather than an offer alongside it. Enter on an empty answer refuses and names what is
// missing.
func TestEveryWizardQuestionIsNeeded(t *testing.T) {
	for _, walk := range [][]string{
		{"workspace", "other"},
		{"project", "acme", "gardening"},
		{"secret", "acme", "tok-xyz"},
		{"context", "acme", "house-bills", "the water bill"},
		{"session", "acme", "house-bills", "hello"},
	} {
		for answered := 0; answered < len(walk); answered++ {
			t.Run(fmt.Sprintf("%s question %d", walk[0], answered), func(t *testing.T) {
				client := &wizardClient{}
				model := wizardAt(t, client)
				for i := 0; i < answered; i++ {
					model, _ = answer(t, model, walk[i])
				}
				model, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

				if model.err == nil {
					t.Fatal("an empty answer was accepted")
				}
				if cmd != nil {
					t.Fatal("an empty answer produced a command")
				}
				if client.made() != 0 {
					t.Fatalf("an empty answer made %d things", client.made())
				}
			})
		}
	}
}

// TestTheWizardNeverShowsTheToken: a value on a screen is a value in that terminal's scrollback, and
// this one runs every turn the crew makes.
func TestTheWizardNeverShowsTheToken(t *testing.T) {
	client := &wizardClient{}
	model := wizardAt(t, client)
	model, _ = answerAll(t, model, "secret", "acme")
	model = typeAll(t, model, "sk-ant-oat-secret")

	view := model.View()
	if strings.Contains(view, "sk-ant-oat-secret") {
		t.Fatalf("the token is on screen:\n%s", view)
	}
	if !strings.Contains(view, strings.Repeat("*", len("sk-ant-oat-secret"))) {
		t.Fatalf("nothing shows that anything was typed:\n%s", view)
	}
}

// TestTheWizardClosesWhenItHasMadeSomething. Answering the last question made everything and then
// left the console drawn on the wizard forever: the refreshed list underneath was never seen, so
// nothing looked like it had happened, and the next enter was accepted as an answer to the step that
// was already working, whose prompt is the literal string "making it".
//
// Driving it produced no sign that anything had been made, and then the refusal "making it: this one
// is needed", which names no question anybody was asked.
func TestTheWizardClosesWhenItHasMadeSomething(t *testing.T) {
	client := &wizardClient{}
	model, cmd := answerAll(t, wizardAt(t, client), "project", "acme", "gardening")
	if cmd == nil {
		t.Fatal("the last answer produced no command, so nothing was ever made")
	}
	// Run it and feed the answer back, which is what the runtime does.
	model, _ = update(t, model, cmd())

	if model.mode != modeBrowse {
		t.Fatalf("the wizard is still open after making it, mode = %v", model.mode)
	}
	if !reflect.DeepEqual(model.making, wizard{}) {
		t.Fatalf("the finished wizard left %+v behind", model.making)
	}
}

// TestKeysWhileTheWizardIsWorkingAreNotAnswers: until the crew answers, the wizard is asking nothing,
// so a keypress is not an answer to anything. Enter used to be accepted as an empty answer to the
// working step and refused as "making it: this one is needed", which names no question the operator
// was ever asked.
func TestKeysWhileTheWizardIsWorkingAreNotAnswers(t *testing.T) {
	client := &wizardClient{}
	model, _ := answerAll(t, wizardAt(t, client), "project", "acme", "gardening")
	if model.making.step() != stepWorking {
		t.Fatalf("the wizard is at step %v, want it working", model.making.step())
	}

	model, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.err != nil {
		t.Fatalf("enter while working reported %q, want nothing: the operator was asked no question", model.err)
	}
	if cmd != nil {
		t.Fatal("enter while working asked the crew to make it a second time")
	}

	model = typeAll(t, model, "stray")
	if model.making.typed != "" {
		t.Fatalf("keys while working were kept as %q", model.making.typed)
	}
	// Escape is the one key that still works, so a crew that never answers is not a trap.
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.mode != modeBrowse {
		t.Fatalf("escape while working left the console in %v", model.mode)
	}
}

// TestTheHelpPanelScrollsRatherThanDroppingItsEnd. Everything the header used to carry is in there
// now, so on a short window there is more of it than there is room. Cutting the end silently is how a
// panel missing half its keys looks exactly like a complete one.
func TestTheHelpPanelScrollsRatherThanDroppingItsEnd(t *testing.T) {
	client := &fakeClient{}
	model := newTestModel(t, Sessions(client), Workspaces(client), Contexts(client))
	model.info = Info{Version: "dev", Address: "localhost:50051", Workspace: "acme", Store: "postgres"}
	model, _ = update(t, model, runes("?"))

	first := model.View()
	if !strings.Contains(first, "more, ↑↓ to scroll") {
		t.Fatalf("a short window shows everything, so this test proves nothing:\n%s", first)
	}
	// What is off the end is reachable, not gone.
	for i := 0; i < 30; i++ {
		model, _ = update(t, model, runes("j"))
	}
	if model.mode != modeHelp {
		t.Fatal("scrolling closed the help panel")
	}
	if !strings.Contains(model.View(), "Quit") {
		t.Fatalf("scrolling down never reaches the end of the panel:\n%s", model.View())
	}
	// And any other key still closes it.
	model, _ = update(t, model, runes("x"))
	if model.mode != modeBrowse {
		t.Fatal("a key that is not a movement no longer closes the help panel")
	}
	if model.helpTop != 0 {
		t.Fatalf("the help panel stayed scrolled to %d after closing", model.helpTop)
	}
}

// TestTheHelpPanelNeverAsksAQuestionItHasAnswered: the branch that says nothing is known yet fires
// only when nothing is known. It used to be the last one standing when the others were suppressed,
// so the console drew "asking what this control plane is running" under a header that had answered.
func TestTheHelpPanelNeverAsksAQuestionItHasAnswered(t *testing.T) {
	told := tallTestModel(t, Sessions(&fakeClient{}))
	told.info = Info{Version: "dev", Address: "localhost:50051", Workspace: "acme"}
	if strings.Contains(strings.Join(told.crewLines(), "\n"), "still asking") {
		t.Fatalf("the help panel asks what it has been told:\n%s", strings.Join(told.crewLines(), "\n"))
	}
	// And it still says so when it is the truth, or this passes by deleting the line.
	untold := tallTestModel(t, Sessions(&fakeClient{}))
	if !strings.Contains(strings.Join(untold.crewLines(), "\n"), "still asking") {
		t.Fatal("a console that has been told nothing says nothing about it")
	}
}

// TestTheStatsViewCarriesWhatTheHeaderStoppedShowing, or the information was not moved, it was lost.
func TestTheStatsViewCarriesWhatTheHeaderStoppedShowing(t *testing.T) {
	client := &fakeClient{}
	rows, err := Stats(client).List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing the stats: %v", err)
	}
	shown := make(map[string]bool, len(rows))
	for _, row := range rows {
		shown[row.Cells[0]] = true
	}
	for _, want := range []string{"Model", "Sandbox engine", "Store engine", "Secrets", "Events engine", "State"} {
		if !shown[want] {
			t.Fatalf("the stats view does not carry %q, so moving it out of the header lost it", want)
		}
	}
}

// TestTheKeysViewCarriesEveryKeyThatWorksEverywhere, from the same list the overlay reads, so the two
// cannot drift.
func TestTheKeysViewCarriesEveryKeyThatWorksEverywhere(t *testing.T) {
	registry, err := NewDefaultRegistry(&fakeClient{})
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	view, found := registry.Get("keys")
	if !found {
		t.Fatal("the console has no keys view")
	}
	rows, err := view.List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing the keys: %v", err)
	}
	shown := make(map[string]bool, len(rows))
	for _, row := range rows {
		shown[row.Cells[1]] = true
	}
	for _, everywhere := range everywhereKeys {
		if !shown[everywhere[0]] {
			t.Fatalf("the keys view leaves out %q, which works everywhere", everywhere[0])
		}
	}
	// And a view's own keys, or it is only half a list.
	if len(rows) <= len(everywhereKeys) {
		t.Fatalf("the keys view carries %d rows and only the everywhere keys", len(rows))
	}
}

// TestPressingPWithNoCrewSaysSo rather than looking like a key that does nothing.
func TestPressingPWithNoCrewSaysSo(t *testing.T) {
	model, cmd := update(t, newTestModel(t, Sessions(&fakeClient{})), runes("p"))
	if cmd != nil {
		t.Fatal("p tried to open something with nothing to open")
	}
	if model.err == nil {
		t.Fatal("p did nothing and said nothing")
	}
}

// TestPressingPOutsideTmuxSaysWhatToRun. tmux is what splits the screen, so outside it there is
// nothing to split, and a key that silently does nothing reads as broken.
func TestPressingPOutsideTmuxSaysWhatToRun(t *testing.T) {
	t.Setenv("TMUX_PANE", "")
	model := newTestModel(t, Sessions(&fakeClient{})).
		Beside(func(string) ([]string, error) { return []string{"quay", "attach", "s1"}, nil })

	model, cmd := update(t, model, runes("p"))
	if cmd != nil {
		t.Fatal("p tried to split a screen with no tmux to split it")
	}
	if model.err == nil || !strings.Contains(model.err.Error(), "quay panel") {
		t.Fatalf("p said %v, want it to name what to run", model.err)
	}
}

// TestPressingPOpensTheConversationUnderTheCursor: the row being looked at is the one meant, and the
// console hands it over rather than deciding for itself which conversation is interesting.
func TestPressingPOpensTheConversationUnderTheCursor(t *testing.T) {
	t.Setenv("TMUX_PANE", "%3")
	asked := make([]string, 0, 1)
	model := newTestModel(t, Sessions(&fakeClient{})).
		Beside(func(selected string) ([]string, error) {
			asked = append(asked, selected)
			return []string{"quay", "attach", selected}, nil
		})
	model, _ = update(t, model, rowsFor(model, row("s1", "s1", "acme"), row("s2", "s2", "acme")))
	model, _ = update(t, model, runes("j"))

	if _, cmd := update(t, model, runes("p")); cmd == nil {
		t.Fatal("p produced no command inside tmux")
	}
	if len(asked) != 1 || asked[0] != "s2" {
		t.Fatalf("p asked to open %v, want the row under the cursor", asked)
	}
}

// TestPressingPClosesTheConversationBesideIt, which may be one the console never opened: `quay` opens
// the panel with a conversation already there, so a console that only knew about the ones it opened
// would answer the first p by opening a second.
//
// It is asked of tmux instead: the pane at the same top and further right is the conversation. A pane
// above or below is the header.
func TestPressingPClosesTheConversationBesideIt(t *testing.T) {
	listing := "%1 0 0\n%2 0 11\n%3 100 11\n"
	for _, test := range []struct {
		name  string
		me    string
		want  string
		found bool
	}{
		{"a conversation to the right", "%2", "%3", true},
		{"nothing to the right", "%3", "", false},
		{"the header, which has no conversation beside it", "%1", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, found := rightOf(listing, test.me)
			if found != test.found || got != test.want {
				t.Fatalf("beside %s is %q (%v), want %q (%v)", test.me, got, found, test.want, test.found)
			}
		})
	}
}

// TestTheKeyIsInTheHelp, or nobody finds it.
func TestTheKeyIsInTheHelp(t *testing.T) {
	model := tallTestModel(t, Sessions(&fakeClient{}))
	model.mode = modeHelp
	if !strings.Contains(model.View(), "conversation beside") {
		t.Fatalf("the help does not mention the key that shows a conversation:\n%s", model.View())
	}
}

// TestTheWordmarkIsDrawnInAHeaderOfOneRow. The panel's header pane is one row tall, and a height check
// left over from the six line wordmark dropped the wordmark from every pane shorter than seven rows.
// The header was the only thing in it, so there was nothing underneath to starve.
func TestTheWordmarkIsDrawnInAHeaderOfOneRow(t *testing.T) {
	registry, err := NewDefaultRegistry(&fakeClient{})
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	for _, height := range []int{1, 2, 6, 24} {
		lines, err := HeaderOnly(registry, Default, Info{Version: "b8919a4"}, 200, height)
		if err != nil {
			t.Fatalf("HeaderOnly at height %d: %v", height, err)
		}
		if !strings.Contains(strings.Join(lines, "\n"), logo[0]) {
			t.Fatalf("at height %d the wordmark is gone:\n%s", height, strings.Join(lines, "\n"))
		}
	}
}
