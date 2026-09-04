package main

import (
	"context"
	"fmt"
	"io"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// drainAnyway is the word that drains over an exec in flight. A word rather than a flag, because this
// tool has none.
const drainAnyway = "anyway"

// runDrain puts every live session down before something else takes their containers away.
//
// `make upgrade` removes sandboxes by name from the daemon, which ends an exec in flight as "exit
// status 137" and says nothing about whose exec it was. Draining first stops each session through the
// system, so the row says stopped and the sandbox is closed rather than ripped out.
func runDrain(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	force := false
	switch {
	case len(args) == 0:
	case len(args) == 1 && args[0] == drainAnyway:
		force = true
	default:
		return fmt.Errorf("usage: krewe drain [%s]\n\n%s drains over an exec that is still working, "+
			"and says what it interrupted", drainAnyway, drainAnyway)
	}

	resp, err := client.DrainSessions(ctx, &quaycrewv1.DrainSessionsRequest{Force: force})
	if err != nil {
		// A system from before this existed cannot be put down cleanly, and refusing would block the
		// upgrade that installs the answer. It says so and lets the caller carry on.
		if status.Code(err) == codes.Unimplemented {
			fmt.Fprintln(out, "this system is from before draining, so its sessions cannot be put down "+
				"cleanly. Whatever takes their containers will end any exec still working.")
			return nil
		}
		// A system that is not up runs no execs, so there is nothing to lose and nothing to refuse. It
		// says so rather than failing, because the caller is usually an upgrade about to start one.
		if status.Code(err) == codes.Unavailable {
			fmt.Fprintln(out, "the system is not up, so no exec is running and there is nothing to put down")
			return nil
		}
		return err
	}

	stopped := resp.GetStopped()
	if len(stopped) == 0 {
		fmt.Fprintln(out, "no session was live, so there was nothing to put down")
		return nil
	}
	for _, session := range stopped {
		fmt.Fprintf(out, "stopped %s\n", sessionSaid(session))
	}
	// What was interrupted is said after the list rather than instead of it, because the operator
	// asked for this and still has to know which conversation lost an exec.
	for _, session := range resp.GetWorking() {
		fmt.Fprintf(out, "%s was working, and that exec is gone\n", sessionSaid(session))
	}
	fmt.Fprintf(out, "\n%s down. Each keeps its conversation, and dispatching to one builds it a new "+
		"sandbox on whatever the system holds then.\n", sessionCount(len(stopped)))
	return nil
}

// sessionSaid is a session the way the operator sees it: the handle they would type, and the label
// they read the listing by.
func sessionSaid(session *quaycrewv1.Session) string {
	said := display.ShortID(session.GetHandle())
	if label := session.GetLabel(); label != "" {
		said += " (" + label + ")"
	}
	return said
}

// sessionCount counts sessions in words, because "1 sessions down" is the line that makes an operator
// doubt the rest of the message.
func sessionCount(count int) string {
	if count == 1 {
		return "1 session is"
	}
	return fmt.Sprintf("%d sessions are", count)
}
