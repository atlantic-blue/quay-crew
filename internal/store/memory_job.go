package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/job"
)

// CreateJob writes a job and the record of its declaration together, under one lock,
// which is what one transaction means here.
func (m *Memory) CreateJob(_ context.Context, declared *job.Job, event *job.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.writeJob(declared); err != nil {
		return err
	}
	return m.appendJobEvent(event)
}

// writeJob puts one job in the store. The caller holds the lock, which is what lets a
// declaration land in the same transaction as whatever asked for it.
func (m *Memory) writeJob(declared *job.Job) error {
	if m.jobs == nil {
		m.jobs = map[string]*job.Job{}
	}
	if _, held := m.jobs[declared.ID]; held {
		return fmt.Errorf("store: job %s already exists", declared.ID)
	}
	kept := cloneJob(*declared)
	// The Postgres store stamps these with the database clock, so this one stamps them too. A store
	// that leaves them empty passes every test the other fails.
	if kept.CreatedAt.IsZero() {
		kept.CreatedAt = time.Now().UTC()
	}
	if kept.UpdatedAt.IsZero() {
		kept.UpdatedAt = kept.CreatedAt
	}
	m.jobs[declared.ID] = &kept
	return nil
}

// GetJob reads one job back, whole.
func (m *Memory) GetJob(_ context.Context, id string) (*job.Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	kept := cloneJob(*found)
	return &kept, nil
}

