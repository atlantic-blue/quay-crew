package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/hook"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

// The process gate holds one sentence: a session cannot end the operator's containers or the
// operator's terminal without asking first. These drive the real control plane, the real store and
// the real hooks directory, and say what actually holds a session to it.
//
// The entry points are built rather than committed, so `make hooks` comes first. A failure here that
// names a missing entry point means that step was skipped.

// The state before, and the reason the gate is seeded rather than offered. The machine that lost
// every pane at 13:41 was a machine nobody had attached anything to.
func TestASystemNobodyHasSetUpIsUnderNoProcessGate(t *testing.T) {
	server, workspace := fresh(t)
	ctx := context.Background()

	held, err := server.ListHooks(ctx, &quaycrewv1.ListHooksRequest{Workspace: workspace})
	if err != nil {
		t.Fatalf("ListHooks: %v", err)
	}
	for _, one := range held.GetHooks() {
		if one.GetName() == "process-gate" {
			t.Fatal("a system that was never seeded is under the gate, so this test proves nothing about seeding")
		}
	}
}

func TestAFreshSystemPutsEverySessionUnderTheProcessGate(t *testing.T) {
	server, workspace := fresh(t)
	ctx := context.Background()

	// The directory the image carries, so a manifest that stopped loading fails here.
	server.SeedHooks(ctx, "../../hooks", slog.New(slog.DiscardHandler))

	held, err := server.ListHooks(ctx, &quaycrewv1.ListHooksRequest{Workspace: workspace})
	if err != nil {
		t.Fatalf("ListHooks: %v", err)
	}
	for _, one := range held.GetHooks() {
		if one.GetName() != "process-gate" {
			continue
		}
		if !one.GetSystem() {
			t.Errorf("the workspace holds the gate as its own, so a workspace made later would not have it")
		}
		return
	}
	t.Fatalf("the workspace is under no process gate: %+v", held.GetHooks())
}

// The wiring, end to end on this machine: the control plane writes the gate into a session's own
// directory, binds it to the event the runtime raises, and the file it wrote refuses a command.
//
// Each half alone proves nothing. The hook's own module says what it decides, from Go values. This
// says the thing a session actually runs under decides the same way, so a gate that was mounted and
// never bound, or bound and never built, fails here rather than approving of everything in silence.
func TestASessionsOwnCopyOfTheProcessGateRefusesTheCommand(t *testing.T) {
	server, workspace := fresh(t)
	ctx := context.Background()
	server.SeedHooks(ctx, "../../hooks", slog.New(slog.DiscardHandler))

	project, err := server.CreateProject(ctx, &quaycrewv1.CreateProjectRequest{
		Workspace: workspace, Name: "transcript",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := server.Dispatch(ctx, &quaycrewv1.DispatchRequest{
		Project: project.GetProject().GetId(), Text: "clear some room on this machine",
	}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	provider, ok := server.provider.(*sandbox.FakeProvider)
	if !ok || len(provider.Created) == 0 {
		t.Fatal("no sandbox was made")
	}
	mounted := false
	for _, mount := range provider.Created[0].Mounts {
		if mount.Target != sandbox.HooksPath {
			continue
		}
		mounted = true
		if !mount.ReadOnly {
			t.Error("the hooks are mounted writable, so a session can edit the gate it is held to")
		}
	}
	if !mounted {
		t.Fatalf("the sandbox has no mount at %s: %+v", sandbox.HooksPath, provider.Created[0].Mounts)
	}

	dir, canWrite := server.storage.WorkspaceHooksDir(workspace)
	if !canWrite {
		t.Fatal("the workspace has nowhere to write its hooks")
	}
	entry := filepath.Join(dir, "process-gate", "bin", "hook")
	if _, err := os.Stat(entry); err != nil {
		t.Fatalf("the session carries no entry point at %s: %v (run `make hooks` first)", entry, err)
	}

	// The binding. PreToolUse for Bash, because that is where a session runs a shell command.
	body, err := os.ReadFile(filepath.Join(dir, hook.SettingsFile)) //nolint:gosec // a path this test built, under its own temporary directory
	if err != nil {
		t.Fatalf("no settings file was written: %v", err)
	}
	var document struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("the settings file is not valid json: %v", err)
	}
	bound := ""
	for _, group := range document.Hooks["PreToolUse"] {
		for _, run := range group.Hooks {
			if !strings.Contains(run.Command, "/process-gate/") {
				continue
			}
			if group.Matcher != "Bash" {
				t.Errorf("the gate is bound for %q, and the command it refuses is run with Bash", group.Matcher)
			}
			bound = run.Command
		}
	}
	if bound == "" {
		t.Fatalf("nothing binds the process gate to PreToolUse, so it is never called:\n%s", body)
	}

	// The settings name the path inside a sandbox, and the same file sits under the workspace's own
	// directory on this machine, which is what the mount above carries in. So the binding is checked
	// against the sandbox path, and the file is run from the host path.
	if want := sandbox.HooksPath + "/process-gate/bin/hook"; bound != want {
		t.Errorf("the settings run %q and the mount carries %q, so the runtime finds nothing", bound, want)
	}

	// And now the file the runtime would run, run the way the runtime runs it.
	for _, one := range []struct {
		name    string
		command string
		refused bool
	}{
		{name: "the terminal", command: "tmux kill-server", refused: true},
		{name: "the containers", command: "docker compose down", refused: true},
		{name: "a signal", command: "pkill -9 -f claude", refused: true},
		// The other direction. Ending a job ends the work in the record and signals nothing, and a
		// gate that blocks the product is worse than no gate.
		{name: "ending a job", command: "krewe job stop 31a6d96d", refused: false},
		{name: "the ordinary work", command: "go test -count=1 ./...", refused: false},
	} {
		t.Run(one.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{
				"tool_name":  "Bash",
				"tool_input": map[string]string{"command": one.command},
			})
			if err != nil {
				t.Fatalf("payload: %v", err)
			}
			run := exec.CommandContext(ctx, entry)
			run.Stdin = strings.NewReader(string(payload))
			var said strings.Builder
			run.Stderr = &said
			refused := false
			switch err := run.Run(); {
			case err == nil:
			case exitedWith(err, 2):
				refused = true
			default:
				t.Fatalf("running %s: %v\n%s", entry, err, said.String())
			}
			if refused != one.refused {
				t.Fatalf("the session's own copy of the gate refused: %t, want %t\n%s",
					refused, one.refused, said.String())
			}
		})
	}
}

// exitedWith says whether the command ended with this exit code, which is how a hook answers.
func exitedWith(err error, code int) bool {
	var ended *exec.ExitError
	return errors.As(err, &ended) && ended.ExitCode() == code
}
