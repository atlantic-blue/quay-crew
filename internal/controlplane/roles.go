package controlplane

import (
	"context"
	"errors"
	"sort"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/role"
	"github.com/atlantic-blue/quay-crew/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The roles a crew holds. A role is imported, pinned to a version, and attached at a level, which is
// the shape a skill and a hook already have.
//
// Nothing here reaches a sandbox. A role is the instruction and the boundary of a session that runs
// as it, and no session runs as one yet, so attaching a role changes what the crew may be asked for
// and changes nothing about a conversation already open.

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

// asRole renders a role for a client. The brief never travels back: a client asked what the crew
// holds, not for a copy of every instruction.
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
