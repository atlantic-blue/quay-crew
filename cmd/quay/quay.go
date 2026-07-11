// Command quay is the CLI channel: a synchronous client of the control plane. You create projects,
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
)

const usage = `usage: quay <command>

commands:
  project create <name>            create a project
  project list                     list projects
  dispatch --project <id> <text>   start or continue a thread (add --thread <id> to continue)
  sessions [--project <id>]        list sessions
  secret set --project <id> <key> <value>   set a project secret (for example the model token)
`

// run executes one CLI invocation against the control plane client, writing output to out.
func run(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", usage)
	}
	switch args[0] {
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
		return fmt.Errorf("usage: quay secret set --project <id> <key> <value>")
	}
	fs := flag.NewFlagSet("secret set", flag.ContinueOnError)
	fs.SetOutput(out)
	project := fs.String("project", "", "project id (required)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *project == "" {
		return fmt.Errorf("secret set requires --project")
	}
	rest := fs.Args()
	if len(rest) != 2 {
		return fmt.Errorf("usage: quay secret set --project <id> <key> <value>")
	}
	key, value := rest[0], rest[1]

	if _, err := client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{Project: *project, Key: key, Value: value}); err != nil {
		return err
	}
	// Confirm without echoing the value.
	fmt.Fprintf(out, "set secret %s for project %s\n", key, *project)
	return nil
}

func runProject(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: quay project <create|list>")
	}
	switch args[0] {
	case "create":
		if len(args) < 2 {
			return fmt.Errorf("usage: quay project create <name>")
		}
		resp, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{Name: args[1]})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "created project %s (%s)\n", resp.GetProject().GetId(), resp.GetProject().GetName())
		return nil
	case "list":
		resp, err := client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{})
		if err != nil {
			return err
		}
		if len(resp.GetProjects()) == 0 {
			fmt.Fprintln(out, "no projects")
			return nil
		}
		for _, p := range resp.GetProjects() {
			fmt.Fprintf(out, "%s  %s\n", p.GetId(), p.GetName())
		}
		return nil
	default:
		return fmt.Errorf("usage: quay project <create|list>")
	}
}

func runDispatch(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("dispatch", flag.ContinueOnError)
	fs.SetOutput(out)
	project := fs.String("project", "", "project id (required)")
	thread := fs.String("thread", "", "thread id to continue (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *project == "" {
		return fmt.Errorf("dispatch requires --project")
	}
	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if text == "" {
		return fmt.Errorf("dispatch requires message text")
	}

	resp, err := client.Dispatch(ctx, &quaycrewv1.DispatchRequest{Project: *project, ThreadId: *thread, Text: text})
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
	project := fs.String("project", "", "filter by project id (optional)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resp, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: *project})
	if err != nil {
		return err
	}
	if len(resp.GetSessions()) == 0 {
		fmt.Fprintln(out, "no sessions")
		return nil
	}
	for _, s := range resp.GetSessions() {
		fmt.Fprintf(out, "%s  project=%s  thread=%s  %s\n", s.GetId(), s.GetProject(), s.GetThreadId(), s.GetStatus())
	}
	return nil
}
