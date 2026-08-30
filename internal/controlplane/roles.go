package controlplane

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"github.com/atlantic-blue/quay-crew/internal/role"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The roles a crew holds. A role is imported, pinned to a version, and attached at a level, which is
// the shape a skill and a hook already have.
//
// A session may run as one. Dispatch names the role when it makes the session, the session keeps it
// for as long as it lives, and what that session is given is what the role declares and nothing
// else. Attaching a role changes nothing about a conversation already open, because a role is read
// when a session is made.

// ImportRole takes a role into the crew from the files a client read out of its directory.
//
// The files travel and this side validates, because the control plane runs in a container where a
// path on the operator's machine means nothing, and because one validator is one answer.
func (s *Server) ImportRole(ctx context.Context, req *quaycrewv1.ImportRoleRequest) (*quaycrewv1.ImportRoleResponse, error) {
	files := make([]role.File, 0, len(req.GetFiles()))
	for _, file := range req.GetFiles() {
		files = append(files, role.File{Path: file.GetPath(), Body: file.GetBody()})
	}
	loaded, err := role.FromFiles(files)
	if err != nil {
		// The refusal is the role package's own sentence, which names what is wrong and what to do
		// about it. Wrapping it in something vaguer would lose the only useful part.
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := s.store.ImportRole(ctx, store.ImportedRole{Role: loaded}); err != nil {
		if errors.Is(err, store.ErrRoleChanged) {
			return nil, status.Errorf(codes.FailedPrecondition,
				"%s version %d is already imported and is a different role. Raise the version in %s: a workspace pins the version it holds, so changing one underneath it would change how a session already running as it was told to work.",
				loaded.Name, loaded.Version, role.ManifestFile)
		}
		return nil, storeError(err, "import role")
	}
	stored, err := s.store.GetRole(ctx, loaded.Name, loaded.Version)
	if err != nil {
		return nil, storeError(err, "read the imported role")
	}
	return &quaycrewv1.ImportRoleResponse{Role: asRole(stored)}, nil
}

// ListRoles says what the crew has imported, or what one workspace holds.
func (s *Server) ListRoles(ctx context.Context, req *quaycrewv1.ListRolesRequest) (*quaycrewv1.ListRolesResponse, error) {
	var held []store.ImportedRole
	var err error
	if req.GetWorkspace() != "" {
		held, err = s.store.WorkspaceRoles(ctx, req.GetWorkspace())
		if err == nil {
			held = s.withCrewRoles(ctx, held)
		}
	} else {
		held, err = s.store.ListRoles(ctx)
	}
	if err != nil {
		return nil, storeError(err, "list roles")
	}
	// Which of them the crew holds, so a listing says where a role came from rather than leaving the
	// operator to guess why a workspace they attached nothing to has three.
	crew := map[string]bool{}
	if roles, err := s.store.CrewRoles(ctx); err == nil {
		for _, one := range roles {
			crew[one.Name] = true
		}
	}
	out := make([]*quaycrewv1.Role, 0, len(held))
	for _, one := range held {
		carried := asRole(one)
		carried.Crew = crew[one.Name]
		out = append(out, carried)
	}
	return &quaycrewv1.ListRolesResponse{Roles: out}, nil
}

// AttachRole gives a workspace a role, or gives it to the whole crew.
func (s *Server) AttachRole(ctx context.Context, req *quaycrewv1.AttachRoleRequest) (*quaycrewv1.AttachRoleResponse, error) {
	if req.GetScope() == crewScope {
		attached, err := s.store.AttachCrewRole(ctx, req.GetName())
		if err != nil {
			return nil, storeError(err, "attach role")
		}
		carried := asRole(attached)
		carried.Crew = true
		return &quaycrewv1.AttachRoleResponse{Role: carried}, nil
	}
	attached, err := s.store.AttachRole(ctx, req.GetWorkspace(), req.GetName())
	if err != nil {
		return nil, storeError(err, "attach role")
	}
	return &quaycrewv1.AttachRoleResponse{Role: asRole(attached)}, nil
}

// DetachRole takes a role away from a workspace, or away from the crew.
func (s *Server) DetachRole(ctx context.Context, req *quaycrewv1.DetachRoleRequest) (*quaycrewv1.DetachRoleResponse, error) {
	if req.GetScope() == crewScope {
		if err := s.store.DetachCrewRole(ctx, req.GetName()); err != nil {
			return nil, storeError(err, "detach role")
		}
		return &quaycrewv1.DetachRoleResponse{}, nil
	}
	if err := s.store.DetachRole(ctx, req.GetWorkspace(), req.GetName()); err != nil {
		return nil, storeError(err, "detach role")
	}
	return &quaycrewv1.DetachRoleResponse{}, nil
}

// withCrewRoles adds what the crew holds to what a workspace attached for itself.
//
// The workspace's own wins a name, being the narrower and more deliberate statement of what that
// workspace should hold, which is the rule skills already follow.
func (s *Server) withCrewRoles(ctx context.Context, workspace []store.ImportedRole) []store.ImportedRole {
	crew, err := s.store.CrewRoles(ctx)
	if err != nil {
		return workspace
	}
	own := make(map[string]bool, len(workspace))
	for _, one := range workspace {
		own[one.Name] = true
	}
	out := workspace
	for _, one := range crew {
		if !own[one.Name] {
			out = append(out, one)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// asRole renders a role for a client. The brief does not travel here: a listing was asked what the
// crew holds, not for a copy of every instruction, so the brief travels in GetRole and nowhere else.
func asRole(one store.ImportedRole) *quaycrewv1.Role {
	out := &quaycrewv1.Role{
		Name:     one.Name,
		Version:  int32(one.Version),
		Summary:  one.Summary,
		Model:    one.Model,
		Receives: one.Receives,
	}
	if !one.ImportedAt.IsZero() {
		out.ImportedAt = timestamppb.New(one.ImportedAt)
	}
	return out
}

// roleFor is the role a workspace holds under this name, at the version it pinned.
//
// A workspace's own attachment wins over the crew's, which is the rule everywhere else: the narrower
// statement is the more deliberate one. A name nobody attached is refused rather than ignored, and
// the refusal names the role and how to give it, because a step that names a role the workspace does
// not hold has to fail with a sentence rather than half run.
func (s *Server) roleFor(ctx context.Context, workspace, name string) (store.ImportedRole, error) {
	held, err := s.store.WorkspaceRoles(ctx, workspace)
	if err != nil {
		return store.ImportedRole{}, storeError(err, "read the workspace's roles")
	}
	for _, one := range s.withCrewRoles(ctx, held) {
		if one.Name == name {
			return one, nil
		}
	}
	return store.ImportedRole{}, status.Errorf(codes.FailedPrecondition,
		"this workspace does not hold the %s role, so nothing can run as it. Import it and attach it with: quay role attach %s",
		name, name)
}

// receives says whether this session is given a kind of material.
//
// A session that runs as nobody in particular receives everything, which is every session the crew
// had before roles existed. A session that runs as a role receives what the role declares and
// nothing else.
//
// A role that cannot be read is treated as receiving nothing. That is the safe direction and the
// only honest one: the alternative hands a session material its boundary may have forbidden, and a
// boundary that opens when the store is slow is not a boundary.
func (s *Server) receives(ctx context.Context, session *quaycrewv1.Session, material string) bool {
	named := session.GetRole()
	if named == "" {
		return true
	}
	held, err := s.roleFor(ctx, session.GetWorkspace(), named)
	if err != nil {
		return false
	}
	return held.Gets(material)
}

// roleBrief is the instruction of the role a session runs as, empty for a session running as nobody
// in particular or for a role the crew can no longer read.
//
// It is read from the version the workspace pinned, so a role edited after the session started does
// not change how that session was told to work.
func (s *Server) roleBrief(ctx context.Context, session *quaycrewv1.Session) string {
	named := session.GetRole()
	if named == "" {
		return ""
	}
	held, err := s.roleFor(ctx, session.GetWorkspace(), named)
	if err != nil {
		return ""
	}
	return held.Brief
}

// RoleFor is how the job controller reads the role a job names, over an interface that
// carries only the question it asks: does this role receive this material.
//
// The role the workspace holds now, which is the same one the crew would build the session from. Two
// answers to "which role is this" is how a boundary comes to be checked against one role and applied
// against another.
func (s *Server) RoleFor(ctx context.Context, workspace, named string) (job.Receiver, error) {
	held, err := s.roleFor(ctx, workspace, named)
	if err != nil {
		// The sentence, without the status wrapping around it: the controller writes this onto the
		// job's own row, where a reader wants what to do rather than a code and a method name.
		return nil, errors.New(status.Convert(err).Message())
	}
	return held, nil
}

// GetRole reads one role back whole, the brief included.
//
// The brief is the role. It is the several hundred words that decide how a session behaves, and
// until this existed an imported role could not be read back at all: an operator could not diff what
// the crew holds against the file it came from, and could not tell whether a run went the way it did
// because of a clause they edited an hour ago. So this is the audit, and it reads from the same
// place a session is built from rather than from a second copy.
func (s *Server) GetRole(ctx context.Context, req *quaycrewv1.GetRoleRequest) (*quaycrewv1.GetRoleResponse, error) {
	name := req.GetName()
	held, where, err := s.rolesVisibleTo(ctx, req.GetWorkspace())
	if err != nil {
		return nil, err
	}
	var found *store.ImportedRole
	names := make([]string, 0, len(held))
	for at := range held {
		names = append(names, held[at].Name)
		if held[at].Name == name {
			found = &held[at]
		}
	}
	if found == nil {
		return nil, status.Error(codes.NotFound, missingRole(where, name, names))
	}

	carried := asRole(*found)
	crew, err := s.store.CrewRoles(ctx)
	if err != nil {
		return nil, storeError(err, "read the crew's roles")
	}
	for _, one := range crew {
		if one.Name == name {
			carried.Crew = true
		}
	}
	holders, err := s.workspacesHolding(ctx, name)
	if err != nil {
		return nil, err
	}
	return &quaycrewv1.GetRoleResponse{
		Role: carried, Brief: found.Brief, Verbs: found.Verbs, HeldBy: holders,
	}, nil
}

// rolesVisibleTo is what a name could mean at an address, and what to call that address in a
// refusal: the roles one workspace holds, its own and the crew's, or everything the crew has
// imported. It is the rule a listing already follows, so show and list cannot disagree about which
// roles exist.
func (s *Server) rolesVisibleTo(ctx context.Context, workspace string) ([]store.ImportedRole, string, error) {
	if workspace == "" {
		held, err := s.store.ListRoles(ctx)
		if err != nil {
			return nil, "", storeError(err, "list roles")
		}
		return held, "the crew", nil
	}
	held, err := s.store.WorkspaceRoles(ctx, workspace)
	if err != nil {
		return nil, "", storeError(err, "list roles")
	}
	return s.withCrewRoles(ctx, held), "this workspace", nil
}

// workspacesHolding names the workspaces that attached a role for themselves, sorted.
//
// The crew's own holding is not in here and is said separately, because the two are different
// statements: one workspace decided this, or everybody has it including the workspace made tomorrow.
//
// A workspace whose roles cannot be read is left out rather than failing the whole answer. The
// brief is what was asked for, and losing it because one workspace listing was slow would be the
// audit refusing to answer over the least important line of it.
func (s *Server) workspacesHolding(ctx context.Context, name string) ([]string, error) {
	workspaces, err := s.store.ListWorkspaces(ctx)
	if err != nil {
		return nil, storeError(err, "list workspaces")
	}
	var out []string
	for _, workspace := range workspaces {
		held, err := s.store.WorkspaceRoles(ctx, workspace.GetId())
		if err != nil {
			continue
		}
		for _, one := range held {
			if one.Name == name {
				out = append(out, workspace.GetName())
				break
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// missingRole is what to say when a role is asked for by a name nothing here holds.
//
// Saying no and stopping leaves the operator guessing between a typo, a role they never imported and
// a workspace that never attached one. So the refusal names what is actually there: the near
// spellings when there are any, and everything held when there are not, because a short list of real
// names is more use than a correct silence.
func missingRole(where, name string, held []string) string {
	if len(held) == 0 {
		return fmt.Sprintf("%s holds no roles at all, so there is no %s to read. Import one with: quay role import <directory>",
			where, name)
	}
	near := nearRoles(name, held)
	if len(near) > 0 {
		return fmt.Sprintf("%s holds no role called %s. The nearest names it holds are: %s",
			where, name, strings.Join(near, ", "))
	}
	return fmt.Sprintf("%s holds no role called %s. It holds: %s",
		where, name, strings.Join(held, ", "))
}

// nearRoles is the held names a typed name was plausibly meant to be: one that contains the other,
// or one within two edits of it, which covers a transposition, a dropped letter and a plural.
func nearRoles(name string, held []string) []string {
	typed := strings.ToLower(name)
	var near []string
	for _, one := range held {
		lowered := strings.ToLower(one)
		if strings.Contains(lowered, typed) || strings.Contains(typed, lowered) || edits(typed, lowered) <= 2 {
			near = append(near, one)
		}
	}
	sort.Strings(near)
	return near
}

// edits is how many single character changes turn one word into the other.
func edits(from, to string) int {
	previous := make([]int, len(to)+1)
	current := make([]int, len(to)+1)
	for at := range previous {
		previous[at] = at
	}
	for row := 1; row <= len(from); row++ {
		current[0] = row
		for column := 1; column <= len(to); column++ {
			cost := 1
			if from[row-1] == to[column-1] {
				cost = 0
			}
			current[column] = min(previous[column]+1, min(current[column-1]+1, previous[column-1]+cost))
		}
		previous, current = current, previous
	}
	return previous[len(to)]
}
