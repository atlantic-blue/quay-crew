package store

import (
	"context"

	"github.com/atlantic-blue/quay-crew/internal/job"
)

// WorkspaceLimits reads what a workspace lets its sessions declare. A workspace nobody has set
// limits on takes the defaults, and the default for max_depth is zero: default deny.
func (m *Memory) WorkspaceLimits(_ context.Context, workspace string) (job.Limits, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	held, set := m.limits[workspace]
	if !set {
		return job.Limits{Workspace: workspace}, nil
	}
	return held, nil
}

// SetWorkspaceLimits writes the ceiling, whole.
func (m *Memory) SetWorkspaceLimits(_ context.Context, limits job.Limits) (job.Limits, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.limits == nil {
		m.limits = map[string]job.Limits{}
	}
	m.limits[limits.Workspace] = limits
	return limits, nil
}
