package console

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/atlantic-blue/quay-crew/features"
	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// Workspaces lists the workspaces the control plane knows about, and drills into their sessions.
func Workspaces(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "workspaces",
		Aliases: []string{"w", "ws", "workspace"},
		Columns: []Column{
			{Title: "id", Width: 10},
			{Title: "name", Width: 0},
			{Title: "age", Width: 10},
		},
		DrillTo: "projects",
		SortBy:  1,
		List: func(ctx context.Context, _ string) ([]Row, error) {
			resp, err := client.ListWorkspaces(ctx, &quaycrewv1.ListWorkspacesRequest{})
			if err != nil {
				return nil, err
			}
			rows := make([]Row, 0, len(resp.GetWorkspaces()))
			for _, workspace := range resp.GetWorkspaces() {
				rows = append(rows, workspaceRow(workspace))
			}
			return rows, nil
		},
	}
}

func workspaceRow(workspace *quaycrewv1.Workspace) Row {
	// ID stays whole: it is what actions and drilling down use. Only the cell is shortened.
	return Row{
		ID:    workspace.GetId(),
		Label: workspace.GetName(),
		Cells: []string{display.ShortID(workspace.GetId()), workspace.GetName(), display.Age(workspace.GetCreatedAt())},
		State: StateReady,
	}
}

// Projects lists the bodies of work inside a workspace, and drills into their sessions.
func Projects(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "projects",
		Aliases: []string{"p", "proj", "project"},
		Columns: []Column{
			{Title: "id", Width: 10},
			{Title: "name", Width: 24},
			{Title: "workspace", Width: 18},
			{Title: "age", Width: 0},
		},
		DrillTo: "sessions",
		SortBy:  1,
		List: func(ctx context.Context, workspace string) ([]Row, error) {
			resp, err := client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{Workspace: workspace})
			if err != nil {
				return nil, err
			}
			names := workspaceNames(ctx, client)
			rows := make([]Row, 0, len(resp.GetProjects()))
			for _, project := range resp.GetProjects() {
				rows = append(rows, projectRow(project, names[project.GetWorkspace()]))
			}
			return rows, nil
		},
	}
}

func projectRow(project *quaycrewv1.Project, workspaceName string) Row {
	// ID and Parent stay whole: they are what drilling and actions use.
	return Row{
		ID:     project.GetId(),
		Parent: project.GetWorkspace(),
		Label:  project.GetName(),
		Cells: []string{
			display.ShortID(project.GetId()),
			project.GetName(),
			display.Name(workspaceName, project.GetWorkspace()),
			display.Age(project.GetCreatedAt()),
		},
		State: StateReady,
	}
}

// workspaceNames maps workspace id to name. An error yields an empty map rather than failing a list.
func workspaceNames(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) map[string]string {
	resp, err := client.ListWorkspaces(ctx, &quaycrewv1.ListWorkspacesRequest{})
	if err != nil {
		return map[string]string{}
	}
	names := make(map[string]string, len(resp.GetWorkspaces()))
	for _, w := range resp.GetWorkspaces() {
		names[w.GetId()] = w.GetName()
	}
	return names
}

// Contexts lists the directories the model reads. An empty one is the normal state, so whether the
// memory file exists is a column rather than something to infer from a listing that shows nothing.
func Contexts(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "context",
		Aliases: []string{"c", "contexts", "ctx"},
		Columns: []Column{
			{Title: "scope", Width: 10},
			{Title: "name", Width: 18},
			{Title: "written", Width: 16},
			{Title: "what it says", Width: 0},
		},
		SortBy: 1,
		Actions: []Action{
			{
				// The console already suspends itself to run a command, which is how opening a thread
				// and shelling in work, so an editor is the same mechanism. It creates the file when
				// it saves, which is why nothing here has to write one first.
				Key:   "enter",
				Also:  []string{"e"},
				Label: "Edit",
				// A scratch file, not the rendered one. A level's context is rendered into every
				// session that reads it, so there is no single file to open, and the crew's is
				// rendered into every workspace. The store is what is being edited; this is a
				// window onto it.
				Shell: func(row Row) (*exec.Cmd, error) {
					if len(row.Cells) == 0 {
						return nil, fmt.Errorf("no context selected")
					}
					// Seeded with what this level already says, so editing is editing rather than
					// starting again, and quitting without saving changes nothing.
					draft := draftFor(row)
					if err := os.WriteFile(draft, []byte(row.Detail), 0o600); err != nil {
						return nil, fmt.Errorf("make room to write: %w", err)
					}
					return editorFor(draft)
				},
				// The editor wrote the scratch file; this is what tells the crew.
				After: func(ctx context.Context, row Row) error {
					body, err := os.ReadFile(draftFor(row))
					if err != nil {
						return fmt.Errorf("reading what you wrote: %w", err)
					}
					_, err = client.SetContext(ctx, &quaycrewv1.SetContextRequest{
						Scope: row.Cells[0], Owner: row.Parent, Body: string(body),
					})
					return err
				},
			},
		},
		List: func(ctx context.Context, _ string) ([]Row, error) {
			resp, err := client.ListContexts(ctx, &quaycrewv1.ListContextsRequest{})
			if err != nil {
				return nil, err
			}
			rows := make([]Row, 0, len(resp.GetDirs()))
			for _, dir := range resp.GetDirs() {
				rows = append(rows, contextRow(dir))
			}
			return rows, nil
		},
	}
}

