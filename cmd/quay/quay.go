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
	"github.com/atlantic-blue/quay-crew/internal/workspace"
)

const usage = `usage: quay [command]

with no command, quay opens the console: a full screen view of every resource the crew has.
press : to switch resource, / to filter, enter to drill in, s to shell into a session, q to quit.

commands:
  workspace create <name>            create a workspace
  workspace list                     list workspaces
  dispatch --workspace <id or name> <text>    start or continue a thread (--thread <id> continues)
  sessions [--workspace <id or name>]         list sessions
  secret set --workspace <id or name> <key> <value>   set a workspace secret (for example the model token)
`

// run executes one CLI invocation against the control plane client, writing output to out.
func run(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", usage)
	}
	switch args[0] {
	case "workspace":
		return runWorkspace(ctx, client, args[1:], out)
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

func runDispatch(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("dispatch", flag.ContinueOnError)
	fs.SetOutput(out)
	workspaceRef := fs.String("workspace", "", "workspace id or name (required)")
	thread := fs.String("thread", "", "thread id to continue (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *workspaceRef == "" {
		return fmt.Errorf("dispatch requires --workspace")
	}
	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if text == "" {
		return fmt.Errorf("dispatch requires message text")
	}

	workspaceID, err := workspace.Resolve(ctx, client, *workspaceRef)
	if err != nil {
		return err
	}
	resp, err := client.Dispatch(ctx, &quaycrewv1.DispatchRequest{Workspace: workspaceID, ThreadId: *thread, Text: text})
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
	resp, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Workspace: workspaceID})
	if err != nil {
		return err
	}
	if len(resp.GetSessions()) == 0 {
		fmt.Fprintln(out, "no sessions")
		return nil
	}
	for _, s := range resp.GetSessions() {
		fmt.Fprintf(out, "%s  workspace=%s  thread=%s  %s\n", s.GetId(), s.GetWorkspace(), s.GetThreadId(), s.GetStatus())
	}
	return nil
}
