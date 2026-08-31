// Command krewe is the command line channel, a client of the control plane. You create workspaces,
// start a task and list sessions. `krewe task` waits here for the answer and `krewe task --dispatch`
// lets go of it, which is the one difference between the two ways of talking to a session. Async
// chat channels use the event log instead; this tool talks to the ControlPlaneService gRPC API
// directly.
package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/atlantic-blue/krewe/internal/manual"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/krewe/features"
	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/console"
	"github.com/atlantic-blue/krewe/internal/contextsize"
	"github.com/atlantic-blue/krewe/internal/display"
	"github.com/atlantic-blue/krewe/internal/repository"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/workspace"
)

// usage is the command list, kept in internal/manual so the tool and the document a session is told
// with cannot describe two different tools.
var usage = manual.Commands

// runFeatures prints what the product does, from the specification embedded in this binary. With a
// name, it prints only the features that mention it.
//
// It talks to nothing. What the system can do is a property of the build, not of a running stack, and
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
// because ignoring one is worse than not having it: `krewe task --project default "hello"` reads
// as a perfectly good command, and what actually happened was that the flag and its value became the
// first three words of the message.
// Each entry carries the whole of its own advice, because what to do instead differs: the three that
// addresses replaced are answered by an address, and the fourth is answered by a different command.
var removedFlags = map[string]string{
	"--project": "an address names the project: krewe task <workspace>/<project> \"...\"" +
		"\n\nor move there once and stop saying it: krewe use <workspace>/<project>",
	"--workspace": "an address names the workspace: krewe sessions <workspace>" +
		"\n\nor move there once and stop saying it: krewe use <workspace>/<project>",
	"--session": "an address names the session: krewe task <workspace>/<project>/<session> \"...\"" +
		"\n\nor move there once and stop saying it: krewe use <workspace>/<project>",
	"--thread": "an address names the session: krewe task <workspace>/<project>/<session> \"...\"" +
		"\n\nor move there once and stop saying it: krewe use <workspace>/<project>",
	"--remote": "a repository is cloned in conversation now, following the git skill: attach it " +
		"with krewe skill attach <workspace> git and ask the session to clone what it works on. To say " +
		"which repository a project's work lands in: krewe project repository <owner>/<name>",
	// Not flags this tool ever took, but the ones everybody's fingers type first. Refusing them with
	// "say where with an address instead" was advice that could not be acted on, since neither is
	// asking where anything is.
	"--version": "which build this is: krewe version",
	// Letting go is one flag on one word now, so every other spelling of it names that flag, and
	// none of them can be quietly swallowed into the message.
	"--detach": "letting go is krewe task --dispatch [<address>] \"...\", and krewe task list <session> " +
		"reads it back",
	"--wait": "krewe task waits for the answer already: krewe task [<address>] \"...\"",
	"--no-wait": "letting go is krewe task --dispatch [<address>] \"...\"" +
		"\n\nand krewe task on its own waits for the answer instead",
	// The word reads correctly in both directions now: this job requires context, and the architect
	// role receives context.
	"--hands": "what a job cannot be done without is what it requires: " +
		"krewe job create --requires <material>",
}

// removedCommands are the words this tool used to take, each against what to type now.
//
// A removed command refuses by being absent from the table in run and present here, rather than by a
// case of its own, so the next word removed cannot forget to say anything. The three that one word
// replaced are why: ask, dispatch and tasks were three top level commands for one entity, while job
// and flow were each one noun with verbs under it.
//
// None of these is a quiet alias. A word that still works keeps two spellings alive for one thing, a
// word that becomes an unknown command reads as the tool being broken, and a word absorbed into the
// next argument is worse than both, because the command then succeeds. Every entry here leaves run
// holding an error, so the process exits non zero and a caller reading the status cannot take a
// refusal for a success.
var removedCommands = map[string]string{
	"ask": "a task is one word now, and waiting here for the answer is what it does" +
		"\n\n  krewe task [<address>] \"...\"",
	"dispatch": "a task is one word now, and letting go of one is a flag on it" +
		"\n\n  krewe task --dispatch [<address>] \"...\"",
	"tasks": "a task is one word now, and reading a session's history back is a verb under it" +
		"\n\n  krewe task list <session>",
	"threads": "a thread is called a session now, because the system has one word for a conversation " +
		"and this was the second. Use krewe sessions",
	"thread": "a thread is called a session now, because the system has one word for a conversation " +
		"and this was the second. Use krewe sessions",
	"turns": "a turn is called a task now, because a turn is a word from conversation analysis and " +
		"nothing about it said how long it takes. Use krewe task list <session>",
	"turn": "a turn is called a task now, because a turn is a word from conversation analysis and " +
		"nothing about it said how long it takes. Use krewe task list <session>",
	"repository": "a repository is cloned in conversation now, following the git skill. Import it " +
		"once with krewe skill import skills/git, attach it with krewe skill attach <workspace> git, " +
		"and ask the session to clone what it works on. To say which repository a project's work " +
		"lands in: krewe project repository <owner>/<name>",
	"panel": "`krewe` on its own opens the system, and p shows or hides the conversation beside it",
	"work": "declared intent is called a job now, because that is what Kubernetes calls the same " +
		"thing: run to completion, watched by a controller, with a disposable container underneath" +
		"\n\n  krewe job <create|list|show|stop>",
}

