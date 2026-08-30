package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
)

// runLabel reads or sets what the operator calls a session.
//
// A listing is a column of hexadecimal, and fourteen of those is an operator reading identifiers to
// work out which conversation was the one about the electricity bill. This is the half they type; the
// system writes the other half itself.
//
// Giving no text reads the label rather than clearing it, because clearing is destructive and the
// shorter command should not be the one that destroys something. `quay label <session> ""` clears it.
func runLabel(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 || len(args) > 2 {
		return fmt.Errorf(`usage: quay label <session> [<text>]` + "\n\n" +
			`no text reads it, and "" clears it`)
	}

	sessionID, err := resolveSession(ctx, client, args[0])
	if err != nil {
		return err
	}
	if len(args) == 1 {
		return sayLabel(ctx, client, sessionID, out)
	}

	resp, err := client.SetSessionLabel(ctx, &quaycrewv1.SetSessionLabelRequest{
		Id: sessionID, Label: args[1],
	})
	if err != nil {
		return err
	}
	if label := resp.GetSession().GetLabel(); label != "" {
		fmt.Fprintf(out, "%s is %q\n", display.ShortID(sessionID), label)
		return nil
	}
	fmt.Fprintf(out, "%s has no name, so it is listed as %s again\n",
		display.ShortID(sessionID), display.ShortID(resp.GetSession().GetHandle()))
	return nil
}

// sayLabel reads out what a session is called, and says what the system calls it when nobody has.
func sayLabel(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, sessionID string, out io.Writer) error {
	resp, err := client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: sessionID})
	if err != nil {
		return err
	}
	session := resp.GetSession()
	if label := strings.TrimSpace(session.GetLabel()); label != "" {
		fmt.Fprintf(out, "%s is %q\n", display.ShortID(sessionID), label)
		return nil
	}
	fmt.Fprintf(out, "%s has no name: quay label %s \"what it is about\"\n",
		display.ShortID(sessionID), display.ShortID(sessionID))
	return nil
}
