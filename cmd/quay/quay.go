// Command quay is the CLI channel: a synchronous client of the control plane. You create workspaces,
// dispatch a turn, and list sessions, and the reply comes straight back. Async chat channels use the
// event log instead; the CLI talks to the ControlPlaneService gRPC API directly.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/quay-crew/features"
	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/console"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/workspace"
)

const usage = `usage: quay [command]

with no command, quay opens the console: a full screen view of every resource the crew has.
press : to switch resource, / to filter, enter to drill in, s to shell into a session, q to quit.

you work in one place at a time, and say where with an address: workspace/project/thread.

  quay use me/house-bills
  quay dispatch "when is the electricity bill due"

commands:
  version                                 print which build this is
  features                                what this crew can do, and what proves it
  use [<address>]                         show where you are, or move there
  workspace create <name>                 create a workspace and move into it
  workspace list                          list workspaces
  project create [<workspace>/]<name>     create a project and move into it
  project list [<workspace>]              list projects
  dispatch [<address>] <text>             start or continue a thread
  sessions [<address>]                    list sessions
  context [<address>]                     where the files the model reads live
  context edit [<address>]                open a project's context in $EDITOR
  attach <session id>                     open a session's conversation, with its history
  secret set [<workspace>] <key> <value>  set a workspace secret (for example the model token)

a level of an address is a name or an id, so me/house-bills and me/3db6b81e both work, and a thread
may be the shortened id a listing prints. An address typed on the command line applies to that
command only and does not move you.
`

// runFeatures prints what the product does, from the specification embedded in this binary. With a
// name, it prints only the features that mention it.
//
// It talks to nothing. What the crew can do is a property of the build, not of a running stack, and
// the question is usually asked by somebody who has not started one yet.
func runFeatures(args []string, out io.Writer) error {
	needle := strings.ToLower(strings.TrimSpace(strings.Join(args, " ")))
	shown := 0
	for _, feature := range features.All() {
		if needle != "" && !mentions(feature, needle) {
			continue
		}
		shown++
		fmt.Fprintf(out, "%s\n", feature.Title)
		if feature.Summary != "" {
			fmt.Fprintf(out, "  %s\n", feature.Summary)
		}
		for _, scenario := range feature.Scenarios {
			fmt.Fprintf(out, "    %s\n", scenario)
		}
		fmt.Fprintln(out)
	}
	if shown == 0 {
		fmt.Fprintf(out, "nothing here mentions %q\n", needle)
	}
	return nil
}

// mentions reports whether a feature is about what was asked for, title, summary or scenarios.
func mentions(feature features.Feature, needle string) bool {
	haystack := strings.ToLower(feature.Title + " " + feature.Summary + " " + strings.Join(feature.Scenarios, " "))
	return strings.Contains(haystack, needle)
}

// removedFlags are the flags addresses replaced. They are refused by name rather than ignored,
// because ignoring one is worse than not having it: `quay dispatch --project default "hello"` reads
// as a perfectly good command, and what actually happened was that the flag and its value became the
// first three words of the message.
var removedFlags = map[string]string{
	"--project":   "an address names the project: quay dispatch <workspace>/<project> \"...\"",
	"--workspace": "an address names the workspace: quay sessions <workspace>",
	"--thread":    "an address names the thread: quay dispatch <workspace>/<project>/<thread> \"...\"",
}

// refuseFlags returns an error when an invocation uses a flag. This tool has none: everything it used
// to take one for is said with an address now, and a flag that is quietly ignored is worse than one
// that never existed, because `quay dispatch --project default "hello"` reads as a good command and
// what actually happened was that both words became the start of the message.
func refuseFlags(args []string) error {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		name, _, _ := strings.Cut(arg, "=")
		if instead, removed := removedFlags[name]; removed {
			return fmt.Errorf("%s is gone: %s\n\nor move there once and stop saying it: quay use <workspace>/<project>", name, instead)
		}
		return fmt.Errorf("quay takes no flags, and %s is not one: say where with an address instead\n\n%s", name, usage)
	}
	return nil
}

