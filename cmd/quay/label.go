package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
)

// runLabel reads or sets what the operator calls a thread.
//
// A listing is a column of hexadecimal, and fourteen of those is an operator reading identifiers to
// work out which conversation was the one about the electricity bill. This is the half they type; the
// crew writes the other half itself.
//
// Giving no text reads the label rather than clearing it, because clearing is destructive and the
// shorter command should not be the one that destroys something. `quay label <thread> ""` clears it.
func runLabel(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 || len(args) > 2 {
		return fmt.Errorf(`usage: quay label <thread> [<text>]` + "\n\n" +
			`no text reads it, and "" clears it`)
	}

	threadID, err := resolveSession(ctx, client, args[0])
	if err != nil {
		return err
	}
	if len(args) == 1 {
		return sayLabel(ctx, client, threadID, out)
	}

	resp, err := client.SetThreadLabel(ctx, &quaycrewv1.SetThreadLabelRequest{
		Id: threadID, Label: args[1],
	})
	if err != nil {
		return err
	}
	if label := resp.GetThread().GetLabel(); label != "" {
		fmt.Fprintf(out, "%s is %q\n", display.ShortID(threadID), label)
		return nil
	}
	fmt.Fprintf(out, "%s has no name, so it is listed as %s again\n",
		display.ShortID(threadID), display.ShortID(resp.GetThread().GetHandle()))
	return nil
}

// sayLabel reads out what a thread is called, and says what the crew calls it when nobody has.
func sayLabel(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, threadID string, out io.Writer) error {
	resp, err := client.GetThread(ctx, &quaycrewv1.GetThreadRequest{Id: threadID})
	if err != nil {
		return err
	}
	thread := resp.GetThread()
	if label := strings.TrimSpace(thread.GetLabel()); label != "" {
		fmt.Fprintf(out, "%s is %q\n", display.ShortID(threadID), label)
		return nil
	}
	fmt.Fprintf(out, "%s has no name: quay label %s \"what it is about\"\n",
		display.ShortID(threadID), display.ShortID(threadID))
	return nil
}
