package store

import (
	"context"
	"sort"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/flow"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// The pending trigger queue, held to the same contract the Postgres one is.
//
// One lock is what a transaction means here, and the claim is taken under it for the reason the
// Postgres claim is one statement: two pollers reading the same pending row must leave one holder,
// and a read followed by a write leaves two.

// RaiseTrigger writes down that something happened, so a run of the flow it names starts on the next
// tick of the poller.
func (m *Memory) RaiseTrigger(_ context.Context, trigger *flow.Trigger) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.triggers == nil {
		m.triggers = map[string]*flow.Trigger{}
	}
	kept := cloneTrigger(*trigger)
	kept.Status = flow.TriggerPending
	if kept.RaisedAt.IsZero() {
		kept.RaisedAt = time.Now().UTC()
	}
	// The time is stamped on the row rather than on the caller's copy, which is what Postgres does
	// with now(): a caller that wants it reads the row back.
	m.triggers[kept.ID] = &kept
	return nil
}

// PendingTriggers are the triggers nothing has started a run from and nobody is holding, oldest
// first.
func (m *Memory) PendingTriggers(_ context.Context, limit int) ([]*flow.Trigger, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now().UTC()
	out := make([]*flow.Trigger, 0)
	for _, trigger := range m.triggers {
		if trigger.Status != flow.TriggerPending || heldNow(trigger.Lease, now) {
			continue
		}
		kept := cloneTrigger(*trigger)
		out = append(out, &kept)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].RaisedAt.Equal(out[b].RaisedAt) {
			return out[a].ID < out[b].ID
		}
		return out[a].RaisedAt.Before(out[b].RaisedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ClaimTrigger takes a lease on one trigger, and applies only where it is still pending and nobody
// else's claim is live.
func (m *Memory) ClaimTrigger(_ context.Context, id string, lease job.Lease) (*flow.Trigger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	trigger, held := m.triggers[id]
	if !held {
		return nil, ErrNotFound
	}
	if trigger.Status != flow.TriggerPending || heldNow(trigger.Lease, time.Now().UTC()) {
		return nil, flow.ErrTriggerTaken
	}
	trigger.Lease = lease
	trigger.Attempts++
	kept := cloneTrigger(*trigger)
	return &kept, nil
}

// FailTrigger records that a claimed trigger started no run, and why. It applies only while the row
// is still pending, so a trigger that did start a run is never marked failed underneath it.
func (m *Memory) FailTrigger(_ context.Context, id, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	trigger, held := m.triggers[id]
	if !held || trigger.Status != flow.TriggerPending {
		return ErrNotFound
	}
	trigger.Status, trigger.Reason = flow.TriggerFailed, reason
	trigger.Lease = job.Lease{}
	return nil
}

// GetTrigger reads one trigger back.
func (m *Memory) GetTrigger(_ context.Context, id string) (*flow.Trigger, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	trigger, held := m.triggers[id]
	if !held {
		return nil, ErrNotFound
	}
	kept := cloneTrigger(*trigger)
	return &kept, nil
}

// startTrigger marks a trigger started, under the lock the run was written in, which is what one
// transaction means here. It answers whether the row was still there to mark, because a run must not
// be written for a trigger somebody else already acted on.
func (m *Memory) startTrigger(id, run string) bool {
	trigger, held := m.triggers[id]
	if !held || trigger.Status != flow.TriggerPending {
		return false
	}
	trigger.Status, trigger.Run = flow.TriggerStarted, run
	return true
}

// heldNow says whether a claim is still somebody's.
func heldNow(lease job.Lease, now time.Time) bool {
	return lease.Owner != "" && lease.Until.After(now)
}

// cloneTrigger is a trigger that shares no map with the one held, so a caller reading one cannot
// write into the store.
func cloneTrigger(trigger flow.Trigger) flow.Trigger {
	payload := make(map[string]string, len(trigger.Payload))
	for key, value := range trigger.Payload {
		payload[key] = value
	}
	trigger.Payload = payload
	return trigger
}