// run executes one CLI invocation against the control plane client, writing output to out.
func run(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", usage)
	}
	if err := refuseFlags(args); err != nil {
		return err
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(out, version)
		return nil
	case "features":
		return runFeatures(args[1:], out)
	case "use":
		return runUse(ctx, client, args[1:], out)
	case "workspace":
		return runWorkspace(ctx, client, args[1:], out)
	case "project":
		return runProject(ctx, client, args[1:], out)
	case "dispatch":
		return runDispatch(ctx, client, args[1:], out)
	case "attach":
		return runAttach(ctx, client, args[1:], out)
	case "sessions":
		return runSessions(ctx, client, args[1:], out)
	case "context":
		return runContext(ctx, client, args[1:], out)
	case "secret":
		return runSecret(ctx, client, args[1:], out)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
}

// locate works out which address a command is about, the one typed or the one the operator is
// already in, and resolves it, so a command never acts on an address that does not exist.
func locate(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string) (workspace.Location, error) {
	path, err := addressFrom(typed)
	if err != nil {
		return workspace.Location{}, err
	}
	return workspace.ResolvePath(ctx, client, path)
}

// addressFrom returns the address to act on, without touching the control plane.
func addressFrom(typed string) (workspace.Path, error) {
	if strings.TrimSpace(typed) != "" {
		return workspace.ParsePath(typed)
	}
	current, err := currentPath()
	if err != nil {
		return workspace.Path{}, err
	}
	if current.IsZero() {
		return workspace.Path{}, fmt.Errorf(
			"you are nowhere yet: run `quay use <workspace>/<project>`, or give an address, for example `quay dispatch me/house-bills \"hello\"`")
	}
	return current, nil
}

// runUse shows where the operator is, or moves them somewhere else.
func runUse(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		current, err := currentPath()
		if err != nil {
			return err
		}
		if current.IsZero() {
			fmt.Fprintln(out, "you are nowhere yet: quay use <workspace>/<project>")
			return nil
		}
		fmt.Fprintln(out, current.String())
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: quay use <workspace>[/<project>[/<thread>]]")
	}

	path, err := workspace.ParsePath(args[0])
	if err != nil {
		return err
	}
	// Resolve before recording it, so an address that names nothing is refused now rather than by
	// every command that comes after.
	if _, err := workspace.ResolvePath(ctx, client, path); err != nil {
		return err
	}
	return move(path, out)
}

// move records the new address and says so, so the operator is never guessing where they are.
func move(path workspace.Path, out io.Writer) error {
	if err := moveTo(path); err != nil {
		return err
	}
	fmt.Fprintf(out, "now in %s\n", path.String())
	return nil
}

func runSecret(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] != "set" {
		return fmt.Errorf("usage: quay secret set [<workspace>] <key> <value>")
	}
	rest := args[1:]
	typed := ""
	if len(rest) == 3 {
		typed, rest = rest[0], rest[1:]
	}
	if len(rest) != 2 {
		return fmt.Errorf("usage: quay secret set [<workspace>] <key> <value>")
	}
	key, value := rest[0], rest[1]

	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if _, err := client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
		Workspace: located.WorkspaceID, Key: key, Value: value,
	}); err != nil {
		return err
	}
	// Confirm without echoing the value.
	fmt.Fprintf(out, "set secret %s for workspace %s\n", key, located.Path.Workspace)
	return nil
}

func runWorkspace(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: quay workspace <create|list>")
	}
	switch args[0] {
	case "create":
		if len(args) != 2 {
			return fmt.Errorf("usage: quay workspace create <name>")
		}
		resp, err := client.CreateWorkspace(ctx, &quaycrewv1.CreateWorkspaceRequest{Name: args[1]})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "created workspace %s (%s)\n", resp.GetWorkspace().GetId(), resp.GetWorkspace().GetName())
		// Creating something is the clearest statement there is of where you want to be.
		return move(workspace.Path{Workspace: resp.GetWorkspace().GetName()}, out)
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
	switch args[0] {
	case "create":
		if len(args) != 2 {
			return fmt.Errorf("usage: quay project create [<workspace>/]<name>")
		}
		// The last level is the new project's name, so anything before it says where to put it.
		holder, projectName := splitLast(args[1])
		located, err := locate(ctx, client, holder)
		if err != nil {
			return err
		}
		resp, err := client.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
			Workspace: located.WorkspaceID, Name: projectName,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "created project %s (%s)\n", resp.GetProject().GetId(), resp.GetProject().GetName())
		return move(workspace.Path{Workspace: located.Path.Workspace, Project: resp.GetProject().GetName()}, out)

	case "list":
		scope := ""
		if len(args) > 1 {
			located, err := locate(ctx, client, args[1])
			if err != nil {
				return err
			}
			scope = located.WorkspaceID
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
			fmt.Fprintf(out, "%s  %s/%s\n",
				display.ShortID(p.GetId()), display.Name(names[p.GetWorkspace()], p.GetWorkspace()), p.GetName())
		}
		return nil

	default:
		return fmt.Errorf("usage: quay project <create|list>")
	}
}

