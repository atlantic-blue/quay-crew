package sandbox_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

// Where a session's work is. The bytes survive the container, on a mount the system made itself, so
// the only question that ever mattered is which directory they are in.

func aSession(dir string) (sandbox.Storage, sandbox.Config) {
	return sandbox.Storage{Dir: dir, Host: "/qdata"},
		sandbox.Config{ID: "145c0173", Workspace: "e5b4c0ac", Project: "12e5b9b0"}
}

// A system that keeps nothing on disk has nowhere for anybody to look, and has to say so rather than
// hand back a path that does not exist.
func TestASystemThatKeepsNothingOnDiskHasNoPlacesAtAll(t *testing.T) {
	places := sandbox.Storage{}.WorkPlaces(sandbox.Config{
		ID: "145c0173", Workspace: "e5b4c0ac", Project: "12e5b9b0",
	})
	if len(places) != 0 {
		t.Fatalf("a system with no data directory offered %d places to look: %+v", len(places), places)
	}
}

// Both shapes the git skill teaches. A session takes a working tree in the workspace's volume, and a
// system with no volume is told to clone into the session's own directory instead: looking in one of
// them would leave half the sessions unreadable.
func TestBothPlacesASessionKeepsWorkAreOffered(t *testing.T) {
	storage, cfg := aSession(t.TempDir())
	places := storage.WorkPlaces(cfg)

	if len(places) != 2 {
		t.Fatalf("the system offered %d places to look, want the session's own directory and its "+
			"working trees: %+v", len(places), places)
	}
	if places[0].Sandbox != sandbox.WorkingPath {
		t.Fatalf("the first place is %q inside a container, want %q", places[0].Sandbox, sandbox.WorkingPath)
	}
	if !strings.HasSuffix(places[1].Sandbox, "worktrees/145c0173") {
		t.Fatalf("the second place is %q inside a container, want this session's working trees",
			places[1].Sandbox)
	}
	// The host's view is what an operator opens, and it is a different path from this process's own:
	// the control plane is in a container and the daemon is not.
	for _, place := range places {
		if !strings.HasPrefix(place.Host, "/qdata/") {
			t.Fatalf("the host path is %q, want it under the directory the daemon sees", place.Host)
		}
		if place.Dir == place.Host {
			t.Fatalf("this process's view and the host's are the same path, %q", place.Dir)
		}
	}
}

// A session that cloned nothing. There is no repository to find, and saying so is the difference
// between "the session did no work" and "the system did not look".
func TestASessionThatClonedNothingHoldsNoRepository(t *testing.T) {
	storage, cfg := aSession(t.TempDir())
	dir, _ := storage.WorkingDir(cfg)
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o777); err != nil {
		t.Fatal(err)
	}
	if _, held := sandbox.Repository(storage.WorkPlaces(cfg)); held {
		t.Fatalf("the system found a repository in a session that cloned nothing")
	}
}

// A repository cloned into the session's own directory, which is what the git skill says to do where
// the system keeps no volume.
func TestARepositoryClonedIntoTheSessionsOwnDirectoryIsFound(t *testing.T) {
	storage, cfg := aSession(t.TempDir())
	dir, _ := storage.WorkingDir(cfg)
	makeRepository(t, filepath.Join(dir, "krewe"), true)

	place, held := sandbox.Repository(storage.WorkPlaces(cfg))
	if !held {
		t.Fatalf("the system found no repository in the session's own directory")
	}
	if !strings.HasSuffix(place.Sandbox, "/home/agent/workspace/krewe") {
		t.Fatalf("the repository is at %q inside a container, want it under the working directory",
			place.Sandbox)
	}
	if !strings.HasSuffix(place.Host, "/sessions/145c0173/workspace/krewe") {
		t.Fatalf("the repository is at %q on the machine, want it under the session's directory", place.Host)
	}
}

// A working tree in the workspace's volume, which is the shape the git skill teaches first. Its `.git`
// is a file rather than a directory, and refusing that would miss every session that followed the
// brief.
func TestAWorkingTreeInTheVolumeIsFoundThoughItsGitIsAFile(t *testing.T) {
	storage, cfg := aSession(t.TempDir())
	volume, _ := storage.VolumeDir(cfg.Workspace)
	makeRepository(t, filepath.Join(volume, "worktrees", cfg.ID, "krewe"), false)

	place, held := sandbox.Repository(storage.WorkPlaces(cfg))
	if !held {
		t.Fatalf("the system found no working tree in the workspace's volume")
	}
	if place.Sandbox != "/home/agent/shared/worktrees/145c0173/krewe" {
		t.Fatalf("the working tree is at %q inside a container", place.Sandbox)
	}
}

// makeRepository writes something that looks like the top of a repository. A directory `.git` is a
// clone and a file `.git` is a working tree, and both are things a session leaves behind.
func makeRepository(t *testing.T, at string, clone bool) {
	t.Helper()
	if err := os.MkdirAll(at, 0o777); err != nil {
		t.Fatal(err)
	}
	if clone {
		if err := os.MkdirAll(filepath.Join(at, ".git"), 0o777); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(filepath.Join(at, ".git"), []byte("gitdir: /home/agent/shared/repos/krewe/.git/worktrees/145c0173\n"), 0o666); err != nil {
		t.Fatal(err)
	}
}