// ListJob returns what matches, newest first, without answers: a listing of a hundred answers is a
// listing nobody can read.
func (m *Memory) ListJobs(_ context.Context, filter job.Filter) ([]*job.Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	listed := make([]*job.Job, 0, len(m.jobs))
	for _, held := range m.jobs {
		if !matchesJob(held, filter) {
			continue
		}
		kept := cloneJob(*held)
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

// StopJob halts job that has not ended, keeping the reason. Job that already ended is refused
// rather than overwritten.
func (m *Memory) StopJob(_ context.Context, id, reason string, event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if job.Terminal(found.Phase) {
		return nil, fmt.Errorf("store: job %s is %s, and a job that already ended is not stopped again", id, found.Phase)
	}
	now := time.Now().UTC()
	found.Phase, found.Reason = job.PhaseStopped, reason
	// The hold goes with the job: a lease left on a job that ended reads as held forever.
	found.LeaseOwner, found.LeaseUntil = "", nil
	found.FinishedAt, found.UpdatedAt = &now, now
	if err := m.appendJobEvent(event); err != nil {
		return nil, err
	}
	kept := cloneJob(*found)
	return &kept, nil
}

// ListJobEvents returns one job's own history, oldest first.
func (m *Memory) ListJobEvents(_ context.Context, id string) ([]*job.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	events := make([]*job.Event, 0, len(m.jobEvents))
	for _, held := range m.jobEvents {
		if held.Job != id {
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

// appendJobEvent records one thing that happened. The same event twice leaves one, the way a task
// does. The caller holds the lock.
func (m *Memory) appendJobEvent(event *job.Event) error {
	if event == nil {
		return nil
	}
	if event.ID == "" || event.Kind == "" {
		return fmt.Errorf("store: a job event needs an id and a kind")
	}
	if m.jobEvents == nil {
		m.jobEvents = map[string]*job.Event{}
	}
	if _, held := m.jobEvents[event.ID]; held {
		return nil
	}
	kept := *event
	m.jobEvents[event.ID] = &kept
	return nil
}

// matchesJob is the filter, in one place, so the two stores narrow a listing the same way.
func matchesJob(held *job.Job, filter job.Filter) bool {
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

// cloneJob hands out a copy, so a caller holding a job cannot reach into the store's own.
func cloneJob(from job.Job) job.Job {
	if from.After != nil {
		from.After = append([]string(nil), from.After...)
	}
	if from.Requires != nil {
		from.Requires = append([]string(nil), from.Requires...)
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

// RunnableJob is the job a controller may start: pending with nothing it waits for, oldest
// declared first. Job under a parent is in it, because a flow run declares every step under its own
// a job, and a job that names a role is in it, and the controller runs it as that role.
func (m *Memory) RunnableJob(_ context.Context, limit int) ([]*job.Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobMatching(limit, func(one *job.Job) bool {
		return one.Phase == job.PhasePending && len(one.After) == 0
	}), nil
}

// HeldJob is the job this controller is holding, and only this one: another controller's job is
// not this one's to move. Job with no session yet is left out, because there is no task to read back.
func (m *Memory) HeldJob(_ context.Context, owner string, limit int) ([]*job.Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	return m.jobMatching(limit, func(one *job.Job) bool {
		return one.Phase == job.PhaseRunning && one.Session != "" &&
			one.LeaseOwner == owner && one.LeaseUntil != nil && one.LeaseUntil.After(now)
	}), nil
}

// ExpiredJob is the job whose holder went away: running, under a lease that has run out or was
// never written.
func (m *Memory) ExpiredJob(_ context.Context, limit int) ([]*job.Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now()
	return m.jobMatching(limit, func(one *job.Job) bool {
		return one.Phase == job.PhaseRunning &&
			(one.LeaseUntil == nil || !one.LeaseUntil.After(now))
	}), nil
}

// jobMatching is the oldest declared job that matches, capped. The caller holds the lock.
func (m *Memory) jobMatching(limit int, matches func(*job.Job) bool) []*job.Job {
	found := make([]*job.Job, 0, len(m.jobs))
	for _, held := range m.jobs {
		if !matches(held) {
			continue
		}
		kept := cloneJob(*held)
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

// StartJob claims one job. It applies only to a job that is still pending, so two
// controllers asking at once leave one task, not two.
func (m *Memory) StartJob(_ context.Context, id string, lease job.Lease, events []*job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhasePending {
		return nil, job.ErrNotPending
	}
	now := time.Now().UTC()
	found.Phase, found.Attempts = job.PhaseRunning, found.Attempts+1
	found.LeaseOwner, found.LeaseUntil = lease.Owner, leaseEnd(lease)
	found.StartedAt, found.UpdatedAt = &now, now
	if err := m.appendJobEvents(events); err != nil {
		return nil, err
	}
	kept := cloneJob(*found)
	return &kept, nil
}

// TakeOverJob takes the lease on a job whose holder went away. Only where the lease has run out, so
// two controllers finding the same abandoned row leave one holder.
func (m *Memory) TakeOverJob(_ context.Context, id string, lease job.Lease, events []*job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhaseRunning || (found.LeaseUntil != nil && found.LeaseUntil.After(time.Now())) {
		return nil, job.ErrHeld
	}
	found.LeaseOwner, found.LeaseUntil = lease.Owner, leaseEnd(lease)
	found.UpdatedAt = time.Now().UTC()
	if err := m.appendJobEvents(events); err != nil {
		return nil, err
	}
	kept := cloneJob(*found)
	return &kept, nil
}

// ReleaseJob puts job back to pending. Only running job with no session under a lease that has
// run out, which is the one state that says for certain no task was ever sent.
func (m *Memory) ReleaseJob(_ context.Context, id string, events []*job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhaseRunning || found.Session != "" ||
		(found.LeaseUntil != nil && found.LeaseUntil.After(time.Now())) {
		return nil, job.ErrHeld
	}
	found.Phase, found.LeaseOwner, found.LeaseUntil = job.PhasePending, "", nil
	found.StartedAt, found.UpdatedAt = nil, time.Now().UTC()
	if err := m.appendJobEvents(events); err != nil {
		return nil, err
	}
	kept := cloneJob(*found)
	return &kept, nil
}

// RequeueJob puts a running job back to pending because the crew could not start it. Only where
// this controller still holds the lease, so a controller that lost the row cannot put another
// controller's job back under it.
func (m *Memory) RequeueJob(_ context.Context, id string, back job.Requeue, events []*job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhaseRunning || found.LeaseOwner != back.Owner {
		return nil, job.ErrHeld
	}
	found.Phase, found.Reason = job.PhasePending, back.Reason
	found.LeaseOwner, found.LeaseUntil = "", nil
	// The start goes with the attempt that did not happen, so the moment on the row is the moment the
	// attempt that runs it actually began. What it has already cost stays: attempts counts the tries.
	found.StartedAt, found.UpdatedAt = nil, time.Now().UTC()
	if err := m.appendJobEvents(events); err != nil {
		return nil, err
	}
	kept := cloneJob(*found)
	return &kept, nil
}

// RenewLease moves the holder's hold on. Only the holder renews, so a controller that lost a row
// cannot take it back by renewing.
func (m *Memory) RenewLease(_ context.Context, id string, lease job.Lease) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return ErrNotFound
	}
	if found.LeaseOwner != lease.Owner {
		return job.ErrHeld
	}
	found.LeaseUntil, found.UpdatedAt = leaseEnd(lease), time.Now().UTC()
	return nil
}

// RecordJobSession writes which conversation a job runs in. Not a movement, so it writes
// no record of its own.
func (m *Memory) RecordJobSession(_ context.Context, id, session string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return ErrNotFound
	}
	found.Session, found.UpdatedAt = session, time.Now().UTC()
	return nil
}

// LandJob writes what came of the job. It applies only to a job that is still running, so what a
// job ended as is written once.
func (m *Memory) LandJob(_ context.Context, id string, landed job.Landing, event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhaseRunning {
		return nil, job.ErrNotRunning
	}
	now := time.Now().UTC()
	found.Phase, found.Answer, found.Reason = landed.Phase, landed.Answer, landed.Reason
	found.SpentTokens, found.ObservedVersion = landed.SpentTokens, found.Version
	// The hold goes with the job. A lease left on finished job would read as held forever.
	found.LeaseOwner, found.LeaseUntil = "", nil
	found.FinishedAt, found.UpdatedAt = &now, now
	if err := m.appendJobEvent(event); err != nil {
		return nil, err
	}
	kept := cloneJob(*found)
	return &kept, nil
}

// appendJobEvents records several things that happened, the way a store writes them in one
// transaction with the change they describe. The caller holds the lock.
func (m *Memory) appendJobEvents(events []*job.Event) error {
	for _, event := range events {
		if err := m.appendJobEvent(event); err != nil {
			return err
		}
	}
	return nil
}

// leaseEnd is when a hold runs out, as the row keeps it.
func leaseEnd(lease job.Lease) *time.Time {
	until := lease.Until.UTC()
	return &until
}
