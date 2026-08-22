package main

import (
	"fmt"
	"io"

	"github.com/atlantic-blue/quay-crew/internal/statusline"
)

// statusLineLimit is how much of standard input this will read. The runtime hands over one line of
// JSON describing the session; a megabyte is far more than that and stops a pipe that never ends
// from holding the draw open.
const statusLineLimit = 1 << 20

// runStatusLine draws the line the model runtime keeps under the conversation, so an operator
// attached to a session can see how much of the context window it has used without asking for it.
//
// The runtime runs this itself on every draw, handing the session over on standard input. It talks
// to nothing: everything the line says is in what was handed to it, and a status line that dialled
// the crew would be dialling it several times a second from inside a container that may not be able
// to reach it at all.
func runStatusLine(args []string, in io.Reader, out io.Writer) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: quay statusline, and the model runtime runs it for you, " +
			"handing it the session on standard input")
	}
	// A read that fails is handled the way an unreadable payload is: this has one line to say
	// anything in, and exiting with an error says nothing at all.
	payload, _ := io.ReadAll(io.LimitReader(in, statusLineLimit))
	fmt.Fprintln(out, statusline.Line(payload))
	return nil
}
