package controlplane

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// projectStore is an in memory store of projects and their attached channels. A durable backend
// implements the same behaviour behind the control plane later.
type projectStore struct {
	mu       sync.RWMutex
	byID     map[string]*quaycrewv1.Project
	channels map[string][]*quaycrewv1.Channel
}

func newProjectStore() *projectStore {
	return &projectStore{byID: make(map[string]*quaycrewv1.Project), channels: make(map[string][]*quaycrewv1.Channel)}
}

func (s *projectStore) create(name string) *quaycrewv1.Project {
	s.mu.Lock()
	defer s.mu.Unlock()
	project := &quaycrewv1.Project{Id: newID(), Name: name, CreatedAt: timestamppb.New(time.Now())}
	s.byID[project.GetId()] = project
	return proto.Clone(project).(*quaycrewv1.Project)
}

func (s *projectStore) get(id string) (*quaycrewv1.Project, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	project, ok := s.byID[id]
	if !ok {
		return nil, false
	}
	return proto.Clone(project).(*quaycrewv1.Project), true
}

func (s *projectStore) exists(id string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.byID[id]
	return ok
}

func (s *projectStore) list() []*quaycrewv1.Project {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*quaycrewv1.Project, 0, len(s.byID))
	for _, project := range s.byID {
		out = append(out, proto.Clone(project).(*quaycrewv1.Project))
	}
	return out
}

func (s *projectStore) delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[id]; !ok {
		return false
	}
	delete(s.byID, id)
	delete(s.channels, id)
	return true
}

func (s *projectStore) attachChannel(project, id, kind string) *quaycrewv1.Channel {
	s.mu.Lock()
	defer s.mu.Unlock()
	channel := &quaycrewv1.Channel{Project: project, Id: id, Kind: kind}
	s.channels[project] = append(s.channels[project], channel)
	return proto.Clone(channel).(*quaycrewv1.Channel)
}

// sessionStore is an in memory store of sessions, keyed by id and by (project, thread).
type sessionStore struct {
	mu       sync.RWMutex
	byID     map[string]*quaycrewv1.Session
	byThread map[string]string
}

func newSessionStore() *sessionStore {
	return &sessionStore{byID: make(map[string]*quaycrewv1.Session), byThread: make(map[string]string)}
}

func threadKey(project, thread string) string { return project + "\x00" + thread }

// findOrCreate returns the session for (project, thread), creating it if needed. thread must be set.
func (s *sessionStore) findOrCreate(project, thread string) *quaycrewv1.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.byThread[threadKey(project, thread)]; ok {
		return proto.Clone(s.byID[id]).(*quaycrewv1.Session)
	}
	now := timestamppb.New(time.Now())
	session := &quaycrewv1.Session{
		Id:        newID(),
		Project:   project,
		ThreadId:  thread,
		Status:    "idle",
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.byID[session.GetId()] = session
	s.byThread[threadKey(project, thread)] = session.GetId()
	return proto.Clone(session).(*quaycrewv1.Session)
}

// recordTurn updates a session's model thread id and status after a turn.
func (s *sessionStore) recordTurn(id, modelSessionID, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.byID[id]
	if !ok {
		return
	}
	if modelSessionID != "" {
		session.ModelSessionId = modelSessionID
	}
	session.Status = status
	session.UpdatedAt = timestamppb.New(time.Now())
}

func (s *sessionStore) get(id string) (*quaycrewv1.Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.byID[id]
	if !ok {
		return nil, false
	}
	return proto.Clone(session).(*quaycrewv1.Session), true
}

func (s *sessionStore) list(project string) []*quaycrewv1.Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*quaycrewv1.Session, 0)
	for _, session := range s.byID {
		if project == "" || session.GetProject() == project {
			out = append(out, proto.Clone(session).(*quaycrewv1.Session))
		}
	}
	return out
}

func (s *sessionStore) stop(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.byID[id]
	if !ok {
		return false
	}
	session.Status = "stopped"
	session.UpdatedAt = timestamppb.New(time.Now())
	return true
}
