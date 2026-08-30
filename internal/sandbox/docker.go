package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/capacity"
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
	// Network is the system's own network, and the driver is the only sandbox on it. It carries the
	// store, the broker and the observability stack as well as the control plane, so it is a real
	// widening, and it is configuration rather than a default. Empty puts the driver on the session
	// network below, which is all it needs to drive the system.
	Network string
	// SessionNetwork is the network every sandbox joins to reach the control plane. The control plane
	// is on it and nothing else of the system's is, so a session can reach the system and no store, no
	// broker and no dashboard.
	//
	// Reaching the system is not permission to do anything there. A session is refused every call until
	// it presents a credential the system minted for the job it is running, and that credential
	// carries the verbs its role declared and expires with the job. So this decides what
	// a session can address, and the credential decides what it may do.
	//
	// Empty leaves a sandbox on the daemon's default network, where the control plane cannot be
	// reached by name, which is what every session had before this existed.
	SessionNetwork string
	// DriverMounts are host paths the driver gets and an ordinary session does not, each
	// "host:container[:ro]". They are what makes the driver the glue between the machine and the
	// system: without them it can reach the control plane and see nothing to bring to it.
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

// Attached asks the container whether the operator's conversation has anybody watching it.
//
// `quay attach` runs `docker exec --interactive --tty <container> tmux new-session -A -s quay ...`,
// so the conversation an operator types into is a tmux session called AttachedSessionName inside the
// container, and tmux itself already knows whether a client is on it. This is the same question asked
// from outside, through one more exec against the same tmux server.
//
// No state is written and nothing has to be refreshed, which is the reason this signal was chosen
// over stamping the session's row on attach: a stamp needs somebody to keep it fresh while a pane is
// open, and how often is a number, and no measurement has set one.
//
// What each answer means:
//   - a client listed, so somebody is attached.
//   - the command ran and exited non zero, so there is no tmux server, no such conversation, or no
//     such container. In every one of those, nobody is typing into it.
//   - the command could not be run at all, so the daemon is unreachable and the system cannot tell. The
//     error is returned rather than swallowed, because a caller must not read it as nobody.
func (d DockerProvider) Attached(ctx context.Context, sessionID string) (bool, error) {
	cmd := exec.CommandContext(ctx, "docker", "exec", ContainerName(sessionID),
		"tmux", "list-clients", "-t", AttachedSessionName, "-F", "#{client_name}")
	out, err := cmd.Output()
	if err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			return false, nil
		}
		return false, fmt.Errorf("sandbox: ask %s whether anybody is attached: %w",
			ContainerName(sessionID), err)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// processTable dumps the command line of everything running in the sandbox, one process per line,
// read from the container's own /proc.
//
// It carries no part of the runtime's name on purpose. The shell that runs this is itself a process
// in that container, so a reader that named what it was looking for would find its own command line
// and report every sandbox as running the runtime. The matching happens in Go, where it can be
// tested, and this only gathers.
//
// Every failure inside is swallowed and the script always succeeds: a process that exits between the
// glob and the read is ordinary, and it must not come back as the sandbox being unreachable. What
// does fail here is docker itself, which is the answer the caller needs.
//
// The arguments arrive separated by a zero byte, which is how the kernel writes them, and they are
// left that way. Translating them here would put one more command between the system and the answer,
// and a sandbox image whose translation flag behaved differently would report every container as
// empty. Go splits them instead.
const processTable = `for p in /proc/[0-9]*/cmdline; do cat "$p" 2>/dev/null; echo; done; exit 0`

// RuntimeRunning asks the container's own process table whether a model runtime is up in it.
//
// This is the state the system could not see. `quay attach` opens the conversation in tmux inside the
// sandbox, and detaching leaves the runtime answering with nobody watching it, so the tmux question
// says nobody is there and the row says no task is open, and both are true while a conversation is
// mid answer.
//
// Nothing is written and nothing has to be kept fresh, which is why it is asked rather than stamped:
// a stamp needs somebody to refresh it while a conversation runs, and how often is a number nobody
// has measured.
//
// What each answer means, which is Attached's contract on purpose:
//   - a matching command line, so a runtime is up.
//   - the command ran and exited non zero, so there is no container, or no shell in it. Nothing is
//     running in either.
//   - the command could not be run at all, so the daemon is unreachable and the system cannot tell. The
//     error is returned rather than swallowed, because a caller must not read it as nothing.
func (d DockerProvider) RuntimeRunning(ctx context.Context, sessionID string) (bool, error) {
	cmd := exec.CommandContext(ctx, "docker", "exec", ContainerName(sessionID), "sh", "-c", processTable)
	out, err := cmd.Output()
	if err != nil {
		var exited *exec.ExitError
		if errors.As(err, &exited) {
			return false, nil
		}
		return false, fmt.Errorf("sandbox: ask %s what it is running: %w",
			ContainerName(sessionID), err)
	}
	return runtimeAmong(string(out)), nil
}

