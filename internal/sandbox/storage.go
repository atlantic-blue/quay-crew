package sandbox

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/skill"
)

// Storage keeps a sandbox's state on the host, so removing a container does not destroy the
// conversation the database holds a handle to. The same directories carry the context the model
// reads, because it already looks for CLAUDE.md in its home and working directories.
//
// Bind mounted rather than a named volume, so the operator can edit a project's files directly.
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
// it. The workspace's conversation store is shared by every project in it, so a session started in
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
	if err := usableAsPath("session", cfg.ID); err != nil {
		return nil, err
	}

	mounts := make([]Mount, 0, len(layout(cfg)))
	for _, dir := range layout(cfg) {
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

// dir is one of a sandbox's two directories, as path parts under the data directory and where it
// appears inside the container.
type dir struct {
	parts  []string
	target string
	// memory says this directory carries a memory file the model reads, which is what makes it a level
	// of context. Not every mounted directory is one: the workspace's volume is shared storage, and a
	// listing of where context lives should not offer it as somewhere to write context.
	memory bool
}

// layout is where a sandbox's two directories go.
//
// The conversation store is shared by every project in a workspace, so a session started in one
// project can still be resumed. The working directory belongs to **one session**, not to the project:
// a session is a conversation with its own files, and two conversations sharing a working directory
// means one of them changing a file under the other. It also gives the session a place of its own for
// what it is told, which is the level the operator asked for.
func layout(cfg Config) []dir {
	return []dir{
		{[]string{"workspaces", cfg.Workspace, "claude"}, ConversationPath, true},
		{[]string{"workspaces", cfg.Workspace, "projects", cfg.Project, "sessions", cfg.ID, "workspace"}, WorkingPath, true},
		// The workspace's own volume, shared by every session in it. A repository is cloned here once
		// rather than per session, which is the difference between one copy of a large checkout and one
		// per conversation, and it is what lets two sessions share the history they are working on.
		{[]string{"workspaces", cfg.Workspace, "volume"}, SharedPath, false},
	}
}

// memoryDirs are the ones carrying a memory file, in order, which is what everything about context
// means by a session's directories.
func memoryDirs(cfg Config) []dir {
	out := make([]dir, 0, 2)
	for _, one := range layout(cfg) {
		if one.memory {
			out = append(out, one)
		}
	}
	return out
}

// WorkingDir is a session's own working directory as this process sees it, and false where this
// storage keeps nothing or the configuration names a directory it could not make.
//
// Read from the same layout the mounts come from, so the two cannot drift into describing different
// directories. It is this process's view rather than the host's, because the caller is reading the
// directory rather than mounting it.
func (s Storage) WorkingDir(cfg Config) (string, bool) {
	if s.Dir == "" {
		return "", false
	}
	for _, part := range []struct{ kind, value string }{
		{"workspace", cfg.Workspace}, {"project", cfg.Project}, {"session", cfg.ID},
	} {
		if usableAsPath(part.kind, part.value) != nil {
			return "", false
		}
	}
	for _, one := range layout(cfg) {
		if one.target == WorkingPath {
			return filepath.Join(append([]string{s.Dir}, one.parts...)...), true
		}
	}
	return "", false
}

// VolumeDir is the workspace's shared volume as this process sees it, and VolumeHost as the host daemon
// does, which is what a bind mount source has to be.
func (s Storage) VolumeDir(workspace string) (string, bool) {
	if s.Dir == "" || usableAsPath("workspace", workspace) != nil {
		return "", false
	}
	return filepath.Join(s.Dir, "workspaces", workspace, "volume"), true
}

// Context describes a directory the model reads, for a caller that wants to show or edit it rather
// than mount it.
type Context struct {
	// Host is where it is on the machine running the sandboxes, which is where it is edited.
	Host string
	// Sandbox is where the same directory appears inside a container.
	Sandbox string
	// Memory is the file the model reads as its memory for this scope, inside Host.
	Memory string
	// Written says whether that file exists yet.
	Written bool
}

// Contexts describes the two directories a session for this configuration reads, creating nothing.
//
// The paths are the host's, because that is where the operator edits them, while whether the memory
// file exists is answered from this process's own view of the same directory. In a container those
// are two different paths to one place.
func (s Storage) Contexts(cfg Config) []Context {
	if s.Dir == "" {
		return nil
	}
	out := make([]Context, 0, 2)
	for _, dir := range memoryDirs(cfg) {
		host := path.Join(append([]string{s.Host}, dir.parts...)...)
		mine := filepath.Join(append([]string{s.Dir}, dir.parts...)...)
		_, err := os.Stat(filepath.Join(mine, MemoryFile))
		out = append(out, Context{
			Host:    host,
			Sandbox: dir.target,
			Memory:  path.Join(host, MemoryFile),
			Written: err == nil,
		})
	}
	return out
}

// ConversationFile is the extension the model's command line tool gives a stored conversation. It
// keeps one file per conversation, named after the conversation, under a directory per working
// directory.
const ConversationFile = ".jsonl"

// HasConversation says whether a workspace's store still holds one. A handle is a pointer into a
// store this process does not own, so it can outlive what it points at.
//
// True whenever it cannot tell: refusing every attach because there is nowhere to look would be worse
// than the failure this exists to explain.
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

// ReadMemory returns what is written in a directory's memory file, and whether there is one. It reads
// this process's own view of the directory, which is the same place the sandbox writes into.
func ReadMemory(dir string) (string, bool) {
	body, err := os.ReadFile(filepath.Join(dir, MemoryFile))
	if err != nil {
		return "", false
	}
	return string(body), true
}

// WriteMemory puts a body in a directory's memory file, making the directory if it is not there.
//
// An empty body removes the file rather than leaving an empty one, because an empty CLAUDE.md is a
// thing the model opens and reads nothing from, and "there is no context here" is better said by
// there being no file.
func WriteMemory(dir, body string) error {
	if err := makeWritableDir(dir); err != nil {
		return err
	}
	file := filepath.Join(dir, MemoryFile)
	if body == "" {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("sandbox: clear %s: %w", file, err)
		}
		return nil
	}
	if err := os.WriteFile(file, []byte(body), 0o666); err != nil {
		return fmt.Errorf("sandbox: write %s: %w", file, err)
	}
	// The sandbox runs as a user this process did not choose, and it has to be able to write its own
	// context back.
	if err := os.Chmod(file, 0o666); err != nil {
		return fmt.Errorf("sandbox: open up %s to the sandbox user: %w", file, err)
	}
	return nil
}

