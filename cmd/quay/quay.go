// Command quay is the CLI channel: a synchronous client of the control plane. You create workspaces,
// dispatch a turn, and list sessions, and the reply comes straight back. Async chat channels use the
// event log instead; the CLI talks to the ControlPlaneService gRPC API directly.
package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/atlantic-blue/quay-crew/internal/manual"
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

// usage is the command list, kept in internal/manual so the tool and the document a session is told
// with cannot describe two different tools.
var usage = manual.Commands

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

// removedFlags are the flags this tool used to take. They are refused by name rather than ignored,
// because ignoring one is worse than not having it: `quay dispatch --project default "hello"` reads
// as a perfectly good command, and what actually happened was that the flag and its value became the
// first three words of the message.
// Each entry carries the whole of its own advice, because what to do instead differs: the three that
// addresses replaced are answered by an address, and the fourth is answered by a different command.
var removedFlags = map[string]string{
	"--project": "an address names the project: quay dispatch <workspace>/<project> \"...\"" +
		"\n\nor move there once and stop saying it: quay use <workspace>/<project>",
	"--workspace": "an address names the workspace: quay sessions <workspace>" +
		"\n\nor move there once and stop saying it: quay use <workspace>/<project>",
	"--thread": "an address names the thread: quay dispatch <workspace>/<project>/<thread> \"...\"" +
		"\n\nor move there once and stop saying it: quay use <workspace>/<project>",
	"--remote": "a repository is cloned in conversation now, following the git skill: attach it " +
		"with quay skill attach <workspace> git and ask the session to clone what it works on",
	// Not flags this tool ever took, but the ones everybody's fingers type first. Refusing them with
	// "say where with an address instead" was advice that could not be acted on, since neither is
	// asking where anything is.
	"--version": "which build this is: quay version",
}

// helpSpellings are every way somebody asks what this tool does. Asking for help is the one thing
// that should never be refused, whichever convention the person came from, so all of these print the
// usage and succeed rather than being taken for an unknown command or a flag.
var helpSpellings = map[string]bool{
	"help": true, "-h": true, "--help": true, "-help": true, "?": true,
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
			return fmt.Errorf("%s is gone: %s", name, instead)
		}
		return fmt.Errorf("quay takes no flags, and %s is not one: say where with an address instead\n\n%s", name, usage)
	}
	return nil
}

// run executes one CLI invocation against the control plane client, writing output to out.
func run(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer, addr string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", usage)
	}
	// Before the flag refusal, because two of the spellings are flags and being told this tool takes
	// no flags is not an answer to "what does this tool do".
	if helpSpellings[args[0]] {
		fmt.Fprintln(out, usage)
		return nil
	}
	if err := refuseFlags(args); err != nil {
		return err
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(out, version)
		return nil
	case "manual":
		return runManual(args[1:], out)
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
	case "panel":
		// The way off the command that used to open this. `quay` opens the crew itself now, so this
		// is refused loudly rather than being taken for an address or an unknown word.
		return fmt.Errorf("there is no panel command: `quay` on its own opens the crew, and p shows " +
			"or hides the conversation beside it")
	// Internal: the panes quay opens run these. Not in the usage, because they are not commands
	// anybody types.
	case "header":
		return runHeader(ctx, client, args[1:], out, addr)
	case "console":
		return runBareConsole(ctx, client, args[1:], addr)
	// thread and threads answer here too. The protocol says thread, the usage says thread three
	// times over, and the command was sessions alone, so the tool taught a word it then refused.
	case "sessions", "session", "threads", "thread":
		return runSessions(ctx, client, args[1:], out)
	case "turns":
		return runTurns(ctx, client, args[1:], out)
	case "mode":
		return runMode(ctx, client, args[1:], out)
	case "context":
		return runContext(ctx, client, args[1:], out)
	case "secret":
		return runSecret(ctx, client, args[1:], out)
	case "skill":
		return runSkill(ctx, client, args[1:], out)
	case "flow":
		return runFlow(ctx, client, args[1:], out)
	case "repository":
		// The way off the commands that used to be here. Refused by name rather than treated as an
		// unknown word, because they are still in somebody's fingers, their scripts and their notes.
		return fmt.Errorf(
			"there are no repository commands: a repository is cloned in conversation now, following " +
				"the git skill. Import it once with quay skill import skills/git, attach it with " +
				"quay skill attach <workspace> git, and ask the session to clone what it works on")
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
	located, err := workspace.ResolvePath(ctx, client, path)
	return located, standing(typed, path, err)
}

// standing rewrites a failure to resolve an address the operator did not type, because an address
// nobody typed came from the place they are standing in.
//
// That place is kept on this machine and the crew's own state is not, so anything that empties a crew
// leaves the tool pointing at something gone: a wipe, a fresh install against a different crew, or a
// colleague's crew on another address. Every command that defaults to where you are then refuses with
// a sentence about a missing workspace, which reads as the crew being broken rather than as you being
// nowhere.
func standing(typed string, path workspace.Path, err error) error {
	if err == nil || strings.TrimSpace(typed) != "" || !errors.Is(err, workspace.ErrNotFound) {
		return err
	}
	return fmt.Errorf("you are standing in %s, which this crew does not have: %w"+
		"\n\nmove with quay use <workspace>/<project>, or see what there is with quay workspace list",
		path, err)
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

// runSecretList says which secrets a workspace has, and never what any of them says.
func runSecretList(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: quay secret list [<workspace>]")
	}
	request := &quaycrewv1.ListSecretsRequest{}
	if len(args) == 1 {
		located, err := workspace.Resolve(ctx, client, args[0])
		if err != nil {
			return err
		}
		request.Workspace = located
	}
	resp, err := client.ListSecrets(ctx, request)
	if err != nil {
		return err
	}
	if len(resp.GetSecrets()) == 0 {
		fmt.Fprintln(out, "no secrets set")
		return nil
	}
	for _, secret := range resp.GetSecrets() {
		fmt.Fprintf(out, "%-20s %-32s set, and not shown anywhere\n",
			display.Name(secret.GetWorkspaceName(), secret.GetWorkspace()), secret.GetName())
	}
	return nil
}

