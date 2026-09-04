package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
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

// Remove has nothing to do: a local sandbox is the host, and there is no container to take down.
func (LocalProvider) Remove(context.Context, string) error { return nil }

// Stranded is empty for the same reason: nothing was created, so nothing strays.
func (LocalProvider) Stranded(context.Context) ([]string, error) { return nil, nil }

// Attached is false because there is nothing to attach to. A local sandbox is the host, so the
// conversation an operator opens is a process on their own machine rather than one inside a
// container, and no container exists for the system to reclaim either.
func (LocalProvider) Attached(context.Context, string) (bool, error) { return false, nil }

// RuntimeRunning is false for the same reason Attached is: a local sandbox is the host. A command
// runs as the operator's own process rather than inside a container of this session's, so there is no
// process table belonging to one session to read, and no container for anybody to take away either.
func (LocalProvider) RuntimeRunning(context.Context, string) (bool, error) { return false, nil }

type localSandbox struct{ env []string }

var _ Sandbox = localSandbox{}

// cmdProcess adapts an *exec.Cmd to Process. Shared by the local and docker backends.
type cmdProcess struct {
	cmd    *exec.Cmd
	stdout io.Reader
	stderr *tail
}

func (p *cmdProcess) Stdout() io.Reader { return p.stdout }
func (p *cmdProcess) Wait() error       { return p.cmd.Wait() }
func (p *cmdProcess) Stderr() string    { return p.stderr.String() }

// stderrTail is how much of a failing command's error stream is kept. Enough to carry the reason,
// bounded so a command that fails by writing forever cannot take the control plane with it.
const stderrTail = 8 << 10

// newCmdProcess starts cmd with its error stream captured into a bounded tail.
func newCmdProcess(cmd *exec.Cmd, stdout io.Reader) *cmdProcess {
	kept := &tail{limit: stderrTail}
	cmd.Stderr = kept
	return &cmdProcess{cmd: cmd, stdout: stdout, stderr: kept}
}

// tail keeps the last limit bytes written to it and throws away what came before, so a command that
// writes megabytes before dying still costs one buffer. The reason a command failed is at the end.
type tail struct {
	mu    sync.Mutex
	limit int
	kept  []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.kept = append(t.kept, p...)
	if len(t.kept) > t.limit {
		t.kept = t.kept[len(t.kept)-t.limit:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.kept))
}

// Exec runs spec.Argv on the host and streams its stdout.
func (l localSandbox) Exec(ctx context.Context, spec Spec) (Process, error) {
	if len(spec.Argv) == 0 {
		return nil, fmt.Errorf("sandbox: empty argv")
	}
	cmd := exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = spec.Workdir
	// The sandbox's own environment first, then the command's, so a per exec value wins the way it
	// would inside a container.
	if len(l.env) > 0 || len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), l.env...)
		cmd.Env = append(cmd.Env, spec.Env...)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("sandbox: stdout pipe: %w", err)
	}
	// Built before Start, because that is what attaches the error stream. Setting it afterwards
	// compiles, runs, and captures nothing at all.
	proc := newCmdProcess(cmd, stdout)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sandbox: start %s: %w", spec.Argv[0], err)
	}
	return proc, nil
}

// Close is a no op for the host backend.
func (localSandbox) Close(context.Context) error { return nil }

// Existing is false, because a local sandbox is the host and there is no container holding this
// session's work.
//
// It is not a gap in the stopgap. The system reaches into a session to run git in the directory its
// work is in, and it names that directory the way a container sees it, which on the host means a
// path belonging to somebody else or to nothing. Answering true here would run git somewhere the
// work is not. A local system says so instead, and the reason it writes still carries the path,
// which on the host is the path an operator opens.
func (LocalProvider) Existing(context.Context, string) (Sandbox, bool, error) {
	return nil, false, nil
}
