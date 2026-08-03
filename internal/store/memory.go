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
	mu         sync.RWMutex
	workspaces map[string]*quaycrewv1.Workspace
	deleted    map[string]bool
	channels   map[string][]*quaycrewv1.Channel
	projects   map[string]*quaycrewv1.Project
	deletedPrj map[string]bool
	sessions   map[string]*quaycrewv1.Session
	byThread   map[string]string
}

var _ Store = (*Memory)(nil)

// NewMemory returns an empty in memory store.
func NewMemory() *Memory {
	return &Memory{
		workspaces: make(map[string]*quaycrewv1.Workspace),
		deleted:    make(map[string]bool),
		channels:   make(map[string][]*quaycrewv1.Channel),
		projects:   make(map[string]*quaycrewv1.Project),
		deletedPrj: make(map[string]bool),
		sessions:   make(map[string]*quaycrewv1.Session),
		byThread:   make(map[string]string),
	}
}

func threadKey(workspace, thread string) string { return workspace + "\x00" + thread }

// CreateWorkspace stores a new workspace.
func (m *Memory) CreateWorkspace(_ context.Context, name string) (*quaycrewv1.Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	workspace := &quaycrewv1.Workspace{Id: NewID(), Name: name, CreatedAt: timestamppb.New(time.Now().UTC())}
	m.workspaces[workspace.GetId()] = workspace
	return clone(workspace), nil
}

// GetWorkspace returns a workspace that has not been deleted.
func (m *Memory) GetWorkspace(_ context.Context, id string) (*quaycrewv1.Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	workspace, ok := m.workspaces[id]
	if !ok || m.deleted[id] {
		return nil, ErrNotFound
	}
	return clone(workspace), nil
}

// ListWorkspaces returns every workspace that has not been deleted.
func (m *Memory) ListWorkspaces(_ context.Context) ([]*quaycrewv1.Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*quaycrewv1.Workspace, 0, len(m.workspaces))
	for id, workspace := range m.workspaces {
		if !m.deleted[id] {
			out = append(out, clone(workspace))
		}
	}
	return out, nil
}

// DeleteWorkspace soft deletes a workspace.
func (m *Memory) DeleteWorkspace(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workspaces[id]; !ok || m.deleted[id] {
		return ErrNotFound
	}
	m.deleted[id] = true
	return nil
}

// AttachChannel records a channel against a workspace.
func (m *Memory) AttachChannel(_ context.Context, workspace, id, kind string) (*quaycrewv1.Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workspaces[workspace]; !ok || m.deleted[workspace] {
		return nil, ErrNotFound
	}
	channel := &quaycrewv1.Channel{Workspace: workspace, Id: id, Kind: kind}
	m.channels[workspace] = append(m.channels[workspace], channel)
	return clone(channel), nil
}

// CreateProject adds a body of work to a live workspace.
func (m *Memory) CreateProject(_ context.Context, workspace, name string) (*quaycrewv1.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.workspaces[workspace]; !ok || m.deleted[workspace] {
		return nil, ErrNotFound
	}
	project := &quaycrewv1.Project{
		Id:        NewID(),
		Workspace: workspace,
		Name:      name,
		CreatedAt: timestamppb.New(time.Now().UTC()),
	}
	m.projects[project.GetId()] = project
	return clone(project), nil
}

// GetProject returns a project that has not been deleted, in a workspace that has not been deleted.
func (m *Memory) GetProject(_ context.Context, id string) (*quaycrewv1.Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getProjectLocked(id)
}

func (m *Memory) getProjectLocked(id string) (*quaycrewv1.Project, error) {
	project, ok := m.projects[id]
	if !ok || m.deletedPrj[id] {
		return nil, ErrNotFound
	}
	// A deleted workspace takes its projects with it, or a project would outlive its own parent.
	if m.deleted[project.GetWorkspace()] {
		return nil, ErrNotFound
	}
	return clone(project), nil
}

// ListProjects returns live projects, filtered to one workspace when set.
func (m *Memory) ListProjects(_ context.Context, workspace string) ([]*quaycrewv1.Project, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*quaycrewv1.Project, 0, len(m.projects))
	for id, project := range m.projects {
		if m.deletedPrj[id] || m.deleted[project.GetWorkspace()] {
			continue
		}
		if workspace == "" || project.GetWorkspace() == workspace {
			out = append(out, clone(project))
		}
	}
	return out, nil
}

// DeleteProject soft deletes a project.
func (m *Memory) DeleteProject(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.getProjectLocked(id); err != nil {
		return err
	}
	m.deletedPrj[id] = true
	return nil
}

// FindOrCreateSession returns the project's session for a thread, creating it on first use.
func (m *Memory) FindOrCreateSession(_ context.Context, project, thread string) (*quaycrewv1.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	owner, err := m.getProjectLocked(project)
	if err != nil {
		return nil, err
	}
	if id, ok := m.byThread[threadKey(project, thread)]; ok {
		return clone(m.sessions[id]), nil
	}
	now := timestamppb.New(time.Now().UTC())
	session := &quaycrewv1.Session{
		Id:        NewID(),
		Workspace: owner.GetWorkspace(),
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

// ListSessions returns sessions, filtered to one project when set, else to one workspace when set.
func (m *Memory) ListSessions(_ context.Context, workspace, project string) ([]*quaycrewv1.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*quaycrewv1.Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		switch {
		case project != "":
			if session.GetProject() != project {
				continue
			}
		case workspace != "":
			if session.GetWorkspace() != workspace {
				continue
			}
		}
		out = append(out, clone(session))
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
