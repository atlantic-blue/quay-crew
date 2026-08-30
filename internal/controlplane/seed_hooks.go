package controlplane

import (
	"context"
	"log/slog"

	"github.com/atlantic-blue/quay-crew/internal/hook"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// SeedHooksDir is where the shipped hooks are in the image, and what QC_SEED_HOOKS_DIR defaults to.
const SeedHooksDir = "/hooks"

// SeedHooksToCrew is which of the shipped hooks a fresh crew is put under, rather than merely offered.
//
// The analyser only adds context and can never wrongly refuse, so it costs a crew nothing to hold.
//
// The merge gate does refuse, and it is here anyway. It holds the one boundary the whole shape of
// this crew rests on: every role pushes and opens a pull request, and no role merges, because a push
// applies nothing and a merge runs the pipeline that spends money. A gate an operator has to remember
// to attach is a gate that is off in every crew nobody set up, which is the crews the boundary
// matters most in. It refuses one thing, that thing is never a session's to do, and
// `quay hook detach crew merge-gate` is how somebody decides otherwise.
//
// So the rule is not that a seeded hook never refuses. It is that a seeded hook refuses something no
// session in this crew is ever meant to do, exactly, and says what to do instead.
var SeedHooksToCrew = []string{"merge-gate", "prompt-analyser"}

// SeedHooks offers the hooks this build ships, and puts a crew that holds none under them.
//
// The two halves are separate on purpose.
//
// Importing runs every time, so a newer version of a shipped hook reaches the catalogue of any crew
// that upgrades. Without that a fix could only ever reach a crew with no hooks at all, which is no
// crew that has been used, and the analyser's first fix was stranded exactly there.
//
// Attaching runs only into a crew that held nothing. An operator who takes a hook off has said
// something, and starting the control plane again must not undo it. Nor does an upgrade quietly move
// a crew onto a newer version of a constraint it is already under: a hook is pinned so it cannot
// change under a running session, and `quay hook attach` is how somebody decides to take the new one.
//
// A failure to seed is logged and not fatal, and it is logged rather than swallowed because a crew
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
	// Whether this is a fresh crew, which decides whether anything is attached below. Read before
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
		// Attaching is only for a crew that held nothing. Importing happens every time, and the two
		// are deliberately different: the catalogue is what the crew could run, and an attachment is
		// what it does run. A fix to a shipped hook that only reached a crew with no hooks at all
		// could never reach anybody, which is exactly what happened to the analyser's first fix.
		if !fresh || !seededToCrew(one.Name) {
			continue
		}
		if _, err := s.store.AttachCrewHook(ctx, one.Name); err != nil {
			logger.Warn("a shipped hook was not given to the crew", "hook", one.Name, "error", err)
			continue
		}
		given++
	}
	if seeded == 0 {
		return
	}
	logger.Info("hooks seeded", "imported", seeded, "held by the crew", given, "directory", dir)
}

// seededToCrew says whether a shipped hook is one the crew is put under rather than merely offered.
func seededToCrew(name string) bool {
	for _, one := range SeedHooksToCrew {
		if one == name {
			return true
		}
	}
	return false
}
