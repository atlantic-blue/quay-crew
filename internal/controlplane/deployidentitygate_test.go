package controlplane

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/hook"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/secrets"
	"github.com/atlantic-blue/krewe/internal/store"
)

// The deploy identity rule is a skill, and a skill is a rule a session reads. These drive the real
// control plane, the real store and the real hooks directory, and say what actually holds a session to
// it.
//
// The entry points are built rather than committed, so `make hooks` comes first. A failure here that
// names a missing entry point means that step was skipped.

// The state before, and the reason the gate is seeded rather than offered. A system nobody set up is
// the system this failure happens in, so a constraint waiting to be attached is a constraint that was
// not there.
func TestASystemNobodyHasSetUpIsUnderNoDeployIdentityGate(t *testing.T) {
	server, workspace := fresh(t)
	ctx := context.Background()

	held, err := server.ListHooks(ctx, &quaycrewv1.ListHooksRequest{Workspace: workspace})
	if err != nil {
		t.Fatalf("ListHooks: %v", err)
	}
	for _, one := range held.GetHooks() {
		if one.GetName() == "deploy-identity-gate" {
			t.Fatal("a system that was never seeded is under the gate, so this test proves nothing about seeding")
		}
	}
}

// Nobody imports anything and nobody attaches anything, which is the state every system is in on its
// first day. The pull request that started this was opened by a job in exactly that system.
func TestAFreshSystemPutsEverySessionUnderTheDeployIdentityGate(t *testing.T) {
	server, workspace := fresh(t)
	ctx := context.Background()

	// The directory the image carries, so a manifest that stopped loading fails here.
	server.SeedHooks(ctx, "../../hooks", slog.New(slog.DiscardHandler))

	held, err := server.ListHooks(ctx, &quaycrewv1.ListHooksRequest{Workspace: workspace})
	if err != nil {
		t.Fatalf("ListHooks: %v", err)
	}
	for _, one := range held.GetHooks() {
		if one.GetName() != "deploy-identity-gate" {
			continue
		}
		if !one.GetSystem() {
			t.Errorf("the workspace holds the gate as its own, so a workspace made later would not have it")
		}
		return
	}
	t.Fatalf("the workspace is under no deploy identity gate: %+v", held.GetHooks())
}

// A hook reaches a session two ways at once, and either one alone does nothing: the files mounted, and
// the settings binding them to an event. A gate that is mounted and never called approves of
// everything, and it looks exactly like a gate that is working.
func TestASessionIsBuiltWithTheDeployIdentityGateBoundToACommand(t *testing.T) {
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
		Project: project.GetProject().GetId(), Text: "write the terraform for the transcript service",
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

	// The entry point, in the directory the session's sandbox carries. A binding that names a file
	// that is not there is a gate that fails inside the container with nothing pointing back here.
	dir, canWrite := server.storage.WorkspaceHooksDir(workspace)
	if !canWrite {
		t.Fatal("the workspace has nowhere to write its hooks")
	}
	entry := filepath.Join(dir, "deploy-identity-gate", "bin", "hook")
	if _, err := os.Stat(entry); err != nil {
		t.Errorf("the session carries no entry point at %s: %v (run `make hooks` first)", entry, err)
	}

	// And the binding. PreToolUse for Bash, because that is where a session runs gh.
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
	for _, group := range document.Hooks["PreToolUse"] {
		for _, run := range group.Hooks {
			if !strings.Contains(run.Command, "/deploy-identity-gate/") {
				continue
			}
			if group.Matcher != "Bash" {
				t.Errorf("the gate is bound for %q, and the command it refuses is run with Bash", group.Matcher)
			}
			return
		}
	}
	t.Errorf("nothing binds the deploy identity gate to PreToolUse, so it is never called:\n%s", body)
}

// fresh is a control plane holding nothing, with one workspace in it.
func fresh(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	server := NewServer(Config{
		Store: store.NewMemory(), Runner: &model.FakeRunner{Reply: "ok"},
		Provider: &sandbox.FakeProvider{}, Secrets: secrets.NewMemory(),
		Storage: sandbox.Storage{Dir: dir, Host: dir},
	})
	workspace, err := server.CreateWorkspace(context.Background(), &quaycrewv1.CreateWorkspaceRequest{
		Name: "atlantic-blue",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	return server, workspace.GetWorkspace().GetId()
}
