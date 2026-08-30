package storetest

import (
	"context"
	"errors"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/hook"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// The hook half of the contract, held against both implementations.
//
// A double whose behaviour is looser than the real store manufactures a green suite over a broken
// system, and the stakes are particular here: a hook that does not survive the round trip is a
// constraint the system believes it has.
func runHookConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a hook is imported with its files and its bindings, and comes back whole", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if err := s.ImportHook(ctx, aHook("git-approval", 1)); err != nil {
			t.Fatalf("ImportHook: %v", err)
		}
		got, err := s.GetHook(ctx, "git-approval", 1)
		if err != nil {
			t.Fatalf("GetHook: %v", err)
		}
		if got.Name != "git-approval" || got.Version != 1 || got.Summary == "" {
			t.Fatalf("bad hook: %+v", got.Hook)
		}
		if got.ImportedAt.IsZero() {
			t.Error("the hook came back with no import time, so a listing cannot say when it arrived")
		}
		if len(got.Binaries) != 1 || got.Binaries[0] != "node" {
			t.Errorf("the declared binaries did not survive: %+v", got.Binaries)
		}
		if got.Secrets["GH_TOKEN"] == "" {
			t.Errorf("the named secret and its purpose did not survive: %+v", got.Secrets)
		}

		// The bindings are the whole of what a hook does. Order matters, because the settings file
		// is rendered from them and one that reorders between reads is a diff nobody can review.
		if len(got.Events) != 2 {
			t.Fatalf("want 2 bindings, got %d: %+v", len(got.Events), got.Events)
		}
		if got.Events[0].On != "PreToolUse" || got.Events[0].Matcher != "Bash" {
			t.Errorf("the first binding did not survive: %+v", got.Events[0])
		}
		if got.Events[0].Entry != "bin/hook" || got.Events[0].TimeoutSeconds != 20 {
			t.Errorf("the first binding lost its entry or its timeout: %+v", got.Events[0])
		}
		if got.Events[1].On != "UserPromptSubmit" || got.Events[1].Matcher != "" {
			t.Errorf("the second binding did not survive: %+v", got.Events[1])
		}

		// The executable bit is the one that fails silently: an entry point that arrives without it
		// cannot run, and the failure surfaces inside a container with nothing pointing back here.
		var entry hook.File
		for _, file := range got.Files {
			if file.Path == "bin/hook" {
				entry = file
			}
		}
		if entry.Path == "" {
			t.Fatalf("the entry point did not come back: %+v", got.Files)
		}
		if !entry.Executable {
			t.Error("the entry point lost its executable bit, so it cannot run inside a sandbox")
		}
		if string(entry.Body) != "#!/bin/sh\nexit 0\n" {
			t.Errorf("the entry point's body did not survive: %q", entry.Body)
		}
	})

	t.Run("importing the same version twice is harmless, and importing a different hook as it is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if err := s.ImportHook(ctx, aHook("git-approval", 1)); err != nil {
			t.Fatalf("ImportHook: %v", err)
		}
		// A pull that changed nothing is not an error.
		if err := s.ImportHook(ctx, aHook("git-approval", 1)); err != nil {
			t.Fatalf("importing the same hook again: %v", err)
		}

		// A version that changed underneath a workspace is a constraint changing under a session
		// already running under it, which is the one thing pinning exists to stop.
		changed := aHook("git-approval", 1)
		changed.Files = append(changed.Files, hook.File{Path: "extra", Body: []byte("different")})
		if err := s.ImportHook(ctx, changed); !errors.Is(err, store.ErrHookChanged) {
			t.Fatalf("importing a changed hook at the same version returned %v, want ErrHookChanged", err)
		}
	})

	t.Run("a listing gives the newest revision of each hook and not its files", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		for _, one := range []store.ImportedHook{
			aHook("git-approval", 1), aHook("git-approval", 2), aHook("no-em-dash", 1),
		} {
			if err := s.ImportHook(ctx, one); err != nil {
				t.Fatalf("ImportHook: %v", err)
			}
		}
		listed, err := s.ListHooks(ctx)
		if err != nil {
			t.Fatalf("ListHooks: %v", err)
		}
		if len(listed) != 2 {
			t.Fatalf("want 2 hooks, got %d", len(listed))
		}
		if listed[0].Name != "git-approval" || listed[0].Version != 2 {
			t.Errorf("want git-approval at version 2, got %s at %d", listed[0].Name, listed[0].Version)
		}
		for _, one := range listed {
			if len(one.Files) != 0 {
				t.Errorf("%s came back with its files, and a listing is the cheapest call", one.Name)
			}
			// What it fires on is the whole of what a listing is asked, so the bindings do come.
			if len(one.Events) == 0 {
				t.Errorf("%s came back with no bindings, so a listing cannot say what it fires on", one.Name)
			}
		}
	})

	t.Run("a workspace holds the hooks attached to it, pinned to a version", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		if err := s.ImportHook(ctx, aHook("git-approval", 1)); err != nil {
			t.Fatalf("ImportHook: %v", err)
		}
		attached, err := s.AttachHook(ctx, workspace.GetId(), "git-approval")
		if err != nil {
			t.Fatalf("AttachHook: %v", err)
		}
		if attached.Version != 1 {
			t.Fatalf("attached version %d, want 1", attached.Version)
		}

		// A newer revision arriving does not move a workspace that already pinned one.
		if err := s.ImportHook(ctx, aHook("git-approval", 2)); err != nil {
			t.Fatalf("ImportHook: %v", err)
		}
		held, err := s.WorkspaceHooks(ctx, workspace.GetId())
		if err != nil {
			t.Fatalf("WorkspaceHooks: %v", err)
		}
		if len(held) != 1 || held[0].Version != 1 {
			t.Fatalf("the workspace should still be pinned to version 1, got %+v", held)
		}
		// A sandbox is built out of this, so the files have to come.
		if len(held[0].Files) == 0 {
			t.Error("a workspace's hooks came back with no files, so a sandbox cannot be built from them")
		}
		if len(held[0].Events) == 0 {
			t.Error("a workspace's hooks came back with no bindings, so nothing could be wired up")
		}

		// Attaching again is how a workspace moves to a newer revision.
		if _, err := s.AttachHook(ctx, workspace.GetId(), "git-approval"); err != nil {
			t.Fatalf("AttachHook again: %v", err)
		}
		held, err = s.WorkspaceHooks(ctx, workspace.GetId())
		if err != nil {
			t.Fatalf("WorkspaceHooks: %v", err)
		}
		if len(held) != 1 || held[0].Version != 2 {
			t.Fatalf("attaching again should move to version 2, got %+v", held)
		}

		if err := s.DetachHook(ctx, workspace.GetId(), "git-approval"); err != nil {
			t.Fatalf("DetachHook: %v", err)
		}
		held, err = s.WorkspaceHooks(ctx, workspace.GetId())
		if err != nil {
			t.Fatalf("WorkspaceHooks: %v", err)
		}
		if len(held) != 0 {
			t.Fatalf("the workspace should hold nothing, got %+v", held)
		}
		// Detaching does not unimport: another workspace may hold it.
		if _, err := s.GetHook(ctx, "git-approval", 1); err != nil {
			t.Errorf("detaching removed the hook from the system: %v", err)
		}
	})

	t.Run("the system holds a hook for every workspace, and a workspace's own is separate", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		if err := s.ImportHook(ctx, aHook("git-approval", 1)); err != nil {
			t.Fatalf("ImportHook: %v", err)
		}
		if _, err := s.AttachSystemHook(ctx, "git-approval"); err != nil {
			t.Fatalf("AttachSystemHook: %v", err)
		}
		system, err := s.SystemHooks(ctx)
		if err != nil {
			t.Fatalf("SystemHooks: %v", err)
		}
		if len(system) != 1 || system[0].Version != 1 {
			t.Fatalf("the system should hold git-approval at 1, got %+v", system)
		}
		if len(system[0].Files) == 0 || len(system[0].Events) == 0 {
			t.Error("a system hook came back without what a sandbox needs to be built from it")
		}

		// The workspace attaching it too is a separate, narrower statement.
		if _, err := s.AttachHook(ctx, workspace.GetId(), "git-approval"); err != nil {
			t.Fatalf("AttachHook: %v", err)
		}
		if err := s.DetachSystemHook(ctx, "git-approval"); err != nil {
			t.Fatalf("DetachSystemHook: %v", err)
		}
		held, err := s.WorkspaceHooks(ctx, workspace.GetId())
		if err != nil {
			t.Fatalf("WorkspaceHooks: %v", err)
		}
		if len(held) != 1 {
			t.Fatalf("the wider statement undid the narrower one: %+v", held)
		}
	})

	t.Run("attaching a hook that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		if _, err := s.AttachHook(ctx, workspace.GetId(), "nowhere"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("attaching a hook the system has not imported returned %v, want ErrNotFound", err)
		}
		if err := s.DetachHook(ctx, workspace.GetId(), "nowhere"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("detaching a hook the workspace does not hold returned %v, want ErrNotFound", err)
		}
		if _, err := s.AttachSystemHook(ctx, "nowhere"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("attaching a system hook that does not exist returned %v, want ErrNotFound", err)
		}
		if err := s.DetachSystemHook(ctx, "nowhere"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("detaching a system hook the system does not hold returned %v, want ErrNotFound", err)
		}
		if _, err := s.GetHook(ctx, "nowhere", 1); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("getting a hook that does not exist returned %v, want ErrNotFound", err)
		}
	})

	t.Run("attaching a hook to a workspace that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		if err := s.ImportHook(ctx, aHook("git-approval", 1)); err != nil {
			t.Fatalf("ImportHook: %v", err)
		}
		if _, err := s.AttachHook(ctx, "ghost", "git-approval"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("attaching to a workspace that does not exist returned %v, want ErrNotFound", err)
		}
	})

	t.Run("a hook that names no secret and needs no binary is imported", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		// The column is not null with a default, so a nil slice arriving as an explicit NULL is
		// refused. Every hook shipped so far declares a binary, so the one that needs nothing from
		// the sandbox is the first thing to find it, and it should not find it on a real system.
		bare := store.ImportedHook{Hook: hook.Hook{
			Name: "bare", Version: 1, Summary: "needs nothing at all",
			Events: []hook.Binding{{On: "Stop", Entry: "bin/hook"}},
			Files:  []hook.File{{Path: "bin/hook", Body: []byte("#!/bin/sh\n"), Executable: true}},
		}}
		if err := s.ImportHook(ctx, bare); err != nil {
			t.Fatalf("importing a hook that asks for nothing: %v", err)
		}
		got, err := s.GetHook(ctx, "bare", 1)
		if err != nil {
			t.Fatalf("GetHook: %v", err)
		}
		if len(got.Events) != 1 || got.Events[0].On != "Stop" {
			t.Fatalf("the binding did not survive: %+v", got.Events)
		}
	})
}

// aHook is a hook to put in the store, whole enough that the round trip is worth asserting on: two
// bindings in a deliberate order, one with a matcher and one without, a declared binary, a named
// secret, and an entry point that has to stay executable.
func aHook(name string, version int) store.ImportedHook {
	return store.ImportedHook{Hook: hook.Hook{
		Name:     name,
		Version:  version,
		Summary:  "Refuses a commit nobody approved.",
		Binaries: []string{"node"},
		Secrets:  map[string]string{"GH_TOKEN": "a token with repo scope"},
		Events: []hook.Binding{
			{On: "PreToolUse", Matcher: "Bash", Entry: "bin/hook", TimeoutSeconds: 20},
			{On: "UserPromptSubmit", Entry: "bin/hook"},
		},
		Files: []hook.File{
			{Path: "bin/hook", Body: []byte("#!/bin/sh\nexit 0\n"), Executable: true},
			{Path: "hook.yaml", Body: []byte("name: " + name + "\n")},
		},
	}}
}
