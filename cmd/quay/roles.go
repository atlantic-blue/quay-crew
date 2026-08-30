package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/origin"
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
		return fmt.Errorf("usage: quay role <import|list|show|attach|detach>")
	}
	switch args[0] {
	case "import":
		return runRoleImport(ctx, client, args[1:], out)
	case "list":
		return runRoleList(ctx, client, args[1:], out)
	case "show":
		return runRoleShow(ctx, client, args[1:], out)
	case "attach":
		return runRoleAttach(ctx, client, args[1:], out)
	case "detach":
		return runRoleDetach(ctx, client, args[1:], out)
	default:
		return fmt.Errorf("usage: quay role <import|list|show|attach|detach>")
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
	// Where the files came from, read here because only here can: the repository is on this machine
	// and the control plane runs in a container that cannot see it.
	from := origin.Of(args[0])
	resp, err := client.ImportRole(ctx, &quaycrewv1.ImportRoleRequest{
		Files: files,
		Origin: &quaycrewv1.RoleOrigin{
			Repository: from.Repository,
			Commit:     from.Commit,
			Path:       from.Path,
			Dirty:      from.Dirty,
			Unpushed:   from.Unpushed,
		},
	})
	if err != nil {
		return err
	}
	imported := resp.GetRole()
	fmt.Fprintf(out, "imported %s version %d: %s\n",
		imported.GetName(), imported.GetVersion(), imported.GetSummary())
	fmt.Fprintf(out, "it runs on %s and receives %s\n",
		imported.GetModel(), strings.Join(imported.GetReceives(), ", "))
	writeOrigin(out, "", imported)
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
	read := heldBy("roles", where, "quay role list on its own reads what the crew holds")
	if len(resp.GetRoles()) == 0 {
		read.nothing(out)
		return nil
	}
	for _, held := range resp.GetRoles() {
		fmt.Fprintf(out, "%-16s v%-3d %s\n", held.GetName(), held.GetVersion(), held.GetSummary())
		fmt.Fprintf(out, "%-16s      runs on %s\n", "", held.GetModel())
		// What it receives is the boundary, which is the part worth reading twice, so it is a line
		// of its own rather than a word at the end of another one.
		fmt.Fprintf(out, "%-16s      receives %s\n", "", strings.Join(held.GetReceives(), ", "))
		writeOrigin(out, fmt.Sprintf("%-16s      ", ""), held)
		if held.GetCrew() {
			fmt.Fprintf(out, "%-16s      held by the crew, so every workspace has it\n", "")
		}
	}
	read.counted(out, len(resp.GetRoles()))
	return nil
}

// runRoleShow reads one role back whole, which is the only way to audit what a session running as it
// was told.
//
// The brief is the role. Everything above it in this output is a line a listing already prints, and
// the reason they are repeated here is that an operator reading a brief wants to know which brief:
// the version, the model and the boundary are what a role is, and reading a brief without them is
// reading a document with no idea which crew is running it.
func runRoleShow(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	name, typed, err := roleAndAddress(args, "show")
	if err != nil {
		return err
	}
	request := &quaycrewv1.GetRoleRequest{Name: name}
	if typed != "" {
		located, err := locate(ctx, client, typed)
		if err != nil {
			return err
		}
		request.Workspace = located.WorkspaceID
	}
	resp, err := client.GetRole(ctx, request)
	if err != nil {
		return err
	}
	writeRole(out, resp)
	return nil
}

// writeRole prints a role, the brief last and whole.
//
// Last because it is what the reader came for and it runs to pages, so anything printed after it is
// printed where nobody is looking. Whole because the point of the command is a byte for byte read:
// truncating a brief here would make this command agree with a role it does not hold.
func writeRole(out io.Writer, resp *quaycrewv1.GetRoleResponse) {
	shown := resp.GetRole()
	fmt.Fprintf(out, "%s v%d  %s\n", shown.GetName(), shown.GetVersion(), shown.GetSummary())
	fmt.Fprintf(out, "runs on %s\n", shown.GetModel())
	fmt.Fprintf(out, "receives %s\n", strings.Join(shown.GetReceives(), ", "))
	// The manifest's own word, under the manifest's other one, so an operator reading this back can
	// find the line it came from. Always, including when it is empty: a role that may call nothing is
	// the default rather than an oversight, and a line that appears only when something is granted
	// reads as a missing line.
	if verbs := resp.GetVerbs(); len(verbs) > 0 {
		fmt.Fprintf(out, "verbs %s\n", strings.Join(verbs, ", "))
	} else {
		fmt.Fprintln(out, "verbs none, so it may call nothing")
	}
	if shown.GetCrew() {
		fmt.Fprintln(out, "held by the crew, so every workspace has it")
	}
	if holders := resp.GetHeldBy(); len(holders) > 0 {
		fmt.Fprintf(out, "attached by %s\n", strings.Join(holders, ", "))
	}
	if !shown.GetCrew() && len(resp.GetHeldBy()) == 0 {
		fmt.Fprintln(out, "nothing holds it, so no session runs as it yet")
	}
	if stamp := shown.GetImportedAt(); stamp.IsValid() {
		fmt.Fprintf(out, "imported %s ago\n", display.Age(stamp))
	}
	writeOrigin(out, "", shown)
	fmt.Fprintf(out, "\n%s\n", resp.GetBrief())
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

// writeOrigin says where a role's files came from, and says it loudly when nobody but whoever
// imported it could read them.
//
// Always a line, never a line that appears only when something is wrong. A role in a repository and
// a role in a folder on one laptop printed identically for as long as roles existed, which is how
// the three roles that drove a three hour acceptance run were never reviewed by anybody: nothing in
// front of the operator ever said they were not in the code.
func writeOrigin(out io.Writer, indent string, shown *quaycrewv1.Role) {
	from := origin.Origin{
		Repository: shown.GetOrigin().GetRepository(),
		Commit:     shown.GetOrigin().GetCommit(),
		Path:       shown.GetOrigin().GetPath(),
		Dirty:      shown.GetOrigin().GetDirty(),
		Unpushed:   shown.GetOrigin().GetUnpushed(),
	}
	for _, said := range from.Says() {
		fmt.Fprintf(out, "%s%s\n", indent, said)
	}
}
