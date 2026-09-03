package main

import (
	"context"
	"fmt"
	"io"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/statusline"
)

// statusLineLimit is how much of standard input this will read. The runtime hands over one line of
// JSON describing the session; a megabyte is far more than that and stops a pipe that never ends
// from holding the draw open.
const statusLineLimit = 1 << 20

// surfaceStatusLine is what the line under a conversation calls itself on the record when it carries
// the telling.
const surfaceStatusLine = "status line"

// waitingFile is where the last count of what waits for a person is kept, inside the conversation
// directory the system already mounts into every sandbox.
const waitingFile = "waiting"

// waitingFreshFor is how long that count stands before this asks the system again.
//
// Three seconds, which is the console's own refresh, so the two surfaces cannot be more than one
// beat apart. The runtime redraws this line several times a second, and a call on every draw would
// be several calls a second from inside a container for a number that changes when a job stops.
const waitingFreshFor = 3 * time.Second

// waitingTimeout caps the call. This line has to be drawn now, so a system that is slow to answer
// leaves the telling off rather than holding up the prompt.
const waitingTimeout = 500 * time.Millisecond

// runStatusLine draws the line the model runtime keeps under the conversation, so an operator
// attached to a session can see how much of the context window it has used, and whether anything is
// waiting on them, without asking for either.
//
// The runtime runs this itself on every draw, handing the session over on standard input. The
// context half talks to nothing: everything it says is in what was handed to it.
//
// The telling half does ask the system, because nothing else can answer it, and it is cached in the
// conversation directory for as long as the console waits between its own refreshes. A status line
// that dialled on every draw would be dialling several times a second; one that never dialled would
// leave the person typing as the one person the system cannot reach.
func runStatusLine(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient,
	args []string, in io.Reader, out io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: krewe statusline, and the model runtime runs it for you, " +
			"handing it the session on standard input")
	}
	// A read that fails is handled the way an unreadable payload is: this has one line to say
	// anything in, and exiting with an error says nothing at all.
	payload, _ := io.ReadAll(io.LimitReader(in, statusLineLimit))
	// Written down before the line is drawn, because this is the only place the system can learn how big
	// the model's context window is: the console has to answer the same question for a session nobody
	// is attached to, and nothing else in the system is ever told the size.
	if size, said := statusline.WindowSize(payload); said {
		rememberWindowSize(conversationDir, size)
	}
	fmt.Fprintln(out, statusline.Line(payload))
	return nil
}
