package console

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
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

// Sessions lists sessions, scoped to a project when drilled into from one. The operator can stop a
// session and shell into its container.
func Sessions(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "sessions",
		Aliases: []string{"s", "sess", "session"},
		Columns: []Column{
			{Title: "id", Width: 10},
			{Title: "workspace", Width: 16},
			{Title: "project", Width: 20},
			{Title: "thread", Width: 10},
			{Title: "status", Width: 10},
			{Title: "age", Width: 0},
		},
		// Ordered by thread, so a session keeps its place in the list as its age and status change
		// under it.
		SortBy:  3,
		List:    sessionLister(client),
		Actions: sessionActions(client),
	}
}

func sessionLister(client quaycrewv1.ControlPlaneServiceClient) Lister {
	// parent is a project id when drilled into from one, and empty at the top level.
	return func(ctx context.Context, parent string) ([]Row, error) {
		resp, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: parent})
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
			age(session.GetUpdatedAt()),
		},
		State: stateFromStatus(session.GetStatus()),
	}
}

func sessionActions(client quaycrewv1.ControlPlaneServiceClient) []Action {
	return []Action{
		{
			Key:   "a",
			Label: "Attach",
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
			Key:   "x",
			Label: "Stop",
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
