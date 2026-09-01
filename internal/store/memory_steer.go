package store

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// RecordSteer writes one steer and adds it to the count on each job it belongs to, under one lock,
// which is what one transaction means here.
//
// The row and the counts go together deliberately. A mark with no count reads as a job nobody
// steered, and a count with no mark is a number nobody can check, and both are worse than either
// half being missing.
func (m *Memory) RecordSteer(_ context.Context, steer *job.Steer, counted []string) error {
	if steer == nil {
		return errors.New("store: a steer is not nothing")
	}
	if steer.ID == "" || steer.Job == "" || steer.Root == "" {
		return errors.New("store: a steer needs an id, the job it landed on, and the job at the top")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.jobSteers == nil {
		m.jobSteers = map[string]*job.Steer{}
	}
	// The same steer twice leaves one, the way a job event does: a caller that retried a call it
	// never saw the answer to has recorded one moment, not two.
	if _, held := m.jobSteers[steer.ID]; held {
		return nil
	}
	for _, id := range counted {
		if _, held := m.jobs[id]; !held {
			return ErrNotFound
		}
	}
	kept := *steer
	if kept.OccurredAt.IsZero() {
		kept.OccurredAt = time.Now().UTC()
	}
	m.jobSteers[kept.ID] = &kept
	for _, id := range counted {
		m.jobs[id].Steers++
	}
	return nil
}

// ListSteers returns every steer under one job at the top of a tree, oldest first.
func (m *Memory) ListSteers(_ context.Context, root string) ([]*job.Steer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	listed := make([]*job.Steer, 0, len(m.jobSteers))
	for _, held := range m.jobSteers {
		if held.Root != root {
			continue
		}
		kept := *held
		listed = append(listed, &kept)
	}
	// The identifier breaks a tie, so two steers stamped in the same millisecond read back in one
	// order rather than in whichever order the map was walked in.
	sort.SliceStable(listed, func(i, j int) bool {
		if listed[i].OccurredAt.Equal(listed[j].OccurredAt) {
			return listed[i].ID < listed[j].ID
		}
		return listed[i].OccurredAt.Before(listed[j].OccurredAt)
	})
	return listed, nil
}
