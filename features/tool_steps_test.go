package features_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/cucumber/godog"
)

// The steps that run the real command line tool in its own process.
//
// What a caller sees is which stream a thing went to and what the exit status was, and neither of
// those exists inside the test process. Every other scenario dials the system over an in memory
// connection, which a second process cannot reach, so a scenario that runs the tool asks the system
// for a network address of its own first.

// toolBuild is the build the scenarios' copy of the tool reports for itself, stamped the way the
// install target stamps it. A scenario about two parts being different builds needs to know what
// this one is.
const toolBuild = "7001b17d"

type toolKey struct{}

// toolWorld is what came back out of the last run of the tool.
type toolWorld struct {
	address string
	// stdin is what one step prepares for a later step to pipe in. Context is read from standard
	// input rather than from an argument, so the body has to survive between the two.
	stdin    string
	stdout   string
	stderr   string
	exitCode int
	ran      bool
}

func toolFrom(ctx context.Context) *toolWorld {
	t, _ := ctx.Value(toolKey{}).(*toolWorld)
	return t
}

func initializeToolSteps(sc *godog.ScenarioContext) {
	sc.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
		return context.WithValue(ctx, toolKey{}, &toolWorld{}), nil
	})

	sc.Step(`^the system listens on an address the tool can dial$`, listenForTool)

	// An address with nothing on it, which is what a system that is down looks like from here.
	sc.Step(`^the system cannot be reached$`, func(ctx context.Context) error {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		address := listener.Addr().String()
		if err := listener.Close(); err != nil {
			return err
		}
		toolFrom(ctx).address = address
		return nil
	})

	sc.Step(`^standard output is empty$`, func(ctx context.Context) error {
		if got := toolFrom(ctx).stdout; got != "" {
			return fmt.Errorf("standard output carries %q, and a caller reading it would take that for the answer", got)
		}
		return nil
	})

	// Exactly, rather than carries, because what a redirection captures is the whole of standard
	// output. A heading or a trailing newline added for the look of it becomes part of the level the
	// moment somebody pipes this back into the command that writes one.
	sc.Step(`^standard output is exactly "([^"]*)"$`, func(ctx context.Context, want string) error {
		if got := toolFrom(ctx).stdout; got != want {
			return fmt.Errorf("standard output is %q, want exactly %q", got, want)
		}
		return nil
	})

	sc.Step(`^standard error says nothing$`, func(ctx context.Context) error {
		if got := toolFrom(ctx).stderr; got != "" {
			return fmt.Errorf("standard error says %q", got)
		}
		return nil
	})

	sc.Step(`^the command succeeds$`, func(ctx context.Context) error {
		t := toolFrom(ctx)
		if !t.ran {
			return fmt.Errorf("the command was never run")
		}
		if t.exitCode != 0 {
			return fmt.Errorf("the command exited %d, saying %q", t.exitCode, t.stderr)
		}
		return nil
	})

	sc.Step(`^the command fails$`, func(ctx context.Context) error {
		t := toolFrom(ctx)
		if !t.ran {
			return fmt.Errorf("the command was never run")
		}
		if t.exitCode == 0 {
			return fmt.Errorf("the command succeeded, so a caller cannot tell anything went wrong")
		}
		return nil
	})
}

// listenForTool puts the system on a network address, which is the only way a second process reaches
// it. A control plane started again is a different server, so this runs again after any restart.
func listenForTool(ctx context.Context) error {
	w, t := worldFrom(ctx), toolFrom(ctx)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen for the tool: %w", err)
	}
	go func() { _ = w.grpcServer.Serve(listener) }()
	t.address = listener.Addr().String()
	return nil
}

// runTool runs the tool the way a caller runs it, with its streams kept apart and nothing on
// standard input.
func runTool(ctx context.Context, args ...string) error {
	return runToolSaying(ctx, "", args...)
}

// runToolSaying runs the tool with something piped into it, which is how a file becomes a context,
// a secret or anything else this tool reads rather than takes as an argument.
//
// The home directory is a temporary one: the tool keeps where the operator is standing on the
// machine it runs on, and a scenario must not read or write the operator's own.
func runToolSaying(ctx context.Context, in string, args ...string) error {
	binary, err := kreweBinary()
	if err != nil {
		return err
	}
	return runBinarySaying(ctx, binary, in, args...)
}

// runBinarySaying is the same run, for a scenario that names which binary it wants. The name the tool
// used to have is a second binary beside it, and what it does is only observable from outside the
// process: which stream it wrote on, and what it exited with.
func runBinarySaying(ctx context.Context, binary, in string, args ...string) error {
	t := toolFrom(ctx)
	if t.address == "" {
		return fmt.Errorf("the system has no address the tool can dial")
	}
	home, err := os.MkdirTemp("", "quaycrew-tool-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(home) }()

	command := exec.CommandContext(ctx, binary, args...)
	command.Env = append(os.Environ(),
		"QC_GRPC_ADDR="+t.address,
		"QC_TOKEN="+worldFrom(ctx).token,
		"QUAY_HOME="+home,
		"HOME="+home,
	)
	var out, said bytes.Buffer
	command.Stdin = strings.NewReader(in)
	command.Stdout, command.Stderr = &out, &said
	runErr := command.Run()
	t.ran, t.stdout, t.stderr = true, out.String(), said.String()

	var exit *exec.ExitError
	switch {
	case runErr == nil:
		t.exitCode = 0
	case errors.As(runErr, &exit):
		t.exitCode = exit.ExitCode()
	default:
		return fmt.Errorf("running the tool: %w", runErr)
	}
	return nil
}

// says is one assertion on a stream, kept in one place so every scenario reports a miss the same way.
func says(stream, got, want string) error {
	if !strings.Contains(got, want) {
		return fmt.Errorf("%s says %q, want it to say %q", stream, got, want)
	}
	return nil
}
