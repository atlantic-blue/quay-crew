package main

import (
	"context"
	"fmt"
	"io"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
)

// runRepository drives the repositories a workspace works in.
//
// On the workspace rather than the project, because that is already where a credential lives and where a
// skill attaches, and those are the two things a repository needs. Every session in the workspace gets a
// checkout of each, in a directory of its own under its working directory.
func runRepository(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return runRepositoryList(ctx, client, nil, out)
	}
	switch args[0] {
	case "add":
		return runRepositoryAdd(ctx, client, args[1:], out)
	case "list":
		return runRepositoryList(ctx, client, args[1:], out)
	case "remove":
		return runRepositoryRemove(ctx, client, args[1:], out)
	default:
		return fmt.Errorf("usage: quay repository <add|list|remove>")
	}
}

func runRepositoryAdd(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	typed, remote, err := addressAndValue(args, "add", "<url>")
	if err != nil {
		return err
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	resp, err := client.AddRepository(ctx, &quaycrewv1.AddRepositoryRequest{
		Workspace: located.WorkspaceID, Remote: remote,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s works in %s\n", located.Path.Workspace, resp.GetRepository().GetRemote())
	fmt.Fprintf(out, "its sessions clone it into %s on their next turn\n", resp.GetRepository().GetName())
	return nil
}

func runRepositoryList(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: quay repository list [<workspace>]")
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	resp, err := client.ListRepositories(ctx, &quaycrewv1.ListRepositoriesRequest{
		Workspace: located.WorkspaceID,
	})
	if err != nil {
		return err
	}
	if len(resp.GetRepositories()) == 0 {
		fmt.Fprintf(out, "%s works in no repositories, so its sessions start with an empty directory\n",
			located.Path.Workspace)
		return nil
	}
	for _, one := range resp.GetRepositories() {
		fmt.Fprintf(out, "%-24s %s\n", display.Name(one.GetName(), one.GetName()), one.GetRemote())
	}
	return nil
}

func runRepositoryRemove(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	typed, name, err := addressAndValue(args, "remove", "<name>")
	if err != nil {
		return err
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if _, err := client.RemoveRepository(ctx, &quaycrewv1.RemoveRepositoryRequest{
		Workspace: located.WorkspaceID, Name: name,
	}); err != nil {
		return err
	}
	// The checkouts stay: this says where new work comes from, not that a conversation should lose what
	// it is in the middle of.
	fmt.Fprintf(out, "%s no longer works in %s; the checkouts already made are untouched\n",
		located.Path.Workspace, name)
	return nil
}

// addressAndValue reads the two shapes these commands take: a value on its own, acting where the operator
// already is, or a workspace and a value.
func addressAndValue(args []string, verb, what string) (typed, value string, err error) {
	switch len(args) {
	case 1:
		return "", args[0], nil
	case 2:
		return args[0], args[1], nil
	default:
		return "", "", fmt.Errorf("usage: quay repository %s [<workspace>] %s", verb, what)
	}
}
