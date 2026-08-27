package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/work"
)

// CreateWork writes a piece of work and the record of its declaration together, under one lock,
// which is what one transaction means here.
func (m *Memory) CreateWork(_ context.Context, declared *work.Work, event *work.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.work == nil {
		m.work = map[string]*work.Work{}
	}
	if _, held := m.work[declared.ID]; held {
		return fmt.Errorf("store: work %s already exists", declared.ID)
	}
	kept := cloneWork(*declared)
	// The Postgres store stamps these with the database clock, so this one stamps them too. A store
	// that leaves them empty passes every test the other fails.
	if kept.CreatedAt.IsZero() {
		kept.CreatedAt = time.Now().UTC()
	}
	if kept.UpdatedAt.IsZero() {
		kept.UpdatedAt = kept.CreatedAt
	}
	m.work[declared.ID] = &kept
	return m.appendWorkEvent(event)
}

// GetWork reads one piece of work back, whole.
func (m *Memory) GetWork(_ context.Context, id string) (*work.Work, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	found, held := m.work[id]
	if !held {
		return nil, ErrNotFound
	}
	kept := cloneWork(*found)
	return &kept, nil
}

// ListWork returns what matches, newest first, without answers: a listing of a hundred answers is a
// listing nobody can read.
func (m *Memory) ListWork(_ context.Context, filter work.Filter) ([]*work.Work, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	listed := make([]*work.Work, 0, len(m.work))
	for _, held := range m.work {
		if !matchesWork(held, filter) {
			continue
		}
		kept := cloneWork(*held)
		kept.Answer = ""
		listed = append(listed, &kept)
	}
	sort.SliceStable(listed, func(i, j int) bool {
		if listed[i].CreatedAt.Equal(listed[j].CreatedAt) {
			return listed[i].ID > listed[j].ID
		}
		return listed[i].CreatedAt.After(listed[j].CreatedAt)
	})
	return listed, nil
}

// StopWork halts work that has not ended, keeping the reason. Work that already ended is refused
// rather than overwritten.
func (m *Memory) StopWork(_ context.Context, id, reason string, event *work.Event) (*work.Work, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.work[id]
	if !held {
		return nil, ErrNotFound
	}
	if work.Terminal(found.Phase) {
		return nil, fmt.Errorf("store: work %s is %s, and work that already ended is not stopped again", id, found.Phase)
	}
	now := time.Now().UTC()
	found.Phase, found.Reason = work.PhaseStopped, reason
	found.FinishedAt, found.UpdatedAt = &now, now
	if err := m.appendWorkEvent(event); err != nil {
		return nil, err
	}
	kept := cloneWork(*found)
	return &kept, nil
}

// ListWorkEvents returns one piece of work's own history, oldest first.
func (m *Memory) ListWorkEvents(_ context.Context, id string) ([]*work.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	events := make([]*work.Event, 0, len(m.workEvents))
	for _, held := range m.workEvents {
		if held.Work != id {
			continue
		}
		kept := *held
		events = append(events, &kept)
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].OccurredAt.Equal(events[j].OccurredAt) {
			return events[i].ID < events[j].ID
		}
		return events[i].OccurredAt.Before(events[j].OccurredAt)
	})
	return events, nil
}

// appendWorkEvent records one thing that happened. The same event twice leaves one, the way a task
// does. The caller holds the lock.
func (m *Memory) appendWorkEvent(event *work.Event) error {
	if event == nil {
		return nil
	}
	if event.ID == "" || event.Kind == "" {
		return fmt.Errorf("store: a work event needs an id and a kind")
	}
	if m.workEvents == nil {
		m.workEvents = map[string]*work.Event{}
	}
	if _, held := m.workEvents[event.ID]; held {
		return nil
	}
	kept := *event
	m.workEvents[event.ID] = &kept
	return nil
}

// matchesWork is the filter, in one place, so the two stores narrow a listing the same way.
func matchesWork(held *work.Work, filter work.Filter) bool {
	switch {
	case filter.Project != "" && held.Project != filter.Project:
		return false
	case filter.Project == "" && filter.Workspace != "" && held.Workspace != filter.Workspace:
		return false
	case filter.Parent != "" && held.Parent != filter.Parent:
		return false
	case filter.Parent == "" && filter.Root && held.Parent != "":
		return false
	case filter.Phase != "" && held.Phase != filter.Phase:
		return false
	}
	if filter.LabelKey == "" {
		return true
	}
	value, carried := held.Labels[filter.LabelKey]
	return carried && (filter.LabelValue == "" || value == filter.LabelValue)
}

// cloneWork hands out a copy, so a caller holding a piece of work cannot reach into the store's own.
func cloneWork(from work.Work) work.Work {
	if from.After != nil {
		from.After = append([]string(nil), from.After...)
	}
	if from.Labels != nil {
		labels := make(map[string]string, len(from.Labels))
		for key, value := range from.Labels {
			labels[key] = value
		}
		from.Labels = labels
	}
	from.Deadline = cloneTime(from.Deadline)
	from.StartedAt = cloneTime(from.StartedAt)
	from.FinishedAt = cloneTime(from.FinishedAt)
	return from
}

func cloneTime(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	copied := *at
	return &copied
}
