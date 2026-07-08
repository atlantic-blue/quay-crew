package session

import (
	"context"
	"fmt"
	"os/exec"
)

// Docker runs a session's commands in a throwaway container (docker run --rm). This is the default
// Runtime: the run is isolated from the host. The image must contain whatever the command needs (for
// the model runner, the CLI and its config, mounted in).
type Docker struct {
	// Image is the container image to run the command in.
	Image string
	// Mounts are bind mounts, each "host:container[:ro]".
	Mounts []string
}

var _ Runtime = Docker{}

// Start runs spec.Argv inside a container and streams its stdout.
func (d Docker) Start(ctx context.Context, spec Spec) (Process, error) {
	if d.Image == "" {
		return nil, fmt.Errorf("session: docker image is required")
	}
	if len(spec.Argv) == 0 {
		return nil, fmt.Errorf("session: empty argv")
	}

	args := []string{"run", "--rm", "-i"}
	if spec.Workdir != "" {
		args = append(args, "-w", spec.Workdir)
	}
	for _, mount := range d.Mounts {
		args = append(args, "-v", mount)
	}
	for _, env := range spec.Env {
		args = append(args, "-e", env)
	}
	args = append(args, d.Image)
	args = append(args, spec.Argv...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("session: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("session: docker run: %w", err)
	}
	return &cmdProcess{cmd: cmd, stdout: stdout}, nil
}
