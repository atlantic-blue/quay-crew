package console_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/console"
)

// The runner the console actually ships with, driven against a real program rather than a double.
//
// A double proves the bar reacts to output; only running something proves the console can start a
// process, wait for it, and read what it said. The program is a stub on PATH rather than the real
// tool, because what is under test is the running, not what quay prints.
func TestTheToolItselfRunsAndCapturesOutput(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "quay")
	// It answers on standard output and on the error stream, because a refusal comes back on the
	// second one and folding them together is the whole point of using CombinedOutput.
	script := "#!/bin/sh\necho \"said: $*\"\necho \"a refusal\" >&2\nexit 3\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("write the stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// os.Executable answers with the test binary, so this drives the PATH fallback deliberately by
	// asking for a name rather than a path.
	output, err := console.RunNamed(context.Background(), "quay",
		[]string{"workspace", "list"})
	if err == nil {
		t.Fatal("a command that exited 3 came back with no error")
	}
	if !strings.Contains(output, "said: workspace list") {
		t.Errorf("the output does not carry what the command printed: %q", output)
	}
	if !strings.Contains(output, "a refusal") {
		t.Errorf("the output does not carry the error stream, which is where a refusal comes back: %q", output)
	}
}

// A command that will never answer must give the console back rather than freezing it.
func TestTheToolItselfGivesUpOnACommandThatNeverAnswers(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "quay")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nsleep 30\n"), 0o700); err != nil {
		t.Fatalf("write the stub: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := console.RunNamed(ctx, stub, []string{"hang"})
	took := time.Since(started)

	if err == nil {
		t.Fatal("a command that never answers came back with no error")
	}
	// Promptly, not eventually. Killing a program does not close the pipes its children inherited,
	// so without giving up on those pipes this returns only when the child finally exits: the error
	// would be right and the console would have been frozen for thirty seconds getting it.
	if took > 5*time.Second {
		t.Fatalf("giving up took %s, so the console was held by the command's child long past the deadline", took)
	}
}
