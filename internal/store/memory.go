package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Memory is an in memory Store. It outlives any one control plane instance it is handed to, which
// is what lets a test restart the server and assert that no state was hiding in the process.
type Memory struct {
	mu sync.RWMutex
	// probed counts the health probes this store has taken, so a test can say the write path was
	// exercised rather than that the call did not complain.
	probed     int
	workspaces map[string]*quaycrewv1.Workspace
	deleted    map[string]bool
	channels   map[string][]*quaycrewv1.Channel
	projects   map[string]*quaycrewv1.Project
	deletedPrj map[string]bool
	sessions   map[string]*quaycrewv1.Session
	bySession  map[string]string
	// skillsBorn is what skill set each session's live sandbox was born with. Absent means no live
	// sandbox is known, so the session can never be stale.
	skillsBorn map[string]string
	// contexts is what the model should be told, keyed by scope and owner.
	contexts map[string]string
	// repositories are the repositories each workspace works in, in the order they were added.
	// skills is every revision the crew holds, keyed by name and version, and attached is which
	// version of which skill each workspace pinned.
	skills   map[string]Imported
	attached map[string]map[string]int
	// crewAttached is which version of which skill the whole crew pinned, so every workspace holds
	// it without an attachment of its own.
	crewAttached map[string]int
	// hooks is every revision of every hook the crew holds, and hooksAttached and crewHooks are the
	// same two attachment levels again. Separate maps rather than one generic set: a skill and a
	// hook are different entities, and sharing the storage here would be the first step to sharing
	// the semantics.
	hooks         map[string]ImportedHook
	hooksAttached map[string]map[string]int
	crewHooks     map[string]int
	// roles is every revision of every role the crew holds, and rolesAttached and crewRoles are the
	// same two attachment levels once more.
	roles           map[string]ImportedRole
	rolesAttached   map[string]map[string]int
	crewRoles       map[string]int
	flowGraphs      map[string]map[int]string
	flowRuns        map[string]*flow.Run
	flowTransitions map[string][]flow.RecordedTransition
	flowDispatches  map[string]bool
	flowSchedules   map[string]*schedule
	// tasks is a session's history, oldest first, and taskSeen is what makes writing the
	// same record twice harmless.
	tasks    []*quaycrewv1.Task
	taskSeen map[string]bool
	// sessionEvents is what happened to each session, oldest first, and eventSeen does for them what
	// taskSeen does for tasks.
	sessionEvents []*quaycrewv1.SessionEvent
	eventSeen     map[string]bool
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
		bySession:  make(map[string]string),
		skillsBorn: make(map[string]string),
	}
}

func sessionKey(workspace, session string) string { return workspace + "\x00" + session }

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

