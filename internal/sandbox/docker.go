package sandbox

import (
	"context"
	"fmt"
	"os/exec"
)

// DockerProvider gives each session its own long lived container. The container starts once, the
// session's turns exec inside it (so state, like the CLI's conversation store, persists across
// turns), and it is removed when the session ends.
type DockerProvider struct {
	// Image is the container image sessions run in.
	Image string
	// Mounts are bind mounts, each "host:container[:ro]".
	Mounts []string
}

var _ Provider = DockerProvider{}

// Create starts a detached container for the session and returns a sandbox that execs into it.
func (d DockerProvider) Create(ctx context.Context, id string, env []string) (Sandbox, error) {
	if d.Image == "" {
		return nil, fmt.Errorf("sandbox: docker image is required")
	}
	name := ContainerName(id)
	args := []string{"run", "--detach", "--name", name}
	for _, entry := range env {
		args = append(args, "--env", entry)
	}
	for _, mount := range d.Mounts {
		args = append(args, "-v", mount)
	}
	args = append(args, d.Image, "sleep", "infinity")

	if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("sandbox: create container: %w: %s", err, out)
	}
	return &dockerSandbox{name: name}, nil
}

type dockerSandbox struct {
	name string
}

var _ Sandbox = (*dockerSandbox)(nil)

// Exec runs spec.Argv inside the session's container and streams its stdout.
func (s *dockerSandbox) Exec(ctx context.Context, spec Spec) (Process, error) {
	if len(spec.Argv) == 0 {
		return nil, fmt.Errorf("sandbox: empty argv")
	}
	args := []string{"exec", "-i"}
	if spec.Workdir != "" {
		args = append(args, "-w", spec.Workdir)
	}
	for _, env := range spec.Env {
		args = append(args, "-e", env)
	}
	args = append(args, s.name)
	args = append(args, spec.Argv...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("sandbox: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sandbox: docker exec: %w", err)
	}
	return &cmdProcess{cmd: cmd, stdout: stdout}, nil
}

// Close removes the session's container.
func (s *dockerSandbox) Close(ctx context.Context) error {
	if err := exec.CommandContext(ctx, "docker", "rm", "-f", s.name).Run(); err != nil {
		return fmt.Errorf("sandbox: remove container: %w", err)
	}
	return nil
}
