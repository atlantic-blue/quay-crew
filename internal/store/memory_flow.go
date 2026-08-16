package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/flow"
)

// ImportFlowGraph stores a graph at a version. A version that exists is refused rather than
// replaced, because a run is pinned to the version it started with and a pin that can move is not
// one.
func (m *Memory) ImportFlowGraph(_ context.Context, name string, version int, definition string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.flowGraphs == nil {
		m.flowGraphs = map[string]map[int]string{}
	}
	if m.flowGraphs[name] == nil {
		m.flowGraphs[name] = map[int]string{}
	}
	if _, held := m.flowGraphs[name][version]; held {
		return fmt.Errorf("store: graph %s version %d is already imported, and a version never changes; import the next one", name, version)
	}
	m.flowGraphs[name][version] = definition
	return nil
}

// LatestFlowGraph returns the newest version of a graph.
func (m *Memory) LatestFlowGraph(_ context.Context, name string) (int, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	versions := m.flowGraphs[name]
	if len(versions) == 0 {
		return 0, "", ErrNotFound
	}
	latest := 0
	for version := range versions {
		if version > latest {
			latest = version
		}
	}
	return latest, versions[latest], nil
}

// FlowGraph returns one exact version, which is what a run already under way is carried on with.
func (m *Memory) FlowGraph(_ context.Context, name string, version int) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	definition, held := m.flowGraphs[name][version]
	if !held {
		return "", ErrNotFound
	}
	return definition, nil
}

// DueFlowRuns are the waiting runs whose time has come.
func (m *Memory) DueFlowRuns(_ context.Context, now time.Time) ([]*flow.Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*flow.Run, 0)
	for _, run := range m.flowRuns {
		if run.Status != flow.StatusWaiting || run.DueAt == nil || run.DueAt.After(now) {
			continue
		}
		kept := cloneRun(*run)
		out = append(out, &kept)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}

// CreateFlowRun writes a fresh run.
func (m *Memory) CreateFlowRun(_ context.Context, run *flow.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.flowRuns == nil {
		m.flowRuns = map[string]*flow.Run{}
	}
	if _, held := m.flowRuns[run.ID]; held {
		return fmt.Errorf("store: run %s already exists", run.ID)
	}
	kept := cloneRun(*run)
	m.flowRuns[run.ID] = &kept
	return nil
}

// StopFlowRun halts a run that is still running, keeping the reason.
func (m *Memory) StopFlowRun(_ context.Context, id, reason string) (*flow.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	run, held := m.flowRuns[id]
	if !held {
		return nil, ErrNotFound
	}
	// Running, waiting and asking are all live, so any of them can be stopped. A run that ended is
	// not stopped again, because how it ended is the useful part.
	if run.Status != flow.StatusRunning && run.Status != flow.StatusWaiting && run.Status != flow.StatusAsking {
		return nil, fmt.Errorf("store: run %s is %s, and a run that already ended is not stopped again", id, run.Status)
	}
	run.Status, run.Reason = flow.StatusStopped, reason
	kept := cloneRun(*run)
	return &kept, nil
}

// AdvanceFlowRun moves a run, appends the transition, and claims the dispatch key, all together the
// way the Postgres store does in one transaction. A claimed key refuses the whole movement, and a
// run somebody has stopped is refused too, so a stop that lands mid task is not written back over.
func (m *Memory) AdvanceFlowRun(_ context.Context, run *flow.Run, transition flow.Transition) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	held, found := m.flowRuns[run.ID]
	if !found {
		return ErrNotFound
	}
	// Running and waiting are both live: a waiting run is one the poller will carry on, so it
	// moves. Anything else has ended, and a movement written over that would undo the record of
	// how it ended.
	if held.Status != flow.StatusRunning && held.Status != flow.StatusWaiting && held.Status != flow.StatusAsking {
		return flow.ErrRunHalted
	}
	if transition.Dispatch != nil {
		if m.flowDispatches == nil {
			m.flowDispatches = map[string]bool{}
		}
		key := fmt.Sprintf("%s|%s|%d", run.ID, transition.Dispatch.Node, transition.Dispatch.Attempt)
		if m.flowDispatches[key] {
			return fmt.Errorf("store: run %s already dispatched node %s attempt %d, and the same task is never sent twice", run.ID, transition.Dispatch.Node, transition.Dispatch.Attempt)
		}
		m.flowDispatches[key] = true
	}
	kept := cloneRun(*run)
	// The due time lands with the position it belongs to, so a run recorded as waiting can never
	// be left with nothing to wake it.
	kept.DueAt = transition.Due
	m.flowRuns[run.ID] = &kept
	if m.flowTransitions == nil {
		m.flowTransitions = map[string][]flow.RecordedTransition{}
	}
	m.flowTransitions[run.ID] = append(m.flowTransitions[run.ID], flow.RecordedTransition{
		Seq: len(m.flowTransitions[run.ID]) + 1, Event: transition.Event, Node: transition.Node,
	})
	return nil
}

