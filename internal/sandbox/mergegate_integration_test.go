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

	"github.com/atlantic-blue/krewe/internal/hook"
	"github.com/atlantic-blue/krewe/internal/sandbox"
)

// The shipped merge gate, written out and mounted the way the system writes and mounts it, and run
// inside the real sandbox image the way the model runtime runs it.
//
// The unit tests in the hook's own module say what it decides. They cannot say that the file a
// sandbox mounts is an executable this image can run: the entry point is built rather than
// committed, so a binary built for the wrong processor loads here and nowhere else, and the failure
// is a hook that never refuses anything. That reads exactly like a hook that approves.
//
// QC_TEST_SANDBOX_IMAGE names the image, and without one there is nothing to prove against, so this
// says so rather than passing.
func TestTheShippedMergeGateRefusesAMergeInsideTheRealSandboxImage(t *testing.T) {
	image := os.Getenv("QC_TEST_SANDBOX_IMAGE")
	if image == "" {
		t.Skip("set QC_TEST_SANDBOX_IMAGE to the system's sandbox image to run this")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	root := t.TempDir()
	shipped, err := hook.Load("../../hooks")
	if err != nil {
		t.Fatalf("loading the shipped hooks: %v (run `make hooks` first: the entry point is built)", err)
	}
	var gate hook.Hook
	for _, one := range shipped {
		if one.Name == "merge-gate" {
			gate = one
		}
	}
	if gate.Name == "" {
		t.Fatal("this build ships no merge gate, so this test proves nothing")
	}
	if err := sandbox.WriteHooks(root, shipped); err != nil {
		t.Fatalf("WriteHooks: %v", err)
	}

	box, err := sandbox.DockerProvider{Image: image}.Create(ctx, sandbox.Config{
		ID:     "itest-merge-gate-1",
		Mounts: []sandbox.Mount{{Source: root, Target: sandbox.HooksPath, ReadOnly: true}},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = box.Close(context.Background()) })

	command := sandbox.HooksPath + "/" + gate.Name + "/" + gate.Events[0].Entry

	for _, one := range []struct {
		name    string
		payload string
		refused bool
	}{
		{
			name:    "the merge every brief says is the operator's",
			payload: `{"tool_name":"Bash","tool_input":{"command":"gh pr merge 12 --squash"}}`,
			refused: true,
		},
		{
			// The half that decides whether the gate is worth mounting. Every role in this system
			// pushes a branch on every slice, so a wrong refusal here stops the system delivering.
			name:    "the push every role does on every slice",
			payload: `{"tool_name":"Bash","tool_input":{"command":"git push -u origin work"}}`,
			refused: false,
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			proc, err := box.Exec(ctx, sandbox.Spec{Argv: []string{
				"sh", "-c", "printf '%s' '" + one.payload + "' | " + command,
			}})
			if err != nil {
				t.Fatalf("Exec: %v", err)
			}
			out, err := io.ReadAll(proc.Stdout())
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			waited := proc.Wait()

			// What a binary that cannot run says on the way out. Any of these means the gate never
			// decided anything, and a gate that never runs looks exactly like one that approves.
			for _, broken := range []string{
				"exec format error", "cannot execute binary file", "Permission denied", "not found",
			} {
				if strings.Contains(proc.Stderr(), broken) || strings.Contains(string(out), broken) {
					t.Fatalf("the merge gate failed to load inside the sandbox image: %s\n%s",
						proc.Stderr(), out)
				}
			}

			if one.refused {
				if waited == nil {
					t.Fatalf("the gate let %q through inside the image, and it merges", one.payload)
				}
				if !strings.Contains(proc.Stderr(), "open a pull request") {
					t.Errorf("the gate refused and told the session nothing it can act on: %s", proc.Stderr())
				}
				return
			}
			if waited != nil {
				t.Fatalf("the gate refused a push inside the image: %v\n%s", waited, proc.Stderr())
			}
		})
	}
}

// The settings the system renders have to name the gate on the event that fires before a command runs,
// or every scenario above passes against a hook the runtime never calls.
func TestTheRenderedSettingsBindTheMergeGateBeforeACommandRuns(t *testing.T) {
	root := t.TempDir()
	shipped, err := hook.Load("../../hooks")
	if err != nil {
		t.Fatalf("loading the shipped hooks: %v (run `make hooks` first)", err)
	}
	if err := sandbox.WriteHooks(root, shipped); err != nil {
		t.Fatalf("WriteHooks: %v", err)
	}
	rendered, err := os.ReadFile(filepath.Join(root, hook.SettingsFile))
	if err != nil {
		t.Fatalf("the settings file was not written: %v", err)
	}
	var document struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(rendered, &document); err != nil {
		t.Fatalf("the settings file is not valid json: %v\n%s", err, rendered)
	}
	bound := false
	for _, group := range document.Hooks["PreToolUse"] {
		if group.Matcher != "Bash" {
			continue
		}
		for _, one := range group.Hooks {
			if strings.Contains(one.Command, "merge-gate") {
				bound = true
			}
		}
	}
	if !bound {
		t.Fatalf("nothing binds the merge gate to a Bash command about to run:\n%s", rendered)
	}
}