// splitLast divides an address into everything above the last level and the last level itself, which
// is how a create reads: "me/house-bills" makes "house-bills" inside "me".
func splitLast(address string) (holder, last string) {
	trimmed := strings.TrimSpace(address)
	cut := strings.LastIndex(trimmed, workspace.Separator)
	if cut < 0 {
		return "", trimmed
	}
	return trimmed[:cut], trimmed[cut+1:]
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
	typed, words := splitAddressFromText(args)
	text := strings.TrimSpace(strings.Join(words, " "))
	if text == "" {
		return fmt.Errorf("usage: quay dispatch [<address>] <text>")
	}

	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if !located.HasProject() {
		return needsAProject(ctx, client, located)
	}

	resp, err := client.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: located.ProjectID, ThreadId: located.ThreadID, Text: text,
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(out, resp.GetReply())
	fmt.Fprintf(out, "(session %s, thread %s)\n", resp.GetSessionId(), resp.GetThreadId())
	return nil
}

// splitAddressFromText decides whether the first word is an address or the start of the message.
//
// An address has to reach a project for a turn to run, so it always carries a separator. That keeps
// `quay dispatch hello there` a message rather than a mystifying lookup of "hello".
func splitAddressFromText(args []string) (address string, words []string) {
	if len(args) > 1 && strings.Contains(args[0], workspace.Separator) {
		return args[0], args[1:]
	}
	return "", args
}

// needsAProject explains an address that stops at a workspace and lists what it holds, because the
// next thing the operator needs is the name of one of those projects.
func needsAProject(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, located workspace.Location) error {
	resp, err := client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{Workspace: located.WorkspaceID})
	if err != nil {
		return fmt.Errorf("%s is a workspace: a turn runs in a project inside it", located.Path.Workspace)
	}
	if len(resp.GetProjects()) == 0 {
		return fmt.Errorf("%s holds no projects yet: create one with `quay project create <name>`", located.Path.Workspace)
	}
	names := make([]string, 0, len(resp.GetProjects()))
	for _, project := range resp.GetProjects() {
		names = append(names, project.GetName())
	}
	return fmt.Errorf("%s is a workspace: a turn runs in a project inside it, one of %s",
		located.Path.Workspace, strings.Join(names, ", "))
}

// runContext says where the files the model reads live. It asks the control plane rather than working
// the paths out, because this tool runs on the operator's machine and the layout belongs to the crew.
func runContext(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "edit" {
		return runContextEdit(ctx, client, args[1:], out)
	}
	if len(args) > 0 && args[0] == "set" {
		return runContextSet(ctx, client, args[1:], out)
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: quay context [<address>] | quay context edit [<address>]")
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}

	request := &quaycrewv1.ListContextsRequest{}
	// The crew's level is in every listing, so asking for it by name is asking for the listing.
	if typed == "crew" {
		typed = ""
	}
	// An address typed in wins; otherwise the operator's own place narrows it. Standing nowhere shows
	// the whole crew, because then the question was about the crew.
	path, err := addressFrom(typed)
	switch {
	case err != nil && typed != "":
		return err
	case err == nil && !path.IsZero():
		located, err := workspace.ResolvePath(ctx, client, path)
		if err != nil {
			return err
		}
		request.Project = located.ProjectID
	}

	resp, err := client.ListContexts(ctx, request)
	if err != nil {
		return err
	}
	if len(resp.GetDirs()) == 0 {
		fmt.Fprintln(out, "no context directories: this crew keeps a session's state in its container")
		return nil
	}
	for _, dir := range resp.GetDirs() {
		state := "nothing written yet"
		if dir.GetWritten() {
			state = firstLine(dir.GetBody())
		}
		fmt.Fprintf(out, "%-10s %-20s %s\n", dir.GetScope(), dir.GetName(), state)
	}
	return nil
}