// helpSpellings are every way somebody asks what this tool does. Asking for help is the one thing
// that should never be refused, whichever convention the person came from, so all of these print the
// usage and succeed rather than being taken for an unknown command or a flag.
var helpSpellings = map[string]bool{
	"help": true, "-h": true, "--help": true, "-help": true, "?": true,
}

// takenFlags are the flags a command genuinely takes, against the command that takes them.
//
// Almost none, and that is deliberate: everything a flag used to say here is said with an address.
// The ones that remain say what shape the output takes, or which of two things a word does, rather
// than where anything is.
var takenFlags = map[string]map[string]bool{
	"answer":  {allAnswers: true},
	"task":    {flagDispatch: true},
	"job":     jobFlagsTaken(),
	"history": historyFlagsTaken(),
	"limits":  limitsFlagsTaken(),
	"target":  targetFlagsTaken(),
}

// refuseFlags returns an error when an invocation uses a flag the command it names does not take. A
// flag that is quietly ignored is worse than one that never existed, because
// `krewe task --project default "hello"` reads as a good command and what actually happened was
// that both words became the start of the message.
func refuseFlags(args []string) error {
	taken := takenFlags[args[0]]
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		name, _, _ := strings.Cut(arg, "=")
		if taken[name] {
			continue
		}
		if instead, removed := removedFlags[name]; removed {
			return fmt.Errorf("%s is gone: %s", name, instead)
		}
		return fmt.Errorf("krewe takes no flags, and %s is not one: say where with an address instead\n\n%s", name, usage)
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
	// Before the flags, because a flag on a word that no longer exists is not what the operator has
	// to fix: naming the flag would send them to correct one part of a command that is gone whole.
	if instead, removed := removedCommands[args[0]]; removed {
		return fmt.Errorf("there is no %s command: %s", args[0], instead)
	}
	if err := refuseFlags(args); err != nil {
		return err
	}
	switch args[0] {
	case "version":
		return runVersion(ctx, client, out)
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
	case "task":
		return runTask(ctx, client, args[1:], out)
	case "attach":
		return runAttach(ctx, client, args[1:], out, os.Stdin)
	case "web":
		return runWeb(ctx, client, args[1:], out)
	case "render":
		return runRender(args[1:], out)
	case "room":
		return runRoom(ctx, client, out)
	// Internal: the panes krewe opens run these, and the model runtime in a sandbox runs the last of
	// them. Not in the usage, because they are not commands anybody types.
	case "header":
		return runHeader(ctx, client, args[1:], out, addr)
	case "statusline":
		return runStatusLine(args[1:], os.Stdin, out)
	case "console":
		return runBareConsole(ctx, client, args[1:], addr)
	case "sessions", "session":
		return runSessions(ctx, client, args[1:], out)
	case "answer":
		return runAnswer(ctx, client, args[1:], out)
	case "stop":
		return runStop(ctx, client, args[1:], out)
	case "drain":
		return runDrain(ctx, client, args[1:], out)
	case "mode":
		return runMode(ctx, client, args[1:], out)
	case "label":
		return runLabel(ctx, client, args[1:], out)
	case "context":
		return runContext(ctx, client, args[1:], out)
	case "secret":
		return runSecret(ctx, client, args[1:], out)
	case "skill":
		return runSkill(ctx, client, args[1:], out)
	case "hook":
		return runHook(ctx, client, args[1:], out)
	case "role":
		return runRole(ctx, client, args[1:], out)
	case "steer":
		return runSteer(ctx, client, args[1:], out)
	case "steers":
		return runSteers(ctx, client, args[1:], out)
	case "history":
		return runHistory(ctx, client, args[1:], out)
	case "job":
		return runJob(ctx, client, args[1:], out)
	case "target":
		return runTarget(ctx, client, args[1:], out)
	case "limits":
		return runLimits(ctx, client, args[1:], out)
	case "flow":
		return runFlow(ctx, client, args[1:], out)
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
// That place is kept on this machine and the system's own state is not, so anything that empties a system
// leaves the tool pointing at something gone: a wipe, a fresh install against a different system, or a
// colleague's system on another address. Every command that defaults to where you are then refuses with
// a sentence about a missing workspace, which reads as the system being broken rather than as you being
// nowhere.
func standing(typed string, path workspace.Path, err error) error {
	if err == nil || strings.TrimSpace(typed) != "" || !errors.Is(err, workspace.ErrNotFound) {
		return err
	}
	return fmt.Errorf("you are standing in %s, which this system does not have: %w"+
		"\n\nmove with krewe use <workspace>/<project>, or see what there is with krewe workspace list",
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
			"you are nowhere yet: run `krewe use <workspace>/<project>`, or give an address, for example `krewe task me/house-bills \"hello\"`")
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
			fmt.Fprintln(out, "you are nowhere yet: krewe use <workspace>/<project>")
			return nil
		}
		fmt.Fprintln(out, current.String())
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: krewe use <workspace>[/<project>[/<session>]]")
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
		return fmt.Errorf("usage: krewe secret list [<workspace>|system]")
	}
	request := &quaycrewv1.ListSecretsRequest{}
	// "system" asks for what the system holds and nothing else. It is not a workspace, so it is answered
	// by filtering the listing rather than by resolving an address.
	onlySystem := len(args) == 1 && args[0] == systemScope
	where := systemWide("secrets")
	if onlySystem {
		where = narrowedTo("secrets", "the system's own", "krewe secret list on its own reads every workspace")
	}
	if len(args) == 1 && !onlySystem {
		located, err := workspace.Resolve(ctx, client, args[0])
		if err != nil {
			return err
		}
		request.Workspace = located
		where = narrowedTo("secrets", args[0], "krewe secret list on its own reads every workspace")
	}
	resp, err := client.ListSecrets(ctx, request)
	if err != nil {
		return err
	}
	shown := 0
	for _, secret := range resp.GetSecrets() {
		if onlySystem && !secret.GetSystem() {
			continue
		}
		shown++
		// The system's own belong to no workspace, so the column says the level instead of an
		// identifier. Reading "system" where a workspace name goes is how the listing says every
		// workspace has this one.
		where := display.Name(secret.GetWorkspaceName(), secret.GetWorkspace())
		if secret.GetSystem() {
			where = systemScope
		}
		fmt.Fprintf(out, "%-20s %-32s %s\n", where, secret.GetName(),
			whereItLands(secret.GetName(), secret.GetProjection()))
	}
	if shown == 0 {
		where.nothing(out)
		return nil
	}
	where.counted(out, shown)
	return nil
}

