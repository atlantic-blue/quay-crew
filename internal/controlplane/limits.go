package controlplane

import (
	"context"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/work"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetWorkspaceLimits says what a workspace lets its sessions declare.
//
// A workspace nobody has set limits on answers with the defaults rather than with nothing, because
// the defaults are the answer: max_depth is zero, so no session in it may declare work at all.
func (s *Server) GetWorkspaceLimits(ctx context.Context, req *quaycrewv1.GetWorkspaceLimitsRequest) (
	*quaycrewv1.GetWorkspaceLimitsResponse, error) {
	if req.GetWorkspace() == "" {
		return nil, status.Error(codes.InvalidArgument,
			"which workspace: quay limits <workspace>, because a ceiling belongs to one workspace")
	}
	if _, err := s.store.GetWorkspace(ctx, req.GetWorkspace()); err != nil {
		return nil, storeError(err, "workspace")
	}
	limits, err := s.store.WorkspaceLimits(ctx, req.GetWorkspace())
	if err != nil {
		return nil, storeError(err, "workspace limits")
	}
	return &quaycrewv1.GetWorkspaceLimitsResponse{Limits: asLimits(limits)}, nil
}

// SetWorkspaceLimits writes the ceiling.
//
// The whole row is written as it arrives, so changing one number means reading the row and sending
// it back. A partial write would leave an operator guessing which of the four they had just changed.
func (s *Server) SetWorkspaceLimits(ctx context.Context, req *quaycrewv1.SetWorkspaceLimitsRequest) (
	*quaycrewv1.SetWorkspaceLimitsResponse, error) {
	asked := req.GetLimits()
	if asked.GetWorkspace() == "" {
		return nil, status.Error(codes.InvalidArgument,
			"which workspace: quay limits <workspace> --max-depth <n>")
	}
	if _, err := s.store.GetWorkspace(ctx, asked.GetWorkspace()); err != nil {
		return nil, storeError(err, "workspace")
	}
	for _, refusal := range []struct {
		name  string
		value int64
	}{
		{"max depth", int64(asked.GetMaxDepth())},
		{"max running", int64(asked.GetMaxRunning())},
		{"budget", asked.GetBudgetTokens()},
		{"lease", int64(asked.GetLeaseSeconds())},
	} {
		if refusal.value < 0 {
			return nil, status.Errorf(codes.InvalidArgument,
				"the %s is %d, and a limit cannot be below zero: zero is how a limit is turned off",
				refusal.name, refusal.value)
		}
	}

	written, err := s.store.SetWorkspaceLimits(ctx, work.Limits{
		Workspace: asked.GetWorkspace(), MaxDepth: int(asked.GetMaxDepth()),
		MaxRunning: int(asked.GetMaxRunning()), BudgetTokens: asked.GetBudgetTokens(),
		LeaseSeconds: int(asked.GetLeaseSeconds()),
	})
	if err != nil {
		return nil, storeError(err, "set workspace limits")
	}
	return &quaycrewv1.SetWorkspaceLimitsResponse{Limits: asLimits(written)}, nil
}

// asLimits puts a workspace's ceiling on the wire.
func asLimits(from work.Limits) *quaycrewv1.WorkspaceLimits {
	return &quaycrewv1.WorkspaceLimits{
		Workspace: from.Workspace, MaxDepth: int32(from.MaxDepth),
		MaxRunning: int32(from.MaxRunning), BudgetTokens: from.BudgetTokens,
		LeaseSeconds: int32(from.LeaseSeconds),
	}
}
