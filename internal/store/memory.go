package store

import (
	"context"
	"sync"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Memory is an in memory Store. It outlives any one control plane instance it is handed to, which
// is what lets a test restart the server and assert that no state was hiding in the process.
type Memory struct {
	mu       sync.RWMutex
	projects map[string]*quaycrewv1.Project
	deleted  map[string]bool
	channels map[string][]*quaycrewv1.Channel
	sessions map[string]*quaycrewv1.Session
	byThread map[string]string
}

var _ Store = (*Memory)(nil)

// NewMemory returns an empty in memory store.
func NewMemory() *Memory {
	return &Memory{
		projects: make(map[string]*quaycrewv1.Project),
		deleted:  make(map[string]bool),
		channels: make(map[string][]*quaycrewv1.Channel),
		sessions: make(map[string]*quaycrewv1.Session),
		byThread: make(map[string]string),
	}
}

func threadKey(project, thread string) string { return project + "\x00" + thread }

// CreateProject stores a new project.
func (m *Memory) CreateProject(_ context.Context, name string) (*quaycrewv1.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	project := &quaycrewv1.Project{Id: NewID(), Name: name, CreatedAt: timestamppb.New(time.Now().UTC())}
	m.projects[project.GetId()] = project
	return clone(project), nil
}

// GetProject returns a project that has not been deleted.
func (m *Memory) GetProject(_ context.Context, id string) (*quaycrewv1.Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	project, ok := m.projects[id]
	if !ok || m.deleted[id] {
		return nil, ErrNotFound
	}
	return clone(project), nil
}

// ListProjects returns every project that has not been deleted.
func (m *Memory) ListProjects(_ context.Context) ([]*quaycrewv1.Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*quaycrewv1.Project, 0, len(m.projects))
	for id, project := range m.projects {
		if !m.deleted[id] {
			out = append(out, clone(project))
		}
	}
	return out, nil
}

// DeleteProject soft deletes a project.
func (m *Memory) DeleteProject(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[id]; !ok || m.deleted[id] {
		return ErrNotFound
	}
	m.deleted[id] = true
	return nil
}

// AttachChannel records a channel against a project.
func (m *Memory) AttachChannel(_ context.Context, project, id, kind string) (*quaycrewv1.Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[project]; !ok || m.deleted[project] {
		return nil, ErrNotFound
	}
	channel := &quaycrewv1.Channel{Project: project, Id: id, Kind: kind}
	m.channels[project] = append(m.channels[project], channel)
	return clone(channel), nil
}

// FindOrCreateSession returns the project's session for a thread, creating it on first use.
func (m *Memory) FindOrCreateSession(_ context.Context, project, thread string) (*quaycrewv1.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.projects[project]; !ok || m.deleted[project] {
		return nil, ErrNotFound
	}
	if id, ok := m.byThread[threadKey(project, thread)]; ok {
		return clone(m.sessions[id]), nil
	}
	now := timestamppb.New(time.Now().UTC())
	session := &quaycrewv1.Session{
		Id:        NewID(),
		Project:   project,
		ThreadId:  thread,
		Status:    "idle",
		CreatedAt: now,
		UpdatedAt: now,
	}
	m.sessions[session.GetId()] = session
	m.byThread[threadKey(project, thread)] = session.GetId()
	return clone(session), nil
}

// RecordTurn stores the model conversation handle and status after a turn. An empty handle leaves
// the stored one alone, so a failed turn cannot erase the pointer to a live conversation.
func (m *Memory) RecordTurn(_ context.Context, id, modelSessionID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	if modelSessionID != "" {
		session.ModelSessionId = modelSessionID
	}
	session.Status = status
	session.UpdatedAt = timestamppb.New(time.Now().UTC())
	return nil
}

// GetSession returns a session by id.
func (m *Memory) GetSession(_ context.Context, id string) (*quaycrewv1.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return clone(session), nil
}

// ListSessions returns sessions, optionally filtered to one project.
func (m *Memory) ListSessions(_ context.Context, project string) ([]*quaycrewv1.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*quaycrewv1.Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		if project == "" || session.GetProject() == project {
			out = append(out, clone(session))
		}
	}
	return out, nil
}

// StopSession marks a session stopped.
func (m *Memory) StopSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	session.Status = "stopped"
	session.UpdatedAt = timestamppb.New(time.Now().UTC())
	return nil
}

// Close is a no op for the in memory store.
func (m *Memory) Close() {}

// clone copies a message so a caller cannot mutate what the store holds.
func clone[T proto.Message](message T) T { return proto.Clone(message).(T) }
