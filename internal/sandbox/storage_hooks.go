package sandbox

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/atlantic-blue/quay-crew/internal/hook"
)

// HooksDir is what a workspace's own hooks directory is called under its directory in the data
// directory. Everything a session runs under comes from the store, so unlike skills there is no
// second source on the operator's disk.
const HooksDir = "hooks"

// WorkspaceHooksDir is where this process writes a workspace's hooks, and whether there is anywhere
// to write them at all.
func (s Storage) WorkspaceHooksDir(workspace string) (string, bool) {
	if s.Dir == "" || usableAsPath("workspace", workspace) != nil {
		return "", false
	}
	return filepath.Join(s.Dir, "workspaces", workspace, HooksDir), true
}

// WorkspaceHooksHost is the same directory as the host daemon sees it, which is what a bind mount
// source has to be.
func (s Storage) WorkspaceHooksHost(workspace string) (string, bool) {
	if s.Dir == "" || s.Host == "" || usableAsPath("workspace", workspace) != nil {
		return "", false
	}
	return path.Join(s.Host, "workspaces", workspace, HooksDir), true
}

// WriteHooks puts a workspace's hooks in the directory a sandbox reads them from, takes away the ones
// it no longer holds, and renders the settings file that binds them to their events.
//
// The settings go in the same directory as the hooks and not in the conversation directory, which is
// written by the runtime and edited by the operator. One directory the crew owns entirely means no
// merge, and no losing somebody's edit the first time a merge is wrong. It is also the only place the
// crew can say anything to the runtime at all: the conversation directory is a mount, and a mount
// hides whatever the image put under it.
//
// Detaching removes the hook's own files and stops the settings binding it. A hook left behind is a
// constraint the operator believes they took off, which is worse than one they know is there. The
// directory itself stays, holding settings that bind nothing, because they carry the status line too
// and a session under no hooks needs that as much as any other.
func WriteHooks(root string, hooks []hook.Hook) error {
	held := make(map[string]bool, len(hooks))
	for _, one := range hooks {
		held[one.Name] = true
	}

	// Take away what is no longer held before writing, so a rename lands as a rename rather than as
	// both names existing at once.
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("sandbox: read %s: %w", root, err)
	}
	for _, entry := range entries {
		if held[entry.Name()] || entry.Name() == hook.SettingsFile {
			continue
		}
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return fmt.Errorf("sandbox: remove %s: %w", filepath.Join(root, entry.Name()), err)
		}
	}

	for _, one := range hooks {
		for _, file := range one.Files {
			// The paths were checked when the hook was built, and they are checked again here because
			// this is the line that writes them: a hook that reached the store through an older build
			// must not be able to write outside its own directory now.
			target := filepath.Join(root, one.Name, filepath.FromSlash(file.Path))
			if !strings.HasPrefix(target, filepath.Join(root, one.Name)+string(filepath.Separator)) {
				return fmt.Errorf("sandbox: hook %s carries file %q, which does not stay inside its own directory",
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
			// The mode is filtered through the umask on create, and an entry point that is not
			// executable fails inside the container with nothing pointing back here.
			if err := os.Chmod(target, mode); err != nil {
				return fmt.Errorf("sandbox: open up %s to the sandbox user: %w", target, err)
			}
		}
	}

	// Rendered against the path inside the container, because that is where the runtime will look for
	// the command, not where this process just wrote it.
	rendered, err := hook.Settings(HooksPath, hooks)
	if err != nil {
		return err
	}
	if err := makeWritableDir(root); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, hook.SettingsFile), rendered, 0o666); err != nil {
		return fmt.Errorf("sandbox: write %s: %w", filepath.Join(root, hook.SettingsFile), err)
	}
	return nil
}
