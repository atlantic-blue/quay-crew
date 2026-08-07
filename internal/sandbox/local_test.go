package sandbox

import (
	"context"
	"io"
	"strings"
	"testing"
)

// TestALocalProcessKeepsWhatItSaidAboutFailing. Nothing read the error stream at all, so whatever a
// command said about why it could not run went nowhere and every failure arrived as an exit status.
func TestALocalProcessKeepsWhatItSaidAboutFailing(t *testing.T) {
	box, err := LocalProvider{}.Create(context.Background(), Config{ID: "s1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	proc, err := box.Exec(context.Background(), Spec{
		Argv: []string{"sh", "-c", "echo the reason it failed >&2; exit 3"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	// Standard output is drained first, the way a caller reading a stream does, so this is the order
	// the runner actually uses.
	if _, err := io.Copy(io.Discard, proc.Stdout()); err != nil {
		t.Fatalf("draining stdout: %v", err)
	}
	if err := proc.Wait(); err == nil {
		t.Fatal("a command that exited 3 reported success")
	}
	if got := proc.Stderr(); got != "the reason it failed" {
		t.Fatalf("the process kept %q, want what the command said", got)
	}
}

// TestALocalProcessKeepsOnlyTheTailOfALoudFailure: a command that fails by writing forever must not
// take the control plane with it, and the reason is at the end anyway.
func TestALocalProcessKeepsOnlyTheTailOfALoudFailure(t *testing.T) {
	box, err := LocalProvider{}.Create(context.Background(), Config{ID: "s1"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	proc, err := box.Exec(context.Background(), Spec{
		Argv: []string{"sh", "-c", "i=0; while [ $i -lt 4000 ]; do echo noise line $i >&2; i=$((i+1)); done; echo the last word >&2; exit 1"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if _, err := io.Copy(io.Discard, proc.Stdout()); err != nil {
		t.Fatalf("draining stdout: %v", err)
	}
	_ = proc.Wait()

	kept := proc.Stderr()
	if len(kept) > stderrTail {
		t.Fatalf("kept %d bytes, want at most %d", len(kept), stderrTail)
	}
	if !strings.HasSuffix(kept, "the last word") {
		t.Fatalf("the tail does not end with what the command said last: %q", kept[max(0, len(kept)-80):])
	}
}
