package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/workspace"
)

// runWorkspaceDelete removes a workspace, everything under it, and everything those held.
//
// Until this existed a crew only ever grew: a workspace made by a typo was there for good, and
// starting again meant going around the tool into Docker and the data directory.
func runWorkspaceDelete(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: quay workspace delete <workspace>")
	}
	id, err := workspace.Resolve(ctx, client, args[0])
	if err != nil {
		return err
	}
	name, holds, err := whatAWorkspaceHolds(ctx, client, id)
	if err != nil {
		return err
	}
	if err := confirmedBy(name, holds, out); err != nil {
		return err
	}
	if _, err := client.DeleteWorkspace(ctx, &quaycrewv1.DeleteWorkspaceRequest{Id: id}); err != nil {
		return err
	}
	fmt.Fprintf(out, "deleted workspace %s, and %s\n", name, holds)
	// Standing in something that no longer exists is what every command then refuses about, so the
	// tool steps back out of it rather than leaving the operator nowhere without saying so.
	if here, err := currentPath(); err == nil && here.Workspace == name {
		if err := moveTo(workspace.Path{}); err != nil {
			return err
		}
		fmt.Fprintln(out, "you were standing there, so you are now nowhere: quay use <workspace>/<project>")
	}
	return nil
}

// runProjectDelete removes a project and the sessions inside it.
func runProjectDelete(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: quay project delete [<workspace>/]<project>")
	}
	located, err := locate(ctx, client, args[0])
	if err != nil {
		return err
	}
	if located.ProjectID == "" {
		return fmt.Errorf("%q names a workspace, not a project: delete a workspace with quay workspace delete", args[0])
	}
	name := located.Path.Project
	holds, err := howManySessions(ctx, client, located.ProjectID)
	if err != nil {
		return err
	}
	if err := confirmedBy(name, holds, out); err != nil {
		return err
	}
	if _, err := client.DeleteProject(ctx, &quaycrewv1.DeleteProjectRequest{Id: located.ProjectID}); err != nil {
		return err
	}
	fmt.Fprintf(out, "deleted project %s, and %s\n", name, holds)
	if here, err := currentPath(); err == nil && here.Project == name {
		if err := moveTo(workspace.Path{Workspace: here.Workspace}); err != nil {
			return err
		}
		fmt.Fprintf(out, "you were standing there, so you are now in %s\n", here.Workspace)
	}
	return nil
}

// confirmedBy makes the operator type the name back before anything is removed.
//
// A name typed twice is the only guard this tool can offer: it takes no flags, so there is no
// --yes to require, and what is being destroyed here is conversations, which nothing brings back.
// Piped in, it is one line, which is what makes the removal scriptable without being silent.
func confirmedBy(name, holds string, out io.Writer) error {
	fmt.Fprintf(out, "this removes %s, and %s. nothing brings it back.\n", name, holds)
	fmt.Fprintf(out, "type its name to confirm: ")

	typed, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(typed) == "" {
		return fmt.Errorf("nothing was typed, so %s is untouched", name)
	}
	if strings.TrimSpace(typed) != name {
		return fmt.Errorf("that is not %q, so nothing was removed", name)
	}
	return nil
}

// whatAWorkspaceHolds is the sentence a confirmation needs: what goes with it.
func whatAWorkspaceHolds(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, id string) (string, string, error) {
	found, err := client.GetWorkspace(ctx, &quaycrewv1.GetWorkspaceRequest{Id: id})
	if err != nil {
		return "", "", err
	}
	projects, err := client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{Workspace: id})
	if err != nil {
		return "", "", err
	}
	sessions, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Workspace: id})
	if err != nil {
		return "", "", err
	}
	secrets, err := client.ListSecrets(ctx, &quaycrewv1.ListSecretsRequest{Workspace: id})
	if err != nil {
		return "", "", err
	}
	// Only what this workspace set itself. The crew's own reach it and survive it, so counting them
	// here would say a removal takes credentials with it that every other workspace still has.
	own := 0
	for _, secret := range secrets.GetSecrets() {
		if !secret.GetCrew() {
			own++
		}
	}
	return found.GetWorkspace().GetName(), fmt.Sprintf("%s, %s and %s",
		counted(len(projects.GetProjects()), "project"),
		counted(len(sessions.GetSessions()), "session"),
		counted(own, "secret")), nil
}

func howManySessions(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, projectID string) (string, error) {
	sessions, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{Project: projectID})
	if err != nil {
		return "", err
	}
	return counted(len(sessions.GetSessions()), "session"), nil
}

// counted says how many of something there are, in words a sentence can hold.
func counted(many int, thing string) string {
	if many == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", many, thing)
}
