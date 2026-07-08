package session

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// Local runs a session's commands directly on the host. It is a short term stopgap: it does not
// isolate the run, so prefer Docker. It exists so the system works before container isolation is
// fully wired.
type Local struct{}

var _ Runtime = Local{}

// cmdProcess adapts an *exec.Cmd to Process. Shared by the host and docker backends.
type cmdProcess struct {
	cmd    *exec.Cmd
	stdout io.Reader
}

func (p *cmdProcess) Stdout() io.Reader { return p.stdout }
func (p *cmdProcess) Wait() error       { return p.cmd.Wait() }

// Start runs spec.Argv on the host and streams its stdout.
func (Local) Start(ctx context.Context, spec Spec) (Process, error) {
	if len(spec.Argv) == 0 {
		return nil, fmt.Errorf("session: empty argv")
	}
	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Workdir
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("session: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("session: start %s: %w", spec.Argv[0], err)
	}
	return &cmdProcess{cmd: cmd, stdout: stdout}, nil
}
