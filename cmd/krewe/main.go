package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/auth"
	"github.com/atlantic-blue/quay-krewe/internal/console"
	"github.com/atlantic-blue/quay-krewe/internal/workspace"
	"github.com/mattn/go-isatty"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// version is the build this binary is, stamped in at compile time by `make install` and by the
// release workflow. A binary that cannot say what it is leaves the operator guessing whether the
// thing they are looking at is the thing they fixed.
var version = "dev"

func main() {
	// Before anything reads a token or an address, because both are in the directory this checks for.
	if err := theOldLayout(); err != nil {
		fmt.Fprintln(os.Stderr, "krewe:", err)
		os.Exit(1)
	}

	told := os.Getenv("QC_GRPC_ADDR")
	addr := told
	if addr == "" {
		addr = "localhost:50051"
	}

	options := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if token := systemToken(os.Getenv, os.ReadFile); token != "" {
		options = append(options, grpc.WithPerRPCCredentials(auth.Credentials(token)))
	}
	conn, err := grpc.NewClient(addr, options...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "krewe: connect to %s: %v\n", addr, err)
		os.Exit(1)
	}
	defer func() { _ = conn.Close() }()

	client := quaycrewv1.NewControlPlaneServiceClient(conn)
	if err := dispatch(context.Background(), client, os.Args[1:], addr); err != nil {
		// A command that already put the reason on the operator's screen, and waited there while they
		// read it, does not get it printed underneath a second time.
		if !errors.Is(err, ErrSaid) {
			fmt.Fprintln(os.Stderr, "krewe:", unreachable(err, told, inAContainer()))
		}
		os.Exit(1)
	}
}

// dockerEnvFile is the file Docker puts in every container it runs. Its presence is how a process
// knows it is inside one without being told.
const dockerEnvFile = "/.dockerenv"

// inAContainer says whether this krewe is running inside a container, which for this tool means inside
// a session's sandbox.
func inAContainer() bool {
	_, err := os.Stat(dockerEnvFile)
	return err == nil
}

// unreachable turns "connection refused" inside a sandbox into the thing that is actually wrong.
//
// A session that was never told where the system is falls back to localhost, and localhost inside a
// container is the container: there is nothing there and there never will be. The dial error names an
// address the operator did not choose and cannot fix, which reads as the system being down. What is
// actually true is that nothing told this session where to go, and that cannot be set from in here.
//
// A task is told where the system is when it runs a job, so the ordinary reason to see this
// is a task that is running none. The other reason is a system whose own address is unset.
func unreachable(err error, told string, sandboxed bool) error {
	if err == nil || !sandboxed || told != "" {
		return err
	}
	if status.Code(err) != codes.Unavailable {
		return err
	}
	return fmt.Errorf("this session was not told where the system is, so there is nothing at the "+
		"address it fell back to. A task is told where the system is when it runs a job, and what "+
		"it may do there comes from that job's role. So either this task is running none, or the "+
		"system has no address of its own: QC_SANDBOX_CONTROL_PLANE on the control plane, which is "+
		"the system's configuration file, ~/.krewe/env on a compose stack. (%w)", err)
}

// opening is what an invocation asks the tool to put on the screen. `krewe` with no arguments has one
// answer and only one, so the choice is made here where a test can read it rather than inside the
// call that takes the terminal.
type opening int

const (
	// theConsole is `krewe` with a terminal in front of it: the console, full width, on its own.
	theConsole opening = iota
	// plainLines is `krewe` with its output going somewhere else, so `krewe | grep` still works.
	plainLines
	// aCommand is every invocation that named one.
	aCommand
)

// kreweOpens says which of the three an invocation is.
//
// Nothing else is on this list. `krewe` used to open a tmux window with the console in one half and a
// conversation in the other, so typing the name of the tool gave a person a split terminal and a
// conversation they had not asked for. A conversation is asked for by name now: p in the console, or
// `krewe attach <session>`.
func kreweOpens(args []string, terminal bool) opening {
	switch {
	case len(args) > 0:
		return aCommand
	case !terminal:
		return plainLines
	default:
		return theConsole
	}
}

// dispatch routes an invocation: no arguments opens the console, anything else runs a subcommand.
// With no terminal attached the console prints plain lines instead, so `krewe | grep` still works.
func dispatch(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, addr string) error {
	switch kreweOpens(args, isatty.IsTerminal(os.Stdout.Fd())) {
	case aCommand:
		// Before the command, so an operator watching a task run sees it rather than finding it
		// above the answer afterwards. The console says which build the system is in its own header
		// and which part of it is down in its stats view, and a line drawn over a full screen view
		// would corrupt it.
		reportDrift(ctx, client, os.Stderr)
		reportDegraded(ctx, client, os.Stderr)
		// Last of the three, so it is the line closest to the command the operator typed. It is also the
		// only one of them about their work rather than about the system.
		//
		// Not under the status line, which is the model runtime redrawing several times a second and
		// carries the same count itself. Two tellings of one wait, one of them printed where nobody can
		// read it, is worse than one.
		if args[0] != "statusline" {
			reportWaiting(ctx, client, os.Stderr)
		}
		return run(ctx, client, args, os.Stdout, addr)
	case plainLines:
		return console.Plain(ctx, client, os.Stdout)
	default:
		return openTheConsole(ctx, client, addr)
	}
}

// openTheConsole is what `krewe` with no arguments does: the console, full width, on its own.
//
// Nothing opens beside it. A conversation is a thing the operator asks for by name, with p in the
// console or with `krewe attach`, so opening the tool never puts one on half of the screen unasked.
func openTheConsole(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, addr string) error {
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
	}, conversationBeside(ctx, client), endConversationBeside(ctx, client), remembering())
}
