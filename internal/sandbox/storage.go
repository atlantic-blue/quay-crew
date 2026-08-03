package sandbox

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Storage keeps a sandbox's state on the host rather than in the container layer.
//
// Without it a session's conversation lives only inside its container, so removing that container
// destroys the conversation the database still holds a handle to. With it, the same directory is
// also where a workspace and a project keep the context the model reads, because the model's command
// line tool already looks for CLAUDE.md in its home directory and in its working directory. One
// mechanism, both problems.
//
// The directories are bind mounted rather than kept in a named volume so the operator can drop a
// file into a project with an editor instead of a throwaway container.
type Storage struct {
	// Dir is the data directory as this process sees it. Empty keeps nothing, which is the old
	// behaviour: state lives in the container and dies with it.
	Dir string
	// Host is the same directory as the host daemon sees it. The control plane runs in a container
	// and starts sandboxes as siblings on the host daemon, so a bind mount source has to be a host
	// path; its own view of the directory means nothing to that daemon. Run the control plane
	// outside a container and the two are the same path.
	Host string
}

// Prepare creates the directories this sandbox needs and returns the mounts that carry them into
// it. The workspace's conversation store is shared by every project in it, so a thread started in
// one project can still be resumed; the working directory belongs to a single project.
func (s Storage) Prepare(cfg Config) ([]Mount, error) {
	if s.Dir == "" {
		return nil, nil
	}
	if s.Host == "" {
		return nil, fmt.Errorf("sandbox: a data directory needs the host path it maps to")
	}
	if err := usableAsPath("workspace", cfg.Workspace); err != nil {
		return nil, err
	}
	if err := usableAsPath("project", cfg.Project); err != nil {
		return nil, err
	}

	wanted := []struct {
		parts  []string
		target string
	}{
		{[]string{"workspaces", cfg.Workspace, "claude"}, ConversationPath},
		{[]string{"workspaces", cfg.Workspace, "projects", cfg.Project, "workspace"}, WorkingPath},
	}

	mounts := make([]Mount, 0, len(wanted))
	for _, dir := range wanted {
		if err := makeWritableDir(filepath.Join(append([]string{s.Dir}, dir.parts...)...)); err != nil {
			return nil, err
		}
		mounts = append(mounts, Mount{
			Source: path.Join(append([]string{s.Host}, dir.parts...)...),
			Target: dir.target,
		})
	}
	return mounts, nil
}

// ConversationFile is the extension the model's command line tool gives a stored conversation. It
// keeps one file per conversation, named after the conversation, under a directory per working
// directory.
const ConversationFile = ".jsonl"

// HasConversation says whether a workspace's conversation store still holds a conversation.
//
// A session's handle is a pointer into a store this process does not own, so a handle can outlive
// what it points at: every conversation from a sandbox created before state was kept on the host died
// with that container, while the row kept the handle. Resuming one of those prints "No conversation
// found" and exits, which from the console looks like nothing happening at all.
//
// It answers true whenever it cannot tell. An unconfigured store keeps nothing on the host, and
// refusing every attach because there is nowhere to look would be worse than the failure this exists
// to explain.
func (s Storage) HasConversation(workspace, conversation string) bool {
	if s.Dir == "" || workspace == "" || conversation == "" {
		return true
	}
	if usableAsPath("workspace", workspace) != nil || !plainIdentifier(conversation) {
		return true
	}
	matches, err := filepath.Glob(filepath.Join(
		s.Dir, "workspaces", workspace, "claude", "projects", "*", conversation+ConversationFile))
	if err != nil {
		return true
	}
	return len(matches) > 0
}

// plainIdentifier keeps anything with a glob character in it out of the pattern above, so a handle
// from somewhere unexpected widens nothing.
func plainIdentifier(id string) bool {
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// makeWritableDir creates a directory the sandbox's own user can write to.
//
// The sandbox runs as a non root user from its image, and this process does not choose that user's
// id, so there is nobody to hand ownership to. The directory sits inside the operator's data
// directory and holds one workspace's own state, so it is made writable by anyone on the host
// instead. MkdirAll's permission is filtered through the umask, hence the explicit change after it.
func makeWritableDir(dir string) error {
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return fmt.Errorf("sandbox: create %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		return fmt.Errorf("sandbox: open up %s to the sandbox user: %w", dir, err)
	}
	return nil
}

// usableAsPath refuses an identifier that would land somewhere other than where it says. Ids are
// generated hex today, so this is a guard against a future id, or a hand written one, reaching
// outside the data directory.
func usableAsPath(what, id string) error {
	if id == "" {
		return fmt.Errorf("sandbox: a sandbox needs its %s to keep state for it", what)
	}
	if id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return fmt.Errorf("sandbox: %s %q cannot be used as a directory name", what, id)
	}
	return nil
}
