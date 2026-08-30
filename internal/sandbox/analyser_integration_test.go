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
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// The shipped analyser, run inside the real sandbox image, the way the runtime runs it.
//
// This is the test that would have caught the defect it was written after. Back when the hook was
// TypeScript the entry point was named bin/hook, and node decides whether to strip types by the file
// extension rather than by the flag, so it was read as plain JavaScript and died on its own type
// imports:
//
//	SyntaxError: Unexpected identifier 'AnalysisFacts'
//
// Everything else was green. The hook loaded, the manifest validated, the settings bound it, the
// mount was right. It failed on the first message, inside a container, with nothing outside saying so.
//
// The image is the crew's own sandbox image rather than busybox, because what is being proved is that
// this image can run this hook. QC_TEST_SANDBOX_IMAGE names it; without one there is nothing to prove
// against and the test says so rather than passing.
//
// Both tests in this file read QC_SANDBOX_IMAGE until 16 August 2026. Nothing sets that: it is the
// control plane's own variable, and every other integration test here reads QC_TEST_SANDBOX_IMAGE,
// which is the one the pipeline sets. So both skipped on every pipeline run since they were written,
// and a skipped test reads exactly like a passing one. These two are the only check that the hook a
// sandbox mounts actually runs inside it.
func TestTheShippedAnalyserRunsInsideTheRealSandboxImage(t *testing.T) {
	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to the crew's sandbox image to run this")
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
	// The hook exits 0 by design, so the exit code alone proves little. These are what a binary that
	// cannot run says on the way out, and the first matters most now: the entry point is built rather
	// than committed, so a build for the wrong processor lands here and nowhere else. The node errors
	// stay because a hook written in something else would land on them again.
	for _, broken := range []string{
		"exec format error", "cannot execute binary file", "Permission denied",
		"SyntaxError", "Cannot find module", "ERR_MODULE_NOT_FOUND",
	} {
		if strings.Contains(proc.Stderr(), broken) || strings.Contains(string(out), broken) {
			t.Fatalf("the analyser failed to load inside the sandbox image: %s\n%s", proc.Stderr(), out)
		}
	}
}

// The child model call must keep the credential the sandbox gave the session.
//
// The analyser drops every CLAUDE_ variable before running its child, so the child does not inherit
// what the running session set for itself. On a machine with a logged in install that costs nothing,
// because the credential is a file. A quay sandbox has no credentials file: the workspace's
// subscription arrives as CLAUDE_CODE_OAUTH_TOKEN, and dropping it left the child unable to
// authenticate.
//
// Nothing looked wrong. The hook ran in under a second, exited 0 and let the message through, because
// it fails open by design. The only sign was the word "no answer" in a file in /tmp.
//
// A stub on the path stands in for the model, so this needs no subscription and still proves the
// variable arrives.
func TestTheAnalysersChildKeepsTheSubscriptionToken(t *testing.T) {
	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to the crew's sandbox image to run this")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	root := t.TempDir()
	shipped, err := hook.Load("../../hooks")
	if err != nil {
		t.Fatalf("loading the shipped hooks: %v", err)
	}
	if err := sandbox.WriteHooks(root, shipped); err != nil {
		t.Fatalf("WriteHooks: %v", err)
	}

	const sentinel = "sk-ant-oat-sentinel-value"
	box, err := sandbox.DockerProvider{Image: image}.Create(ctx, sandbox.Config{
		ID:     "itest-analyser-2",
		Env:    []string{"CLAUDE_CODE_OAUTH_TOKEN=" + sentinel},
		Mounts: []sandbox.Mount{{Source: root, Target: sandbox.HooksPath, ReadOnly: true}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = box.Close(context.Background()) })

	// A stub claude that records the token it was given and answers with something the hook will
	// print, so a failure tells the two cases apart: the child never ran, or it ran without the token.
	stub := `mkdir -p /tmp/stub && cat > /tmp/stub/claude <<'SH'
#!/bin/sh
printf '%s' "${CLAUDE_CODE_OAUTH_TOKEN:-NO-TOKEN}" > /tmp/stub/seen
cat > /dev/null
echo "goal: something"
SH
chmod +x /tmp/stub/claude
echo '{"prompt":"fix the flaky test","cwd":"/home/agent/workspace"}' | PATH=/tmp/stub:$PATH ` +
		sandbox.HooksPath + `/prompt-analyser/bin/hook
echo "---seen---"
cat /tmp/stub/seen`

	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c", stub}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	out, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("the analyser did not run: %v\nstderr: %s\nstdout: %s", err, proc.Stderr(), out)
	}
	if strings.Contains(string(out), "NO-TOKEN") {
		t.Fatal("the child model call ran without the subscription token, so it cannot authenticate and the hook silently produces nothing")
	}
	if !strings.Contains(string(out), sentinel) {
		t.Fatalf("the child was never given the token: %s", out)
	}
}

