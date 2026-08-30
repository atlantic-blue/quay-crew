package controlplane

import (
	"context"
	"os"
	"path"
	"path/filepath"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/hook"
	"github.com/atlantic-blue/krewe/internal/sandbox"
)

// renderHooks writes the hooks a session runs under to the host and answers with the mount that
// carries them into the sandbox, and whether there is one.
//
// One mount for the whole directory rather than one per hook, because the settings file that binds
// them sits beside them and has to travel too. Read only: a session that can edit the file binding
// its own constraints is a session with no constraints.
//
// A failure writing is a session with fewer hooks, not a failed task, and that is a deliberate and
// uncomfortable trade. The alternative is a system where one unwritable directory stops every
// conversation. The listing is where an operator finds out which hooks a session actually holds.
func (s *Server) renderHooks(ctx context.Context, session *quaycrewv1.Session) (sandbox.Mount, bool) {
	held := s.hooksFor(ctx, session.GetWorkspace())
	dir, canWrite := s.storage.WorkspaceHooksDir(session.GetWorkspace())
	if !canWrite {
		return sandbox.Mount{}, false
	}
	hooks := make([]hook.Hook, 0, len(held))
	for _, one := range held {
		hooks = append(hooks, one.Hook)
	}
	if err := sandbox.WriteHooks(dir, hooks); err != nil {
		return sandbox.Mount{}, false
	}
	// A session under no hooks is mounted this directory too, because the settings file in it carries
	// more than hooks now: it is where the system says what the runtime draws under the conversation.
	// The file is written whether or not there is a hook in it, so the runtime is never pointed at
	// something that is not there.
	host, known := s.storage.WorkspaceHooksHost(session.GetWorkspace())
	if !known {
		return sandbox.Mount{}, false
	}
	return sandbox.Mount{Source: host, Target: sandbox.HooksPath, ReadOnly: true}, true
}

// settingsFor is the settings file a task should load, and empty when there is none to load.
//
// Read from the disk rather than remembered from sandbox creation, because a sandbox is adopted
// across tasks and this process may not be the one that built it. The file itself is the question,
// not the hooks in it: a task told to load settings that are not there does not start at all, and the
// runtime says only "Settings file not found" before exiting. That is every task on the system, so it
// is worth a stat.
func (s *Server) settingsFor(_ context.Context, session *quaycrewv1.Session) string {
	dir, canWrite := s.storage.WorkspaceHooksDir(session.GetWorkspace())
	if !canWrite {
		return ""
	}
	if _, err := os.Stat(filepath.Join(dir, hook.SettingsFile)); err != nil {
		return ""
	}
	return path.Join(sandbox.HooksPath, hook.SettingsFile)
}