// whereItLands says how a secret reaches a session, which is the one thing about it worth showing:
// a session looking for a credential at a path and a session looking for it in the environment fail
// in different places, and the listing is where that is answered.
func whereItLands(name string, projection quaycrewv1.SecretProjection) string {
	if projection == quaycrewv1.SecretProjection_SECRET_PROJECTION_FILE {
		return "mounted at " + sandbox.SecretFilePath(name)
	}
	return "set, and not shown anywhere"
}

// secretUsage names the piped form first, because it is the one that keeps a credential out of the
// shell history and out of the process list.
const secretUsage = "usage: <value> | krewe secret set [<workspace>] <key>" +
	"\n   or: krewe secret set [<workspace>] <key> <value>" +
	"\n   or: krewe secret mount [<workspace>] <name> <path>   (a credential that is a file)" +
	"\n\nsay system where a workspace goes to set it once for every workspace:" +
	"\n   gh auth token | krewe secret set system GITHUB_TOKEN"

// standardInputIsPiped says whether something is being fed in rather than a person typing. A
// character device is a terminal; anything else is a pipe or a file redirection.
func standardInputIsPiped() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

func runSecret(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "list" {
		return runSecretList(ctx, client, args[1:], out)
	}
	if len(args) > 0 && args[0] == "mount" {
		return runSecretMount(ctx, client, args[1:], out)
	}
	if len(args) == 0 || args[0] != "set" {
		return fmt.Errorf("%s", secretUsage)
	}
	rest := args[1:]

	// Whether something is being piped in decides how the arguments read, because a value on standard
	// input is the one that never reaches the shell history or the process list, and that is the road
	// worth making obvious. With a pipe every argument is an address or a name; without one the last
	// argument is still the value, so the shape that scripts already use keeps working.
	piped := standardInputIsPiped()
	var typed, key, value string
	switch {
	case piped && len(rest) == 1:
		key = rest[0]
	case piped && len(rest) == 2:
		typed, key = rest[0], rest[1]
	case !piped && len(rest) == 2:
		key, value = rest[0], rest[1]
	case !piped && len(rest) == 3:
		typed, key, value = rest[0], rest[1], rest[2]
	case !piped && len(rest) == 1:
		return fmt.Errorf("no value for %s, and nothing is being piped in"+
			"\n\npipe it, so it never reaches your shell history: gh auth token | krewe secret set %s"+
			"\nor from a file: krewe secret set %s < token.txt", rest[0], rest[0], rest[0])
	default:
		return fmt.Errorf("%s", secretUsage)
	}

	if piped {
		read, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading the value: %w", err)
		}
		// Trimmed, because the tools that produce a credential end with a newline. `gh auth token`
		// does, and a token carrying one authenticates nothing while looking exactly right.
		value = strings.TrimSpace(string(read))
		if value == "" {
			return fmt.Errorf("nothing was piped in, so %s was not set", key)
		}
	}

	// The system's level, said the same way a skill, a hook and a piece of context are given to it. It
	// is not an address, so there is nothing to resolve: no workspace is named and every workspace
	// reads it.
	if typed == systemScope {
		if _, err := client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
			Scope: systemScope, Key: key, Value: value,
		}); err != nil {
			return err
		}
		fmt.Fprintf(out, "set secret %s for the system, so every workspace has it\n", key)
		fmt.Fprintln(out, "a workspace that sets the same name keeps its own")
		return nil
	}

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

