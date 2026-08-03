//go:build integration

package sandbox_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// TestDockerProvider creates a real per session container, execs a command inside it, reads the
// output back, and tears it down, validating the Docker backend end to end.
func TestDockerProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	box, err := sandbox.DockerProvider{Image: "busybox:latest"}.Create(ctx, sandbox.Config{ID: "itest-1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = box.Close(context.Background()) })

	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"echo", "hi from the sandbox"}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	out, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if strings.TrimSpace(string(out)) != "hi from the sandbox" {
		t.Fatalf("stdout = %q, want 'hi from the sandbox'", string(out))
	}

	if err := box.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestDockerProviderKeepsStateAcrossContainers is the assertion this whole design exists for: a
// session writes something, its container is destroyed, a new container is created for the same
// session, and what it wrote is still there. Before the state was mounted in, removing the container
// destroyed the conversation the database still held a handle to, permanently.
func TestDockerProviderKeepsStateAcrossContainers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Dir and Host are the same path because this test runs on the host, not inside the control
	// plane's container, so the two views of the directory are one directory.
	data := t.TempDir()
	provider := sandbox.DockerProvider{Image: "busybox:latest", Storage: sandbox.Storage{Dir: data, Host: data}}
	config := sandbox.Config{ID: "itest-durable", Workspace: "ws-durable", Project: "prj-durable"}

	first, err := provider.Create(ctx, config)
	if err != nil {
		t.Fatalf("create the first sandbox: %v", err)
	}
	t.Cleanup(func() { _ = first.Close(context.Background()) })

	// Write into both mounts: the conversation store the model keeps, and the project's files.
	for _, dir := range []string{sandbox.ConversationPath, sandbox.WorkingPath} {
		proc, err := first.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c", "echo remembered > " + dir + "/note"}})
		if err != nil {
			t.Fatalf("write into %s: %v", dir, err)
		}
		if _, err := io.ReadAll(proc.Stdout()); err != nil {
			t.Fatalf("read the write's output: %v", err)
		}
		if err := proc.Wait(); err != nil {
			t.Fatalf("write into %s exited: %v", dir, err)
		}
	}

	if err := first.Close(ctx); err != nil {
		t.Fatalf("destroy the first sandbox: %v", err)
	}

	second, err := provider.Create(ctx, config)
	if err != nil {
		t.Fatalf("create the replacement sandbox: %v", err)
	}
	t.Cleanup(func() { _ = second.Close(context.Background()) })

	for _, dir := range []string{sandbox.ConversationPath, sandbox.WorkingPath} {
		proc, err := second.Exec(ctx, sandbox.Spec{Argv: []string{"cat", dir + "/note"}})
		if err != nil {
			t.Fatalf("read %s back: %v", dir, err)
		}
		out, err := io.ReadAll(proc.Stdout())
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if err := proc.Wait(); err != nil {
			t.Fatalf("%s did not survive its container: %v", dir, err)
		}
		if strings.TrimSpace(string(out)) != "remembered" {
			t.Fatalf("%s/note reads %q after the container was replaced, want 'remembered'", dir, string(out))
		}
	}

	// And a different session in the same project reads the same working directory, which is what
	// makes it the project's shared context rather than one thread's scratch space.
	sibling, err := provider.Create(ctx, sandbox.Config{ID: "itest-sibling", Workspace: config.Workspace, Project: config.Project})
	if err != nil {
		t.Fatalf("create a sibling session's sandbox: %v", err)
	}
	t.Cleanup(func() { _ = sibling.Close(context.Background()) })

	proc, err := sibling.Exec(ctx, sandbox.Spec{Argv: []string{"cat", sandbox.WorkingPath + "/note"}})
	if err != nil {
		t.Fatalf("read the project's files from the sibling: %v", err)
	}
	out, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("the sibling session cannot see the project's files: %v", err)
	}
	if strings.TrimSpace(string(out)) != "remembered" {
		t.Fatalf("the sibling session reads %q from the project, want 'remembered'", string(out))
	}
}

// TestDockerProviderDeliversEnv proves the mechanism the subscription token rides on: a value put in
// Spec.Env reaches the process running inside the sandbox. This needs only Docker, so it runs in CI
// and guards the token delivery without any subscription.
func TestDockerProviderDeliversEnv(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	box, err := sandbox.DockerProvider{Image: "busybox:latest"}.Create(ctx, sandbox.Config{ID: "itest-env"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = box.Close(context.Background()) })

	proc, err := box.Exec(ctx, sandbox.Spec{
		Argv: []string{"printenv", "CLAUDE_CODE_OAUTH_TOKEN"},
		Env:  []string{"CLAUDE_CODE_OAUTH_TOKEN=tok-from-secret"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	out, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if strings.TrimSpace(string(out)) != "tok-from-secret" {
		t.Fatalf("env in sandbox = %q, want 'tok-from-secret'", string(out))
	}
}
