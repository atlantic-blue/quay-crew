//go:build integration

package session_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/session"
)

// TestDockerRunsCommand runs a command inside a real container and reads its output back, validating
// the Docker runtime end to end.
func TestDockerRunsCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	proc, err := session.Docker{Image: "busybox:latest"}.Start(ctx, session.Spec{Argv: []string{"echo", "hi from docker"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	out, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if strings.TrimSpace(string(out)) != "hi from docker" {
		t.Fatalf("stdout = %q, want 'hi from docker'", string(out))
	}
}
