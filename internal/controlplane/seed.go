package controlplane

import (
	"context"
	"log/slog"

	"github.com/atlantic-blue/krewe/internal/skill"
	"github.com/atlantic-blue/krewe/internal/store"
)

// SeedToSystem is which of the shipped skills a fresh system is given, rather than merely offered.
//
// The first two are how work is done in any repository, they are what the system's own documentation
// assumes, and neither is specific to one kind of work the way the cloud and tracker skills are.
// Everything else is imported so `krewe skill list` can show it, and attaching one is a decision.
//
// The third is here for a different reason: it is a rule rather than a capability. A job wrote six
// resources, opened a pull request, and every check went green, because validating a configuration
// never talks to the account. The identity that would apply it held read only access and could not
// have created any of them. A rule that only arrives when somebody attaches it is a rule that is
// missing in every system nobody set up, which is exactly where this failure happens. It names no
// secret, so no workspace loses it for want of a credential, and the one binary it needs is already
// in the image for the cloud skills.
var SeedToSystem = []string{"git", "github", "deploy-identity"}

// Seed puts the skills this build ships with into a system that has none.
//
// A system that starts empty makes every operator do the same setup: import each skill, then attach
// each one to each workspace. The files are already in the image, so the system can simply have them.
//
// Only when the catalogue is empty, which is what makes this a starting point rather than a policy
// that reasserts itself. An operator who takes a skill off the system has said something, and starting
// the control plane again must not undo it.
//
// A failure to seed is logged and not fatal. A system that starts with no skills is a system that works,
// and refusing to start over a skill nobody has asked for yet would be the worse answer.
func (s *Server) Seed(ctx context.Context, dir string, logger *slog.Logger) {
	if dir == "" {
		return
	}
	held, err := s.store.ListSkills(ctx)
	if err != nil {
		logger.Warn("skills were not seeded", "error", err)
		return
	}
	if len(held) > 0 {
		return
	}
	shipped, err := skill.Load(dir)
	if err != nil {
		logger.Warn("skills were not seeded", "directory", dir, "error", err)
		return
	}
	seeded, given := 0, 0
	for _, one := range shipped {
		if err := s.store.ImportSkill(ctx, store.Imported{Skill: one}); err != nil {
			logger.Warn("a shipped skill was not imported", "skill", one.Name, "error", err)
			continue
		}
		seeded++
		if !wanted(one.Name) {
			continue
		}
		if _, err := s.store.AttachSystemSkill(ctx, one.Name); err != nil {
			logger.Warn("a shipped skill was not given to the system", "skill", one.Name, "error", err)
			continue
		}
		given++
	}
	if seeded == 0 {
		return
	}
	logger.Info("skills seeded", "imported", seeded, "held by the system", given, "directory", dir)
}

// wanted says whether a shipped skill is one the system is given rather than merely offered.
func wanted(name string) bool {
	for _, one := range SeedToSystem {
		if one == name {
			return true
		}
	}
	return false
}

// SeedDir is where the shipped skills are in the image, and what QC_SEED_SKILLS_DIR defaults to.
const SeedDir = "/skills"
