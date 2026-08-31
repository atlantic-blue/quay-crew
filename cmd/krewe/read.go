package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/display"
)

// The command that reads work out of a session.
//
// Before this the only road into a session that had finished was to attach to it, which is a person
// driving a terminal: it does not compose into a script, a flow or a report, and it needs a container
// that a settled session may no longer have. This reads the directory the system keeps, so it answers
// for a session whose sandbox has gone.

// runRead prints what a session made: a listing of the directory, or the bytes of one file in it.
func runRead(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 || len(args) > 2 {
		return fmt.Errorf("usage: krewe read <session> [<path>]" +
			"\n\nwith no path it lists what the session made, and with one it prints that file")
	}
	sessionID, err := resolveSession(ctx, client, args[0])
	if err != nil {
		return err
	}
	path := ""
	if len(args) == 2 {
		path = args[1]
	}
	resp, err := client.ReadSessionWork(ctx, &quaycrewv1.ReadSessionWorkRequest{
		Session: sessionID, Path: path,
	})
	if err != nil {
		return err
	}
	if !resp.GetDirectory() {
		// The bytes as they are, so this composes: a caller can redirect it into a file and get the
		// file back, which is the whole reason for a command rather than an instruction to attach.
		_, err := out.Write(resp.GetContent())
		return err
	}
	// The path first, on its own line, because it is the thing an operator acts on: it names the
	// directory on the machine running the sandboxes, which is where the work is.
	fmt.Fprintln(out, resp.GetHost())
	if len(resp.GetEntries()) == 0 {
		fmt.Fprintln(out, "nothing in it")
		return nil
	}
	rows := make([][]string, 0, len(resp.GetEntries()))
	for _, entry := range resp.GetEntries() {
		rows = append(rows, []string{readName(entry), readSize(entry)})
	}
	fmt.Fprint(out, display.Rows([]string{"NAME", "SIZE"}, rows))
	return nil
}

// workName marks a directory with a trailing slash, the way every listing of files does, so a name
// that could be either is not ambiguous.
func readName(entry *quaycrewv1.SessionWorkEntry) string {
	if entry.GetDirectory() {
		return entry.GetName() + "/"
	}
	return entry.GetName()
}

// workSize is how big a file is, and empty for a directory. A directory's size on disk is the size of
// its own record rather than of what is in it, so printing one answers a question nobody asked.
func readSize(entry *quaycrewv1.SessionWorkEntry) string {
	if entry.GetDirectory() {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%d", entry.GetSize()))
}