// FindOrCreateSession returns the project's session for a session, creating it on first use.
func (m *Memory) FindOrCreateSession(_ context.Context, project, session string, born Birth) (*quaycrewv1.Session, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	owner, err := m.getProjectLocked(project)
	if err != nil {
		return nil, false, err
	}
	if id, ok := m.bySession[sessionKey(project, session)]; ok {
		return clone(m.sessions[id]), false, nil
	}
	now := timestamppb.New(time.Now().UTC())
	made := &quaycrewv1.Session{
		Id:             NewID(),
		Workspace:      owner.GetWorkspace(),
		Project:        project,
		Handle:         session,
		Status:         "idle",
		PermissionMode: model.PermissionModeBornIn(born.Mode),
		Role:           born.Role,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	m.sessions[made.GetId()] = made
	m.bySession[sessionKey(project, session)] = made.GetId()
	return clone(made), true, nil
}

// RecordTask stores the model conversation handle and status after a task. An empty handle leaves
// the stored one alone, so a failed task cannot erase the pointer to a live conversation.
func (m *Memory) RecordTask(_ context.Context, id, modelSessionID, status string) error {
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
func (m *Memory) ListSessions(_ context.Context, filter SessionFilter) ([]*quaycrewv1.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*quaycrewv1.Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		if (session.GetArchivedAt() != nil) != filter.Archived {
			continue
		}
		switch {
		case filter.Project != "":
			if session.GetProject() != filter.Project {
				continue
			}
		case filter.Workspace != "":
			if session.GetWorkspace() != filter.Workspace {
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
	// Stopping closes the sandbox, and with it goes what it was born holding: the next one is born
	// with the current set, so a stopped session is never stale.
	delete(m.skillsBorn, id)
	return nil
}

// SetSessionSkills records the skill set a session's live sandbox was born with; empty clears it.
func (m *Memory) SetSessionSkills(_ context.Context, id, fingerprint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[id]; !ok {
		return ErrNotFound
	}
	if fingerprint == "" {
		delete(m.skillsBorn, id)
		return nil
	}
	m.skillsBorn[id] = fingerprint
	return nil
}

// SessionSkills reads what skill set the session's live sandbox was born with, empty when no live
// sandbox is known.
func (m *Memory) SessionSkills(_ context.Context, id string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.sessions[id]; !ok {
		return "", ErrNotFound
	}
	return m.skillsBorn[id], nil
}

// RestartSession marks a session idle again.
func (m *Memory) RestartSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	session.Status = "idle"
	session.UpdatedAt = timestamppb.New(time.Now().UTC())
	return nil
}

// SetPermissionMode records what a session's tasks may do without asking.
func (m *Memory) SetPermissionMode(_ context.Context, id, mode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	session.PermissionMode = mode
	session.UpdatedAt = timestamppb.New(time.Now().UTC())
	return nil
}

// SetLabel records what the operator calls a session. Empty clears it.
func (m *Memory) SetLabel(_ context.Context, id, label string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	session.Label = label
	session.UpdatedAt = timestamppb.New(time.Now().UTC())
	return nil
}

// SetDescription records what the crew observed a session to be.
func (m *Memory) SetDescription(_ context.Context, id, description string, atTask int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	session.Description = description
	session.DescribedAtTask = int32(atTask)
	session.UpdatedAt = timestamppb.New(time.Now().UTC())
	return nil
}

// CountTasks is how many tasks a session has had.
func (m *Memory) CountTasks(_ context.Context, session string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, task := range m.tasks {
		if task.GetSession() == session {
			count++
		}
	}
	return count, nil
}

// ArchiveSession stamps a session as put away.
func (m *Memory) ArchiveSession(_ context.Context, id string) error {
	m.mu.Lock()
	delete(m.skillsBorn, id)
	m.mu.Unlock()
	return m.stampArchived(id, timestamppb.New(time.Now().UTC()))
}

// RestoreSession clears the stamp, bringing the session back into the default listing.
func (m *Memory) RestoreSession(_ context.Context, id string) error {
	return m.stampArchived(id, nil)
}

func (m *Memory) stampArchived(id string, at *timestamppb.Timestamp) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	session.ArchivedAt = at
	session.UpdatedAt = timestamppb.New(time.Now().UTC())
	return nil
}

// GetContext returns what the model should be told at a scope.
func (m *Memory) GetContext(_ context.Context, scope ContextScope, owner string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.contexts[contextKey(scope, owner)], nil
}

// SetContext records what the model should be told at a scope.
func (m *Memory) SetContext(_ context.Context, scope ContextScope, owner, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.contexts == nil {
		m.contexts = make(map[string]string)
	}
	m.contexts[contextKey(scope, owner)] = body
	return nil
}

func contextKey(scope ContextScope, owner string) string { return string(scope) + "/" + owner }

// ImportSkill takes a skill into the crew, refusing a version that already exists carrying something
// different.
func (m *Memory) ImportSkill(_ context.Context, imported Imported) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.skills == nil {
		m.skills = make(map[string]Imported)
	}
	key := skillKey(imported.Name, imported.Version)
	if held, already := m.skills[key]; already {
		if held.Fingerprint() != imported.Fingerprint() {
			return fmt.Errorf("%w: %s version %d", ErrSkillChanged, imported.Name, imported.Version)
		}
		return nil
	}
	// Stamped here because Postgres stamps it in the table's default, and a store that leaves it empty
	// is a double that accepts what the real one would not.
	if imported.ImportedAt.IsZero() {
		imported.ImportedAt = time.Now().UTC()
	}
	m.skills[key] = imported
	return nil
}

// GetSkill returns one revision of a skill.
func (m *Memory) GetSkill(_ context.Context, name string, version int) (Imported, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	held, found := m.skills[skillKey(name, version)]
	if !found {
		return Imported{}, ErrNotFound
	}
	return held, nil
}

// ListSkills returns the newest revision of every skill, without their files.
func (m *Memory) ListSkills(_ context.Context) ([]Imported, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	newest := make(map[string]Imported, len(m.skills))
	for _, held := range m.skills {
		if seen, found := newest[held.Name]; !found || held.Version > seen.Version {
			newest[held.Name] = held
		}
	}
	out := make([]Imported, 0, len(newest))
	for _, held := range newest {
		held.Files = nil
		out = append(out, held)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// AttachSkill gives a workspace a skill at the newest revision the crew holds.
func (m *Memory) AttachSkill(_ context.Context, workspace, name string) (Imported, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, found := m.workspaces[workspace]; !found || m.deleted[workspace] {
		return Imported{}, ErrNotFound
	}
	newest, found := m.newestSkill(name)
	if !found {
		return Imported{}, ErrNotFound
	}
	if m.attached == nil {
		m.attached = make(map[string]map[string]int)
	}
	if m.attached[workspace] == nil {
		m.attached[workspace] = make(map[string]int)
	}
	m.attached[workspace][name] = newest.Version
	return newest, nil
}

// DetachSkill takes a skill away from a workspace.
func (m *Memory) DetachSkill(_ context.Context, workspace, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	held, found := m.attached[workspace]
	if !found {
		return ErrNotFound
	}
	if _, found := held[name]; !found {
		return ErrNotFound
	}
	delete(held, name)
	return nil
}

// WorkspaceSkills returns what a workspace holds, at the versions it pinned.
func (m *Memory) WorkspaceSkills(_ context.Context, workspace string) ([]Imported, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Imported, 0, len(m.attached[workspace]))
	for name, version := range m.attached[workspace] {
		held, found := m.skills[skillKey(name, version)]
		if !found {
			continue
		}
		out = append(out, held)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// AttachCrewSkill gives the whole crew a skill at the newest revision it holds.
func (m *Memory) AttachCrewSkill(_ context.Context, name string) (Imported, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	newest, found := m.newestSkill(name)
	if !found {
		return Imported{}, ErrNotFound
	}
	if m.crewAttached == nil {
		m.crewAttached = make(map[string]int)
	}
	m.crewAttached[name] = newest.Version
	return newest, nil
}

// DetachCrewSkill takes a skill away from the crew.
func (m *Memory) DetachCrewSkill(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, found := m.crewAttached[name]; !found {
		return ErrNotFound
	}
	delete(m.crewAttached, name)
	return nil
}

// CrewSkills returns what the crew holds, at the versions it pinned.
func (m *Memory) CrewSkills(_ context.Context) ([]Imported, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Imported, 0, len(m.crewAttached))
	for name, version := range m.crewAttached {
		held, found := m.skills[skillKey(name, version)]
		if !found {
			continue
		}
		out = append(out, held)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// newestSkill is the highest version of a name the crew holds. Callers hold the lock.
func (m *Memory) newestSkill(name string) (Imported, bool) {
	var newest Imported
	var found bool
	for _, held := range m.skills {
		if held.Name != name {
			continue
		}
		if !found || held.Version > newest.Version {
			newest, found = held, true
		}
	}
	return newest, found
}

func skillKey(name string, version int) string { return name + "\x00" + strconv.Itoa(version) }

// Probe takes the store's own lock and writes, which is what proves this store still takes a write:
// the lock is the only thing here that another caller can be holding.
func (m *Memory) Probe(ctx context.Context) error {
	// The context is read even though nothing here blocks on it, because a double that answers what
	// the real store refuses makes a suite green over a store that cannot write.
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.probed++
	return nil
}

// Probes is how many times this store took a probe.
func (m *Memory) Probes() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.probed
}

// Close is a no op for the in memory store.
func (m *Memory) Close() {}

// clone copies a message so a caller cannot mutate what the store holds.
func clone[T proto.Message](message T) T { return proto.Clone(message).(T) }

// AppendTask records a task, ignoring one it has already seen.
func (m *Memory) AppendTask(_ context.Context, task *quaycrewv1.Task, _, _, _ string) error {
	if task.GetId() == "" {
		return errors.New("store: a task needs an id, so writing the same one twice leaves one task")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.taskSeen == nil {
		m.taskSeen = make(map[string]bool)
	}
	if m.taskSeen[task.GetId()] {
		return nil
	}
	m.taskSeen[task.GetId()] = true
	m.tasks = append(m.tasks, clone(task))
	return nil
}

// FinishTask closes the record a task opened when it started.
func (m *Memory) FinishTask(_ context.Context, id, status, reply, failure string) error {
	if id == "" {
		return errors.New("store: a task needs an id to be finished")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, task := range m.tasks {
		if task.GetId() != id {
			continue
		}
		task.Status, task.Reply, task.Failure = status, reply, failure
		return nil
	}
	return nil
}

// ListTasks returns a session's tasks oldest first, capped at limit, keeping the most recent when
// there are more than the cap: the end of a conversation is the part somebody wants.
func (m *Memory) ListTasks(_ context.Context, session string, limit int) ([]*quaycrewv1.Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*quaycrewv1.Task, 0)
	for _, task := range m.tasks {
		if task.GetSession() == session {
			out = append(out, clone(task))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].GetOccurredAt().AsTime().Before(out[j].GetOccurredAt().AsTime())
	})
	if capped := TaskLimit(limit); len(out) > capped {
		out = out[len(out)-capped:]
	}
	return out, nil
}

// AppendSessionEvent records one thing that happened to a session. Writing the same event twice
// leaves one, the way a task does.
func (m *Memory) AppendSessionEvent(_ context.Context, event *quaycrewv1.SessionEvent) error {
	if event.GetId() == "" {
		return errors.New("store: a session event needs an id, so writing the same one twice leaves one event")
	}
	if event.GetKind() == "" {
		return errors.New("store: a session event needs a kind, which is the field a consumer switches on")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.eventSeen == nil {
		m.eventSeen = make(map[string]bool)
	}
	if m.eventSeen[event.GetId()] {
		return nil
	}
	m.eventSeen[event.GetId()] = true
	m.sessionEvents = append(m.sessionEvents, clone(event))
	return nil
}

// ListSessionEvents returns a session's lifecycle oldest first, or the whole crew's when no session
// is named, capped the way a history is: the most recent, turned back the right way round.
func (m *Memory) ListSessionEvents(_ context.Context, session string, limit int) ([]*quaycrewv1.SessionEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*quaycrewv1.SessionEvent, 0)
	for _, event := range m.sessionEvents {
		if session == "" || event.GetSession() == session {
			out = append(out, clone(event))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].GetOccurredAt().AsTime().Before(out[j].GetOccurredAt().AsTime())
	})
	if capped := TaskLimit(limit); len(out) > capped {
		out = out[len(out)-capped:]
	}
	return out, nil
}

// FindOrCreateDriver returns the project's driver, creating it the first time somebody opens it.
func (m *Memory) FindOrCreateDriver(_ context.Context, project string) (*quaycrewv1.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	owner, err := m.getProjectLocked(project)
	if err != nil {
		return nil, err
	}
	for _, session := range m.sessions {
		if session.GetProject() == project && session.GetDriver() {
			return clone(session), nil
		}
	}
	now := timestamppb.New(time.Now().UTC())
	session := &quaycrewv1.Session{
		Id:        NewID(),
		Workspace: owner.GetWorkspace(),
		Project:   project,
		Handle:    NewID(),
		Status:    "idle",
		// The driver acts for the operator rather than doing work of its own, and a driver that stops
		// to ask before every step describes the task instead of doing it. What bounds it is the
		// sandbox, which is the same boundary it would have either way.
		PermissionMode: model.PermissionBypass,
		Driver:         true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	m.sessions[session.GetId()] = session
	m.bySession[sessionKey(project, session.GetHandle())] = session.GetId()
	return clone(session), nil
}