// SkillsDir is what a workspace's own skills directory is called under its directory in the data
// directory. The crew's skills live wherever the operator keeps them and do not come through here.
const SkillsDir = "skills"

// WorkspaceSkillsDir is where this process writes a workspace's own skills, and whether there is
// anywhere to write them at all. An unconfigured data directory keeps nothing, the same as everything
// else here.
func (s Storage) WorkspaceSkillsDir(workspace string) (string, bool) {
	if s.Dir == "" || usableAsPath("workspace", workspace) != nil {
		return "", false
	}
	return filepath.Join(s.Dir, "workspaces", workspace, SkillsDir), true
}

// WorkspaceSkillsHost is the same directory as the host daemon sees it, which is what a bind mount
// source has to be: the control plane may be in a container, and its own view of a path means nothing
// to the daemon starting sandboxes beside it.
func (s Storage) WorkspaceSkillsHost(workspace string) (string, bool) {
	if s.Dir == "" || s.Host == "" || usableAsPath("workspace", workspace) != nil {
		return "", false
	}
	return path.Join(s.Host, "workspaces", workspace, SkillsDir), true
}

// WriteSkills puts a workspace's skills in the directory a sandbox reads them from, and takes away the
// ones it no longer holds.
//
// The files are written rather than mounted from wherever they were authored, because a skill lives in
// the store: a crew on a pod has no host directory to go back to, and a skill has to be whole wherever
// the crew runs. This is the same shape as the memory file, which is a rendering of the store too.
//
// Detaching removes the directory. A brief left behind is a capability the model can still read about
// and no longer has, which is worse than not having it, because it will try.
func WriteSkills(root string, skills []skill.Skill) error {
	held := make(map[string]bool, len(skills))
	for _, one := range skills {
		held[one.Name] = true
	}

	// Take away what is no longer held before writing, so a rename lands as a rename rather than as
	// both names existing at once.
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("sandbox: read %s: %w", root, err)
	}
	for _, entry := range entries {
		if held[entry.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return fmt.Errorf("sandbox: remove %s: %w", filepath.Join(root, entry.Name()), err)
		}
	}

	if len(skills) == 0 {
		// Nothing held means no directory, rather than an empty one, for the same reason an empty
		// memory file is removed: there being nothing there says it better.
		if err := os.RemoveAll(root); err != nil {
			return fmt.Errorf("sandbox: remove %s: %w", root, err)
		}
		return nil
	}

	for _, one := range skills {
		for _, file := range one.Files {
			// The paths were checked when the skill was built, and they are checked again here because
			// this is the line that writes them: a skill that reached the store through an older build
			// must not be able to write outside its own directory now.
			target := filepath.Join(root, one.Name, filepath.FromSlash(file.Path))
			if !strings.HasPrefix(target, filepath.Join(root, one.Name)+string(filepath.Separator)) {
				return fmt.Errorf("sandbox: skill %s carries file %q, which does not stay inside its own directory",
					one.Name, file.Path)
			}
			if err := makeWritableDir(filepath.Dir(target)); err != nil {
				return err
			}
			mode := os.FileMode(0o666)
			if file.Executable {
				mode = 0o777
			}
			if err := os.WriteFile(target, file.Body, mode); err != nil {
				return fmt.Errorf("sandbox: write %s: %w", target, err)
			}
			// The mode is filtered through the umask on create, and a setup script that is not
			// executable fails inside the container with nothing pointing back here.
			if err := os.Chmod(target, mode); err != nil {
				return fmt.Errorf("sandbox: open up %s to the sandbox user: %w", target, err)
			}
		}
	}
	return nil
}

// MyDirs returns this process's own view of a session's two memory directories, workspace first, which
// is what a caller rendering or reading the memory files needs. The workspace's volume is not one of
// them: it is shared storage rather than somewhere the model is told anything.
func (s Storage) MyDirs(cfg Config) []string {
	if s.Dir == "" {
		return nil
	}
	out := make([]string, 0, 2)
	for _, dir := range memoryDirs(cfg) {
		out = append(out, filepath.Join(append([]string{s.Dir}, dir.parts...)...))
	}
	return out
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
