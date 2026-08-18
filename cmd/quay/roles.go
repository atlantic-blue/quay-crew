package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/role"
)

// runRole drives the crew's roles from the command line: what it holds, what a workspace holds, and
// giving or taking one away.
//
// Importing reads the directory here rather than sending a path, for the reason a skill import does:
// the control plane may be in a container that cannot see the operator's machine. The client is a
// pipe, and every rule about what a role is lives on the other side.
func runRole(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: quay role <import|list|attach|detach>")
	}
	switch args[0] {
	case "import":
		return runRoleImport(ctx, client, args[1:], out)
	case "list":
		return runRoleList(ctx, client, args[1:], out)
	case "attach":
		return runRoleAttach(ctx, client, args[1:], out)
	case "detach":
		return runRoleDetach(ctx, client, args[1:], out)
	default:
		return fmt.Errorf("usage: quay role <import|list|attach|detach>")
	}
}

func runRoleImport(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: quay role import <directory>")
	}
	// Read and validate here as well as on the other side, so a malformed role is refused before it
	// is sent anywhere. The control plane refuses it too, and that is the check that counts.
	if _, err := role.One(args[0]); err != nil {
		return err
	}
	read, err := role.ReadDir(args[0])
	if err != nil {
		return err
	}

	files := make([]*quaycrewv1.RoleFile, 0, len(read))
	for _, file := range read {
		files = append(files, &quaycrewv1.RoleFile{Path: file.Path, Body: file.Body})
	}
	resp, err := client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{Files: files})
	if err != nil {
		return err
	}
	imported := resp.GetRole()
	fmt.Fprintf(out, "imported %s version %d: %s\n",
		imported.GetName(), imported.GetVersion(), imported.GetSummary())
	fmt.Fprintf(out, "it runs on %s and receives %s\n",
		imported.GetModel(), strings.Join(imported.GetReceives(), ", "))
	fmt.Fprintf(out, "attach it with: quay role attach %s\n", imported.GetName())
	return nil
}

func runRoleList(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: quay role list [<workspace>]")
	}

	// With no address, this is what the crew holds. With a workspace, what that workspace holds.
	request := &quaycrewv1.ListRolesRequest{}
	where := "the crew"
	if typed != "" {
		located, err := locate(ctx, client, typed)
		if err != nil {
			return err
		}
		request.Workspace = located.WorkspaceID
		where = located.Path.Workspace
	}

	resp, err := client.ListRoles(ctx, request)
	if err != nil {
		return err
	}
	if len(resp.GetRoles()) == 0 {
		fmt.Fprintf(out, "%s holds no roles\n", where)
		return nil
	}
	for _, held := range resp.GetRoles() {
		fmt.Fprintf(out, "%-16s v%-3d %s\n", held.GetName(), held.GetVersion(), held.GetSummary())
		fmt.Fprintf(out, "%-16s      runs on %s\n", "", held.GetModel())
		// What it receives is the boundary, which is the part worth reading twice, so it is a line
		// of its own rather than a word at the end of another one.
		fmt.Fprintf(out, "%-16s      receives %s\n", "", strings.Join(held.GetReceives(), ", "))
		if held.GetCrew() {
			fmt.Fprintf(out, "%-16s      held by the crew, so every workspace has it\n", "")
		}
	}
	return nil
}

func runRoleAttach(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	name, typed, err := roleAndAddress(args, "attach")
	if err != nil {
		return err
	}
	// "crew" where a workspace goes, the same word quay skill attach and quay context set take, and
	// it means the same thing: everything this crew does, the workspaces made after today included.
	if typed == crewScope {
		resp, err := client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{Scope: crewScope, Name: name})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "the crew holds the %s role, version %d, so every workspace has it\n",
			resp.GetRole().GetName(), resp.GetRole().GetVersion())
		return nil
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	resp, err := client.AttachRole(ctx, &quaycrewv1.AttachRoleRequest{
		Workspace: located.WorkspaceID, Name: name,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s holds the %s role, version %d\n",
		located.Path.Workspace, resp.GetRole().GetName(), resp.GetRole().GetVersion())
	return nil
}

func runRoleDetach(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	name, typed, err := roleAndAddress(args, "detach")
	if err != nil {
		return err
	}
	if typed == crewScope {
		if _, err := client.DetachRole(ctx, &quaycrewv1.DetachRoleRequest{
			Scope: crewScope, Name: name,
		}); err != nil {
			return err
		}
		fmt.Fprintf(out, "the crew no longer holds the %s role\n", name)
		fmt.Fprintln(out, "a workspace that attached it for itself keeps it")
		return nil
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if _, err := client.DetachRole(ctx, &quaycrewv1.DetachRoleRequest{
		Workspace: located.WorkspaceID, Name: name,
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s no longer holds the %s role\n", located.Path.Workspace, name)
	return nil
}

// roleAndAddress reads the two shapes these commands take: a role name on its own, acting where the
// operator already is, or a workspace and a role name.
func roleAndAddress(args []string, verb string) (name, typed string, err error) {
	switch len(args) {
	case 1:
		return args[0], "", nil
	case 2:
		return args[1], args[0], nil
	default:
		return "", "", fmt.Errorf("usage: quay role %s [<workspace>] <name>", verb)
	}
}