// GetFlowRun reads one run back.
func (m *Memory) GetFlowRun(_ context.Context, id string) (*flow.Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	run, held := m.flowRuns[id]
	if !held {
		return nil, ErrNotFound
	}
	kept := cloneRun(*run)
	return &kept, nil
}

// ListFlowRuns lists runs, newest first, narrowed to one project when project is set.
func (m *Memory) ListFlowRuns(_ context.Context, project string) ([]*flow.Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*flow.Run, 0, len(m.flowRuns))
	for _, run := range m.flowRuns {
		if project != "" && run.Project != project {
			continue
		}
		kept := cloneRun(*run)
		out = append(out, &kept)
	}
	// Newest first, and a map has no order, so this sorts by the identifier to be at least stable.
	sort.Slice(out, func(a, b int) bool { return out[a].ID > out[b].ID })
	return out, nil
}

// ListFlowTransitions reads a run's movements back, in the order they happened.
func (m *Memory) ListFlowTransitions(_ context.Context, run string) ([]flow.RecordedTransition, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := append([]flow.RecordedTransition(nil), m.flowTransitions[run]...)
	sort.Slice(out, func(a, b int) bool { return out[a].Seq < out[b].Seq })
	return out, nil
}

// cloneRun copies a run so a caller's later edits cannot reach the stored one.
func cloneRun(run flow.Run) flow.Run {
	state := make(map[string]string, len(run.State))
	for key, value := range run.State {
		state[key] = value
	}
	attempts := make(map[string]int, len(run.Attempts))
	for key, value := range run.Attempts {
		attempts[key] = value
	}
	run.State, run.Attempts = state, attempts
	return run
}

// schedule is one graph the crew starts on its own, as the memory store keeps it.
type schedule struct {
	every  time.Duration
	nextAt time.Time
}

// scheduleKey names one graph in one project.
func scheduleKey(graph, project string) string { return graph + "|" + project }

// ScheduleFlow records that a graph runs in a project every so often.
func (m *Memory) ScheduleFlow(_ context.Context, graph, project string, every time.Duration, next time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, held := m.projects[project]; !held {
		return ErrNotFound
	}
	if m.flowSchedules == nil {
		m.flowSchedules = map[string]*schedule{}
	}
	m.flowSchedules[scheduleKey(graph, project)] = &schedule{every: every, nextAt: next}
	return nil
}

// UnscheduleFlow stops a graph running on its own in a project.
func (m *Memory) UnscheduleFlow(_ context.Context, graph, project string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, held := m.flowSchedules[scheduleKey(graph, project)]; !held {
		return ErrNotFound
	}
	delete(m.flowSchedules, scheduleKey(graph, project))
	return nil
}

// DueFlowSchedules are the schedules whose time has come.
func (m *Memory) DueFlowSchedules(_ context.Context, now time.Time) ([]flow.Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]flow.Schedule, 0)
	for key, held := range m.flowSchedules {
		if held.nextAt.After(now) {
			continue
		}
		graph, project, found := strings.Cut(key, "|")
		if !found {
			continue
		}
		out = append(out, flow.Schedule{
			GraphName: graph, Project: project,
			Workspace: m.projects[project].GetWorkspace(), Every: held.every,
		})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].GraphName < out[b].GraphName })
	return out, nil
}

// MarkFlowScheduled moves a schedule on to its next due time.
func (m *Memory) MarkFlowScheduled(_ context.Context, graph, project string, next time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	held, found := m.flowSchedules[scheduleKey(graph, project)]
	if !found {
		return ErrNotFound
	}
	held.nextAt = next
	return nil
}
