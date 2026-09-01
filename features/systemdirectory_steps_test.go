package features_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cucumber/godog"
)

// Steps for the directory a system keeps everything it owns in.
//
// They run the real tool in its own process, with its own home directory, because what is specified
// here is what a caller receives: the tool refuses before it reads a token or an address, and a
// refusal that exits zero is exactly the failure these scenarios exist to catch. Neither the exit
// status nor the stream exists inside the test process.
//
// The tool's own home variable is deliberately unset in these runs, so the default path is what is
// under test. A run that set it would prove only that the tool reads its own variable.

type machineKey struct{}

// machine is an operator's home directory, and what is in it.
type machine struct {
	home string
	// told is the value the run exports for the retired variable, empty for a run that exports
	// neither.
	told string
}

func machineFrom(ctx context.Context) *machine {
	held, ok := ctx.Value(machineKey{}).(*machine)
	if !ok {
		return &machine{}
	}
	return held
}

const (
	theDirectoryThatWent     = ".quay"
	theDirectoryTheToolReads = ".krewe"
)

func initializeSystemDirectorySteps(sc *godog.ScenarioContext) {
	sc.Step(`^a machine whose system is still in the directory that went$`, func(ctx context.Context) (context.Context, error) {
		return aHomeDirectory(ctx, filepath.Join(theDirectoryThatWent, "data"))
	})

	sc.Step(`^a machine whose system is in the directory the tool reads$`, func(ctx context.Context) (context.Context, error) {
		return aHomeDirectory(ctx, filepath.Join(theDirectoryTheToolReads, "data"))
	})

	sc.Step(`^a machine with no system on it at all$`, func(ctx context.Context) (context.Context, error) {
		return aHomeDirectory(ctx, "")
	})

	// The configuration file is what `make config` writes, and it writes it before anything starts. A
	// directory that holds only that is a directory nothing has moved into yet.
	sc.Step(`^the new directory exists and holds only the configuration file$`, func(ctx context.Context) error {
		held := machineFrom(ctx)
		where := filepath.Join(held.home, theDirectoryTheToolReads)
		if err := os.MkdirAll(where, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(where, "env"), []byte("QC_MODEL=echo\n"), 0o644)
	})

	sc.Step(`^a machine whose system is in a directory of its own, named by the variable that went$`,
		func(ctx context.Context) (context.Context, error) {
			ctx, err := aHomeDirectory(ctx, "")
			if err != nil {
				return ctx, err
			}
			held := machineFrom(ctx)
			held.told = filepath.Join(held.home, "somewhere-else")
			return ctx, os.MkdirAll(filepath.Join(held.told, "data"), 0o755)
		})

	sc.Step(`^the caller types "([^"]*)" on that machine$`, func(ctx context.Context, typed string) error {
		return runOnMachine(ctx, strings.Fields(typed)...)
	})

	sc.Step(`^standard error names the directory that went and the one it moves to$`, func(ctx context.Context) error {
		held, said := machineFrom(ctx), toolFrom(ctx).stderr
		want := "mv " + filepath.Join(held.home, theDirectoryThatWent) + " " +
			filepath.Join(held.home, theDirectoryTheToolReads)
		if !strings.Contains(said, want) {
			return fmt.Errorf("it never says to run\n  %s\nit says:\n%s", want, said)
		}
		return nil
	})

}

// aHomeDirectory is an operator's home directory with one thing in it, or nothing at all.
func aHomeDirectory(ctx context.Context, holds string) (context.Context, error) {
	home, err := os.MkdirTemp("", "krewe-machine-")
	if err != nil {
		return ctx, err
	}
	if holds != "" {
		if err := os.MkdirAll(filepath.Join(home, holds), 0o755); err != nil {
			return ctx, err
		}
	}
	return context.WithValue(ctx, machineKey{}, &machine{home: home}), nil
}

// runOnMachine runs the tool as a caller runs it, in that machine's home directory. The tool's own
// home variable is left unset, so the tool works out where the system's directory is the way it does
// on somebody's laptop.
func runOnMachine(ctx context.Context, args ...string) error {
	binary, err := kreweBinary()
	if err != nil {
		return err
	}
	t, held := toolFrom(ctx), machineFrom(ctx)
	if t.address == "" {
		return fmt.Errorf("the system has no address the tool can dial")
	}
	if held.home == "" {
		return fmt.Errorf("this scenario names no machine to run on")
	}

	command := exec.CommandContext(ctx, binary, args...)
	command.Env = append(os.Environ(),
		"QC_GRPC_ADDR="+t.address,
		"QC_TOKEN="+worldFrom(ctx).token,
		"HOME="+held.home,
		"KREWE_HOME=",
		"QUAY_HOME="+held.told,
	)
	var out, said strings.Builder
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