// editorFor opens a file in the operator's own editor.
//
// VISUAL then EDITOR then vi, which is what git, crontab and everything else that opens a file for
// you already does. Refusing when neither is set was the purist reading and it made the feature dead
// on a machine with no EDITOR exported, which is most machines.
func editorFor(file string) (*exec.Cmd, error) {
	editor := Editor()
	// The directory is created for a sandbox that has never run, so an editor writing there does not
	// fail on a path that is not made yet.
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		return nil, fmt.Errorf("make room for %s: %w", file, err)
	}
	parts := strings.Fields(editor)
	return exec.Command(parts[0], append(parts[1:], file)...), nil
}

// Editor is the command that opens a file for the operator: whatever they have said they want, and
// vi when they have said nothing, because it is on every machine this runs on.
func Editor() string {
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if chosen := strings.TrimSpace(os.Getenv(name)); chosen != "" {
			return chosen
		}
	}
	return "vi"
}

// firstLine is what a level says, in one line. The whole of it is what the model reads, not what
// somebody scanning a list needs.
func firstLine(body string) string {
	line := strings.TrimSpace(body)
	if cut := strings.IndexByte(line, '\n'); cut >= 0 {
		line = strings.TrimSpace(line[:cut]) + " ..."
	}
	return line
}

// draftFor is where a level's context is edited: one scratch file per level, kept between edits so a
// crash or a quit without saving does not lose what was being written.
func draftFor(row Row) string {
	return filepath.Join(os.TempDir(), "quay-context-"+row.Cells[0]+"-"+row.Parent+".md")
}

func contextRow(dir *quaycrewv1.ContextDir) Row {
	written, state := "not written yet", StateUnknown
	if dir.GetWritten() {
		written, state = "written", StateReady
	}
	return Row{
		// A level is what an action on this row acts on, so the scope and the owner are what it
		// carries, and the whole of what it says goes in Detail for an editor to open.
		ID:     dir.GetScope() + "/" + dir.GetOwner(),
		Parent: dir.GetOwner(),
		Label:  dir.GetName(),
		Detail: dir.GetBody(),
		Cells:  []string{dir.GetScope(), dir.GetName(), written, firstLine(dir.GetBody())},
		State:  state,
	}
}

// Secrets lists what each workspace has set, and never what any of it says. There is no action on a
// row: setting one means typing a value, and a value typed into a full screen console is a value in a
// terminal's scrollback. `quay secret set` is where that belongs.
func Secrets(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "secrets",
		Aliases: []string{"secret"},
		Columns: []Column{
			{Title: "workspace", Width: 20},
			{Title: "name", Width: 32},
			{Title: "value", Width: 0},
		},
		SortBy: 0,
		List: func(ctx context.Context, _ string) ([]Row, error) {
			resp, err := client.ListSecrets(ctx, &quaycrewv1.ListSecretsRequest{})
			if err != nil {
				return nil, err
			}
			rows := make([]Row, 0, len(resp.GetSecrets()))
			for _, secret := range resp.GetSecrets() {
				rows = append(rows, Row{
					ID:     secret.GetWorkspace() + "/" + secret.GetName(),
					Parent: secret.GetWorkspace(),
					Label:  secret.GetName(),
					Cells: []string{
						display.Name(secret.GetWorkspaceName(), secret.GetWorkspace()),
						secret.GetName(),
						// Said out loud rather than left blank, so nobody wonders whether the column
						// is empty because the value is empty.
						"set, and not shown anywhere",
					},
					State: StateReady,
				})
			}
			return rows, nil
		},
	}
}

