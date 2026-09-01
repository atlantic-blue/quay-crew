package store

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/atlantic-blue/krewe/internal/job"
)

// CreateJob writes a job and the record of its declaration together, under one lock,
// which is what one transaction means here.
func (m *Memory) CreateJob(_ context.Context, declared *job.Job, event *job.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Under the same lock as the write, because a check that answers before the write is a check two
	// callers can both pass. The Postgres store takes a lock on the claim for the same reason.
	if held := m.claimHolder(declared, time.Now().UTC()); held != nil {
		return held
	}
	if err := m.writeJob(declared); err != nil {
		return err
	}
	return m.appendJobEvent(event)
}

// claimHolder is the job already holding the piece of work this declaration claims, and nil where
// nothing holds it.
//
// The oldest holder, so an answer is the same one every time. Two live jobs cannot hold one claim
// once this check is in place, and a store that picked whichever the map handed it first would
// answer differently on two runs of the same data.
func (m *Memory) claimHolder(declared *job.Job, now time.Time) *job.Held {
	if declared.Claim == "" {
		return nil
	}
	var holder *job.Job
	for _, one := range m.jobs {
		if one.Workspace != declared.Workspace || one.Claim != declared.Claim || !one.Holding(now) {
			continue
		}
		if holder == nil || one.CreatedAt.Before(holder.CreatedAt) {
			holder = one
		}
	}
	if holder == nil {
		return nil
	}
	return &job.Held{Claim: declared.Claim, Holder: holder.ID, Title: holder.Title, TakenAt: holder.CreatedAt}
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

// GetJob reads one job back, whole: its answer and the steps its session finished.
func (m *Memory) GetJob(_ context.Context, id string) (*job.Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	return m.jobWithSteps(*found), nil
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
	// A window about what finished orders by the moment a job finished, and everything else orders by
	// the moment it was declared. A row with no moment at all sorts last rather than crashing the
	// listing: the filter above should already have dropped it.
	ordering := func(one *job.Job) time.Time {
		if filter.FinishedSince == nil {
			return one.CreatedAt
		}
		if one.FinishedAt == nil {
			return time.Time{}
		}
		return *one.FinishedAt
	}
	sort.SliceStable(listed, func(i, j int) bool {
		left, right := ordering(listed[i]), ordering(listed[j])
		if left.Equal(right) {
			return listed[i].ID > listed[j].ID
		}
		return left.After(right)
	})
	if filter.Limit > 0 && len(listed) > filter.Limit {
		listed = listed[:filter.Limit]
	}
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

// AskJob puts a running job's question on the record and stops it there.
//
// Only from running, so a job that never started and a job that ended cannot ask. The hold goes with
// it: no controller is holding an asking job, because there is nothing to come back for until a
// person answers.
//
// What it was last told is cleared, so the question on the row and the answer beside it are always
// about the same decision. The previous answer is in the record, which is where the whole history of
// the job is.
func (m *Memory) AskJob(_ context.Context, id, question string, event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhaseRunning {
		return nil, job.ErrNotRunning
	}
	found.Phase, found.Question, found.Told = job.PhaseAsking, question, ""
	// And the resume with it. A job that asked is carried on by the answer rather than by the steps it
	// had finished, so only one of the two is ever the instruction in hand.
	found.Resuming = ""
	found.LeaseOwner, found.LeaseUntil = "", nil
	found.UpdatedAt = time.Now().UTC()
	if err := m.appendJobEvent(event); err != nil {
		return nil, err
	}
	kept := cloneJob(*found)
	return &kept, nil
}

// AnswerJob writes what a person decided and puts the job back to pending, so a controller starts it
// again and hands the answer to the session that asked.
//
// Only from asking, in the same movement, so two people answering at once leave one answer and one
// task rather than two of each.
func (m *Memory) AnswerJob(_ context.Context, id, answer string, event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhaseAsking {
		return nil, job.ErrNotAsking
	}
	found.Phase, found.Told = job.PhasePending, answer
	// The start goes with the attempt that is over, so the moment on the row is the moment the
	// attempt carrying the answer began. The attempt that asked is on the record.
	found.StartedAt, found.UpdatedAt = nil, time.Now().UTC()
	if err := m.appendJobEvent(event); err != nil {
		return nil, err
	}
	kept := cloneJob(*found)
	return &kept, nil
}

// ProposeJobPlan writes the plan the crew wrote and puts the question about it to a person, in one
// movement.
//
// Only from running, which is what asking already applies to: a job nothing is running has nobody to
// write a plan. The hold goes with it, because nothing is coming back until a person answers.
func (m *Memory) ProposeJobPlan(_ context.Context, id, plan, question string,
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
	found.Phase, found.Plan, found.Question, found.Told = job.PhaseAsking, plan, question, ""
	found.LeaseOwner, found.LeaseUntil = "", nil
	found.UpdatedAt = time.Now().UTC()
	if err := m.appendJobEvent(event); err != nil {
		return nil, err
	}
	kept := cloneJob(*found)
	return &kept, nil
}

// ApproveJobPlan records that a person approved the plan and puts the job back to pending, so a
// controller starts the work against it.
//
// Only from asking and only where the plan is not approved yet, in the same movement, so two people
// approving at once leave one approval and one task.
func (m *Memory) ApproveJobPlan(_ context.Context, id string, event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhaseAsking || found.PlanApproved {
		return nil, job.ErrNotAsking
	}
	// What it was told is cleared rather than kept, which is the opposite of what an ordinary answer
	// does, and the difference is what the session is started again with. An ordinary answer is the
	// instruction in hand: the session asked something and carries on from what it was told. An
	// approval is not an instruction to anybody. It says the work may begin, so what the session is
	// given is the work and the plan it is held to, and a "yes" left on the row would be sent instead
	// of it. That a person approved is on the record as the event and on the row as the flag.
	found.Phase, found.Told, found.PlanApproved = job.PhasePending, "", true
	found.StartedAt, found.UpdatedAt = nil, time.Now().UTC()
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
	// A job that has not finished is not late in the window, it is outside the question: the
	// window is about jobs that ended.
	case filter.FinishedSince != nil && held.FinishedAt == nil:
		return false
	case filter.FinishedSince != nil && held.FinishedAt.Before(*filter.FinishedSince):
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

// AnythingMoving says whether any job is running or asking: whether this system is doing anything
// at all.
func (m *Memory) AnythingMoving(_ context.Context) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, one := range m.jobs {
		if one.Phase == job.PhaseRunning || one.Phase == job.PhaseAsking {
			return true, nil
		}
	}
	return false, nil
}

// TurnedAwayJob is the job the machine had no room for: pending, carrying a reason, oldest declared
// first. Only the system writes a reason on a pending job, and only when it holds the job back.
func (m *Memory) TurnedAwayJob(_ context.Context, limit int) ([]*job.Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobMatching(limit, func(one *job.Job) bool {
		return one.Phase == job.PhasePending && one.Reason != ""
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
	// The reason goes with the pending phase it described. A job held for want of room and then
	// admitted must not carry "there is not enough memory" while it runs.
	found.Reason = ""
	found.LeaseOwner, found.LeaseUntil = lease.Owner, leaseEnd(lease)
	found.StartedAt, found.UpdatedAt = &now, now
	if err := m.appendJobEvents(events); err != nil {
		return nil, err
	}
	// With its steps, because this is the row the controller builds the task from: a job being
	// continued is sent what it already finished rather than its brief.
	return m.jobWithSteps(*found), nil
}

// HoldJob says on a pending job why it is not being started. The phase does not move: the job is
// still pending and still the next thing this system will run, and the reason is what tells an
// operator whether it is waiting its turn or waiting for a machine.
func (m *Memory) HoldJob(_ context.Context, id, reason string, event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhasePending {
		return nil, job.ErrNotPending
	}
	found.Reason, found.UpdatedAt = reason, time.Now().UTC()
	if event != nil {
		if err := m.appendJobEvents([]*job.Event{event}); err != nil {
			return nil, err
		}
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

// RequeueJob puts a running job back to pending because the system could not start it. Only where
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

// ReplaceJobProduct writes the one sentence a job serves over what it carried.
//
// The version rises with it, because the sentence is a declared field and a status has to be able to
// tell a current reading from a stale one.
func (m *Memory) ReplaceJobProduct(_ context.Context, id, product string, event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	found.Product = job.TidySentence(product)
	found.Version, found.UpdatedAt = found.Version+1, time.Now().UTC()
	if err := m.appendJobEvent(event); err != nil {
		return nil, err
	}
	kept := cloneJob(*found)
	return &kept, nil
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
	// Unless the landing read none and the row already carries one: a step that named the pull request
	// wrote it before any answer landed, and a job that failed carries no answer to read.
	if landed.PullRequest != "" {
		found.PullRequest = landed.PullRequest
	}
	// What read this work before it settled, so a settled job says whether anything independent
	// agreed with its answer rather than leaving a reader to open two conversations.
	found.Reviewed, found.Tested = landed.Reviewed, landed.Tested
	found.SpentTokens, found.ObservedVersion = landed.SpentTokens, found.Version
	// The hold goes with the job. A lease left on finished job would read as held forever.
	found.LeaseOwner, found.LeaseUntil = "", nil
	found.FinishedAt, found.UpdatedAt = &now, now
	// What the attempt said, in the same movement as what came of it. A landing with no attempt behind
	// it would leave the record unable to say whether this job was going anywhere.
	m.recordAttempt(id, landed.Attempt)
	if err := m.appendJobEvent(event); err != nil {
		return nil, err
	}
	return m.jobWithSteps(*found), nil
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

// JobHistory returns every job declared inside a window, as digests, newest first.
func (m *Memory) JobHistory(_ context.Context, query job.HistoryQuery) ([]*job.Digest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	history := make([]*job.Digest, 0, len(m.jobs))
	for _, held := range m.jobs {
		if !inHistory(held, query) {
			continue
		}
		history = append(history, job.DigestOf(held))
	}
	job.SortDigests(history)
	return history, nil
}

// inHistory says whether one job belongs in a history: it sits where the query is reading, and it was
// declared inside the window.
func inHistory(held *job.Job, query job.HistoryQuery) bool {
	switch {
	case query.Project != "":
		if held.Project != query.Project {
			return false
		}
	case query.Workspace != "":
		if held.Workspace != query.Workspace {
			return false
		}
	}
	return query.Window.Holds(held.CreatedAt)
}
