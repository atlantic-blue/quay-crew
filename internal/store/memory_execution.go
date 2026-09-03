package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The runs of the stages of a job, in the memory store.
//
// They are a map of their own beside the jobs, never among them. A run is not declared work, so a
// store that kept the two together would put a row nobody asked for into every listing of the work
// somebody did ask for, which is the fault this table exists to end.

// CreateExecution writes one run of one stage and the record of it together, under one lock.
//
// The claim is checked under the same lock as the write, the way a job's is, so two controllers
// ticking one stage at the same moment leave one run rather than two sessions on one requirement.
func (m *Memory) CreateExecution(_ context.Context, run *job.Execution, event *job.Event) error {
	if err := run.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if held := m.executionClaimHolder(run, time.Now().UTC()); held != nil {
		return held
	}
	if m.executions == nil {
		m.executions = map[string]*job.Execution{}
	}
	if _, held := m.executions[run.ID]; held {
		return fmt.Errorf("store: execution %s already exists", run.ID)
	}
	kept := *run
	if kept.Phase == "" {
		kept.Phase = job.PhasePending
	}
	// The Postgres store stamps these with the database clock, so this one stamps them too.
	if kept.CreatedAt.IsZero() {
		kept.CreatedAt = time.Now().UTC()
	}
	if kept.UpdatedAt.IsZero() {
		kept.UpdatedAt = kept.CreatedAt
	}
	m.executions[kept.ID] = &kept
	return m.appendJobEvent(event)
}

// executionClaimHolder is the run already holding the piece of work this one claims, and nil where
// nothing holds it. The oldest holder, so an answer is the same one every time.
func (m *Memory) executionClaimHolder(run *job.Execution, now time.Time) *job.Held {
	if run.Claim == "" {
		return nil
	}
	var holder *job.Execution
	for _, one := range m.executions {
		if one.Claim != run.Claim || !one.Holding(now) {
			continue
		}
		if holder == nil || one.CreatedAt.Before(holder.CreatedAt) {
			holder = one
		}
	}
	if holder == nil {
		return nil
	}
	return &job.Held{
		Claim: run.Claim, Holder: holder.ID,
		Title:   fmt.Sprintf("the %s stage of job %s, number %d", holder.Stage, holder.Job, holder.Number),
		TakenAt: holder.CreatedAt,
	}
}

// GetExecution reads one run back, whole.
func (m *Memory) GetExecution(_ context.Context, id string) (*job.Execution, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	found, held := m.executions[id]
	if !held {
		return nil, ErrNotFound
	}
	kept := *found
	return &kept, nil
}

// ListExecutions is the runs of one job, oldest first, and of one of its stages where the filter
// names one. Oldest first because a stage reads the newest run of each number and needs the order to
// say which that is.
func (m *Memory) ListExecutions(_ context.Context, filter job.ExecutionFilter) ([]*job.Execution, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	listed := make([]*job.Execution, 0, len(m.executions))
	for _, one := range m.executions {
		if filter.Job != "" && one.Job != filter.Job {
			continue
		}
		if filter.Stage != "" && one.Stage != filter.Stage {
			continue
		}
		kept := *one
		listed = append(listed, &kept)
	}
	sortExecutions(listed)
	return listed, nil
}

// sortExecutions puts the runs oldest first, and settles a tie on the identifier so two runs written
// in the same instant come back in one order rather than whichever the map handed over.
func sortExecutions(runs []*job.Execution) {
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].CreatedAt.Equal(runs[j].CreatedAt) {
			return runs[i].ID < runs[j].ID
		}
		return runs[i].CreatedAt.Before(runs[j].CreatedAt)
	})
}

// RunnableExecutions is the runs a controller may start: pending, oldest first.
func (m *Memory) RunnableExecutions(_ context.Context, limit int) ([]*job.Execution, error) {
	return m.executionsMatching(limit, func(one *job.Execution) bool {
		return one.Phase == job.PhasePending
	}), nil
}

// HeldExecutions is the runs this controller holds: running, under a lease of its own that has not
// run out.
func (m *Memory) HeldExecutions(_ context.Context, owner string, limit int) ([]*job.Execution, error) {
	now := time.Now().UTC()
	return m.executionsMatching(limit, func(one *job.Execution) bool {
		return one.Phase == job.PhaseRunning && one.LeaseOwner == owner &&
			one.LeaseUntil != nil && one.LeaseUntil.After(now)
	}), nil
}

// ExpiredExecutions is the runs whose holder went away: running, under a lease that has run out or
// was never written.
func (m *Memory) ExpiredExecutions(_ context.Context, limit int) ([]*job.Execution, error) {
	now := time.Now().UTC()
	return m.executionsMatching(limit, func(one *job.Execution) bool {
		return one.Phase == job.PhaseRunning && (one.LeaseUntil == nil || !one.LeaseUntil.After(now))
	}), nil
}

