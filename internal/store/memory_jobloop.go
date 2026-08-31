package store

import (
	"context"
	"time"

	"github.com/atlantic-blue/krewe/internal/job"
)

// What each attempt at a job said, and what the system does when three of them at one step say the
// same thing.
//
// The attempts are kept beside the jobs here and in a table of their own in Postgres, and both are
// held to one conformance suite: a double that accepts what the real store refuses manufactures a
// green suite, and this one decides whether work is stopped.

// LoopJob writes that a job went in circles and takes the route the job declared, in one movement.
//
// Only from running, and only for the controller that holds the lease, so a controller that lost the
// row cannot move another controller's job. Where the job is handed to another role it goes back to
// pending and leaves its conversation behind, because a role is read only when a session is born.
func (m *Memory) LoopJob(_ context.Context, id string, looped job.Loop, event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhaseRunning {
		return nil, job.ErrNotRunning
	}
	if looped.Owner != "" && found.LeaseOwner != looped.Owner {
		return nil, job.ErrHeld
	}
	now := time.Now().UTC()
	found.LoopedStep, found.Phase, found.UpdatedAt = looped.Step, looped.Phase, now
	if looped.To != "" {
		found.EscalatedTo = looped.To
	}
	found.Question, found.Reason = looped.Question, looped.Reason
	// What it was last told, and the failure it was continuing past, both belong to the attempt that
	// has just ended. This job is going somewhere else now, so they come off with the rest of it: a
	// handed session reading an answer to somebody else's question would be given that instead of what
	// the attempts said, and a reader would take the old answer for the answer to the new question.
	found.Told, found.Resuming = "", ""
	if looped.Handed {
		found.Session = ""
	}
	// The hold goes, whichever way the job went. A job waiting for a person, one waiting to be started
	// again as somebody else, and one that has stopped are none of them a job a controller is working
	// on, and a lease left on any of the three reads as held forever.
	found.LeaseOwner, found.LeaseUntil = "", nil
	switch looped.Phase {
	case job.PhaseStopped:
		found.FinishedAt, found.ObservedVersion = &now, found.Version
	case job.PhasePending:
		// Going again, so nothing about it has ended.
		found.StartedAt = nil
	}
	m.recordAttempt(id, looped.Attempt)
	if err := m.appendJobEvent(event); err != nil {
		return nil, err
	}
	return m.jobWithSteps(*found), nil
}

// recordAttempt writes down what one attempt said, and does nothing where that attempt is already on
// the record. The caller holds the lock.
//
// Keyed on the task, because the same task is read again by whichever controller holds the job next.
// An attempt counted twice would manufacture a loop out of one piece of work.
func (m *Memory) recordAttempt(id string, attempt *job.Attempt) {
	if attempt == nil || attempt.Task == "" {
		return
	}
	if job.RecordedAttempt(m.jobAttempts[id], attempt.Task) {
		return
	}
	if m.jobAttempts == nil {
		m.jobAttempts = map[string][]job.Attempt{}
	}
	kept := *attempt
	kept.Job, kept.Seq = id, len(m.jobAttempts[id])+1
	if kept.OccurredAt.IsZero() {
		kept.OccurredAt = time.Now().UTC()
	}
	m.jobAttempts[id] = append(m.jobAttempts[id], kept)
}
