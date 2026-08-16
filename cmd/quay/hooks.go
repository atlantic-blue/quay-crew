package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/hook"
)

// runHook drives the crew's hooks from the command line: what it enforces, what a workspace runs
// under, and giving or taking one away.
//
// Importing reads the directory here rather than sending a path, for the reason skill import does:
// the control plane may be in a container that cannot see the operator's machine. The client is a
// pipe, and every rule about what a hook is lives on the other side.
func runHook(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: quay hook <import|list|attach|detach>")
	}
	switch args[0] {
	case "import":
		return runHookImport(ctx, client, args[1:], out)
	case "list":
		return runHookList(ctx, client, args[1:], out)
	case "attach":
		return runHookAttach(ctx, client, args[1:], out)
	case "detach":
		return runHookDetach(ctx, client, args[1:], out)
	default:
		return fmt.Errorf("usage: quay hook <import|list|attach|detach>")
	}
}

func runHookImport(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: quay hook import <directory>")
	}
	// Read and validate here as well as on the other side, so a malformed hook is refused before it
	// is sent anywhere. The control plane refuses it too, and that is the check that counts.
	loaded, err := hook.One(args[0])
	if err != nil {
		return err
	}

	files := make([]*quaycrewv1.HookFile, 0, len(loaded.Files))
	for _, file := range loaded.Files {
		files = append(files, &quaycrewv1.HookFile{
			Path:       file.Path,
			Body:       file.Body,
			Executable: file.Executable,
		})
	}
	resp, err := client.ImportHook(ctx, &quaycrewv1.ImportHookRequest{Files: files})
	if err != nil {
		return err
	}
	imported := resp.GetHook()
	fmt.Fprintf(out, "imported %s version %d: %s\n",
		imported.GetName(), imported.GetVersion(), imported.GetSummary())
	fmt.Fprintf(out, "it fires on %s\n", firesOn(imported))
	fmt.Fprintf(out, "attach it with: quay hook attach crew %s\n", imported.GetName())
	return nil
}

func runHookList(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: quay hook list [<workspace, or workspace/project/session>]")
	}

	req := &quaycrewv1.ListHooksRequest{}
	if typed != "" {
		located, err := locate(ctx, client, typed)
		if err != nil {
			return err
		}
		if located.SessionID != "" {
			req.Session = located.SessionID
		} else {
			req.Workspace = located.WorkspaceID
		}
	}
	resp, err := client.ListHooks(ctx, req)
	if err != nil {
		return err
	}
	if len(resp.GetHooks()) == 0 {
		fmt.Fprintln(out, "this crew enforces nothing: every rule it carries is advice the model may take or leave")
		fmt.Fprintln(out, "import one with: quay hook import <directory>")
		return nil
	}
	for _, one := range resp.GetHooks() {
		where := ""
		if one.GetCrew() {
			where = "  (the crew's)"
		}
		fmt.Fprintf(out, "%-20s v%-3d %s%s\n", one.GetName(), one.GetVersion(), one.GetSummary(), where)
		fmt.Fprintf(out, "%-20s      fires on %s\n", "", firesOn(one))
		if len(one.GetBinaries()) > 0 {
			fmt.Fprintf(out, "%-20s      needs: %s\n", "", strings.Join(one.GetBinaries(), " "))
		}
		for _, secret := range one.GetSecrets() {
			fmt.Fprintf(out, "%-20s      %s: %s\n", "", secret.GetName(), secret.GetPurpose())
		}
		if one.GetLeftOut() != "" {
			fmt.Fprintf(out, "%-20s      not given: %s\n", "", one.GetLeftOut())
		}
	}
	return nil
}

func runHookAttach(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	name, typed, err := hookAndAddress(args, "attach")
	if err != nil {
		return err
	}
	// "crew" where a workspace goes, the same word quay context set and quay skill attach take. It is
	// the level most hooks want: a constraint the crew agreed on is not usually a per workspace
	// opinion.
	if typed == crewScope {
		resp, err := client.AttachHook(ctx, &quaycrewv1.AttachHookRequest{
			Scope: crewScope, Name: name,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "the crew runs under %s version %d, so every workspace does\n",
			resp.GetHook().GetName(), resp.GetHook().GetVersion())
		fmt.Fprintln(out, staleWarning)
		return nil
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	resp, err := client.AttachHook(ctx, &quaycrewv1.AttachHookRequest{
		Workspace: located.WorkspaceID, Name: name,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s runs under %s version %d\n",
		located.Path.Workspace, resp.GetHook().GetName(), resp.GetHook().GetVersion())
	fmt.Fprintln(out, staleWarning)
	return nil
}

func runHookDetach(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	name, typed, err := hookAndAddress(args, "detach")
	if err != nil {
		return err
	}
	if typed == crewScope {
		if _, err := client.DetachHook(ctx, &quaycrewv1.DetachHookRequest{
			Scope: crewScope, Name: name,
		}); err != nil {
			return err
		}
		fmt.Fprintf(out, "the crew no longer runs under %s\n", name)
		fmt.Fprintln(out, "a workspace that attached it for itself keeps it")
		return nil
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if _, err := client.DetachHook(ctx, &quaycrewv1.DetachHookRequest{
		Workspace: located.WorkspaceID, Name: name,
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s no longer runs under %s\n", located.Path.Workspace, name)
	fmt.Fprintln(out, staleWarning)
	return nil
}

// staleWarning is said on every attach and detach, and it is not a nicety.
//
// A hook reaches a container at creation and never after, so a session that is already running is not
// under the constraint that was just attached. Somebody who believes a gate is on when it is not is
// worse off than somebody who knows there is no gate.
const staleWarning = "sessions already running are not under it: a hook reaches a sandbox when the sandbox is built, so stop and restart a session to put it under this one"

// firesOn says what a hook fires on, in a line, which is the whole of what a listing is asked.
func firesOn(one *quaycrewv1.Hook) string {
	events := make([]string, 0, len(one.GetEvents()))
	for _, binding := range one.GetEvents() {
		if binding.GetMatcher() != "" {
			events = append(events, binding.GetOn()+" ("+binding.GetMatcher()+")")
			continue
		}
		events = append(events, binding.GetOn())
	}
	if len(events) == 0 {
		return "nothing"
	}
	return strings.Join(events, ", ")
}

// hookAndAddress reads the two shapes these commands take: a hook name on its own, acting where the
// operator already is, or a workspace and a hook name.
func hookAndAddress(args []string, verb string) (name, typed string, err error) {
	switch len(args) {
	case 1:
		return args[0], "", nil
	case 2:
		return args[1], args[0], nil
	default:
		return "", "", fmt.Errorf("usage: quay hook %s [<workspace>] <name>", verb)
	}
}
