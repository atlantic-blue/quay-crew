// Command quay is the CLI channel: a synchronous client of the control plane. You create workspaces,
// dispatch a turn, and list sessions, and the reply comes straight back. Async chat channels use the
// event log instead; the CLI talks to the ControlPlaneService gRPC API directly.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/workspace"
)

const usage = `usage: quay [command]

with no command, quay opens the console: a full screen view of every resource the crew has.
press : to switch resource, / to filter, enter to drill in, s to shell into a session, q to quit.

commands:
  workspace create <name>                        create a workspace
  workspace list                                 list workspaces
  project create --workspace <ref> <name>        create a project inside a workspace
  project list [--workspace <ref>]               list projects
  dispatch --project <ref> <text>                start or continue a thread (--thread <id> continues)
  sessions [--workspace <ref>] [--project <ref>] list sessions
  secret set --workspace <ref> <key> <value>     set a workspace secret (for example the model token)

a <ref> is an id or a name. Add --workspace to a project reference when the name is ambiguous.
`

// run executes one CLI invocation against the control plane client, writing output to out.
func run(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", usage)
	}
	switch args[0] {
	case "workspace":
		return runWorkspace(ctx, client, args[1:], out)
	case "project":
		return runProject(ctx, client, args[1:], out)
	case "dispatch":
		return runDispatch(ctx, client, args[1:], out)
	case "sessions":
		return runSessions(ctx, client, args[1:], out)
	case "secret":
		return runSecret(ctx, client, args[1:], out)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
}

func runSecret(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "set" {
		return fmt.Errorf("usage: quay secret set --workspace <id> <key> <value>")
	}
	fs := flag.NewFlagSet("secret set", flag.ContinueOnError)
	fs.SetOutput(out)
	workspaceRef := fs.String("workspace", "", "workspace id or name (required)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *workspaceRef == "" {
		return fmt.Errorf("secret set requires --workspace")
	}
	rest := fs.Args()
	if len(rest) != 2 {
		return fmt.Errorf("usage: quay secret set --workspace <id or name> <key> <value>")
	}
	key, value := rest[0], rest[1]

	workspaceID, err := workspace.Resolve(ctx, client, *workspaceRef)
	if err != nil {
		return err
	}
	if _, err := client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{Workspace: workspaceID, Key: key, Value: value}); err != nil {
		return err
	}
	// Confirm without echoing the value.
	fmt.Fprintf(out, "set secret %s for workspace %s\n", key, workspaceID)
	return nil
}

func runWorkspace(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: quay workspace <create|list>")
	}
	switch args[0] {
	case "create":
		if len(args) < 2 {
			return fmt.Errorf("usage: quay workspace create <name>")
		}
		resp, err := client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: args[1]})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "created workspace %s (%s)\n", resp.GetWorkspace().GetId(), resp.GetWorkspace().GetName())
		return nil
	case "list":
		resp, err := client.ListWorkspaces(ctx, &quaycrewv1.ListWorkspacesRequest{})
		if err != nil {
			return err
		}
		if len(resp.GetWorkspaces()) == 0 {
			fmt.Fprintln(out, "no workspaces")
			return nil
		}
		for _, p := range resp.GetWorkspaces() {
			fmt.Fprintf(out, "%s  %s\n", p.GetId(), p.GetName())
		}
		return nil
	default:
		return fmt.Errorf("usage: quay workspace <create|list>")
	}
}

