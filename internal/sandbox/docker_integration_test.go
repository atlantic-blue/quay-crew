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

	box, err := sandbox.DockerProvider{Image: "busybox:latest"}.Create(ctx, "itest-1")
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

// TestDockerProviderDeliversEnv proves the mechanism the subscription token rides on: a value put in
// Spec.Env reaches the process running inside the sandbox. This needs only Docker, so it runs in CI
// and guards the token delivery without any subscription.
func TestDockerProviderDeliversEnv(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	box, err := sandbox.DockerProvider{Image: "busybox:latest"}.Create(ctx, "itest-env")
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
