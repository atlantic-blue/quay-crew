package main

import (
	"context"
	"fmt"
	"os"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/console"
	"github.com/atlantic-blue/quay-crew/internal/workspace"
	"github.com/mattn/go-isatty"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// version is the build this binary is, stamped in at compile time by `make install` and by the
// release workflow. A binary that cannot say what it is leaves the operator guessing whether the
// thing they are looking at is the thing they fixed.
var version = "dev"

func main() {
	addr := os.Getenv("QC_GRPC_ADDR")
	if addr == "" {
		addr = "localhost:50051"
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "quay: connect to %s: %v\n", addr, err)
		os.Exit(1)
	}
	defer func() { _ = conn.Close() }()

	client := quaycrewv1.NewControlPlaneServiceClient(conn)
	if err := dispatch(context.Background(), client, os.Args[1:], addr); err != nil {
		fmt.Fprintln(os.Stderr, "quay:", err)
		os.Exit(1)
	}
}

// dispatch routes an invocation: no arguments opens the console, anything else runs a subcommand.
// With no terminal attached the console prints plain lines instead, so `quay | grep` still works.
func dispatch(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, addr string) error {
	if len(args) > 0 {
		return run(ctx, client, args, os.Stdout, addr)
	}
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		return console.Plain(ctx, client, os.Stdout)
	}
	return openTheCrew(
		func() error { return runPanel(ctx, client, nil, os.Stdout, addr) },
		func() error { return openConsoleAlone(ctx, client, addr) },
	)
}

// openTheCrew is what `quay` with no arguments does: the panel, and the console on its own when there
// is nothing to put beside it yet.
//
// One command. Julian: "I dont understand why I need quay panel, is confusing, I need one command
// only, the panel should appear when I press quay and toggled with the key p". A crew with no
// conversation in it is exactly the first run, and refusing to open at all then would be absurd.
func openTheCrew(panel, alone func() error) error {
	if err := panel(); err == nil {
		return nil
	}
	return alone()
}

func openConsoleAlone(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, addr string) error {
	current, err := currentPath()
	if err != nil {
		// Not being able to read where you are standing is not a reason to refuse to open the
		// console. It opens, and says nothing about a context rather than the wrong thing.
		current = workspace.Path{}
	}
	return console.Run(ctx, client, console.Info{
		Version:   version,
		Address:   addr,
		Workspace: current.Workspace,
		Project:   current.Project,
	}, conversationBeside(ctx, client))
}
