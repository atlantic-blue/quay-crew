package console

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// sandboxPrefix is how internal/sandbox names a session's container. Shelling in derives the name
// from the session id rather than asking, which is why it works before the sandbox labelling in
// issue 35 lands. When Session carries a sandbox id, use that instead.
const sandboxPrefix = "quaycrew-"

// Projects lists the projects the control plane knows about, and drills into their sessions.
func Projects(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "projects",
		Aliases: []string{"p", "proj", "project"},
		Columns: []Column{
			{Title: "id", Width: 22},
			{Title: "name", Width: 0},
			{Title: "age", Width: 10},
		},
		DrillTo: "sessions",
		List: func(ctx context.Context, _ string) ([]Row, error) {
			resp, err := client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{})
			if err != nil {
				return nil, err
			}
			rows := make([]Row, 0, len(resp.GetProjects()))
			for _, project := range resp.GetProjects() {
				rows = append(rows, projectRow(project))
			}
			return rows, nil
		},
	}
}

func projectRow(project *quaycrewv1.Project) Row {
	return Row{
		ID:    project.GetId(),
		Cells: []string{project.GetId(), project.GetName(), age(project.GetCreatedAt())},
		State: StateReady,
	}
}

// Sessions lists sessions, scoped to a project when drilled into from one. The operator can stop a
// session and shell into its container.
func Sessions(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "sessions",
		Aliases: []string{"s", "sess", "session"},
		Columns: []Column{
			{Title: "id", Width: 22},
			{Title: "project", Width: 18},
			{Title: "thread", Width: 22},
			{Title: "status", Width: 10},
			{Title: "age", Width: 0},
		},
		List:    sessionLister(client),
		Actions: sessionActions(client),
	}
}

func sessionLister(client quaycrewv1.ControlPlaneServiceClient) Lister {
	return func(ctx context.Context, project string) ([]Row, error) {
		resp, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: project})
		if err != nil {
			return nil, err
		}
		rows := make([]Row, 0, len(resp.GetSessions()))
		for _, session := range resp.GetSessions() {
			rows = append(rows, sessionRow(session))
		}
		return rows, nil
	}
}

func sessionRow(session *quaycrewv1.Session) Row {
	return Row{
		ID:     session.GetId(),
		Parent: session.GetProject(),
		Cells: []string{
			session.GetId(),
			session.GetProject(),
			session.GetThreadId(),
			session.GetStatus(),
			age(session.GetUpdatedAt()),
		},
		State: stateFromStatus(session.GetStatus()),
	}
}

func sessionActions(client quaycrewv1.ControlPlaneServiceClient) []Action {
	return []Action{
		{
			Key:   "s",
			Label: "Shell",
			Shell: func(row Row) *exec.Cmd {
				if row.ID == "" {
					return nil
				}
				return exec.Command("docker", "exec", "-it", sandboxPrefix+row.ID, "sh")
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
