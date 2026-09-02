package main

import (
	"context"
	"fmt"
	"io"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/telling"
)

// surfaceCommandLine is what this tool calls itself when it records that it carried the telling. It
// is written on the record beside the moment, so a person reading a job back a week later can see
// which surface reached them.
const surfaceCommandLine = "command line"

// waitingAtMost is how many waiting jobs this line names before it says how many are left.
//
// Three, because this prints above every command an operator runs and the command itself is what
// they came for. A system with nine jobs waiting must not push the answer they asked for off the
// screen; the console and the briefing are where all nine are read.
const waitingAtMost = 3

// reportWaiting puts the telling on the error stream above whatever the command was going to print.
//
// This is the surface for the operator who did not go looking. The briefing is a page they must
// open, `krewe job list` is a command they must type, and the console draws to whoever is watching
// it. All three answer the question and all three wait to be asked. The next shell an operator opens
// says it without being asked, whatever they typed.
//
// It prints nothing when nothing waits. A line that appears on every command forever is a line that
// stops being read, and the whole value of this one is that its presence means something.
//
// It never refuses a command and never holds one up: a system too old to have the call, and one that
// cannot be reached at all, both say nothing.
func reportWaiting(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, said io.Writer) {
	asking, giveUp := context.WithTimeout(ctx, driftTimeout)
	defer giveUp()
	answer, err := client.GetWaiting(asking, &quaycrewv1.GetWaitingRequest{Surface: surfaceCommandLine})
	if err != nil {
		return
	}
	for _, line := range waitingLines(answer.GetWaiting()) {
		fmt.Fprintln(said, line)
	}
}

// waitingLines is what the operator reads: the count, then the longest waits, then how many were
// left out.
func waitingLines(waiting []*quaycrewv1.Waiting) []string {
	if len(waiting) == 0 {
		return nil
	}
	lines := []string{"krewe: " + telling.Count(waiting) + ", answer one with krewe job answer <job> \"...\""}
	for at, one := range waiting {
		if at == waitingAtMost {
			lines = append(lines, fmt.Sprintf("  and %d more, read them with krewe job list --phase asking",
				len(waiting)-waitingAtMost))
			break
		}
		lines = append(lines, "  "+telling.Line(one))
	}
	return lines
}
