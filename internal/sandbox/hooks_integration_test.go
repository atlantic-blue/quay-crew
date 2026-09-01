//go:build integration

package sandbox_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/hook"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

// A hook is only real if the runtime can run it. Everything else can be right while the command in
// the settings file points at a path that is not in the container, or at a file without its
// executable bit, and both failures are silent: the runtime reports a hook that did not run, from
// inside a container, and nothing outside says why.
//
// So this mounts what the system actually writes into a real container and runs the command the
// settings file actually names.
func TestAHookTheSettingsFileNamesIsRunnableInsideARealContainer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// What the system writes on the host, through the same function the control plane calls.
	root := t.TempDir()
	hooks := []hook.Hook{{
		Name: "git-approval", Version: 1, Summary: "Refuses a commit nobody approved.",
		Events: []hook.Binding{{On: "PreToolUse", Matcher: "Bash", Entry: "bin/hook", TimeoutSeconds: 5}},
		Files: []hook.File{
			{Path: "hook.yaml", Body: []byte("name: git-approval\n")},
			{Path: "bin/hook", Body: []byte("#!/bin/sh\necho refused-by-the-hook\n"), Executable: true},
		},
	}}
	if err := sandbox.WriteHooks(root, hooks); err != nil {
		t.Fatalf("WriteHooks: %v", err)
	}

	// Read the command out of the settings file rather than building the path here, so what is run is
	// what the runtime would be told to run.
	rendered, err := os.ReadFile(filepath.Join(root, hook.SettingsFile))
	if err != nil {
		t.Fatalf("the settings file was not written: %v", err)
	}
	var document struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(rendered, &document); err != nil {
		t.Fatalf("the settings file is not valid json: %v\n%s", err, rendered)
	}
	groups := document.Hooks["PreToolUse"]
	if len(groups) != 1 || len(groups[0].Hooks) != 1 {
		t.Fatalf("the settings did not bind the hook: %s", rendered)
	}
	command := groups[0].Hooks[0].Command

	box, err := sandbox.DockerProvider{Image: "busybox:latest"}.Create(ctx, sandbox.Config{
		ID: "itest-hooks-1",
		Mounts: []sandbox.Mount{
			{Source: root, Target: sandbox.HooksPath, ReadOnly: true},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = box.Close(context.Background()) })

	// The command the settings file names, run the way the runtime runs it: as an executable, by
	// absolute path, with nothing helping it along.
	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{command}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	out, err := io.ReadAll(proc.Stdout())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := proc.Wait(); err != nil {
		t.Fatalf("the command the settings file names did not run inside the container: %v: %s",
			err, proc.Stderr())
	}
	if !strings.Contains(string(out), "refused-by-the-hook") {
		t.Fatalf("the hook ran and said %q", out)
	}
}

// A session that can edit the file binding its own constraints is a session with no constraints.
func TestTheHooksAreMountedReadOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	root := t.TempDir()
	if err := sandbox.WriteHooks(root, []hook.Hook{{
		Name: "guard", Version: 1, Summary: "x",
		Events: []hook.Binding{{On: "Stop", Entry: "bin/hook"}},
		Files: []hook.File{
			{Path: "bin/hook", Body: []byte("#!/bin/sh\nexit 0\n"), Executable: true},
		},
	}}); err != nil {
		t.Fatalf("WriteHooks: %v", err)
	}

	box, err := sandbox.DockerProvider{Image: "busybox:latest"}.Create(ctx, sandbox.Config{
		ID: "itest-hooks-2",
		Mounts: []sandbox.Mount{
			{Source: root, Target: sandbox.HooksPath, ReadOnly: true},
		},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = box.Close(context.Background()) })

	proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{
		"sh", "-c", "echo tampered > " + sandbox.HooksPath + "/" + hook.SettingsFile,
	}})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	_, _ = io.Copy(io.Discard, proc.Stdout())
	if err := proc.Wait(); err == nil {
		t.Fatal("a session rewrote the file binding its own hooks, so it is under whatever it likes")
	}
}