// Features lists what this build of the crew can do, from the specification embedded in it. It is the
// one view that asks the control plane nothing: a capability belongs to the build, not to a running
// stack, and this is the view an operator opens before they know what to open.
func Features() Resource {
	return Resource{
		Name:    "features",
		Aliases: []string{"f", "feature", "capabilities"},
		Columns: []Column{
			{Title: "feature", Width: 44},
			{Title: "proved by", Width: 0},
		},
		List: func(context.Context, string) ([]Row, error) {
			rows := make([]Row, 0, 32)
			for _, feature := range features.All() {
				for index, scenario := range feature.Scenarios {
					title := ""
					if index == 0 {
						title = feature.Title
					}
					rows = append(rows, Row{
						ID:    feature.Title + ": " + scenario,
						Label: feature.Title,
						Cells: []string{title, scenario},
						State: StateReady,
					})
				}
			}
			return rows, nil
		},
	}
}

// Sessions lists conversations, scoped to a project when drilled into from one. The operator can stop
// one and shell into its container.
//
// The database calls these sessions and so does the API, and one name across the whole system beats a
// console that translates. Thread stays as an alias, because the command bar should not punish muscle
// memory either way.
func Sessions(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "sessions",
		Aliases: []string{"s", "sess", "session", "threads", "thread", "t"},
		Columns: []Column{
			{Title: "id", Width: 10},
			{Title: "workspace", Width: 16},
			{Title: "project", Width: 20},
			{Title: "thread", Width: 10},
			{Title: "status", Width: 10},
			{Title: "mode", Width: 12},
			// What the conversation has cost. The cache is the largest of the three by a long way and
			// the first to give way, because at half a window the age of a thread is worth more than
			// what it read from a cache.
			{Title: "in", Width: 7, Give: 3},
			{Title: "out", Width: 7, Give: 2},
			{Title: "cache", Width: 7, Give: 1},
			{Title: "age", Width: 0},
		},
		// Ordered by thread, so a session keeps its place in the list as its age and status change
		// under it.
		SortBy:  3,
		List:    sessionLister(client, live),
		Actions: sessionActions(client),
	}
}

// Turns is one session's history, read from the turns the dispatch path writes, so it
// answers without starting a container and keeps answering once the sandbox is gone. It has no tool
// calls and no thinking: opening the conversation is for that.
func Turns(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "turns",
		Aliases: []string{"turn", "history", "h"},
		Columns: []Column{
			{Title: "when", Width: 10},
			{Title: "status", Width: 10},
			{Title: "asked", Width: 34},
			{Title: "answered", Width: 0},
		},
		// Oldest first, the order it happened in, which is the only order a conversation reads in.
		SortBy: 0,
		List: func(ctx context.Context, session string) ([]Row, error) {
			if session == "" {
				return nil, fmt.Errorf("open a session's history from the sessions view: there is no history without one")
			}
			resp, err := client.ListTurns(ctx, &quaycrewv1.ListTurnsRequest{Thread: session})
			if err != nil {
				return nil, err
			}
			rows := make([]Row, 0, len(resp.GetTurns()))
			for _, turn := range resp.GetTurns() {
				rows = append(rows, turnRow(turn))
			}
			return rows, nil
		},
	}
}

// turnRow is one exchange as a listing row. A failed turn shows why it failed where the reply would
// have been, because that is the answer to what happened.
func turnRow(turn *quaycrewv1.Turn) Row {
	answered := oneLine(turn.GetReply())
	state := StateReady
	if turn.GetStatus() == "failed" {
		answered, state = oneLine(turn.GetFailure()), StateFailed
	}
	return Row{
		ID:     turn.GetId(),
		Parent: turn.GetThread(),
		State:  state,
		Label:  display.ShortID(turn.GetId()),
		Cells: []string{
			turn.GetOccurredAt().AsTime().Local().Format("15:04:05"),
			turn.GetStatus(),
			oneLine(turn.GetPrompt()),
			answered,
		},
		// The whole of what was said, for a view that can show more than a row.
		Detail: turn.GetPrompt(),
	}
}