// firstLine is what a level says, in one line, for a listing. The whole of it is what the model reads
// and not what somebody scanning a list needs.
func firstLine(body string) string {
	line := strings.TrimSpace(body)
	if cut := strings.IndexByte(line, '\n'); cut >= 0 {
		line = strings.TrimSpace(line[:cut]) + " ..."
	}
	if len(line) > 60 {
		line = line[:57] + "..."
	}
	return line
}

// runContextSet writes a level's context from standard input, which is how a file becomes context:
//
//	quay context set crew < ~/notes/how-we-work.md
//
// Reading a file rather than taking it as an argument is deliberate: context is prose, often long and
// full of everything a shell would like to interpret.
func runContextSet(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: quay context set [<address>|crew] < file")
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	scope, owner, name, err := contextTarget(ctx, client, typed)
	if err != nil {
		return err
	}

	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading what to say: %w", err)
	}
	if _, err := client.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: scope, Owner: owner, Body: string(body),
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s %s now says %d characters\n", scope, name, len(body))
	return nil
}

// contextTarget works out which level an address means. The word "crew" is the level above every
// workspace, a workspace address is that workspace, and anything deeper is its project.
func contextTarget(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string) (scope, owner, name string, err error) {
	if typed == "crew" {
		return "crew", "", "crew", nil
	}
	path, err := addressFrom(typed)
	if err != nil {
		return "", "", "", err
	}
	if path.IsZero() {
		return "", "", "", fmt.Errorf("say which context: crew, a workspace, or a workspace/project")
	}
	located, err := workspace.ResolvePath(ctx, client, path)
	if err != nil {
		return "", "", "", err
	}
	// A workspace on its own means the workspace's own context, which is the level an org lives at.
	if located.ProjectID == "" {
		return "workspace", located.WorkspaceID, path.Workspace, nil
	}
	return "project", located.ProjectID, path.Workspace + "/" + located.Path.Project, nil
}

// runContextEdit opens the project's memory file in the operator's editor. The project's rather than
// the workspace's, because that is the one somebody means when they say "the context for this work";
// the workspace's is reached by naming it.
func runContextEdit(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: quay context edit [<address>]")
	}
	// The same choice the console makes: VISUAL, then EDITOR, then vi.
	editor := console.Editor()

	request := &quaycrewv1.ListContextsRequest{}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	path, err := addressFrom(typed)
	switch {
	case err != nil && typed != "":
		return err
	case err == nil && !path.IsZero():
		located, err := workspace.ResolvePath(ctx, client, path)
		if err != nil {
			return err
		}
		request.Project = located.ProjectID
	}

	resp, err := client.ListContexts(ctx, request)
	if err != nil {
		return err
	}
	file, scope, owner := "", "", ""
	for _, dir := range resp.GetDirs() {
		if dir.GetScope() == "project" {
			file, scope, owner = dir.GetMemory(), dir.GetScope(), dir.GetOwner()
			break
		}
	}
	if file == "" {
		return fmt.Errorf("no project to edit context for: say which with quay use, or name one here")
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o777); err != nil {
		return fmt.Errorf("make room for %s: %w", file, err)
	}

	parts := strings.Fields(editor)
	command := exec.Command(parts[0], append(parts[1:], file)...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	fmt.Fprintf(out, "editing %s\n", file)
	if err := command.Run(); err != nil {
		return err
	}

	// The editor wrote a file; this tells the crew. Context lives in the store, and a file nobody read
	// back is a note left on one machine.
	body, _ := sandbox.ReadMemory(filepath.Dir(file))
	if _, err := client.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: scope, Owner: owner, Body: body,
	}); err != nil {
		return fmt.Errorf("saving what you wrote: %w", err)
	}
	return nil
}

func runSessions(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: quay sessions [<address>]")
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}

	request := &quaycrewv1.ListSessionsRequest{}
	// An address typed in wins; otherwise the operator's own place narrows the listing. Standing
	// nowhere lists everything, because then the question was about the crew rather than a place.
	path, err := addressFrom(typed)
	switch {
	case err != nil && typed != "":
		return err
	case err == nil && !path.IsZero():
		located, err := workspace.ResolvePath(ctx, client, path)
		if err != nil {
			return err
		}
		request.Workspace, request.Project = located.WorkspaceID, located.ProjectID
	}

	resp, err := client.ListSessions(ctx, request)
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
