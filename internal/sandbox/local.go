package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// LocalProvider hands out host backed sandboxes. It is a stopgap: a local sandbox does not isolate
// the run, so prefer Docker. It exists so the system works before container isolation is wired.
type LocalProvider struct{}

var _ Provider = LocalProvider{}

// Create returns a host backed sandbox. The id is ignored; there is nothing to provision.
func (LocalProvider) Create(context.Context, string) (Sandbox, error) { return localSandbox{}, nil }

type localSandbox struct{}

var _ Sandbox = localSandbox{}

// cmdProcess adapts an *exec.Cmd to Process. Shared by the local and docker backends.
type cmdProcess struct {
	cmd    *exec.Cmd
	stdout io.Reader
}

func (p *cmdProcess) Stdout() io.Reader { return p.stdout }
func (p *cmdProcess) Wait() error       { return p.cmd.Wait() }

// Exec runs spec.Argv on the host and streams its stdout.
func (localSandbox) Exec(ctx context.Context, spec Spec) (Process, error) {
	if len(spec.Argv) == 0 {
		return nil, fmt.Errorf("sandbox: empty argv")
	}
	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Workdir
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("sandbox: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sandbox: start %s: %w", spec.Argv[0], err)
	}
	return &cmdProcess{cmd: cmd, stdout: stdout}, nil
}

// Close is a no op for the host backend.
func (localSandbox) Close(context.Context) error { return nil }