// oneLine flattens text onto one line, so a reply that runs to paragraphs does not break the table.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// Archived is its own view rather than a filter, because a listing that quietly grows back the
// threads somebody hid is worse than no archive at all. Nothing was deleted, so the only action is
// bringing one back.
func Archived(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "archived",
		Aliases: []string{"arch", "archive"},
		// The same shape as the live view, because both are drawn from the same row. An archived
		// thread's cost is still worth seeing: it is what that piece of work came to.
		Columns: []Column{
			{Title: "id", Width: 10},
			{Title: "workspace", Width: 16},
			{Title: "project", Width: 20},
			{Title: "thread", Width: 10},
			{Title: "status", Width: 10},
			{Title: "mode", Width: 12},
			{Title: "in", Width: 7, Give: 3},
			{Title: "out", Width: 7, Give: 2},
			{Title: "cache", Width: 7, Give: 1},
			{Title: "archived", Width: 0},
		},
		SortBy: 3,
		List:   sessionLister(client, putAway),
		Actions: []Action{
			{
				Key:   "u",
				Label: "Restore",
				Run: func(ctx context.Context, row Row) error {
					if row.ID == "" {
						return fmt.Errorf("no session selected")
					}
					_, err := client.RestoreThread(ctx, &quaycrewv1.RestoreThreadRequest{Id: row.ID})
					return err
				},
			},
		},
	}
}

// which listing a session lister asks for. Named rather than a bare boolean at the two call sites,
// where true would have said nothing about what it selected.
type shelf bool

const (
	live    shelf = false
	putAway shelf = true
)

func sessionLister(client quaycrewv1.ControlPlaneServiceClient, from shelf) Lister {
	// parent is a project id when drilled into from one, and empty at the top level.
	return func(ctx context.Context, parent string) ([]Row, error) {
		resp, err := client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{
			Project:  parent,
			Archived: bool(from),
		})
		if err != nil {
			return nil, err
		}
		// A session carries its workspace id, and an id says nothing to the operator reading the
		// list. One extra call turns every one of them into a name. If it fails the rows still
		// render, with the id as the fallback, because a listing that refuses to draw because the
		// names could not be looked up is worse than one that shows ids.
		workspaces, projects := workspaceNames(ctx, client), projectNames(ctx, client)

		rows := make([]Row, 0, len(resp.GetThreads()))
		for _, session := range resp.GetThreads() {
			rows = append(rows, sessionRow(session, workspaces[session.GetWorkspace()], projects[session.GetProject()]))
		}
		return rows, nil
	}
}

// projectNames maps project id to name, so a session row can name the body of work it belongs to.
func projectNames(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) map[string]string {
	resp, err := client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{})
	if err != nil {
		return map[string]string{}
	}
	names := make(map[string]string, len(resp.GetProjects()))
	for _, project := range resp.GetProjects() {
		names[project.GetId()] = project.GetName()
	}
	return names
}

func sessionRow(session *quaycrewv1.Thread, workspaceName, projectName string) Row {
	// ID and Parent stay whole: they are what actions and scoping use. Only the cells shorten.
	return Row{
		ID:     session.GetId(),
		Parent: session.GetProject(),
		Label:  display.ShortID(session.GetHandle()),
		Cells:  display.ThreadCells(session, workspaceName, projectName),
		State:  stateFromStatus(session.GetStatus()),
	}
}

// projectColumn is where a thread's project sits in its row, which the shell prompt names so the
// operator can see where they are rather than which twenty four characters they typed.
const projectColumn = 2

// shellPrompt is what a shell in a sandbox says on every line: the thread, shortened the way every
// listing shortens it, and the project it belongs to.
func shellPrompt(row Row) string {
	where := display.ShortID(row.ID)
	if len(row.Cells) > projectColumn && row.Cells[projectColumn] != "" {
		where += " " + row.Cells[projectColumn]
	}
	return where + " $ "
}

// permissionColumn is where the mode sits in a thread row, which is what the toggle reads to know
// which way it is going.
const permissionColumn = 5

// offeredModes are the modes the picker offers, narrowest first, in the words the listing prints. The
// order is the model's, so the console cannot offer a set that `quay mode` does not take.
func offeredModes() []string {
	return model.PermissionModesOffered()
}

