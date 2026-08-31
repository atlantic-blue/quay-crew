package controlplane

import (
	"context"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/job"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetWorkspaceLimits says what a workspace lets its sessions declare.
//
// A workspace nobody has set limits on answers with the defaults rather than with nothing, because
// the defaults are the answer: max_depth is zero, so no session in it may declare a job at all.
func (s *Server) GetWorkspaceLimits(ctx context.Context, req *quaycrewv1.GetWorkspaceLimitsRequest) (
	*quaycrewv1.GetWorkspaceLimitsResponse, error) {
	if req.GetWorkspace() == "" {
		return nil, status.Error(codes.InvalidArgument,
			"which workspace: krewe limits <workspace>, because a ceiling belongs to one workspace")
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
			"which workspace: krewe limits <workspace> --max-depth <n>")
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
		{"reclaim time", int64(asked.GetReclaimSeconds())},
		{"archive time", int64(asked.GetArchiveSeconds())},
	} {
		if refusal.value < 0 {
			return nil, status.Errorf(codes.InvalidArgument,
				"the %s is %d, and a limit cannot be below zero: zero is how a limit is turned off",
				refusal.name, refusal.value)
		}
	}
	// The context ceiling is held to a share rather than to "not below zero", because it is one, and
	// because zero here does not turn it off: it takes the system's own. An operator who wanted no
	// gate and typed zero would get the default, so the refusal says what to type instead.
	if share := asked.GetContextCeilingPercent(); share < 0 || share > 100 {
		return nil, status.Errorf(codes.InvalidArgument,
			"the context ceiling is %d and it is a share of the model's context window, so it is between 1 "+
				"and 100: leave it unset to take the system's own %d, or set 100 to give a session work "+
				"until its window is full", share, job.DefaultContextCeiling)
	}

	written, err := s.store.SetWorkspaceLimits(ctx, job.Limits{
		Workspace: asked.GetWorkspace(), MaxDepth: int(asked.GetMaxDepth()),
		MaxRunning: int(asked.GetMaxRunning()), BudgetTokens: asked.GetBudgetTokens(),
		LeaseSeconds:          int(asked.GetLeaseSeconds()),
		ReclaimSeconds:        int(asked.GetReclaimSeconds()),
		ArchiveSeconds:        int(asked.GetArchiveSeconds()),
		ContextCeilingPercent: int(asked.GetContextCeilingPercent()),
	})
	if err != nil {
		return nil, storeError(err, "set workspace limits")
	}
	return &quaycrewv1.SetWorkspaceLimitsResponse{Limits: asLimits(written)}, nil
}

// asLimits puts a workspace's ceiling on the wire.
func asLimits(from job.Limits) *quaycrewv1.WorkspaceLimits {
	return &quaycrewv1.WorkspaceLimits{
		Workspace: from.Workspace, MaxDepth: int32(from.MaxDepth),
		MaxRunning: int32(from.MaxRunning), BudgetTokens: from.BudgetTokens,
		LeaseSeconds:   int32(from.LeaseSeconds),
		ReclaimSeconds: int32(from.ReclaimSeconds),
		ArchiveSeconds: int32(from.ArchiveSeconds),
		// The share as the row holds it, which is zero where the workspace has said nothing. What zero
		// means is the reader's to say, and both readers say it: the command line prints the system's
		// own beside it, and the controller takes the system's own.
		ContextCeilingPercent: int32(from.ContextCeilingPercent),
	}
}