func runProject(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: quay project <create|list>")
	}
	fs := flag.NewFlagSet("project", flag.ContinueOnError)
	fs.SetOutput(out)
	workspaceRef := fs.String("workspace", "", "workspace id or name")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	switch args[0] {
	case "create":
		name := strings.TrimSpace(strings.Join(fs.Args(), " "))
		if *workspaceRef == "" || name == "" {
			return fmt.Errorf("usage: quay project create --workspace <ref> <name>")
		}
		workspaceID, err := workspace.Resolve(ctx, client, *workspaceRef)
		if err != nil {
			return err
		}
		resp, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Workspace: workspaceID, Name: name})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "created project %s (%s)\n", resp.GetProject().GetId(), resp.GetProject().GetName())
		return nil

	case "list":
		scope := ""
		if *workspaceRef != "" {
			resolved, err := workspace.Resolve(ctx, client, *workspaceRef)
			if err != nil {
				return err
			}
			scope = resolved
		}
		resp, err := client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{Workspace: scope})
		if err != nil {
			return err
		}
		if len(resp.GetProjects()) == 0 {
			fmt.Fprintln(out, "no projects")
			return nil
		}
		names := workspaceNames(ctx, client)
		for _, p := range resp.GetProjects() {
			fmt.Fprintf(out, "%s  %s  workspace=%s\n",
				display.ShortID(p.GetId()), p.GetName(), display.Name(names[p.GetWorkspace()], p.GetWorkspace()))
		}
		return nil

	default:
		return fmt.Errorf("usage: quay project <create|list>")
	}
}

// workspaceNames maps workspace id to name, so a listing can label rather than print identifiers.
// A failure yields an empty map: the listing still renders, falling back to short ids.
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

// projectNames maps project id to name, for the same reason.
func projectNames(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) map[string]string {
	resp, err := client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{})
	if err != nil {
		return map[string]string{}
	}
	names := make(map[string]string, len(resp.GetProjects()))
	for _, p := range resp.GetProjects() {
		names[p.GetId()] = p.GetName()
	}
	return names
}

func runDispatch(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("dispatch", flag.ContinueOnError)
	fs.SetOutput(out)
	projectRef := fs.String("project", "", "project id or name (required)")
	workspaceRef := fs.String("workspace", "", "workspace id or name, to narrow an ambiguous project name")
	thread := fs.String("thread", "", "thread id to continue (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *projectRef == "" {
		return fmt.Errorf("dispatch requires --project")
	}
	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if text == "" {
		return fmt.Errorf("dispatch requires message text")
	}

	projectID, err := workspace.ResolveProject(ctx, client, *workspaceRef, *projectRef)
	if err != nil {
		return err
	}
	resp, err := client.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: projectID, ThreadId: *thread, Text: text})
	if err != nil {
		return err
	}
	fmt.Fprintln(out, resp.GetReply())
	fmt.Fprintf(out, "(session %s, thread %s)\n", resp.GetSessionId(), resp.GetThreadId())
	return nil
}

func runSessions(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("sessions", flag.ContinueOnError)
	fs.SetOutput(out)
	workspaceRef := fs.String("workspace", "", "filter by workspace id or name (optional)")
	projectRef := fs.String("project", "", "filter by project id or name (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	workspaceID := ""
	if *workspaceRef != "" {
		resolved, err := workspace.Resolve(ctx, client, *workspaceRef)
		if err != nil {
			return err
		}
		workspaceID = resolved
	}
	projectID := ""
	if *projectRef != "" {
		resolved, err := workspace.ResolveProject(ctx, client, *workspaceRef, *projectRef)
		if err != nil {
			return err
		}
		projectID = resolved
	}
	resp, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Workspace: workspaceID, Project: projectID})
	if err != nil {
		return err
	}
	if len(resp.GetSessions()) == 0 {
		fmt.Fprintln(out, "no sessions")
		return nil
	}
	// Names, not identifiers: a listing of hex says nothing about what any of it is.
	workspaces, projects := workspaceNames(ctx, client), projectNames(ctx, client)
	for _, s := range resp.GetSessions() {
		fmt.Fprintf(out, "%s  %s/%s  thread %s  %s\n",
			display.ShortID(s.GetId()),
			display.Name(workspaces[s.GetWorkspace()], s.GetWorkspace()),
			display.Name(projects[s.GetProject()], s.GetProject()),
			display.ShortID(s.GetThreadId()),
			s.GetStatus())
	}
	return nil
}
