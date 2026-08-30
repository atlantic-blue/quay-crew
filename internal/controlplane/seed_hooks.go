package controlplane

import (
	"context"
	"log/slog"

	"github.com/atlantic-blue/krewe/internal/hook"
	"github.com/atlantic-blue/krewe/internal/store"
)

// SeedHooksDir is where the shipped hooks are in the image, and what QC_SEED_HOOKS_DIR defaults to.
const SeedHooksDir = "/hooks"

// SeedHooksToSystem is which of the shipped hooks a fresh system is put under, rather than merely offered.
//
// The analyser only adds context and can never wrongly refuse, so it costs a system nothing to hold.
//
// The merge gate does refuse, and it is here anyway. It holds the one boundary the whole shape of
// this system rests on: every role pushes and opens a pull request, and no role merges, because a push
// applies nothing and a merge runs the pipeline that spends money. A gate an operator has to remember
// to attach is a gate that is off in every system nobody set up, which is the systems the boundary
// matters most in. It refuses one thing, that thing is never a session's to do, and
// `krewe hook detach system merge-gate` is how somebody decides otherwise.
//
// So the rule is not that a seeded hook never refuses. It is that a seeded hook refuses something no
// session in this system is ever meant to do, exactly, and says what to do instead.
var SeedHooksToSystem = []string{"merge-gate", "prompt-analyser"}

// SeedHooks offers the hooks this build ships, and puts a system that holds none under them.
//
// The two halves are separate on purpose.
//
// Importing runs every time, so a newer version of a shipped hook reaches the catalogue of any system
// that upgrades. Without that a fix could only ever reach a system with no hooks at all, which is no
// system that has been used, and the analyser's first fix was stranded exactly there.
//
// Attaching runs only into a system that held nothing. An operator who takes a hook off has said
// something, and starting the control plane again must not undo it. Nor does an upgrade quietly move
// a system onto a newer version of a constraint it is already under: a hook is pinned so it cannot
// change under a running session, and `krewe hook attach` is how somebody decides to take the new one.
//
// A failure to seed is logged and not fatal, and it is logged rather than swallowed because a system
// running without the constraint it believes it has is the thing this whole subsystem exists to stop.
func (s *Server) SeedHooks(ctx context.Context, dir string, logger *slog.Logger) {
	if dir == "" {
		return
	}
	held, err := s.store.ListHooks(ctx)
	if err != nil {
		logger.Warn("hooks were not seeded", "error", err)
		return
	}
	// Whether this is a fresh system, which decides whether anything is attached below. Read before
	// importing, because importing is what stops it being empty.
	fresh := len(held) == 0
	shipped, err := hook.Load(dir)
	if err != nil {
		logger.Warn("hooks were not seeded", "directory", dir, "error", err)
		return
	}
	seeded, given := 0, 0
	for _, one := range shipped {
		if err := s.store.ImportHook(ctx, store.ImportedHook{Hook: one}); err != nil {
			logger.Warn("a shipped hook was not imported", "hook", one.Name, "error", err)
			continue
		}
		seeded++
		// Attaching is only for a system that held nothing. Importing happens every time, and the two
		// are deliberately different: the catalogue is what the system could run, and an attachment is
		// what it does run. A fix to a shipped hook that only reached a system with no hooks at all
		// could never reach anybody, which is exactly what happened to the analyser's first fix.
		if !fresh || !seededToSystem(one.Name) {
			continue
		}
		if _, err := s.store.AttachSystemHook(ctx, one.Name); err != nil {
			logger.Warn("a shipped hook was not given to the system", "hook", one.Name, "error", err)
			continue
		}
		given++
	}
	if seeded == 0 {
		return
	}
	logger.Info("hooks seeded", "imported", seeded, "held by the system", given, "directory", dir)
}

// seededToSystem says whether a shipped hook is one the system is put under rather than merely offered.
func seededToSystem(name string) bool {
	for _, one := range SeedHooksToSystem {
		if one == name {
			return true
		}
	}
	return false
}
