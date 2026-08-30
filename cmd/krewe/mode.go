package main

import (
	"context"
	"fmt"
	"io"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/display"
	"github.com/atlantic-blue/krewe/internal/model"
)

// runMode reads or sets what a session's tasks may do without asking.
//
// Until this existed the mode could only be changed by pressing a key in the full screen console, so
// a task dispatched from a script, a flow or the driver was stuck with whatever it was born in, and
// a session that needed to clone a repository asked for an approval nobody was there to give.
func runMode(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 || len(args) > 2 {
		return fmt.Errorf("usage: krewe mode <session> [<mode>]\n\nthe modes are %s", offeredModes())
	}

	sessionID, err := resolveSession(ctx, client, args[0])
	if err != nil {
		return err
	}
	if len(args) == 1 {
		return sayMode(ctx, client, sessionID, out)
	}

	mode, known := model.PermissionModeNamed(args[1])
	if !known {
		return fmt.Errorf("there is no %q mode: the modes are %s", args[1], offeredModes())
	}
	resp, err := client.SetSessionPermissionMode(ctx, &quaycrewv1.SetSessionPermissionModeRequest{
		Id: sessionID, Mode: mode,
	})
	if err != nil {
		return err
	}
	// The mode travels with each task rather than with the container, so the next dispatch runs in it
	// and nothing has to be restarted. Said out loud because every other change to a session's
	// capabilities does need a restart, and the difference is not guessable.
	fmt.Fprintf(out, "%s now runs in %s, from its next task\n",
		display.ShortID(sessionID), spokenOf(resp.GetSession().GetPermissionMode()))
	return nil
}

// sayMode answers what a session runs in today, so reading does not mean setting it to what it
// already is to find out.
func sayMode(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, sessionID string, out io.Writer) error {
	resp, err := client.ListSessions(ctx, &quaycrewv1.ListSessionsRequest{})
	if err != nil {
		return err
	}
	for _, session := range resp.GetSessions() {
		if session.GetId() == sessionID {
			fmt.Fprintf(out, "%s runs in %s\n", display.ShortID(sessionID), spokenOf(session.GetPermissionMode()))
			return nil
		}
	}
	return fmt.Errorf("no session %s", display.ShortID(sessionID))
}

// spokenOf is the word for a stored mode. A session from before the mode was written down holds
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

// offeredModes lists what can be typed, in the order they widen what a task may do.
func offeredModes() string {
	return "plan, edits and dangerous"
}
