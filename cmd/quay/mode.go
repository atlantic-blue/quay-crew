package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/model"
)

// spokenModes are the words an operator types, against the words the model understands. The console
// has always printed "edits" and "dangerous" in its listing, so those are what somebody reads before
// they type anything; the model's own spellings are accepted too, because they are what the protocol
// and the manual say.
var spokenModes = map[string]string{
	"plan":              model.PermissionPlan,
	"edits":             model.PermissionAcceptEdits,
	"acceptedits":       model.PermissionAcceptEdits,
	"dangerous":         model.PermissionBypass,
	"bypasspermissions": model.PermissionBypass,
}

// runMode reads or sets what a thread's turns may do without asking.
//
// Until this existed the mode could only be changed by pressing a key in the full screen console, so
// a turn dispatched from a script, a flow or the driver was stuck with whatever it was born in, and
// a session that needed to clone a repository asked for an approval nobody was there to give.
func runMode(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 || len(args) > 2 {
		return fmt.Errorf("usage: quay mode <thread> [<mode>]\n\nthe modes are %s", offeredModes())
	}

	threadID, err := resolveSession(ctx, client, args[0])
	if err != nil {
		return err
	}
	if len(args) == 1 {
		return sayMode(ctx, client, threadID, out)
	}

	mode, known := spokenModes[strings.ToLower(strings.TrimSpace(args[1]))]
	if !known {
		return fmt.Errorf("there is no %q mode: the modes are %s", args[1], offeredModes())
	}
	resp, err := client.SetThreadPermissionMode(ctx, &quaycrewv1.SetThreadPermissionModeRequest{
		Id: threadID, Mode: mode,
	})
	if err != nil {
		return err
	}
	// The mode travels with each turn rather than with the container, so the next dispatch runs in it
	// and nothing has to be restarted. Said out loud because every other change to a thread's
	// capabilities does need a restart, and the difference is not guessable.
	fmt.Fprintf(out, "%s now runs in %s, from its next turn\n",
		display.ShortID(threadID), spokenOf(resp.GetThread().GetPermissionMode()))
	return nil
}

// sayMode answers what a thread runs in today, so reading does not mean setting it to what it
// already is to find out.
func sayMode(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, threadID string, out io.Writer) error {
	resp, err := client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{})
	if err != nil {
		return err
	}
	for _, thread := range resp.GetThreads() {
		if thread.GetId() == threadID {
			fmt.Fprintf(out, "%s runs in %s\n", display.ShortID(threadID), spokenOf(thread.GetPermissionMode()))
			return nil
		}
	}
	return fmt.Errorf("no thread %s", display.ShortID(threadID))
}

// spokenOf is the word for a stored mode. A thread from before the mode was written down holds
// nothing, and runs acceptEdits, so an empty value reads as "edits" rather than as blank, which
// would look like it asks about everything.
func spokenOf(stored string) string {
	switch stored {
	case model.PermissionPlan:
		return "plan"
	case model.PermissionBypass:
		return "dangerous"
	default:
		return "edits"
	}
}

// offeredModes lists what can be typed, in the order they widen what a turn may do.
func offeredModes() string {
	return "plan, edits and dangerous"
}
