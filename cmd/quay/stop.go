package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// runStop halts the task one session is running, and keeps the session.
//
// There was no way to stop one session before this. `quay drain` puts the whole crew down for the
// sake of one conversation, and what people reached for instead was killing the dispatch client,
// which is not an interface and does not reliably end anything: on 27 August 2026 the same kill ended
// one task at once and left another working for sixteen more minutes, merging two pull requests after
// the operator believed it had stopped.
//
// The session survives. Its conversation, its container and its history all stay, so the next
// dispatch continues it.
func runStop(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 || len(args) > 2 {
		return fmt.Errorf("usage: quay stop <session> [<reason>]\n\n" +
			"a session is its id, its handle, or its address. The reason is kept on the task record,\n" +
			"so whoever reads it later learns why it ended. The session itself stays: its conversation,\n" +
			"its container and its history are untouched, and the next dispatch continues it.\n\n" +
			"to put a whole session down instead, use the console. To put the crew down, quay drain")
	}
	reason := ""
	if len(args) == 2 {
		reason = strings.TrimSpace(args[1])
	}

	sessionID, err := resolveSession(ctx, client, args[0])
	if err != nil {
		return err
	}
	resp, err := client.StopTask(ctx, &quaycrewv1.StopTaskRequest{Id: sessionID, Reason: reason})
	if err != nil {
		if status.Code(err) == codes.Unimplemented {
			return fmt.Errorf("this crew is from before stopping one session was possible: " +
				"upgrade it, or put the whole crew down with quay drain")
		}
		return err
	}

	where := display.ShortID(sessionID)
	if !resp.GetStopped() {
		fmt.Fprintf(out, "nothing is running in %s, so there was nothing to stop\n", where)
		return nil
	}
	fmt.Fprintf(out, "stopped the task in %s%s\n", where, becauseOf(reason))
	fmt.Fprintf(out, "the session is still there: dispatch to it again to carry on\n")
	return nil
}

// becauseOf puts the operator's own reason on the line, when they gave one.
func becauseOf(reason string) string {
	if reason == "" {
		return ""
	}
	return ": " + reason
}