// runtimeAmong says whether any of these command lines is the model runtime.
//
// A line counts when any word in it names the runtime, by base name, so the runtime is found whether
// it was started as `claude --resume ...` or through the interpreter an npm install puts in front of
// it. Which of those a sandbox shows depends on how the package was installed, and a reader that
// only knew one shape would call a live conversation empty on the other.
//
// It is wider than it needs to be, and deliberately so: a session running `grep claude` is read as a
// runtime for as long as the grep lasts. The two mistakes are not the same size. Reading a live
// conversation as empty invites a drain, a restart or a reclaim over the top of it; reading an empty
// container as busy holds it a little longer, and the next listing corrects itself.
func runtimeAmong(dump string) bool {
	// The zero byte the kernel puts between one process's arguments becomes a space, so a command
	// line splits into the words it was made of rather than into one long word that names nothing.
	dump = strings.ReplaceAll(dump, "\x00", " ")
	for line := range strings.SplitSeq(dump, "\n") {
		for _, word := range strings.Fields(line) {
			if path.Base(word) == RuntimeBinary {
				return true
			}
		}
	}
	return false
}

// Close removes the session's container.
func (s *dockerSandbox) Close(ctx context.Context) error {
	if err := exec.CommandContext(ctx, "docker", "rm", "-f", s.name).Run(); err != nil {
		return fmt.Errorf("sandbox: remove container: %w", err)
	}
	return nil
}

// cpuShares turns a processor request into the weight the runtime shares processors out by.
//
// One whole processor is 1024, which is the runtime's own default, so a sandbox asking for one
// processor is weighted exactly as every sandbox was before requests existed. It is the same
// arithmetic kubernetes does when it turns a pod's processor request into a weight.
func cpuShares(percent int) int {
	if percent <= 0 {
		return 0
	}
	shares := percent * 1024 / capacity.OneProcessor
	// The runtime refuses a weight below two.
	if shares < 2 {
		return 2
	}
	return shares
}

// runArgs is the whole `docker run` for a session's sandbox. It is a function of its own so a test can
// read what the daemon would be asked for without a daemon: whether the sandbox joins a network at all
// is the difference between a session that can drive the system and one that cannot.
func (d DockerProvider) runArgs(name string, cfg Config, kept []Mount) []string {
	args := []string{"run", "--detach", "--name", name, "--tmpfs", secretsMount()}
	if shares := cpuShares(cfg.Request.Processor); shares > 0 {
		// The processor half of what this sandbox asked for, in the units the runtime shares
		// processors out in. It binds only when the machine is contended, which is the difference
		// between a request and a limit: a sandbox alone on an idle machine still gets all of it.
		args = append(args, "--cpu-shares", strconv.Itoa(shares))
	}
	if d.Memory != "" {
		// Swap is capped with it, at the same figure. Told a memory limit and nothing else, the
		// daemon allows swap of the same size again, so a session may take twice what the operator
		// said, and reach it by thrashing. Equal figures make the limit mean what it says.
		args = append(args, "--memory", d.Memory, "--memory-swap", d.Memory)
	}
	if network := d.networkFor(cfg); network != "" {
		args = append(args, "--network", network)
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

// networkFor is the one network this sandbox joins, decided here because a sandbox keeps what it was
// created with. There is no promotion: a container that is already running joins nothing later, so a
// session that has to reach the system has to be born able to.
//
// A session joins the session network, where the control plane is and nothing else of the system's is.
// The driver joins the system's own network when the operator named one, because that is the wider
// access it was given deliberately, and falls back to the session network, which is all it needs.
func (d DockerProvider) networkFor(cfg Config) string {
	if cfg.Driver && d.Network != "" {
		return d.Network
	}
	return d.SessionNetwork
}

// secretsMount is the memory backed directory a file projected secret lands in, on every sandbox
// whether or not the workspace has set one. Created with the container rather than on demand,
// because a mount is a create time decision and the alternative is that the first workspace to mount
// a secret needs a fresh container to get the directory.
//
// It is owned by the sandbox user and shut to everybody else. Without the owner it belongs to root,
// and the system, which writes into it as the sandbox's own user, is refused.
func secretsMount() string {
	return fmt.Sprintf("%s:mode=0700,uid=%d,gid=%d", SecretsPath, UserID, UserID)
}

// BuildLabel is where a sandbox image records which build of the system it was made from. `make
// sandbox-image` stamps it; anything reading it treats an image without one as saying nothing.
const BuildLabel = "com.quaycrew.build"

// ImageBuild is the build a sandbox image was made from, read from its label, and empty when there is
// no answer to be had: no image name, no daemon, no image pulled yet, or an image made before this
// was stamped.
//
// Empty is deliberately not a verdict. A system that cannot see which build its image came from should
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
