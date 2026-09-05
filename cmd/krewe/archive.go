package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/workspace"
)

// archiveUsage is what both forms of the word are, in the order somebody reads them.
const archiveUsage = "usage: krewe archive [<address>] [<session>]\n\n" +
	"a session is its id, its handle, or its address, and it is put away on its own.\n" +
	"an address naming a workspace and a project puts away every session in it that holds\n" +
	"no container, and says how many it left.\n\n" +
	"with nothing at all it reads where you are standing.\n\n" +
	"nothing is deleted: krewe sessions --archived lists what is put away, and\n" +
	"krewe unarchive <session> brings one back"

// runArchive puts a session away, or every finished session of one project.
//
// Two forms under one word, because a person archives one session and a project's sessions for the
// same reason: the finished ones bury the live ones. Measured on 5 September 2026 a system held 303
// sessions and 282 of them were stopped, so the three that were working sat at the top of a list
// nobody could read.
//
// The address decides which form runs, exactly as krewe exec and krewe sessions decide.
func runArchive(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("%s", archiveUsage)
	}
	typed := ""
	if len(args) == 1 {
		typed = strings.TrimSpace(args[0])
	}
	naming, err := whatToArchive(typed)
	if err != nil {
		return err
	}
	if naming.session != "" {
		return archiveOneSession(ctx, client, naming.session, out)
	}
	return archiveAProject(ctx, client, naming.project, out)
}

// archiving is which of the two forms an invocation asked for: one of the fields is set and the
// other is empty.
type archiving struct {
	session string
	project workspace.Path
}

// whatToArchive decides between the two forms. A word that names a session is one, an address that
// reaches a project is the other, and nothing at all is whichever of those the operator is standing
// in.
func whatToArchive(typed string) (archiving, error) {
	if typed != "" {
		// A bare word is a session when it is shaped like one of a session's two identifiers, which is
		// what a listing prints. Everything else is an address, and the level it stops at picks the
		// form: a session archives on its own, a project is swept.
		if !strings.Contains(typed, workspace.Separator) && display.LooksLikeIdentifier(typed) {
			return archiving{session: typed}, nil
		}
		path, err := workspace.ParsePath(typed)
		if err != nil {
			return archiving{}, err
		}
		if path.Session != "" {
			return archiving{session: typed}, nil
		}
		// Archiving a whole workspace is not a thing this word does. A workspace holds projects that
		// have nothing to do with each other, and a sweep across all of them is not one decision.
		if path.Project == "" {
			return archiving{}, fmt.Errorf("%q names a workspace, and archiving reads a project or a "+
				"session\n\n%s", typed, archiveUsage)
		}
		return archiving{project: path}, nil
	}

	where, err := currentPath()
	if err != nil {
		return archiving{}, err
	}
	switch {
	case where.Session != "":
		return archiving{session: where.String()}, nil
	case where.Project != "":
		return archiving{project: where}, nil
	default:
		return archiving{}, fmt.Errorf("you are not standing in a project or a session, so there is "+
			"nothing to archive\n\n%s", archiveUsage)
	}
}

// archiveOneSession puts one session away and says the record is kept, because a word that reads like
// a delete stops a person from using it.
func archiveOneSession(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string,
	out io.Writer) error {
	session, err := workspace.Session(ctx, client, typed)
	if err != nil {
		return err
	}
	if session.GetArchivedAt() != nil {
		return fmt.Errorf("session %s is already archived\n\nbring it back with krewe unarchive %s",
			display.ShortID(session.GetHandle()), display.ShortID(session.GetHandle()))
	}
	if _, err := client.ArchiveSession(ctx, &quaycrewv1.ArchiveSessionRequest{Id: session.GetId()}); err != nil {
		return err
	}
	where := display.ShortID(session.GetHandle())
	fmt.Fprintf(out, "archived %s: its conversation, its execs and its files are all still there\n", where)
	fmt.Fprintf(out, "read it with krewe read %s, or bring it back with krewe unarchive %s\n", where, where)
	return nil
}

// archiveAProject sweeps one project, and says what it left as well as what it took. A sweep that
// reports only what it took reads as a sweep that took everything.
func archiveAProject(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, path workspace.Path,
	out io.Writer) error {
	located, err := workspace.ResolvePath(ctx, client, path)
	if err != nil {
		return standing(path.String(), path, err)
	}
	resp, err := client.ArchiveProjectSessions(ctx, &quaycrewv1.ArchiveProjectSessionsRequest{
		Project: located.ProjectID,
	})
	if err != nil {
		return err
	}
	archived := resp.GetArchived()
	if len(archived) == 0 {
		fmt.Fprintf(out, "no session in %s holds no container, so nothing was archived\n", path.String())
		fmt.Fprintf(out, "%s left in the listing\n", plural(int(resp.GetSkipped()), "sessions"))
		return nil
	}
	fmt.Fprintf(out, "archived %s in %s: every one of them still holds its conversation and its files\n",
		plural(len(archived), "sessions"), path.String())
	fmt.Fprintf(out, "%s left in the listing, still holding a container\n", plural(int(resp.GetSkipped()), "sessions"))
	fmt.Fprintf(out, "krewe sessions %s --archived lists what was put away\n", path.String())
	return nil
}

// runUnarchive brings a session back into the default listing.
//
// It ships in the same slice as the word that hides one. A wrong address hides work a person then
// cannot find, so the way back is never a later change.
func runUnarchive(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: krewe unarchive <session>\n\n" +
			"a session is its id, its handle, or its address. With nothing at all it reads where you\n" +
			"are standing")
	}
	typed := ""
	if len(args) == 1 {
		typed = strings.TrimSpace(args[0])
	}
	if typed == "" {
		where, err := currentPath()
		if err != nil {
			return err
		}
		if where.Session == "" {
			return fmt.Errorf("you are not standing in a session: krewe unarchive <session>")
		}
		typed = where.String()
	}

	session, err := workspace.Session(ctx, client, typed)
	if err != nil {
		return err
	}
	if _, err := client.RestoreSession(ctx, &quaycrewv1.RestoreSessionRequest{Id: session.GetId()}); err != nil {
		return err
	}
	where := display.ShortID(session.GetHandle())
	fmt.Fprintf(out, "%s is back in the listing, reading stopped\n", where)
	// Said out loud, because an operator who expects the old container back finds a session that
	// answers nothing.
	fmt.Fprintf(out, "it holds no container: the next exec builds a fresh one over the same conversation\n")
	return nil
}
