package sandbox

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// DockerProvider gives each session its own long lived container. The container starts once, the
// session's tasks exec inside it, and it is removed when the session ends.
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
	// Network is the container network the driver joins. Empty leaves it on the daemon's default,
	// where the control plane is not reachable by name.
	//
	// Only the driver joins it. Joining is what lets a session drive the crew, and a session that can
	// drive the crew can also stop other sessions, so it is the one session marked for it rather than
	// every session in the crew.
	Network string
	// DriverMounts are host paths the driver gets and an ordinary session does not, each
	// "host:container[:ro]". They are what makes the driver the glue between the machine and the
	// crew: without them it can reach the control plane and see nothing to bring to it.
	DriverMounts []string
	// Memory is how much memory one session may take, as the daemon spells it, for example "4g".
	// Empty gives a session no limit, which is what every session had before this existed.
	//
	// A sandbox with no limit reports the whole machine in /proc/meminfo, so node sizes its heap
	// from it, Go sizes its collector from it, and jest and webpack start one worker for each
	// processor. What is really there is whatever the rest of the machine has not taken. The
	// session budgets against the first number, and the kernel kills it against the second. A limit
	// makes the advertised number the true one, and makes a kill the sandbox's own, which the sandbox
	// can read. See internal/room.
	//
	// The figure is the operator's to choose, because it shares one machine between the stack, the
	// sessions already running, and this one.
	Memory string
}

var _ Provider = DockerProvider{}

// Create starts a detached container for the session and returns a sandbox that execs into it.
//
// A container already carrying the session's name is adopted rather than refused, and started if it
// had stopped. A session's name is deterministic, so the alternative is that a control plane which
// has forgotten its sandboxes can never start that session again: the daemon refuses the name and the
// session is undispatchable until somebody removes the container by hand.
//
// What an adopted container carries is what it was created with. A workspace whose token changed
// since needs a fresh one, which is what stopping the session gives you.
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

	args := d.runArgs(name, cfg, kept)

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
	proc := newCmdProcess(cmd, stdout)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sandbox: docker exec: %w", err)
	}
	return proc, nil
}

// Remove tears down the container carrying this session's name, held or not. Absent is success.
func (d DockerProvider) Remove(ctx context.Context, sessionID string) error {
	out, err := exec.CommandContext(ctx, "docker", "rm", "-f", ContainerName(sessionID)).CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "No such container") {
			return nil
		}
		return fmt.Errorf("sandbox: remove container: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// sandboxName is exactly a sandbox container's name and nothing else's. The compose stack's own
// services share the prefix (quaycrew-postgres-1 and friends), so anything looser than the exact
// shape of ContainerName over a session id has, in the past, reaped the stack itself.
var sandboxName = regexp.MustCompile("^" + ContainerPrefix + "[0-9a-f]{24}$")

// Stranded lists the sessions whose containers the daemon still holds, running or not.
func (d DockerProvider) Stranded(ctx context.Context) ([]string, error) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "--all", "--format", "{{.Names}}").Output()
	if err != nil {
		return nil, fmt.Errorf("sandbox: list containers: %w", err)
	}
	var ids []string
	for _, name := range strings.Fields(string(out)) {
		if sandboxName.MatchString(name) {
			ids = append(ids, strings.TrimPrefix(name, ContainerPrefix))
		}
	}
	return ids, nil
}

// Close removes the session's container.
func (s *dockerSandbox) Close(ctx context.Context) error {
	if err := exec.CommandContext(ctx, "docker", "rm", "-f", s.name).Run(); err != nil {
		return fmt.Errorf("sandbox: remove container: %w", err)
	}
	return nil
}

// runArgs is the whole `docker run` for a session's sandbox. It is a function of its own so a test can
// read what the daemon would be asked for without a daemon: whether the sandbox joins a network at all
// is the difference between a session that can drive the crew and one that cannot.
func (d DockerProvider) runArgs(name string, cfg Config, kept []Mount) []string {
	args := []string{"run", "--detach", "--name", name, "--tmpfs", secretsMount()}
	if d.Memory != "" {
		// Swap is capped with it, at the same figure. Told a memory limit and nothing else, the
		// daemon allows swap of the same size again, so a session may take twice what the operator
		// said, and reach it by thrashing. Equal figures make the limit mean what it says.
		args = append(args, "--memory", d.Memory, "--memory-swap", d.Memory)
	}
	if cfg.Driver && d.Network != "" {
		args = append(args, "--network", d.Network)
	}
	if cfg.Driver {
		for _, mount := range d.DriverMounts {
			args = append(args, "-v", mount)
		}
	}
	for _, entry := range cfg.Env {
		args = append(args, "--env", entry)
	}
	for _, mount := range append(kept, cfg.Mounts...) {
		if mount.ReadOnly {
			args = append(args, "-v", mount.Source+":"+mount.Target+":ro")
			continue
		}
		args = append(args, "-v", mount.Source+":"+mount.Target)
	}
	for _, mount := range d.Mounts {
		args = append(args, "-v", mount)
	}
	return append(args, d.Image, "sleep", "infinity")
}

// secretsMount is the memory backed directory a file projected secret lands in, on every sandbox
// whether or not the workspace has set one. Created with the container rather than on demand,
// because a mount is a create time decision and the alternative is that the first workspace to mount
// a secret needs a fresh container to get the directory.
//
// It is owned by the sandbox user and shut to everybody else. Without the owner it belongs to root,
// and the crew, which writes into it as the sandbox's own user, is refused.
func secretsMount() string {
	return fmt.Sprintf("%s:mode=0700,uid=%d,gid=%d", SecretsPath, UserID, UserID)
}

// BuildLabel is where a sandbox image records which build of the crew it was made from. `make
// sandbox-image` stamps it; anything reading it treats an image without one as saying nothing.
const BuildLabel = "com.quaycrew.build"

// ImageBuild is the build a sandbox image was made from, read from its label, and empty when there is
// no answer to be had: no image name, no daemon, no image pulled yet, or an image made before this
// was stamped.
//
// Empty is deliberately not a verdict. A crew that cannot see which build its image came from should
// say nothing about it rather than accuse a perfectly good image of being stale, so every caller
// treats empty as "unknown" and shows nothing.
func ImageBuild(ctx context.Context, image string) string {
	if image == "" {
		return ""
	}
	out, err := exec.CommandContext(ctx, "docker", "image", "inspect",
		"--format", "{{index .Config.Labels \""+BuildLabel+"\"}}", image).Output()
	if err != nil {
		return ""
	}
	build := strings.TrimSpace(string(out))
	// Docker prints this for a label that is not there, and it is not a build.
	if build == "<no value>" {
		return ""
	}
	return build
}
