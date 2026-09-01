package store

import (
	"context"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// What a session leaves behind when it reaches its workspace's context ceiling, and the movement that
// gives the rest of the job to a fresh conversation.
//
// This store keeps the handoffs in a map beside the jobs rather than on the row, which is the table
// the Postgres store keeps, so the two agree about what one job carries. A double whose behaviour is
// looser than the real thing manufactures a green suite.

// RecordJobHandoff writes down the state a fresh session starts this job from.
//
// Only from running, so a handoff cannot be written against a job nobody is doing: a note about work
// that already ended is not a handoff, and a fresh session is never given one.
func (m *Memory) RecordJobHandoff(_ context.Context, id, left, tried, session string,
	event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhaseRunning {
		return nil, job.ErrNotRunning
	}
	if m.jobHandoffs == nil {
		m.jobHandoffs = map[string][]job.Handoff{}
	}
	m.jobHandoffs[id] = append(m.jobHandoffs[id], job.Handoff{
		Job: id, Seq: len(m.jobHandoffs[id]) + 1, Left: left, Tried: tried,
		Session: session, WrittenAt: time.Now().UTC(),
	})
	if err := m.appendJobEvent(event); err != nil {
		return nil, err
	}
	return m.jobWithSteps(*found), nil
}

// HandOffJob puts a running job back to pending and lets go of the session that was doing it.
//
// Only from running and only for the controller holding the lease, which is the condition a requeue
// carries and for the same reason: a controller that lost the row must not take the job away from the
// session another controller is running it in.
//
// The session is cleared rather than kept, which is the one place this differs from a resume. A resume
// goes back into the conversation that did the work, because the work is in it. This exists because
// that conversation is full, so the job has to leave it, and the empty session is also what tells the
// system that the newest handoff is still waiting to be taken up.
func (m *Memory) HandOffJob(_ context.Context, id string, back job.Requeue, event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhaseRunning || found.LeaseOwner != back.Owner {
		return nil, job.ErrHeld
	}
	// Nothing was written down, so a fresh session would start from nothing, which costs more than the
	// session at the ceiling this would replace. Refused here rather than read by the caller first: the
	// session is still answering while a controller decides, and this way a handoff written a moment
	// later is not missed and a job with none is not moved on a stale read.
	if len(m.jobHandoffs[id]) == 0 {
		return nil, job.ErrNothingHandedOver
	}
	// The reason is cleared rather than carrying why it handed over. A pending job with a reason on it
	// is one the system is holding back for want of a machine, and this one is going again at once.
	// Why the conversation changed is on the record as an event, and in the handoff itself.
	found.Phase, found.Session, found.Reason = job.PhasePending, "", ""
	found.LeaseOwner, found.LeaseUntil = "", nil
	// The start goes with the attempt that is over, so the moment on the row is the moment the fresh
	// session actually began. What it has already cost stays, and so do its steps and its pull request:
	// this is one job carrying on, not a second one.
	found.StartedAt, found.UpdatedAt = nil, time.Now().UTC()
	if err := m.appendJobEvent(event); err != nil {
		return nil, err
	}
	return m.jobWithSteps(*found), nil
}
