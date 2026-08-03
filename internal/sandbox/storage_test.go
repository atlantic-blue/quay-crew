package sandbox_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

func TestStoragePrepareMountsTheWorkspaceAndTheProject(t *testing.T) {
	dir := t.TempDir()
	storage := sandbox.Storage{Dir: dir, Host: "/on/the/host"}

	mounts, err := storage.Prepare(sandbox.Config{ID: "sess", Workspace: "ws1", Project: "prj1"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(mounts) != 2 {
		t.Fatalf("got %d mounts (%+v), want 2", len(mounts), mounts)
	}

	// The source is the path the host daemon sees, not the path this process writes through. The
	// control plane starts sandboxes as siblings on that daemon, so its own view of the directory is
	// meaningless to it.
	want := []sandbox.Mount{
		{Source: "/on/the/host/workspaces/ws1/claude", Target: sandbox.ConversationPath},
		{Source: "/on/the/host/workspaces/ws1/projects/prj1/workspace", Target: sandbox.WorkingPath},
	}
	for i, mount := range mounts {
		if mount != want[i] {
			t.Errorf("mount %d is %+v, want %+v", i, mount, want[i])
		}
	}

	// And the directories exist, under this process's own view of the same data directory.
	for _, relative := range []string{"workspaces/ws1/claude", "workspaces/ws1/projects/prj1/workspace"} {
		info, err := os.Stat(filepath.Join(dir, relative))
		if err != nil {
			t.Fatalf("stat %s: %v", relative, err)
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", relative)
		}
		// The sandbox runs as a user whose id we do not choose, and it has to be able to write its
		// own conversation store into this directory.
		if perm := info.Mode().Perm(); perm != 0o777 {
			t.Errorf("%s is %v, want it writable by the sandbox user (0777)", relative, perm)
		}
	}
}

func TestStoragePrepareIsRepeatable(t *testing.T) {
	dir := t.TempDir()
	storage := sandbox.Storage{Dir: dir, Host: dir}
	config := sandbox.Config{ID: "sess", Workspace: "ws1", Project: "prj1"}

	first, err := storage.Prepare(config)
	if err != nil {
		t.Fatalf("first Prepare: %v", err)
	}
	// A file the operator dropped in, or the model wrote, must still be there when the next sandbox
	// for the same project is created. That is the whole point of the directory.
	note := filepath.Join(dir, "workspaces/ws1/projects/prj1/workspace", "CLAUDE.md")
	if err := os.WriteFile(note, []byte("the house bills project"), 0o600); err != nil {
		t.Fatalf("write the project context: %v", err)
	}

	second, err := storage.Prepare(config)
	if err != nil {
		t.Fatalf("second Prepare: %v", err)
	}
	if first[0] != second[0] || first[1] != second[1] {
		t.Fatalf("the same project got different directories: %+v then %+v", first, second)
	}
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("the project context did not survive: %v", err)
	}
}

func TestStoragePrepareSeparatesProjectsAndWorkspaces(t *testing.T) {
	dir := t.TempDir()
	storage := sandbox.Storage{Dir: dir, Host: dir}

	first, err := storage.Prepare(sandbox.Config{ID: "a", Workspace: "ws1", Project: "prj1"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	sameWorkspace, err := storage.Prepare(sandbox.Config{ID: "b", Workspace: "ws1", Project: "prj2"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	otherWorkspace, err := storage.Prepare(sandbox.Config{ID: "c", Workspace: "ws2", Project: "prj1"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	// Two projects in one workspace share the conversation store, so a thread started in either can
	// be resumed, and keep their working directories apart.
	if first[0] != sameWorkspace[0] {
		t.Errorf("projects in one workspace got different conversation stores: %q and %q", first[0].Source, sameWorkspace[0].Source)
	}
	if first[1] == sameWorkspace[1] {
		t.Errorf("two projects share the working directory %q", first[1].Source)
	}
	// Two workspaces share nothing.
	if otherWorkspace[0] == first[0] || otherWorkspace[1] == first[1] {
		t.Errorf("two workspaces share a directory: %+v and %+v", first, otherWorkspace)
	}
}

func TestStorageWithNoDirectoryKeepsNothing(t *testing.T) {
	mounts, err := sandbox.Storage{}.Prepare(sandbox.Config{ID: "sess", Workspace: "ws1", Project: "prj1"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(mounts) != 0 {
		t.Fatalf("got %+v, want no mounts when no data directory is configured", mounts)
	}
}

func TestStoragePrepareRefusesWhatItCannotPlace(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]struct {
		storage sandbox.Storage
		config  sandbox.Config
	}{
		// Without the host path the mount source would be this process's own view of the directory,
		// which means nothing to the host daemon: it would silently create an empty directory there
		// instead, and the conversation would be lost exactly as it is today.
		"no host path":       {sandbox.Storage{Dir: dir}, sandbox.Config{ID: "s", Workspace: "ws1", Project: "prj1"}},
		"no workspace":       {sandbox.Storage{Dir: dir, Host: dir}, sandbox.Config{ID: "s", Project: "prj1"}},
		"no project":         {sandbox.Storage{Dir: dir, Host: dir}, sandbox.Config{ID: "s", Workspace: "ws1"}},
		"workspace escapes":  {sandbox.Storage{Dir: dir, Host: dir}, sandbox.Config{ID: "s", Workspace: "../etc", Project: "prj1"}},
		"project escapes":    {sandbox.Storage{Dir: dir, Host: dir}, sandbox.Config{ID: "s", Workspace: "ws1", Project: "a/b"}},
		"workspace is a dot": {sandbox.Storage{Dir: dir, Host: dir}, sandbox.Config{ID: "s", Workspace: ".", Project: "prj1"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tc.storage.Prepare(tc.config); err == nil {
				t.Fatal("Prepare accepted it, want an error")
			}
		})
	}
}
