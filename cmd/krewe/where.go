package main

import (
	"context"
	"fmt"
	"io"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/workspace"
)

// Where an address is on the machine.
//
// The problem it answers: every level on disk is a generated identifier, none of the names is on the
// filesystem, and the way to learn which directory a sandbox binds where was to inspect a container
// that happened to be running. Somebody holding a screenshot had nothing to type, and with every
// sandbox down there was nothing on the machine to read either.
//
// It is not part of `krewe read`, which is the other command that names a directory. That one prints
// the bytes of a file, and this must never print the contents of anything.

// runWhere prints the directory an address is kept in, and where a session sees the same directory.
func runWhere(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: krewe where [<address>]" +
			"\n\nwith no address it says where you are standing")
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	// The word for the level above every workspace is the one address this refuses. The system's own
	// directory is where its credentials are kept, so naming it would put a road to them in front of
	// somebody who asked where to drop a screenshot.
	if typed == systemScope {
		return fmt.Errorf("the system's own directory holds this system's credentials, so nothing here names it" +
			"\n\nsay which workspace instead, for example krewe where <workspace>")
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}

	resp, err := client.LocateDirectory(ctx, &quaycrewv1.LocateDirectoryRequest{
		Workspace: located.WorkspaceID,
		Project:   located.ProjectID,
		Session:   located.SessionID,
	})
	if err != nil {
		return err
	}

	// The path first, on its own line and with nothing beside it, so it composes: `cd "$(krewe where
	// me)"` is the shape this is typed in, and anything sharing that line breaks it.
	fmt.Fprintln(out, resp.GetHost())
	fmt.Fprintln(out, whatItIs(resp.GetKind(), located.Path, resp.GetSandbox()))
	return nil
}

// whatItIs is the sentence under the path: which directory this is, and what a session inside the
// container calls it. The second half is the part that was actually hard, because a person who has
// copied a file in still has to tell the session where to look.
func whatItIs(kind quaycrewv1.DirectoryKind, path workspace.Path, mount string) string {
	if kind == quaycrewv1.DirectoryKind_DIRECTORY_KIND_SHARED {
		return fmt.Sprintf("the shared folder of %s. Every session in it reads this directory at %s",
			path.Workspace, mount)
	}
	return fmt.Sprintf("the working directory of session %s. That session reads this directory at %s",
		path.Session, mount)
}