// mountUsage names the file form first, because a credential that is a file is what this command is
// for and the piped form is the way to mount one that is not on disk.
const mountUsage = "usage: krewe secret mount [<workspace>] <name> <path>" +
	"\n   or: <contents> | krewe secret mount [<workspace>] <name>" +
	"\n\nsay system where a workspace goes to mount it for every workspace"

// runSecretMount stores a secret that reaches a session as a file rather than as an environment
// variable.
//
// Some credentials are files: a git configuration, a private key, a cloud credentials file. A tool
// opens them by path, so there is nothing an environment variable can do for them. Kubernetes and
// Docker both answer this the same way, by making the presentation of a secret a separate choice
// from the storing of it, and this is that choice said out loud.
func runSecretMount(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	piped := standardInputIsPiped()
	var typed, name, from string
	switch {
	case piped && len(args) == 1:
		name = args[0]
	case piped && len(args) == 2:
		typed, name = args[0], args[1]
	case !piped && len(args) == 2:
		name, from = args[0], args[1]
	case !piped && len(args) == 3:
		typed, name, from = args[0], args[1], args[2]
	default:
		return fmt.Errorf("%s", mountUsage)
	}

	value, err := contentsOf(from)
	if err != nil {
		return err
	}
	if value == "" {
		return fmt.Errorf("there is nothing to mount, so %s was not set", name)
	}

	if typed == systemScope {
		if _, err := client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
			Scope:      systemScope,
			Key:        name,
			Value:      value,
			Projection: quaycrewv1.SecretProjection_SECRET_PROJECTION_FILE,
		}); err != nil {
			return err
		}
		fmt.Fprintf(out, "mounted %s for the system at %s, so every workspace has it\n", name, sandbox.SecretFilePath(name))
		fmt.Fprintln(out, "a session already running was made before this, so stop it to get a sandbox that has it")
		return nil
	}

	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if _, err := client.SetSecret(ctx, &quaycrewv1.SetSecretRequest{
		Workspace:  located.WorkspaceID,
		Key:        name,
		Value:      value,
		Projection: quaycrewv1.SecretProjection_SECRET_PROJECTION_FILE,
	}); err != nil {
		return err
	}
	// Where it lands, because a session has to be told the path and the operator is the one who tells
	// it. Then the caveat, because a mount happens when a container is made: a session already
	// running was made without this one.
	fmt.Fprintf(out, "mounted %s for workspace %s at %s\n", name, located.Path.Workspace, sandbox.SecretFilePath(name))
	fmt.Fprintln(out, "a session already running was made before this, so stop it to get a sandbox that has it")
	return nil
}

// contentsOf reads what to mount, from a path or from standard input.
//
// Byte for byte, unlike `krewe secret set`, which trims. A token gains a newline from the tool that
// printed it and is worth trimming; a file's bytes are the file, and one that arrives a byte shorter
// than the operator's own is a file they cannot reason about.
func contentsOf(from string) (string, error) {
	if from == "" {
		read, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading what to mount: %w", err)
		}
		return string(read), nil
	}
	read, err := os.ReadFile(from)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", from, err)
	}
	return string(read), nil
}

func runWorkspace(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: krewe workspace <create|list|delete>")
	}
	switch args[0] {
	case "delete":
		return runWorkspaceDelete(ctx, client, args[1:], out)
	case "create":
		if len(args) != 2 {
			return fmt.Errorf("usage: krewe workspace create <name>")
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
		where := systemWide("workspaces")
		if len(resp.GetWorkspaces()) == 0 {
			where.nothing(out)
			return nil
		}
		for _, p := range resp.GetWorkspaces() {
			fmt.Fprintf(out, "%s  %s\n", p.GetId(), p.GetName())
		}
		where.counted(out, len(resp.GetWorkspaces()))
		return nil
	default:
		return fmt.Errorf("usage: krewe workspace <create|list|delete>")
	}
}

