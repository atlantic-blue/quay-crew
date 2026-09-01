package store

import (
	"context"
	"sort"

	"github.com/atlantic-blue/quay-krewe/internal/forge"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// UnsettledPullRequests is the jobs whose pull request is worth reading again: one is on the row, and
// the last reading did not say it had merged or closed. Longest unread first, so a batch cap delays a
// reading and never starves one.
func (m *Memory) UnsettledPullRequests(_ context.Context, limit int) ([]*job.Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	found := make([]*job.Job, 0, len(m.jobs))
	for _, held := range m.jobs {
		if held.PullRequest == "" || held.PullRequestState.Settled() {
			continue
		}
		kept := cloneJob(*held)
		found = append(found, &kept)
	}
	// Longest unread first, and a job nobody ever read goes before one that was read at all. Then by
	// when it was declared, so two rows read at the same moment come back in the same order twice.
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i].PullRequestState.ReadAt, found[j].PullRequestState.ReadAt
		if !a.Equal(b) {
			if a.IsZero() || b.IsZero() {
				return a.IsZero()
			}
			return a.Before(b)
		}
		if !found[i].CreatedAt.Equal(found[j].CreatedAt) {
			return found[i].CreatedAt.Before(found[j].CreatedAt)
		}
		return found[i].ID < found[j].ID
	})
	if limit > 0 && len(found) > limit {
		found = found[:limit]
	}
	return found, nil
}

// RecordPullRequest writes what the forge said onto the job.
//
// It is not a movement of the job, so it writes no event and leaves UpdatedAt alone: the job ended
// when it ended, and what happened to the work afterwards happened on the forge.
func (m *Memory) RecordPullRequest(_ context.Context, id string, reading forge.Reading) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	held, found := m.jobs[id]
	if !found {
		return ErrNotFound
	}
	held.PullRequestState = reading.Or()
	return nil
}
