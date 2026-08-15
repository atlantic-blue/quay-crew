//go:build integration

package sandbox_test

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/hook"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// The shipped analyser, run inside the real sandbox image, the way the runtime runs it.
//
// This is the test that would have caught the defect it was written after. The entry point was named
// bin/hook, node decides whether to strip types by the file extension rather than by the flag, so it
// was read as plain JavaScript and died on its own type imports:
//
//	SyntaxError: Unexpected identifier 'AnalysisFacts'
//
// Everything else was green. The hook loaded, the manifest validated, the settings bound it, the
// mount was right. It failed on the first message, inside a container, with nothing outside saying so.
//
// The image is the crew's own sandbox image rather than busybox, because what is being proved is that
// this image can run this hook. QC_SANDBOX_IMAGE names it; without one there is nothing to prove
// against and the test says so rather than passing.
func TestTheShippedAnalyserRunsInsideTheRealSandboxImage(t *testing.T) {
	image := os.Getenv("QC_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_SANDBOX_IMAGE to the crew's sandbox image to run this")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	// Written out through the same function the control plane calls, from the hooks this build ships.
	root := t.TempDir()
	shipped, err := hook.Load("../../hooks")
	if err != nil {
		t.Fatalf("loading the shipped hooks: %v", err)
	}
	if len(shipped) == 0 {
		t.Fatal("no shipped hooks, so this test proves nothing")
	}
	if err := sandbox.WriteHooks(root, shipped); err != nil {
		t.Fatalf("WriteHooks: %v", err)
	}

	box, err := sandbox.DockerProvider{Image: image}.Create(ctx, sandbox.Config{
		ID:     "itest-analyser-1",
		Mounts: []sandbox.Mount{{Source: root, Target: sandbox.HooksPath, ReadOnly: true}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = box.Close(context.Background()) })

	var analyser hook.Hook
	for _, one := range shipped {
		if one.Name == "prompt-analyser" {
			analyser = one
		}
	}
	if analyser.Name == "" {
		t.Fatal("the shipped hooks do not include the prompt analyser")
	}
	command := sandbox.HooksPath + "/" + analyser.Name + "/" + analyser.Events[0].Entry

	// The runtime feeds a hook its payload on standard input and reads what it prints. There is no
	// subscription in this container, so the model call comes back empty and the hook fails open,
	// which is the behaviour being asserted: a message always gets through.
	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{
		"sh", "-c", `echo '{"prompt":"fix the flaky test","cwd":"/home/agent/workspace"}' | ` + command,
	}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	out, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("the shipped analyser did not run inside the sandbox image: %v\nstderr: %s\nstdout: %s",
			err, proc.Stderr(), out)
	}
	// A module or syntax failure prints a stack and exits non zero, which the Wait above catches. This
	// catches the quieter version: node printing a complaint and still exiting 0.
	for _, broken := range []string{"SyntaxError", "Cannot find module", "ERR_MODULE_NOT_FOUND"} {
		if strings.Contains(proc.Stderr(), broken) || strings.Contains(string(out), broken) {
			t.Fatalf("the analyser failed to load inside the sandbox image: %s\n%s", proc.Stderr(), out)
		}
	}
}
