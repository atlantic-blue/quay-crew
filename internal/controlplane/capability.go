package controlplane

import (
	"context"
	"path"
	"slices"
	"sort"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/sandbox"
	"github.com/atlantic-blue/quay-crew/internal/skill"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// capability is everything a session holds, answered in one place: the skills, where each mounts
// inside the sandbox, and which of them live in the store and need their files written to the host
// before a mount can serve them. The question used to be answered four separate times per sandbox
// creation, four store round trips that could disagree; this is the one answer they all read, and
// the one the skill listing reports, so the listing cannot say one thing while the sandbox does
// another.
type capability struct {
	// held is what the session holds, sorted by name: the crew's own skills and the workspace's,
	// the workspace winning a name collision because the narrower statement is the more deliberate
	// one.
	held []skill.Held
	// mounts is where each held skill appears inside the sandbox, read only, one mount per name.
	mounts []sandbox.Mount
	// attached is the subset that lives in the store, whose files are written out to the
	// workspace's own directory before the mount can serve them. attachedKnown says the store
	// answered: an empty answer still sweeps the directory of skills no longer held, while a
	// failed read must not, because sweeping on a failure would take a live capability's files
	// away over a transient error.
	attached      []store.Imported
	attachedKnown bool
}

// capabilityOf answers what a session holds, in one store round trip.
//
// A failure reading the store is not a failure of the turn: the crew's skills still reach the
// session, and a session with one skill instead of two is better than a session that will not
// start.
func (s *Server) capabilityOf(ctx context.Context, session *quaycrewv1.Thread) capability {
	var caps capability
	attached, err := s.store.WorkspaceSkills(ctx, session.GetWorkspace())
	if err == nil {
		caps.attached = attached
		caps.attachedKnown = true
		workspaceHost, hostKnown := s.storage.WorkspaceSkillsHost(session.GetWorkspace())
		for _, one := range attached {
			caps.held = append(caps.held, skill.Held{
				Skill: one.Skill,
				// Written out of the store into the workspace's own directory, and mounted from
				// there.
				BriefPath: skill.BriefPathIn(sandbox.SkillsPath, one.Name),
			})
			if hostKnown {
				caps.mounts = append(caps.mounts, sandbox.Mount{
					Source:   path.Join(workspaceHost, one.Name),
					Target:   skill.DirIn(sandbox.SkillsPath, one.Name),
					ReadOnly: true,
				})
			}
		}
	}
	for _, given := range s.skills {
		// A name the workspace has already claimed is left to the workspace. Two mounts on one
		// target is a container that will not start.
		if slices.ContainsFunc(caps.held, func(one skill.Held) bool { return one.Name == given.Name }) {
			continue
		}
		caps.held = append(caps.held, skill.Held{
			Skill:     given,
			BriefPath: skill.BriefPathIn(sandbox.SkillsPath, given.Name),
		})
		if s.skillsHost != "" {
			caps.mounts = append(caps.mounts, sandbox.Mount{
				Source:   path.Join(s.skillsHost, given.Name),
				Target:   skill.DirIn(sandbox.SkillsPath, given.Name),
				ReadOnly: true,
			})
		}
	}
	sort.Slice(caps.held, func(i, j int) bool { return caps.held[i].Name < caps.held[j].Name })
	return caps
}