func runSecret(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "list" {
		return runSecretList(ctx, client, args[1:], out)
	}
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

	case "remote":
		// The way off the command that used to be here. Refused by name rather than treated as an
		// unknown word, because it is still in somebody's fingers and in their notes.
		return fmt.Errorf(
			"there is no project remote command: a repository is cloned in conversation now, " +
				"following the git skill. Attach it with quay skill attach <workspace> git and ask " +
				"the session to clone what it works on")

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
		return fmt.Errorf("usage: quay project <create|list|remote>")
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
		Project: located.ProjectID, Handle: located.ThreadID, Text: text,
	})
	if err != nil {
		return err
	}
	fmt.Fprintln(out, resp.GetReply())
	fmt.Fprintf(out, "(thread %s, handle %s)\n", resp.GetId(), resp.GetHandle())
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
	if len(args) > 0 && args[0] == "clear" {
		return runContextClear(ctx, client, args[1:], out)
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: quay context [<address>] | quay context set [<address>] < file " +
			"| quay context edit [<address>] | quay context clear [<address>]")
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
			return standing(typed, path, err)
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
	// Nothing on standard input is almost never somebody asking to erase a level. It is a forgotten
	// redirection, or a file that turned out to be empty, and there is no undo: what was there is
	// gone the moment this returns. Emptying a level deliberately has its own command, which says
	// what it is doing.
	if len(strings.TrimSpace(string(body))) == 0 {
		held, err := contextLength(ctx, client, scope, owner)
		if err != nil || held == 0 {
			fmt.Fprintf(out, "%s %s was already empty, and nothing was on standard input\n", scope, name)
			return nil
		}
		return fmt.Errorf("nothing was on standard input, so %s %s is untouched and still says %d "+
			"characters\n\npipe something in: cat notes.md | quay context set %s"+
			"\nor empty it deliberately: quay context clear %s", scope, name, held, typed, typed)
	}
	if _, err := client.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: scope, Owner: owner, Body: string(body),
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s %s now says %d characters\n", scope, name, len(body))
	return nil
}

// runContextClear empties a level, which context set used to do by accident whenever standing input
// was empty. Saying it out loud is the whole point of it being its own command.
func runContextClear(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: quay context clear [<address>|crew]")
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	scope, owner, name, err := contextTarget(ctx, client, typed)
	if err != nil {
		return err
	}
	held, err := contextLength(ctx, client, scope, owner)
	if err != nil {
		return err
	}
	if held == 0 {
		fmt.Fprintf(out, "%s %s was already empty\n", scope, name)
		return nil
	}
	if _, err := client.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: scope, Owner: owner, Body: "",
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s %s emptied, and it said %d characters\n", scope, name, held)
	return nil
}

// contextLength is how much a level says today, so a refusal can name what it is protecting and
// clearing can say what it removed.
func contextLength(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, scope, owner string) (int, error) {
	resp, err := client.ListContexts(ctx, &quaycrewv1.ListContextsRequest{})
	if err != nil {
		return 0, err
	}
	for _, dir := range resp.GetDirs() {
		if dir.GetScope() == scope && dir.GetOwner() == owner {
			return len(dir.GetBody()), nil
		}
	}
	return 0, nil
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
		return "", "", "", standing(typed, path, err)
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
			return standing(typed, path, err)
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

	request := &quaycrewv1.ListThreadsRequest{}
	// An address typed in wins; otherwise the operator's own place narrows the listing. Standing
	// nowhere lists everything, because then the question was about the crew rather than a place.
	path, err := addressFrom(typed)
	switch {
	case err != nil && typed != "":
		return err
	case err == nil && !path.IsZero():
		located, err := workspace.ResolvePath(ctx, client, path)
		if err != nil {
			return standing(typed, path, err)
		}
		request.Workspace, request.Project = located.WorkspaceID, located.ProjectID
	}

	resp, err := client.ListThreads(ctx, request)
	if err != nil {
		return err
	}
	if len(resp.GetThreads()) == 0 {
		fmt.Fprintln(out, "no threads")
		return nil
	}
	// Names, not identifiers: a listing of hex says nothing about what any of it is.
	workspaces, projects := workspaceNames(ctx, client), projectNames(ctx, client)
	for _, s := range resp.GetThreads() {
		fmt.Fprintf(out, "%s  %s/%s  handle %s  %s\n",
			display.ShortID(s.GetId()),
			display.Name(workspaces[s.GetWorkspace()], s.GetWorkspace()),
			display.Name(projects[s.GetProject()], s.GetProject()),
			display.ShortID(s.GetHandle()),
			s.GetStatus())
	}
	return nil
}