func runProject(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: krewe project <create|list|repository|delete>")
	}
	switch args[0] {
	case "delete":
		return runProjectDelete(ctx, client, args[1:], out)
	case "repository":
		return runProjectRepository(ctx, client, args[1:], out)
	case "create":
		if len(args) != 2 {
			return fmt.Errorf("usage: krewe project create [<workspace>/]<name>")
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
		// Where the work lands is asked for here, at the moment the project is made, because that is
		// the moment it is decided. A project that looks finished and has nowhere to push is what
		// leaves a session holding work nobody can read.
		writeRepository(out, "", resp.GetProject())
		return move(workspace.Path{Workspace: located.Path.Workspace, Project: resp.GetProject().GetName()}, out)

	case "remote":
		// The way off the command that used to be here. Refused by name rather than treated as an
		// unknown word, because it is still in somebody's fingers and in their notes. It names both
		// halves of what replaced it: fetching a repository is a conversation, and which repository
		// this project is, is a record.
		return fmt.Errorf(
			"there is no project remote command: a repository is cloned in conversation now, " +
				"following the git skill. Attach it with krewe skill attach <workspace> git and ask " +
				"the session to clone what it works on. To say which repository this project's work " +
				"lands in: krewe project repository <owner>/<name>")

	case "list":
		scope := ""
		where := systemWide("projects")
		if len(args) > 1 {
			located, err := locate(ctx, client, args[1])
			if err != nil {
				return err
			}
			scope = located.WorkspaceID
			where = narrowedTo("projects", located.Path.Workspace, "krewe project list on its own reads every workspace")
		}
		resp, err := client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{Workspace: scope})
		if err != nil {
			return err
		}
		if len(resp.GetProjects()) == 0 {
			where.nothing(out)
			return nil
		}
		names := workspaceNames(ctx, client)
		for _, p := range resp.GetProjects() {
			fmt.Fprintf(out, "%s  %s/%s%s\n",
				display.ShortID(p.GetId()), display.Name(names[p.GetWorkspace()], p.GetWorkspace()), p.GetName(),
				deploysTo(p.GetDeployTarget()))
			writeRepository(out, "          ", p)
		}
		where.counted(out, len(resp.GetProjects()))
		return nil

	default:
		return fmt.Errorf("usage: krewe project <create|list|repository|delete>")
	}
}

// readVerb is how a read of another project is spelled. One argument is a write, so an operator who
// wants to look at a project without touching it needs a word of its own: without one, the only
// spelling within reach is the write, which is what turned a look into an overwrite.
//
// A repository address always carries a separator, so this word can never be mistaken for one.
const readVerb = "show"

// runProjectRepository says where a project's work lands, and records it when told.
//
// The address of a project and the address of a repository are both two words with a separator
// between them, so they are told apart by position, which is how krewe project create already reads:
// the last one is the thing being named and anything in front of it says where.
//
//	krewe project repository                                              what this project works in
//	krewe project repository show me/transcript                           what that project works in
//	krewe project repository atlantic-blue/transcript                     record it here
//	krewe project repository me/transcript atlantic-blue/transcript       record it there
//	krewe project repository atlantic-blue/transcript private             and say what kind
//
// One argument stays a write. Every message in this tool teaches that form as the write, so a new
// meaning for it would break the one spelling an operator learns first. Where that one argument also
// names a project the system holds, the command has two readings and takes neither: it refuses and
// prints the unambiguous spelling of each. Two arguments are unambiguous by position, so an address
// that also names a project is recorded as a repository there, and that form is the way the refusal
// hands back.
func runProjectRepository(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), readVerb) {
		return runProjectRepositoryShow(ctx, client, args[1:], out)
	}

	said := args
	kind := ""
	// The kind comes off the end first. It is one of two words and neither is an address, so nothing
	// else in the command can be mistaken for it, and taking it off leaves the addresses to count.
	if len(said) > 0 {
		if word := strings.ToLower(said[len(said)-1]); word == repository.Public || word == repository.Private {
			said, kind = said[:len(said)-1], word
		}
	}
	if len(said) > 2 || (len(said) == 0 && kind != "") {
		return fmt.Errorf("usage: %s", writeUsage)
	}
	// A last word that is neither an address nor a kind the system knows. A forge has other kinds, and
	// "internal" read as the repository leaves the system resolving atlantic-blue as a workspace and
	// answering that it has no such thing, which sends the operator to fix the wrong half.
	if len(said) == 2 && !strings.Contains(said[1], workspace.Separator) {
		return fmt.Errorf("a repository is an owner and a name, and a kind is %s or %s, and %q is neither"+
			"\n\nusage: %s", repository.Public, repository.Private, said[1], writeUsage)
	}

	typed := ""
	if len(said) == 2 {
		typed = said[0]
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if !located.HasProject() {
		return aRepositoryBelongsToAProject(located, "krewe project repository <workspace>/<project> <owner>/<name>")
	}

	if len(said) == 0 {
		return readRepository(ctx, client, located.ProjectID, out)
	}

	// The one argument the operator typed reads two ways whenever the system holds a project by that
	// address, because a project address and a repository address have the same shape. Refused rather
	// than guessed at: the guess wrote a project address into the repository of whichever project the
	// operator happened to be standing in, and printed the sentence a read prints.
	if len(said) == 1 {
		if names, ok := alsoNamesAProject(ctx, client, said[0]); ok {
			return twoReadings(said[0], kind, names, located)
		}
	}

	// What the project held, read before the write, because the line a write prints says what it
	// changed and what it changed from.
	held, err := client.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: located.ProjectID})
	if err != nil {
		return err
	}
	resp, err := client.SetProjectRepository(ctx, &quaycrewv1.SetProjectRepositoryRequest{
		Project: located.ProjectID, Repository: said[len(said)-1], Visibility: kind,
	})
	if err != nil {
		return err
	}
	writeRecorded(out, held.GetProject(), resp.GetProject())
	return nil
}

