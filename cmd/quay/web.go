package main

import (
	"context"
	"fmt"
	"io"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/web"
)

// runWeb serves the crew to a browser on this machine, and keeps serving until the operator stops it.
//
// It takes an address only so a busy port can be worked around. Whatever is passed has to be on this
// machine, which the server enforces rather than this function, because the rule belongs with the
// thing that binds the socket.
func runWeb(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: quay web [<address>]\n\nwith no address it serves %s", web.DefaultAddress)
	}
	addr := web.DefaultAddress
	if len(args) == 1 {
		addr = args[0]
	}
	return web.Serve(ctx, client, addr, out)
}
