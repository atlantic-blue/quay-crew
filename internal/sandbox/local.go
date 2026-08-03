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

// Create returns a host backed sandbox. There is nothing to provision, so the id, the workspace and
// the project are all ignored. The environment is kept and applied to every command, standing in
// for a container's own environment.
//
// It keeps no state of its own either, and does not need to: a command here runs on the host as the
// operator, so the model's command line tool reads and writes the operator's own home directory,
// which already outlives every session. Mounting state in is what a container needs, because a
// container's filesystem is thrown away with it.
func (LocalProvider) Create(_ context.Context, cfg Config) (Sandbox, error) {
	return localSandbox{env: cfg.Env}, nil
}

type localSandbox struct{ env []string }

var _ Sandbox = localSandbox{}

// cmdProcess adapts an *exec.Cmd to Process. Shared by the local and docker backends.
type cmdProcess struct {
	cmd    *exec.Cmd
	stdout io.Reader
}

func (p *cmdProcess) Stdout() io.Reader { return p.stdout }
func (p *cmdProcess) Wait() error       { return p.cmd.Wait() }

// Exec runs spec.Argv on the host and streams its stdout.
func (l localSandbox) Exec(ctx context.Context, spec Spec) (Process, error) {
	if len(spec.Argv) == 0 {
		return nil, fmt.Errorf("sandbox: empty argv")
	}
	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Workdir
	// The sandbox's own environment first, then the command's, so a per turn value wins the way it
	// would inside a container.
	if len(l.env) > 0 || len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), l.env...)
		cmd.Env = append(cmd.Env, spec.Env...)
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
