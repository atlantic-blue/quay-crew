package sandbox_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
)

func TestStoragePrepareMountsTheWorkspaceTheProjectAndTheVolume(t *testing.T) {
	dir := t.TempDir()
	storage := sandbox.Storage{Dir: dir, Host: "/on/the/host"}

	mounts, err := storage.Prepare(sandbox.Config{ID: "sess", Workspace: "ws1", Project: "prj1"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if len(mounts) != 3 {
		t.Fatalf("got %d mounts (%+v), want 3", len(mounts), mounts)
	}

	// The source is the path the host daemon sees, not the path this process writes through. The
	// control plane starts sandboxes as siblings on that daemon, so its own view of the directory is
	// meaningless to it.
	want := []sandbox.Mount{
		{Source: "/on/the/host/workspaces/ws1/claude", Target: sandbox.ConversationPath},
		{Source: "/on/the/host/workspaces/ws1/projects/prj1/sessions/sess/workspace", Target: sandbox.WorkingPath},
		// The workspace's own volume, the same directory for every session in it, which is what makes a
		// repository cloned once visible to all of them.
		{Source: "/on/the/host/workspaces/ws1/volume", Target: sandbox.SharedPath},
	}
	for i, mount := range mounts {
		if mount != want[i] {
			t.Errorf("mount %d is %+v, want %+v", i, mount, want[i])
		}
	}

	// And the directories exist, under this process's own view of the same data directory.
	for _, relative := range []string{
		"workspaces/ws1/claude",
		"workspaces/ws1/projects/prj1/sessions/sess/workspace",
		"workspaces/ws1/volume",
	} {
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
	note := filepath.Join(dir, "workspaces/ws1/projects/prj1/sessions/sess/workspace", "CLAUDE.md")
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

	// Two projects in one workspace share the conversation store, so a session started in either can
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

// TestHasConversationFindsWhatTheModelKeeps: whether the runtime has opened a conversation decides
// how the next exec names it, so a wrong answer here fails the exec either way. Resuming a name that
// is not there prints "No conversation found" and exits, and starting a name that is there is refused
// as one already in use.
func TestHasConversationFindsWhatTheModelKeeps(t *testing.T) {
	dir := t.TempDir()
	storage := sandbox.Storage{Dir: dir, Host: dir}

	const conversation = "d713b6d1-7873-4376-8ffe-cd5c734a9733"
	cfg := sandbox.Config{ID: "sess1", Workspace: "ws1", Project: "prj1"}
	kept := filepath.Join(dir, "workspaces", cfg.Workspace, "claude", "projects", "-home-agent-workspace")
	if err := os.MkdirAll(kept, 0o777); err != nil {
		t.Fatalf("seeding the conversation store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(kept, conversation+".jsonl"), []byte("{}\n"), 0o666); err != nil {
		t.Fatalf("writing the conversation: %v", err)
	}

	if !storage.HasConversation(cfg, conversation) {
		t.Fatal("the conversation is on disk and was not found, so the next exec would start it again")
	}
	if storage.HasConversation(cfg, "37b8f60b-7ef1-4834-9820-2a62b9937faf") {
		t.Fatal("a conversation that is not on disk was reported as opened, so the next exec would resume nothing")
	}
	other := sandbox.Config{ID: "sess2", Workspace: "other-workspace", Project: "prj1"}
	if storage.HasConversation(other, conversation) {
		t.Fatal("a conversation was found in a workspace that does not hold it")
	}
}

// TestHasConversationSaysNoWhenItCannotTell: a system that keeps nothing on the host has nowhere to
// look. Saying no starts the conversation under the name the system gave it, which is the answer that
// leaves the name true; saying yes would resume a name nothing has written.
func TestHasConversationSaysNoWhenItCannotTell(t *testing.T) {
	tests := map[string]struct {
		storage      sandbox.Storage
		config       sandbox.Config
		conversation string
	}{
		"no data directory": {sandbox.Storage{}, sandbox.Config{Workspace: "ws1"}, "c1"},
		"no workspace":      {sandbox.Storage{Dir: t.TempDir()}, sandbox.Config{}, "c1"},
		"no conversation":   {sandbox.Storage{Dir: t.TempDir()}, sandbox.Config{Workspace: "ws1"}, ""},
		"a name with a glob in it": {
			sandbox.Storage{Dir: t.TempDir()}, sandbox.Config{Workspace: "ws1"}, "*",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			if test.storage.HasConversation(test.config, test.conversation) {
				t.Fatal("want it to say no when it cannot tell")
			}
		})
	}
}

// The volume is shared storage, not a level of context. Offering it as somewhere to write context would
// invite the operator to put something there that the model is never told about.
func TestTheVolumeIsNotAContext(t *testing.T) {
	storage := sandbox.Storage{Dir: t.TempDir(), Host: "/on/the/host"}
	cfg := sandbox.Config{ID: "sess", Workspace: "ws1", Project: "prj1"}

	contexts := storage.Contexts(cfg)
	if len(contexts) != 2 {
		t.Fatalf("there are %d contexts (%+v), want the workspace's and the session's", len(contexts), contexts)
	}
	for _, one := range contexts {
		if one.Sandbox == sandbox.SharedPath {
			t.Errorf("the volume is listed as somewhere context lives: %+v", one)
		}
	}
	if dirs := storage.MyDirs(cfg); len(dirs) != 2 {
		t.Errorf("MyDirs gives %d directories (%v), want the two carrying a memory file", len(dirs), dirs)
	}
}