// writeUsage is how the write is typed, named once so every refusal off this command offers the same
// spelling.
const writeUsage = "krewe project repository [<address>] <owner>/<name> [public|private]"

// runProjectRepositoryShow reads where a project's work lands and writes nothing.
//
// It takes an address so another project can be read from where you are standing. Without it the
// operator's only way to ask about another project was the form that records one, and the answer to
// both is the same sentence.
func runProjectRepositoryShow(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("a read takes one address and records nothing"+
			"\n\nusage: krewe project repository show [<address>]"+
			"\nto record one: %s", writeUsage)
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if !located.HasProject() {
		return aRepositoryBelongsToAProject(located, "krewe project repository show <workspace>/<project>")
	}
	return readRepository(ctx, client, located.ProjectID, out)
}

// readRepository says what one project works in, as the system holds it.
func readRepository(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, projectID string, out io.Writer) error {
	read, err := client.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: projectID})
	if err != nil {
		return err
	}
	writeRepository(out, "", read.GetProject())
	return nil
}

// aRepositoryBelongsToAProject says which of the two levels the operator is holding, and how the
// command is typed at the level that has a repository.
func aRepositoryBelongsToAProject(located workspace.Location, spelling string) error {
	return fmt.Errorf("%s is a workspace, and a repository belongs to a project: %s", located.Path, spelling)
}

// alsoNamesAProject asks the system whether an address is a project it holds.
//
// The two addresses cannot be told apart by shape, so the system is what tells them apart. Anything
// that does not resolve is not a project, which is the ordinary case: a repository on a forge has
// nothing to do with the names in here.
func alsoNamesAProject(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string) (workspace.Location, bool) {
	path, err := workspace.ParsePath(typed)
	if err != nil || path.Project == "" || path.Session != "" {
		return workspace.Location{}, false
	}
	located, err := workspace.ResolvePath(ctx, client, path)
	if err != nil {
		return workspace.Location{}, false
	}
	return located, located.HasProject()
}

// twoReadings refuses the command that reads two ways, and gives the spelling of each.
//
// It names the project in scope as well as the argument. The damage landed on the project the
// operator was standing in, which is the one the command never mentions, so a refusal that named
// only the argument would leave out the project that was about to be overwritten.
func twoReadings(typed, kind string, names, scope workspace.Location) error {
	write := fmt.Sprintf("krewe project repository %s %s", scope.Path, typed)
	if kind != "" {
		write += " " + kind
	}
	return fmt.Errorf("%q is a repository address, and it is also the project %s, which this system holds. "+
		"The command reads two ways, so it does neither."+
		"\n\nto read what %s works in:\n    krewe project repository show %s"+
		"\nto record %s as the repository of %s, where you are standing:\n    %s",
		typed, names.Path, names.Path, typed, typed, scope.Path, write)
}

// writeRecorded says what a write changed, and what it changed from.
//
// A read and a write printed one sentence between them, so a command that overwrote a setting read
// as a command that had confirmed it. The line a write prints now carries the word recorded and the
// state before it, which is the half a read has nothing to say about.
func writeRecorded(out io.Writer, held, now *quaycrewv1.Project) {
	fmt.Fprintf(out, "recorded: this project now works in %s, %s\n",
		now.GetRepository(), repository.Costs(now.GetVisibility()))
	switch {
	case held.GetRepository() == "":
		fmt.Fprintln(out, "it had no repository before this")
	case held.GetRepository() == now.GetRepository() && held.GetVisibility() == now.GetVisibility():
		fmt.Fprintln(out, "it held the same repository before this, so nothing moved")
	default:
		fmt.Fprintf(out, "it worked in %s, %s, before this\n",
			held.GetRepository(), repository.Named(held.GetVisibility()))
	}
}

