package controlplane

import (
	"context"
	"fmt"
	"path"
	"slices"
	"sort"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/name"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/skill"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// systemScope is what a caller says to mean the whole system rather than one workspace. It is the same
// word the context calls address uses, so a skill and a piece of context are given to everything the
// system does the same way, and it is the word no workspace may be called.
const systemScope = name.System

// refusedScope is the refusal for a scope that says the word this level used to take, and nil for
// every other scope. It is here rather than only in the tool because a tool from a build before the
// word moved reaches this process, and so does every channel: without it the call is read as a
// workspace and comes back saying no such workspace, which says nothing about the word.
func refusedScope(scope string) error {
	if err := name.RefuseRetired(scope); err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	return nil
}

// capability is everything a session holds, answered in one place: the skills, where each mounts
// inside the sandbox, and which of them live in the store and need their files written to the host
// before a mount can serve them. The question used to be answered four separate times per sandbox
// creation, four store round trips that could disagree; this is the one answer they all read, and
// the one the skill listing reports, so the listing cannot say one thing while the sandbox does
// another.
type capability struct {
	// held is what the session holds, sorted by name: the system's own skills and the workspace's,
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
	// leftOut is what the workspace holds and the session was not given, each carrying why. Empty
	// when everything held is given. The listing reports these so a skill that is nowhere in the
	// conversation is not also nowhere in the answer.
	leftOut []notGiven
}

// notGiven is a skill the workspace holds that its sessions do not get, and the reason.
type notGiven struct {
	skill.Skill
	// Why is a sentence for a person: what is missing and how to set it.
	Why string
}

// capabilityOf answers what a session holds, in one store round trip.
//
// A failure reading the store is not a failure of the task: the system's skills still reach the
// session, and a session with one skill instead of two is better than a session that will not
// start.
func (s *Server) capabilityOf(ctx context.Context, session *quaycrewv1.Session) capability {
	return s.withoutUnusable(ctx, session.GetWorkspace(), s.heldIn(ctx, session.GetWorkspace()))
}

// heldIn is every skill a workspace's sessions could hold, before the workspace's own secrets are
// held against it: the system's and the workspace's, with where each mounts.
func (s *Server) heldIn(ctx context.Context, workspace string) capability {
	var caps capability
	attached, err := s.store.WorkspaceSkills(ctx, workspace)
	if err == nil {
		attached = s.withSystemSkills(ctx, attached)
		caps.attached = attached
		caps.attachedKnown = true
		workspaceHost, hostKnown := s.storage.WorkspaceSkillsHost(workspace)
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

// withSystemSkills adds what the system holds to what this workspace holds, the workspace winning a name
// collision because the narrower statement is the more deliberate one.
//
// A system skill is rendered into the workspace's own directory and mounted from there, exactly like a
// skill the workspace attached itself. Duplicating the files per workspace costs a few kilobytes and
// buys the whole existing path: the writing out, the sweeping when it is let go, the staleness of a
// sandbox born before it, and one mount root rather than two.
//
// A failure reading the system's skills leaves the workspace's own alone, for the same reason the
// caller survives a failed workspace read: fewer skills is better than no task.
func (s *Server) withSystemSkills(ctx context.Context, attached []store.Imported) []store.Imported {
	system, err := s.store.SystemSkills(ctx)
	if err != nil || len(system) == 0 {
		return attached
	}
	out := attached
	for _, one := range system {
		if slices.ContainsFunc(attached, func(held store.Imported) bool { return held.Name == one.Name }) {
			continue
		}
		out = append(out, one)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// withoutUnusable takes out what the workspace cannot actually use, keeping the reason.
//
// A skill names the secrets it needs and the workspace either has them or does not. Giving a session
// a skill whose secret is missing is worse than not giving it at all: the model reads the brief,
// runs the command, and improvises around the failure, and the operator reads the improvisation as
// the answer.
//
// Refusing the whole task was the earlier answer to that, and it makes one unusable skill enough to
// stop every conversation in the workspace. That trade only held while a skill was attached one
// workspace at a time, deliberately, by the person who had just set its secret.
func (s *Server) withoutUnusable(ctx context.Context, workspace string, caps capability) capability {
	usable := caps.held[:0:0]
	out := map[string]bool{}
	for _, one := range caps.held {
		missing := s.secretMissing(ctx, workspace, one)
		if missing == "" {
			usable = append(usable, one)
			continue
		}
		caps.leftOut = append(caps.leftOut, notGiven{Skill: one.Skill, Why: missing})
		out[one.Name] = true
	}
	if len(caps.leftOut) == 0 {
		return caps
	}
	caps.held = usable
	caps.mounts = slices.DeleteFunc(caps.mounts, func(mount sandbox.Mount) bool {
		return out[path.Base(mount.Target)]
	})
	caps.attached = slices.DeleteFunc(caps.attached, func(one store.Imported) bool {
		return out[one.Name]
	})
	return caps
}

// secretMissing names the first secret the skill needs that the workspace has not set, and says how
// to set it. Empty when the workspace has them all.
func (s *Server) secretMissing(ctx context.Context, workspace string, one skill.Held) string {
	for _, name := range one.SecretNames() {
		value, err := s.secrets.Get(ctx, workspace, name)
		if err != nil || value == "" {
			return fmt.Sprintf("needs the secret %s, which this workspace has not set: %s. "+
				"Set it with krewe secret set %s %s",
				name, one.Secrets[name], workspace, name)
		}
	}
	return ""
}

// boxOf is the sandbox configuration for a session, in one place.
//
// One constructor rather than a struct literal per caller, because the role decides where the
// session's conversation is kept: a caller that built the configuration without it would read the
// workspace's store for a session whose conversation is somewhere else, and answer that a live
// conversation had cost nothing.
func boxOf(session *quaycrewv1.Session) sandbox.Config {
	return sandbox.Config{
		ID:        session.GetId(),
		Workspace: session.GetWorkspace(),
		Project:   session.GetProject(),
	}
}
