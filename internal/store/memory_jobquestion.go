package store

import (
	"context"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The questions a reading of a plan could not settle, and the settling of one by a later reader.
//
// The rows live in a map beside the jobs rather than on the row, which is the table the Postgres
// store keeps, so the two agree about what one job carries. A double whose behaviour is looser than
// the real thing manufactures a green suite.

// RecordJobQuestion writes down one thing the reading doing this job could not settle.
//
// Only from running, so a question cannot be written against a job nobody is doing, which is the
// rule a step already keeps.
func (m *Memory) RecordJobQuestion(_ context.Context, id string, asking job.Question, event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhaseRunning {
		return nil, job.ErrNotRunning
	}
	if m.jobQuestions == nil {
		m.jobQuestions = map[string][]job.Question{}
	}
	asking.Job = id
	asking.Seq = m.nextQuestionSeq(id)
	asking.Status = job.QuestionOpen
	if asking.AskedAt.IsZero() {
		asking.AskedAt = time.Now().UTC()
	}
	m.jobQuestions[id] = append(m.jobQuestions[id], asking)
	if err := m.appendJobEvent(event); err != nil {
		return nil, err
	}
	return m.jobWithSteps(*found), nil
}

// SettleJobQuestion answers a row an earlier reader left open.
//
// Only an open row settles. A row settled twice would take the second reader's answer over the
// first's for no reason anybody could read, and a settled row is already off the list a person sees.
func (m *Memory) SettleJobQuestion(_ context.Context, id string, seq int, answer, settledBy string, event *job.Event) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if found.Phase != job.PhaseRunning {
		return nil, job.ErrNotRunning
	}
	rows := m.jobQuestions[id]
	for at := range rows {
		if rows[at].Seq != seq {
			continue
		}
		if !rows[at].Open() {
			return nil, job.ErrQuestionSettled
		}
		rows[at].Status, rows[at].Answer = job.QuestionSettled, answer
		rows[at].SettledBy, rows[at].SettledAt = settledBy, time.Now().UTC()
		if err := m.appendJobEvent(event); err != nil {
			return nil, err
		}
		return m.jobWithSteps(*found), nil
	}
	return nil, job.ErrNoSuchQuestion
}

// CarryJobQuestions writes rows onto a job that did not write them: down onto the reading that comes
// next, and back up onto the plan the readings are of.
//
// It is the engine's call and never a session's, which is why it asks nothing about the phase. The
// job a run carries is held back while its steps are out, and a reading that finished is not
// running either, so a rule about running would refuse both ends of the carry.
//
// A row already on this job by number is settled rather than added, and only while it is open. That
// is what makes the carry back up idempotent: the same reading merged twice leaves one row.
func (m *Memory) CarryJobQuestions(_ context.Context, id string, rows []job.Question) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	found, held := m.jobs[id]
	if !held {
		return nil, ErrNotFound
	}
	if m.jobQuestions == nil {
		m.jobQuestions = map[string][]job.Question{}
	}
	for _, one := range rows {
		one.Job = id
		if held, at := m.questionAt(id, one.Seq); at >= 0 {
			if held.Open() && !one.Open() {
				m.jobQuestions[id][at].Status = job.QuestionSettled
				m.jobQuestions[id][at].Answer = one.Answer
				m.jobQuestions[id][at].SettledBy = one.SettledBy
				m.jobQuestions[id][at].SettledAt = one.SettledAt
			}
			continue
		}
		if one.Status == "" {
			one.Status = job.QuestionOpen
		}
		if one.AskedAt.IsZero() {
			one.AskedAt = time.Now().UTC()
		}
		m.jobQuestions[id] = append(m.jobQuestions[id], one)
	}
	return m.jobWithSteps(*found), nil
}

// nextQuestionSeq is the number the next row on this job takes, which is one past the highest it
// holds rather than the count: a job handed rows one to three writes its own as four.
func (m *Memory) nextQuestionSeq(id string) int {
	highest := 0
	for _, one := range m.jobQuestions[id] {
		if one.Seq > highest {
			highest = one.Seq
		}
	}
	return highest + 1
}

// questionAt is the row with this number and where it sits, or -1.
func (m *Memory) questionAt(id string, seq int) (job.Question, int) {
	for at, one := range m.jobQuestions[id] {
		if one.Seq == seq {
			return one, at
		}
	}
	return job.Question{}, -1
}
