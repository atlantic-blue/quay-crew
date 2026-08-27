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
	// The hold goes with the work: a lease left on work that ended reads as held forever.
	found.LeaseOwner, found.LeaseUntil = "", nil
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
	if from.Hands != nil {
		from.Hands = append([]string(nil), from.Hands...)
	}
	if from.Labels != nil {
		labels := make(map[string]string, len(from.Labels))
		for key, value := range from.Labels {
			labels[key] = value
		}
		from.Labels = labels
	}
	from.Deadline = cloneTime(from.Deadline)
	from.LeaseUntil = cloneTime(from.LeaseUntil)
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

// RunnableWork is the work a controller may start: pending, with no parent and nothing it waits
// for, oldest declared first. Work that names a role is in it, and the controller runs it as that
// role.
func (m *Memory) RunnableWork(_ context.Context, limit int) ([]*work.Work, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.workMatching(limit, func(one *work.Work) bool {
		return one.Phase == work.PhasePending && one.Parent == "" && len(one.After) == 0
	}), nil
}

// HeldWork is the work this controller is holding, and only this one: another controller's work is
// not this one's to move. Work with no session yet is left out, because there is no task to read back.
func (m *Memory) HeldWork(_ context.Context, owner string, limit int) ([]*work.Work, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	return m.workMatching(limit, func(one *work.Work) bool {
		return one.Phase == work.PhaseRunning && one.Session != "" &&
			one.LeaseOwner == owner && one.LeaseUntil != nil && one.LeaseUntil.After(now)
	}), nil
}

// ExpiredWork is the work whose holder went away: running, under a lease that has run out or was
// never written.
func (m *Memory) ExpiredWork(_ context.Context, limit int) ([]*work.Work, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	return m.workMatching(limit, func(one *work.Work) bool {
		return one.Phase == work.PhaseRunning &&
			(one.LeaseUntil == nil || !one.LeaseUntil.After(now))
	}), nil
}

// workMatching is the oldest declared work that matches, capped. The caller holds the lock.
func (m *Memory) workMatching(limit int, matches func(*work.Work) bool) []*work.Work {
	found := make([]*work.Work, 0, len(m.work))
	for _, held := range m.work {
		if !matches(held) {
			continue
		}
		kept := cloneWork(*held)
		found = append(found, &kept)
	}
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].CreatedAt.Equal(found[j].CreatedAt) {
			return found[i].ID < found[j].ID
		}
		return found[i].CreatedAt.Before(found[j].CreatedAt)
	})
	if limit > 0 && len(found) > limit {
		found = found[:limit]
	}
	return found
}

// StartWork claims one piece of work. It applies only to work that is still pending, so two
// controllers asking at once leave one task, not two.
func (m *Memory) StartWork(_ context.Context, id string, lease work.Lease, events []*work.Event) (*work.Work, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.work[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != work.PhasePending {
		return nil, work.ErrNotPending
	}
	now := time.Now().UTC()
	found.Phase, found.Attempts = work.PhaseRunning, found.Attempts+1
	found.LeaseOwner, found.LeaseUntil = lease.Owner, leaseEnd(lease)
	found.StartedAt, found.UpdatedAt = &now, now
	if err := m.appendWorkEvents(events); err != nil {
		return nil, err
	}
	kept := cloneWork(*found)
	return &kept, nil
}

// TakeOverWork takes the lease on work whose holder went away. Only where the lease has run out, so
// two controllers finding the same abandoned row leave one holder.
func (m *Memory) TakeOverWork(_ context.Context, id string, lease work.Lease, events []*work.Event) (*work.Work, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.work[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != work.PhaseRunning || (found.LeaseUntil != nil && found.LeaseUntil.After(time.Now())) {
		return nil, work.ErrHeld
	}
	found.LeaseOwner, found.LeaseUntil = lease.Owner, leaseEnd(lease)
	found.UpdatedAt = time.Now().UTC()
	if err := m.appendWorkEvents(events); err != nil {
		return nil, err
	}
	kept := cloneWork(*found)
	return &kept, nil
}

// ReleaseWork puts work back to pending. Only running work with no session under a lease that has
// run out, which is the one state that says for certain no task was ever sent.
func (m *Memory) ReleaseWork(_ context.Context, id string, events []*work.Event) (*work.Work, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.work[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != work.PhaseRunning || found.Session != "" ||
		(found.LeaseUntil != nil && found.LeaseUntil.After(time.Now())) {
		return nil, work.ErrHeld
	}
	found.Phase, found.LeaseOwner, found.LeaseUntil = work.PhasePending, "", nil
	found.StartedAt, found.UpdatedAt = nil, time.Now().UTC()
	if err := m.appendWorkEvents(events); err != nil {
		return nil, err
	}
	kept := cloneWork(*found)
	return &kept, nil
}

// RenewLease moves the holder's hold on. Only the holder renews, so a controller that lost a row
// cannot take it back by renewing.
func (m *Memory) RenewLease(_ context.Context, id string, lease work.Lease) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.work[id]
	if !held {
		return ErrNotFound
	}
	if found.LeaseOwner != lease.Owner {
		return work.ErrHeld
	}
	found.LeaseUntil, found.UpdatedAt = leaseEnd(lease), time.Now().UTC()
	return nil
}

// RecordWorkSession writes which conversation a piece of work runs in. Not a movement, so it writes
// no record of its own.
func (m *Memory) RecordWorkSession(_ context.Context, id, session string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.work[id]
	if !held {
		return ErrNotFound
	}
	found.Session, found.UpdatedAt = session, time.Now().UTC()
	return nil
}

// LandWork writes what came of the work. It applies only to work that is still running, so what a
// piece of work ended as is written once.
func (m *Memory) LandWork(_ context.Context, id string, landed work.Landing, event *work.Event) (*work.Work, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.work[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != work.PhaseRunning {
		return nil, work.ErrNotRunning
	}
	now := time.Now().UTC()
	found.Phase, found.Answer, found.Reason = landed.Phase, landed.Answer, landed.Reason
	found.SpentTokens, found.ObservedVersion = landed.SpentTokens, found.Version
	// The hold goes with the work. A lease left on finished work would read as held forever.
	found.LeaseOwner, found.LeaseUntil = "", nil
	found.FinishedAt, found.UpdatedAt = &now, now
	if err := m.appendWorkEvent(event); err != nil {
		return nil, err
	}
	kept := cloneWork(*found)
	return &kept, nil
}

// appendWorkEvents records several things that happened, the way a store writes them in one
// transaction with the change they describe. The caller holds the lock.
func (m *Memory) appendWorkEvents(events []*work.Event) error {
	for _, event := range events {
		if err := m.appendWorkEvent(event); err != nil {
			return err
		}
	}
	return nil
}

// leaseEnd is when a hold runs out, as the row keeps it.
func leaseEnd(lease work.Lease) *time.Time {
	until := lease.Until.UTC()
	return &until
}