// writeRepository says where a project's work lands, in one line, wherever a project is printed.
//
// A project with none says so rather than saying nothing. The gap is the finding: the acceptance run
// had a workspace, a project, three roles and a token that worked, and the one fact nothing held was
// where the work goes, so it read as a system that was ready.
func writeRepository(out io.Writer, indent string, project *quaycrewv1.Project) {
	if project.GetRepository() == "" {
		fmt.Fprintf(out, "%sno repository, so a job declared here is not asked to push anywhere. "+
			"Say where with: krewe project repository <owner>/<name>\n", indent)
		return
	}
	fmt.Fprintf(out, "%sworks in %s, %s\n",
		indent, project.GetRepository(), repository.Costs(project.GetVisibility()))
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

// runContext says where the files the model reads live. It asks the control plane rather than working
// the paths out, because this tool runs on the operator's machine and the layout belongs to the system.
func runContext(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "edit" {
		return runContextEdit(ctx, client, args[1:], out)
	}
	if len(args) > 0 && args[0] == "show" {
		return runContextShow(ctx, client, args[1:], out)
	}
	if len(args) > 0 && args[0] == "set" {
		return runContextSet(ctx, client, args[1:], out)
	}
	if len(args) > 0 && args[0] == "clear" {
		return runContextClear(ctx, client, args[1:], out)
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: krewe context [<address>] | krewe context show [<address>] " +
			"| krewe context set [<address>] < file | krewe context edit [<address>] " +
			"| krewe context clear [<address>]")
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}

	request := &quaycrewv1.ListContextsRequest{}
	// The system's level is in every listing, so asking for it by name is asking for the listing.
	if typed == systemScope {
		typed = ""
	}
	// An address typed in wins; otherwise the operator's own place narrows it. Standing nowhere shows
	// the whole system, because then the question was about the system.
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
		fmt.Fprintln(out, "no context directories: this system keeps a session's state in its container")
		return nil
	}
	fmt.Fprintf(out, "%-10s %-20s %-22s %s\n", "scope", "name", "characters", "what it says")
	var notes []string
	for _, dir := range resp.GetDirs() {
		state := "nothing written yet"
		if dir.GetWritten() {
			state = firstLine(dir.GetBody())
		}
		size := contextsize.Read(dir.GetScope(), dir.GetName(), dir.GetBody())
		fmt.Fprintf(out, "%-10s %-20s %-22s %s\n",
			dir.GetScope(), dir.GetName(), size.Cell(), state)
		if note := size.Note(); note != "" {
			notes = append(notes, note)
		}
	}
	// A row that says a level is over the mark does not say what that costs, and the number on its own
	// is what nobody acted on for a hundred thousand characters. One line per level that is over, and
	// nothing at all for a listing where every level is small.
	for _, note := range notes {
		fmt.Fprintf(out, "\n%s\n", note)
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

// runContextShow prints what one level says, and nothing else, so that
//
//	krewe context show system > file
//	krewe context set system < file
//
// are a pair. Until this existed a level could only be overwritten: adding a paragraph meant already
// holding the whole text, and the only way to recover what the system held was to read the contexts
// table in the database. It also means a level can be diffed, piped, and kept in a repository and
// compared against what the system actually holds.
//
// The body goes out byte for byte, with nothing added, because the round trip has to be a no op.
// A heading, a trailing newline or a count would each become part of the level the moment somebody
// piped this into `krewe context set`.
func runContextShow(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: krewe context show [<address>|system]")
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	scope, owner, name, err := contextTarget(ctx, client, typed)
	if err != nil {
		return err
	}
	body, err := contextBody(ctx, client, scope, owner)
	if err != nil {
		return err
	}
	// A level that says nothing is refused rather than printed as silence. Standard output stays
	// empty either way, so a redirection writes an empty file whatever happens, and the exit status
	// is the only thing that tells a caller which of the two it got.
	if body == "" {
		return fmt.Errorf("%s %s says nothing yet, so there is nothing to read back"+
			"\n\nwrite it: cat notes.md | krewe context set %s", scope, name, typed)
	}
	fmt.Fprint(out, body)
	return nil
}

// runContextSet writes a level's context from standard input, which is how a file becomes context:
//
//	krewe context set system < ~/notes/how-we-job.md
//
// Reading a file rather than taking it as an argument is deliberate: context is prose, often long and
// full of everything a shell would like to interpret.
func runContextSet(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: krewe context set [<address>|system] < file")
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	scope, owner, name, err := contextTarget(ctx, client, typed)
	if err != nil {
		return err
	}
	level := contextsize.Read(scope, name, "")

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
			fmt.Fprintf(out, "%s was already empty, and nothing was on standard input\n", level.Label())
			return nil
		}
		return fmt.Errorf("nothing was on standard input, so %s is untouched and still says %s"+
			"\n\npipe something in: cat notes.md | krewe context set %s"+
			"\nor empty it deliberately: krewe context clear %s",
			level.Label(), contextsize.Characters(held), typed, typed)
	}
	if _, err := client.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: scope, Owner: owner, Body: string(body),
	}); err != nil {
		return err
	}
	written := contextsize.Read(scope, name, string(body))
	fmt.Fprintf(out, "%s now says %s\n", written.Label(), contextsize.Characters(written.Characters))
	// The size on its own is a number nobody acts on: the system level reached 100,179 characters while
	// every write reported its own length. So a level over the mark also says who reads it and what to
	// move down a level, at the moment somebody makes it that big.
	if said := written.Say(); said != "" {
		fmt.Fprintf(out, "\n%s\n", said)
	}
	return nil
}

