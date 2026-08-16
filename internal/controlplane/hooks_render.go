package controlplane

import (
	"context"
	"path"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/hook"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
)

// renderHooks writes the hooks a session runs under to the host and answers with the mount that
// carries them into the sandbox, and whether there is one.
//
// One mount for the whole directory rather than one per hook, because the settings file that binds
// them sits beside them and has to travel too. Read only: a session that can edit the file binding
// its own constraints is a session with no constraints.
//
// A failure writing is a session with fewer hooks, not a failed task, and that is a deliberate and
// uncomfortable trade. The alternative is a crew where one unwritable directory stops every
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
	if len(hooks) == 0 {
		// Nothing held means no directory and no mount. Mounting an empty one would make the daemon
		// create it, and the runtime would then be pointed at a settings file that is not there.
		return sandbox.Mount{}, false
	}
	host, known := s.storage.WorkspaceHooksHost(session.GetWorkspace())
	if !known {
		return sandbox.Mount{}, false
	}
	return sandbox.Mount{Source: host, Target: sandbox.HooksPath, ReadOnly: true}, true
}

// settingsFor is the settings file a task should load, and empty when the session is under no hooks.
//
// Read again rather than remembered from sandbox creation, because a sandbox is adopted across tasks
// and this process may not be the one that built it.
func (s *Server) settingsFor(ctx context.Context, session *quaycrewv1.Session) string {
	if len(s.hooksFor(ctx, session.GetWorkspace())) == 0 {
		return ""
	}
	return path.Join(sandbox.HooksPath, hook.SettingsFile)
}