// modeOfRow is what the selected thread may do now, read out of the listing rather than fetched,
// because the listing is what the operator is looking at when they press the key. A row from before
// the mode was written down has an empty cell, and those threads run acceptEdits.
func modeOfRow(row Row) string {
	if len(row.Cells) <= permissionColumn {
		return model.PermissionAcceptEdits
	}
	named, known := model.PermissionModeNamed(row.Cells[permissionColumn])
	if !known {
		return model.PermissionAcceptEdits
	}
	return named
}

func sessionActions(client quaycrewv1.ControlPlaneServiceClient) []Action {
	return []Action{
		{
			// Enter is the primary key, so the obvious key does the obvious thing on a conversation.
			// It used to do nothing at all here, because a thread has nothing to drill into.
			Key:   "enter",
			Also:  []string{"a"},
			Label: "Open",
			Shell: func(row Row) (*exec.Cmd, error) {
				if row.ID == "" {
					return nil, fmt.Errorf("no session selected")
				}
				// The conversation, not the room. The control plane is asked where it is, so the
				// console never has to know how a sandbox is named or how a resume works.
				return attachCommand(client, row.ID)
			},
		},
		{
			// The history, beside opening the conversation. Lowercase and cheap, because looking at
			// what a session has been doing is something an operator does constantly, and it changes
			// nothing.
			Key:     "l",
			Also:    []string{"h"},
			Label:   "History",
			Descend: "turns",
		},
		{
			Key:   "s",
			Label: "Shell",
			Shell: func(row Row) (*exec.Cmd, error) {
				if row.ID == "" {
					return nil, fmt.Errorf("no session selected")
				}
				// The prompt says which sandbox this is. Every session gets a container of its own
				// with its own empty working directory over the same image, so two shells are
				// identical on the screen and telling them apart meant remembering which one you
				// asked for. It reads as the key opening the same shell every time.
				return exec.Command("docker", "exec", "-it",
					"-e", "PS1="+shellPrompt(row), sandbox.ContainerName(row.ID), "sh"), nil
			},
		},
		{
			// Not destructive, so no question. Restarting a thread that is not stopped is refused by
			// the control plane, and that refusal is what the operator sees.
			//
			// Uppercase, beside Archive: the uppercase letters act on the thread, and `r` refreshes
			// the view, which is the key anybody reaches for far more often.
			Key:   "R",
			Label: "Restart",
			Run: func(ctx context.Context, row Row) error {
				if row.ID == "" {
					return fmt.Errorf("no session selected")
				}
				_, err := client.RestartThread(ctx, &quaycrewv1.RestartThreadRequest{Id: row.ID})
				return err
			},
		},
		{
			// What a thread may do, picked rather than cycled. This used to flip between two of the
			// three, so planning was reachable from the command line and from the wizard and not from
			// the surface an operator lives in.
			//
			// D still opens it. It was the flip for as long as the console has had one, so it is in
			// somebody's fingers, and a key that silently stopped working is worse than one that
			// changed shape.
			Key:     "m",
			Also:    []string{"D"},
			Label:   "Mode",
			Confirm: true,
			Offers:  offeredModes(),
			// Widening gives the model more room, so it asks. Narrowing takes room away and does not:
			// there is nothing to be careful about, and asking would only be in the way.
			Widens: func(row Row, chosen string) bool {
				picked, _ := model.PermissionModeNamed(chosen)
				return model.PermissionModeWidens(modeOfRow(row), picked)
			},
			RunChosen: func(ctx context.Context, row Row, chosen string) error {
				if row.ID == "" {
					return fmt.Errorf("no session selected")
				}
				picked, known := model.PermissionModeNamed(chosen)
				if !known {
					return fmt.Errorf("%q is not a mode", chosen)
				}
				_, err := client.SetThreadPermissionMode(ctx, &quaycrewv1.SetThreadPermissionModeRequest{
					Id: row.ID, Mode: picked,
				})
				return err
			},
		},
		{
			// Uppercase, because `a` already attaches and archiving is the rarer of the two. It asks
			// first: a thread that disappears from the list under an accidental keypress reads
			// exactly like one that was deleted.
			Key:     "A",
			Label:   "Archive",
			Confirm: true,
			Run: func(ctx context.Context, row Row) error {
				if row.ID == "" {
					return fmt.Errorf("no session selected")
				}
				_, err := client.ArchiveThread(ctx, &quaycrewv1.ArchiveThreadRequest{Id: row.ID})
				return err
			},
		},
		{
			// Backspace is the primary key, and it asks before it acts. `x` still works.
			Key:     "backspace",
			Also:    []string{"x"},
			Label:   "Stop",
			Confirm: true,
			Run: func(ctx context.Context, row Row) error {
				if row.ID == "" {
					return fmt.Errorf("no session selected")
				}
				_, err := client.StopThread(ctx, &quaycrewv1.StopThreadRequest{Id: row.ID})
				return err
			},
		},
	}
}

