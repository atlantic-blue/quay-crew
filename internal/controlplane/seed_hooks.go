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
// The analyser only adds context and can never wrongly refuse, so it is the one hook that is safe to
// hand every crew without asking. Anything that refuses is a decision, and a hook that refuses
// wrongly blocks the work, which is worse than no hook.
var SeedHooksToCrew = []string{"prompt-analyser"}

// SeedHooks puts the hooks this build ships with into a crew that has none.
//
// Only when the crew holds none, which is what makes this a starting point rather than a policy that
// reasserts itself. An operator who takes a hook off the crew has said something, and starting the
// control plane again must not undo it. That matters more here than for skills: silently putting a
// constraint back is the crew overruling the person operating it.
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
	if len(held) > 0 {
		return
	}
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
		if !seededToCrew(one.Name) {
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
