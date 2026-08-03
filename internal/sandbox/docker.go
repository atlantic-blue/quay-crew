package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// DockerProvider gives each session its own long lived container. The container starts once, the
// session's turns exec inside it, and it is removed when the session ends.
//
// The state that has to outlive the container comes from Storage, mounted in from the host, so
// removing a container no longer destroys the conversation the database holds a handle to.
type DockerProvider struct {
	// Image is the container image sessions run in.
	Image string
	// Mounts are extra bind mounts for every sandbox, each "host:container[:ro]".
	Mounts []string
	// Storage keeps the workspace's conversation store and the project's files on the host.
	Storage Storage
}

var _ Provider = DockerProvider{}

// Create starts a detached container for the session and returns a sandbox that execs into it.
//
// A container already carrying the session's name is adopted rather than refused, and started if it
// had stopped. A session's name is deterministic, so the alternative is that a control plane which
// has forgotten its sandboxes can never start that thread again: the daemon refuses the name and the
// thread is undispatchable until somebody removes the container by hand.
//
// What an adopted container carries is what it was created with. A workspace whose token changed
// since needs a fresh one, which is what stopping the thread gives you.
func (d DockerProvider) Create(ctx context.Context, cfg Config) (Sandbox, error) {
	if d.Image == "" {
		return nil, fmt.Errorf("sandbox: docker image is required")
	}
	kept, err := d.Storage.Prepare(cfg)
	if err != nil {
		return nil, err
	}

	name := ContainerName(cfg.ID)
	if adopted, err := d.adopt(ctx, name); err != nil {
		return nil, err
	} else if adopted != nil {
		return adopted, nil
	}

	args := []string{"run", "--detach", "--name", name}
	for _, entry := range cfg.Env {
		args = append(args, "--env", entry)
	}
	for _, mount := range kept {
		args = append(args, "-v", mount.Source+":"+mount.Target)
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

// adopt returns a sandbox over an existing container, starting it when it had stopped, or nil when
// there is no container by that name to adopt.
func (d DockerProvider) adopt(ctx context.Context, name string) (Sandbox, error) {
	out, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Running}}", name).Output()
	if err != nil {
		// Nothing by that name. Anything else the daemon has to say about it will be said again, and
		// more usefully, by the create below.
		return nil, nil
	}
	if strings.TrimSpace(string(out)) == "true" {
		return &dockerSandbox{name: name}, nil
	}
	if out, err := exec.CommandContext(ctx, "docker", "start", name).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("sandbox: start existing container %s: %w: %s", name, err, out)
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
