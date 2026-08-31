package store

import (
	"context"
	"time"

	"github.com/atlantic-blue/krewe/internal/job"
)

// The steps a job finished, and the two answers to a job that failed: continue it, or refuse it.
//
// This store keeps the steps in a map beside the jobs rather than on the row, which is what the
// Postgres store does with its own table, so the two agree about what a listing carries and what one
// job carries. A double whose behaviour is looser than the real thing manufactures a green suite.

// RecordJobStep writes down one thing the session doing this job finished, with the record of it in
// the same movement.
//
// Only from running, so a step cannot be recorded against a job nobody is doing. The same words
// twice leave one step: the record is the set of what is finished, and a session that is continued
// says again what it said before.
func (m *Memory) RecordJobStep(_ context.Context, id, summary, pullRequest string, event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhaseRunning {
		return nil, job.ErrNotRunning
	}
	// A step already on the record writes nothing, and no second record of it either.
	if job.Recorded(m.jobSteps[id], summary) {
		return m.jobWithSteps(*found), nil
	}
	if m.jobSteps == nil {
		m.jobSteps = map[string][]job.Step{}
	}
	m.jobSteps[id] = append(m.jobSteps[id], job.Step{
		Job: id, Seq: len(m.jobSteps[id]) + 1, Summary: summary, FinishedAt: time.Now().UTC(),
	})
	// What the job produced, onto the row, where the row carries none. The first address wins: a
	// session that names two has done more than one job, and the record then points at the first
	// rather than at whichever it mentioned last.
	if pullRequest != "" && found.PullRequest == "" {
		found.PullRequest = pullRequest
	}
	if err := m.appendJobEvent(event); err != nil {
		return nil, err
	}
	return m.jobWithSteps(*found), nil
}

// ResumeJob puts a job that failed back to pending, so a controller starts it again in the session
// it has been in all along.
//
// Only from failed, in the same movement, so two resumes leave one attempt rather than two tasks
// against one conversation. What it failed with moves onto the row as the failure this attempt is
// continuing past, and the reason is cleared: a pending job carrying a reason is one the system is
// holding back for want of a machine, and this one is going again.
//
// The moment it finished is left alone. It is when the attempt being continued ended, which is what
// the session is told to measure its base against, and the next landing writes it again.
func (m *Memory) ResumeJob(_ context.Context, id string, event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if !job.Resumable(found.Phase) {
		return nil, job.ErrNotFailed
	}
	found.Phase, found.Resuming, found.Reason = job.PhasePending, found.Reason, ""
	found.StartedAt, found.UpdatedAt = nil, time.Now().UTC()
	if err := m.appendJobEvent(event); err != nil {
		return nil, err
	}
	return m.jobWithSteps(*found), nil
}

// RefuseJob ends a job that failed, on purpose, so nobody continues it.
//
// Only from failed, which is the phase a resume applies to: refusing is the other answer to the same
// question. It lands in stopped, because stopped is the system's word for an end somebody decided
// on, and a stopped job is never continued.
func (m *Memory) RefuseJob(_ context.Context, id, reason string, event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if !job.Refusable(found.Phase) {
		return nil, job.ErrNotFailed
	}
	now := time.Now().UTC()
	found.Phase, found.Reason = job.PhaseStopped, reason
	found.FinishedAt, found.UpdatedAt = &now, now
	if err := m.appendJobEvent(event); err != nil {
		return nil, err
	}
	return m.jobWithSteps(*found), nil
}

// jobWithSteps is one job as a reader gets it, its finished steps included. The caller holds the
// lock.
func (m *Memory) jobWithSteps(from job.Job) *job.Job {
	kept := cloneJob(from)
	kept.Steps = append([]job.Step(nil), m.jobSteps[from.ID]...)
	if len(kept.Steps) == 0 {
		kept.Steps = nil
	}
	kept.Attempted = append([]job.Attempt(nil), m.jobAttempts[from.ID]...)
	if len(kept.Attempted) == 0 {
		kept.Attempted = nil
	}
	return &kept
}