// runContextClear empties a level, which context set used to do by accident whenever standing input
// was empty. Saying it out loud is the whole point of it being its own command.
func runContextClear(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: krewe context clear [<address>|system]")
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	scope, owner, name, err := contextTarget(ctx, client, typed)
	if err != nil {
		return err
	}
	level := contextsize.Read(scope, name, "")
	held, err := contextLength(ctx, client, scope, owner)
	if err != nil {
		return err
	}
	if held == 0 {
		fmt.Fprintf(out, "%s was already empty\n", level.Label())
		return nil
	}
	if _, err := client.SetContext(ctx, &quaycrewv1.SetContextRequest{
		Scope: scope, Owner: owner, Body: "",
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s emptied, and it said %s\n", level.Label(), contextsize.Characters(held))
	return nil
}

// contextBody is what a level says today, read back out of the system.
//
// The listing carries every level's body, so there is nothing else to ask, and one call answers all
// three questions anything here has: what a level says, how long it is, and whether it says anything
// at all. Going through the listing rather than a call of its own is also what keeps the console and
// the command line reading the same answer.
func contextBody(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, scope, owner string) (string, error) {
	resp, err := client.ListContexts(ctx, &quaycrewv1.ListContextsRequest{})
	if err != nil {
		return "", err
	}
	return pickContext(resp.GetDirs(), scope, owner), nil
}

// pickContext is one level out of a listing of every level. A level the listing does not carry says
// nothing, which is the same answer as a level that is there and empty: neither has anything to read
// back and neither has anything to protect.
func pickContext(dirs []*quaycrewv1.ContextDir, scope, owner string) string {
	for _, dir := range dirs {
		if dir.GetScope() == scope && dir.GetOwner() == owner {
			return dir.GetBody()
		}
	}
	return ""
}

// contextLength is how much a level says today, so a refusal can name what it is protecting and
// clearing can say what it removed.
func contextLength(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, scope, owner string) (int, error) {
	body, err := contextBody(ctx, client, scope, owner)
	return contextsize.Read(scope, "", body).Characters, err
}

// contextTarget works out which level an address means. The word "system" is the level above every
// workspace, a workspace address is that workspace, and anything deeper is its project.
func contextTarget(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string) (scope, owner, name string, err error) {
	if typed == "system" {
		return "system", "", "system", nil
	}
	path, err := addressFrom(typed)
	if err != nil {
		return "", "", "", err
	}
	if path.IsZero() {
		return "", "", "", fmt.Errorf("say which context: system, a workspace, or a workspace/project")
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
// the workspace's, because that is the one somebody means when they say "the context for this job";
// the workspace's is reached by naming it.
func runContextEdit(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: krewe context edit [<address>]")
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
		return fmt.Errorf("no project to edit context for: say which with krewe use, or name one here")
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

	// The editor wrote a file; this tells the system. Context lives in the store, and a file nobody read
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
		return fmt.Errorf("usage: krewe sessions [<address>]")
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}

	// Presence, because this listing is what an operator reads before they stop, restart or drain
	// something. It costs a question to each idle session's sandbox and it is what tells a
	// conversation running with nobody watching it from an empty container.
	request := &quaycrewv1.ListSessionsRequest{Presence: true}
	// An address typed in wins; otherwise the operator's own place narrows the listing. Standing
	// nowhere lists everything, because then the question was about the system rather than a place,
	// and so does the word system, which is how somebody standing somewhere widens it again.
	path, err := addressFrom(typed)
	switch {
	case readsTheSystem(typed):
		path = workspace.Path{}
	case err != nil && typed != "":
		return err
	case err == nil && !path.IsZero():
		located, err := workspace.ResolvePath(ctx, client, path)
		if err != nil {
			return standing(typed, path, err)
		}
		request.Workspace, request.Project = located.WorkspaceID, located.ProjectID
	}

	resp, err := client.ListSessions(ctx, request)
	if err != nil {
		return err
	}
	// Said out loud, because a listing narrowed to where you are standing looks exactly like a system
	// with fewer sessions in it, and the operator has no way to tell the two apart.
	where := systemWide("sessions")
	if !path.IsZero() {
		where = narrowedTo("sessions", path.String(), "krewe sessions system lists every session")
	}
	if len(resp.GetSessions()) == 0 {
		where.nothing(out)
		return nil
	}
	// Names, not identifiers: a listing of hex says nothing about what any of it is.
	workspaces, projects := workspaceNames(ctx, client), projectNames(ctx, client)
	rows := make([][]string, 0, len(resp.GetSessions()))
	for _, session := range resp.GetSessions() {
		rows = append(rows, display.SessionCells(session,
			workspaces[session.GetWorkspace()], projects[session.GetProject()]))
	}
	fmt.Fprint(out, display.Rows(display.SessionColumns(), rows))
	where.counted(out, len(rows))
	return nil
}