// attachCommand asks the control plane how to open a session's conversation and builds the command.
//
// The control plane's reason for refusing is passed straight through, because a session with no
// conversation yet or a stopped one are both things the operator can act on, and "nothing to run"
// tells them neither.
func attachCommand(client quaycrewv1.ControlPlaneServiceClient, sessionID string) (*exec.Cmd, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	spec, err := client.AttachThread(ctx, &quaycrewv1.AttachThreadRequest{Id: sessionID})
	if err != nil {
		return nil, fmt.Errorf("attach: %w", err)
	}
	// No credential here: the sandbox already carries the workspace's environment.
	args := []string{"exec", "--interactive", "--tty", spec.GetSandbox()}
	args = append(args, spec.GetArgv()...)
	return exec.Command("docker", args...), nil
}

// stateFromStatus maps the control plane's session status onto a colour. An unrecognised status is
// left uncoloured rather than guessed at, so a new status shows up as plain text instead of a lie.
func stateFromStatus(status string) State {
	switch status {
	case "idle":
		return StateReady
	case "running", "dispatching":
		return StateBusy
	case "stopped":
		return StateStopped
	case "failed":
		return StateFailed
	default:
		return StateUnknown
	}
}

// Stats is what the crew is running underneath: which model backend, which sandbox and store engines,
// where secrets and state are kept, whether anything is reading the event log.
//
// A view rather than more header. Six lines of this in the header makes it ten lines tall, and a
// header that tall in a pane of its own scrolls, which loses its own top line. The header keeps what
// anybody needs at a glance; the rest is a question, and a question gets a view.
func Stats(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "stats",
		Aliases: []string{"stat", "engines", "status"},
		Columns: []Column{
			{Title: "what", Width: 22},
			{Title: "running", Width: 0},
		},
		SortBy: -1,
		List: func(ctx context.Context, _ string) ([]Row, error) {
			described, err := client.GetInfo(ctx, &quaycrewv1.GetInfoRequest{})
			if err != nil {
				return nil, err
			}
			// In the order they matter when something is wrong: what runs a turn, where it runs,
			// what survives a restart, then what is watching.
			stats := []struct{ what, running string }{
				{"Model", described.GetModel()},
				{"Sandbox engine", described.GetSandbox()},
				{"Store engine", described.GetStore()},
				{"Secrets", secretsPhrase(described.GetSecrets())},
				{"Events engine", eventsPhrase(described.GetEvents())},
				{"State", statePhrase(described.GetState())},
			}
			rows := make([]Row, 0, len(stats))
			for _, stat := range stats {
				running := stat.running
				if running == "" {
					running = "not saying"
				}
				rows = append(rows, Row{
					ID:    stat.what,
					Label: stat.what,
					Cells: []string{stat.what, running},
					State: StateReady,
				})
			}
			return rows, nil
		},
	}
}

// Keys is every key the console answers to, as a view rather than only as the overlay behind the
// question mark. The header carries this view's own keys; the rest have to be somewhere you can leave
// open beside what you are doing.
func Keys(registry *Registry) Resource {
	return Resource{
		Name:    "keys",
		Aliases: []string{"key", "hotkeys", "bindings"},
		Columns: []Column{
			{Title: "view", Width: 14},
			{Title: "key", Width: 16},
			{Title: "does", Width: 0},
		},
		SortBy: -1,
		List: func(context.Context, string) ([]Row, error) {
			rows := make([]Row, 0, 32)
			add := func(view, key, does string) {
				rows = append(rows, Row{
					ID:    view + " " + key,
					Label: key,
					Cells: []string{view, key, does},
					State: StateReady,
				})
			}
			for _, everywhere := range everywhereKeys {
				add("everywhere", everywhere[0], everywhere[1])
			}
			if registry != nil {
				for _, name := range registry.Names() {
					resource, found := registry.Get(name)
					if !found {
						continue
					}
					for _, action := range resource.Actions {
						add(name, strings.Join(shortestSpelling(action.Keys()), " "), action.Label)
					}
				}
			}
			return rows, nil
		},
	}
}