func (m *Memory) executionsMatching(limit int, matches func(*job.Execution) bool) []*job.Execution {
	m.mu.RLock()
	defer m.mu.RUnlock()
	found := make([]*job.Execution, 0, len(m.executions))
	for _, one := range m.executions {
		if !matches(one) {
			continue
		}
		kept := *one
		found = append(found, &kept)
	}
	sortExecutions(found)
	if limit > 0 && len(found) > limit {
		found = found[:limit]
	}
	return found
}

// StartExecution claims one run and takes a lease on it. It applies only to a run that is still
// pending, which is what keeps two controllers from both starting it.
func (m *Memory) StartExecution(_ context.Context, id string, lease job.Lease,
	event *job.Event) (*job.Execution, error) {
	return m.moveExecution(id, event, func(one *job.Execution) error {
		if one.Phase != job.PhasePending {
			return job.ErrNotPending
		}
		now := time.Now().UTC()
		one.Phase, one.Attempts = job.PhaseRunning, one.Attempts+1
		one.LeaseOwner, one.LeaseUntil = lease.Owner, leaseUntil(lease)
		one.StartedAt = &now
		return nil
	})
}

// TakeOverExecution takes the lease on a run whose holder went away. It applies only where the lease
// has run out, so two controllers finding the same abandoned row leave one holder.
func (m *Memory) TakeOverExecution(_ context.Context, id string, lease job.Lease) (*job.Execution, error) {
	now := time.Now().UTC()
	return m.moveExecution(id, nil, func(one *job.Execution) error {
		if one.Phase != job.PhaseRunning || (one.LeaseUntil != nil && one.LeaseUntil.After(now)) {
			return job.ErrNotRunning
		}
		one.LeaseOwner, one.LeaseUntil = lease.Owner, leaseUntil(lease)
		return nil
	})
}

// RenewExecutionLease moves this controller's hold on further. Only the holder renews.
func (m *Memory) RenewExecutionLease(_ context.Context, id string, lease job.Lease) error {
	_, err := m.moveExecution(id, nil, func(one *job.Execution) error {
		if one.LeaseOwner != lease.Owner {
			return job.ErrNotRunning
		}
		one.LeaseUntil = leaseUntil(lease)
		return nil
	})
	return err
}

// RecordExecutionSession writes the session the system made for a run. It is not a movement, so it
// carries no record of its own.
func (m *Memory) RecordExecutionSession(_ context.Context, id, session string) error {
	_, err := m.moveExecution(id, nil, func(one *job.Execution) error {
		one.Session = session
		return nil
	})
	return err
}

// RecordExecutionBranch writes where this run's commits ended up. It is not a movement either: the
// run is finished before anything pushes for it, and what this adds is the address a reader opens.
func (m *Memory) RecordExecutionBranch(_ context.Context, id, branch string) error {
	_, err := m.moveExecution(id, nil, func(one *job.Execution) error {
		one.Branch = branch
		return nil
	})
	return err
}

// LandExecution writes what came of the run and lets go of the lease. It applies only to a run that
// is still running.
func (m *Memory) LandExecution(_ context.Context, id string, landed job.ExecutionLanding,
	event *job.Event) (*job.Execution, error) {
	return m.moveExecution(id, event, func(one *job.Execution) error {
		if one.Phase != job.PhaseRunning {
			return job.ErrNotRunning
		}
		now := time.Now().UTC()
		one.Phase, one.Answer, one.Outcome, one.Reason =
			landed.Phase, landed.Answer, landed.Outcome, landed.Reason
		one.PullRequest, one.SpentTokens = landed.PullRequest, landed.SpentTokens
		one.LeaseOwner, one.LeaseUntil = "", nil
		one.FinishedAt = &now
		return nil
	})
}

// moveExecution applies one change to one run under the lock, stamps it, and writes the record of it
// in the same movement.
func (m *Memory) moveExecution(id string, event *job.Event,
	apply func(*job.Execution) error) (*job.Execution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.executions[id]
	if !held {
		return nil, ErrNotFound
	}
	kept := *found
	if err := apply(&kept); err != nil {
		return nil, err
	}
	kept.UpdatedAt = time.Now().UTC()
	m.executions[id] = &kept
	if event != nil {
		if err := m.appendJobEvent(event); err != nil {
			return nil, err
		}
	}
	answer := kept
	return &answer, nil
}

// leaseUntil is the moment a lease runs out, or nil for a lease with no moment.
func leaseUntil(lease job.Lease) *time.Time {
	if lease.Until.IsZero() {
		return nil
	}
	until := lease.Until.UTC()
	return &until
}

// StopExecution halts a run that has not ended, keeping the reason. A run that already ended is
// refused rather than overwritten: how it ended is the record.
func (m *Memory) StopExecution(_ context.Context, id, reason string,
	event *job.Event) (*job.Execution, error) {
	return m.moveExecution(id, event, func(one *job.Execution) error {
		if job.Terminal(one.Phase) {
			return job.ErrNotRunning
		}
		now := time.Now().UTC()
		one.Phase, one.Reason = job.PhaseStopped, reason
		one.LeaseOwner, one.LeaseUntil = "", nil
		one.FinishedAt = &now
		return nil
	})
}