// The child model call runs on the second name when the first one is not there, which is every
// sandbox.
//
// Claude Code removes CLAUDE_CODE_OAUTH_TOKEN from the environment of every process it starts, by
// that name and no other, and a hook is one of those processes. The test above hands the container
// the first name, which is what `docker exec` does and what made the hook look healthy: run by hand
// it worked, and run by a session it never once did. This one hands the container only the second
// name, the way the crew now writes it, and proves the child is given a credential anyway.
//
// A stub on the path stands in for the model, so this needs no subscription.
func TestTheAnalysersChildRunsOnTheTokenTheSessionCannotStrip(t *testing.T) {
	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to the crew's sandbox image to run this")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	root := t.TempDir()
	shipped, err := hook.Load("../../hooks")
	if err != nil {
		t.Fatalf("loading the shipped hooks: %v", err)
	}
	if err := sandbox.WriteHooks(root, shipped); err != nil {
		t.Fatalf("WriteHooks: %v", err)
	}

	const sentinel = "sk-ant-oat-second-name-sentinel"
	box, err := sandbox.DockerProvider{Image: image}.Create(ctx, sandbox.Config{
		ID: "itest-analyser-3",
		// Only the second name, and the first one emptied, which is the environment a hook actually
		// runs in: the session holds the token and what it starts does not.
		Env:    []string{model.ModelTokenEnv + "=" + sentinel},
		Mounts: []sandbox.Mount{{Source: root, Target: sandbox.HooksPath, ReadOnly: true}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = box.Close(context.Background()) })

	stub := `mkdir -p /tmp/stub && cat > /tmp/stub/claude <<'SH'
#!/bin/sh
printf '%s' "${CLAUDE_CODE_OAUTH_TOKEN:-NO-TOKEN}" > /tmp/stub/seen
cat > /dev/null
echo "goal: something"
SH
chmod +x /tmp/stub/claude
echo '{"prompt":"fix the flaky test","cwd":"/home/agent/workspace"}' | PATH=/tmp/stub:$PATH ` +
		sandbox.HooksPath + `/prompt-analyser/bin/hook
echo "---seen---"
cat /tmp/stub/seen`

	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{"sh", "-c", stub}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	out, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("the analyser did not run: %v\nstderr: %s\nstdout: %s", err, proc.Stderr(), out)
	}
	if strings.Contains(string(out), "NO-TOKEN") {
		t.Fatalf("the child ran with no credential, so every message goes unanalysed: %s", out)
	}
	if !strings.Contains(string(out), sentinel) {
		t.Fatalf("the second name never reached the child as the name the command line reads: %s", out)
	}
	// The analysis has to actually come back, because a hook that authenticates and then prints
	// nothing is the same silence from the session's side.
	if !strings.Contains(string(out), "goal: something") {
		t.Fatalf("the hook authenticated and handed the session nothing: %s", out)
	}
}
