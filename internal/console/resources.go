package console

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/atlantic-blue/quay-crew/features"
	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"google.golang.org/protobuf/types/known/timestamppb"
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
		Cells: []string{display.ShortID(workspace.GetId()), workspace.GetName(), age(workspace.GetCreatedAt())},
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
		DrillTo: "threads",
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
			age(project.GetCreatedAt()),
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

// Threads lists conversations, scoped to a project when drilled into from one. The operator can stop
// one and shell into its container.
//
// The console says threads and the control plane says sessions, deliberately. A session is the thread
// running, inside a sandbox, and that distinction is real inside the control plane. It means nothing
// to somebody reading a list of fourteen rows, where every one of them is a conversation. So the old
// name stays as an alias: the command bar should not punish muscle memory.
func Threads(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "threads",
		Aliases: []string{"t", "thread", "sessions", "session", "sess", "s"},
		Columns: []Column{
			{Title: "id", Width: 10},
			{Title: "workspace", Width: 16},
			{Title: "project", Width: 20},
			{Title: "thread", Width: 10},
			{Title: "status", Width: 10},
			{Title: "mode", Width: 12},
			{Title: "age", Width: 0},
		},
		// Ordered by thread, so a session keeps its place in the list as its age and status change
		// under it.
		SortBy:  3,
		List:    sessionLister(client, live),
		Actions: sessionActions(client),
	}
}

// Archived lists the threads that have been put away. Nothing was deleted to get here, so the only
// action is bringing one back.
//
// It is its own view rather than a filter on the threads view, because an archived thread is one the
// operator deliberately hid, and a listing that quietly grows them back is worse than no archive at
// all.
func Archived(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "archived",
		Aliases: []string{"arch", "archive"},
		Columns: []Column{
			{Title: "id", Width: 10},
			{Title: "workspace", Width: 16},
			{Title: "project", Width: 20},
			{Title: "thread", Width: 10},
			{Title: "status", Width: 10},
			{Title: "mode", Width: 12},
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
						return fmt.Errorf("no thread selected")
					}
					_, err := client.RestoreSession(ctx, &quaycrewv1.RestoreSessionRequest{Id: row.ID})
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
		resp, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{
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

		rows := make([]Row, 0, len(resp.GetSessions()))
		for _, session := range resp.GetSessions() {
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

func sessionRow(session *quaycrewv1.Session, workspaceName, projectName string) Row {
	// ID and Parent stay whole: they are what actions and scoping use. Only the cells shorten.
	return Row{
		ID:     session.GetId(),
		Parent: session.GetProject(),
		Label:  display.ShortID(session.GetThreadId()),
		Cells: []string{
			display.ShortID(session.GetId()),
			display.Name(workspaceName, session.GetWorkspace()),
			display.Name(projectName, session.GetProject()),
			display.ShortID(session.GetThreadId()),
			session.GetStatus(),
			permissionLabel(session.GetPermissionMode()),
			// The last column is how long ago it was put away in the archived view, and how long ago
			// it was touched everywhere else. A live thread has no archived stamp, so one rule covers
			// both without either view having to say which it is.
			age(lastMoved(session)),
		},
		State: stateFromStatus(session.GetStatus()),
	}
}

// permissionLabel is what a thread's mode reads as in a listing. A thread from before the mode was
// written down has none, and every one of those has been running acceptEdits, so it is named rather
// than left blank: an empty cell in this column would read as "asks first", which is the opposite.
//
// bypassPermissions becomes "dangerous", which is the word the operator already uses for it and the
// only one of the three worth spotting from across a list.
func permissionLabel(mode string) string {
	switch mode {
	case model.PermissionBypass:
		return "dangerous"
	case model.PermissionPlan:
		return "plan"
	default:
		return "edits"
	}
}

// lastMoved is when the thread last went anywhere: when it was put away if it was, and when it was
// last touched otherwise.
func lastMoved(session *quaycrewv1.Session) *timestamppb.Timestamp {
	if session.GetArchivedAt() != nil {
		return session.GetArchivedAt()
	}
	return session.GetUpdatedAt()
}

// permissionColumn is where the mode sits in a thread row, which is what the toggle reads to know
// which way it is going.
const permissionColumn = 5

// nextPermissionMode is the other side of the toggle: an armed thread goes back to asking before it
// does anything outside its files, and anything else arms it.
func nextPermissionMode(row Row) string {
	if len(row.Cells) > permissionColumn && row.Cells[permissionColumn] == "dangerous" {
		return model.PermissionAcceptEdits
	}
	return model.PermissionBypass
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
			Key:   "s",
			Label: "Shell",
			Shell: func(row Row) (*exec.Cmd, error) {
				if row.ID == "" {
					return nil, fmt.Errorf("no session selected")
				}
				return exec.Command("docker", "exec", "-it", sandbox.ContainerName(row.ID), "sh"), nil
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
					return fmt.Errorf("no thread selected")
				}
				_, err := client.RestartSession(ctx, &quaycrewv1.RestartSessionRequest{Id: row.ID})
				return err
			},
		},
		{
			// The dangerous toggle. It asks first, like every key that changes what a thread is
			// allowed to do, and it flips between the two modes worth flipping between: planning is
			// set deliberately rather than toggled into.
			Key:     "D",
			Label:   "Dangerous",
			Confirm: true,
			Run: func(ctx context.Context, row Row) error {
				if row.ID == "" {
					return fmt.Errorf("no thread selected")
				}
				_, err := client.SetSessionPermissionMode(ctx, &quaycrewv1.SetSessionPermissionModeRequest{
					Id: row.ID, Mode: nextPermissionMode(row),
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
					return fmt.Errorf("no thread selected")
				}
				_, err := client.ArchiveSession(ctx, &quaycrewv1.ArchiveSessionRequest{Id: row.ID})
				return err
			},
		},
		{
			// Backspace is the primary key, Julian's ask, and it asks before it acts. `x` still works.
			Key:     "backspace",
			Also:    []string{"x"},
			Label:   "Stop",
			Confirm: true,
			Run: func(ctx context.Context, row Row) error {
				if row.ID == "" {
					return fmt.Errorf("no session selected")
				}
				_, err := client.StopSession(ctx, &quaycrewv1.StopSessionRequest{Id: row.ID})
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

	spec, err := client.AttachSession(ctx, &quaycrewv1.AttachSessionRequest{Id: sessionID})
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

// age renders how long ago a timestamp was, compactly. An unset timestamp shows a dash rather than
// fifty years, which is what the zero value would otherwise read as.
func age(stamp *timestamppb.Timestamp) string {
	if stamp == nil || !stamp.IsValid() || stamp.AsTime().IsZero() {
		return "-"
	}
	return compactDuration(time.Since(stamp.AsTime()))
}

func compactDuration(elapsed time.Duration) string {
	switch {
	case elapsed < 0:
		return "0s"
	case elapsed < time.Minute:
		return fmt.Sprintf("%ds", int(elapsed.Seconds()))
	case elapsed < time.Hour:
		return fmt.Sprintf("%dm", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("%dh", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("%dd", int(elapsed.Hours())/24)
	}
}
