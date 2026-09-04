package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/deploy"
	"github.com/atlantic-blue/quay-krewe/internal/model"
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
	// designs is what each project is for and what was designed for it, keyed by project. A project
	// with no entry has no design, which is the normal state.
	designs map[string]*quaycrewv1.Design
	// repositories are the repositories each workspace works in, in the order they were added.
	// skills is every revision the system holds, keyed by name and version, and attached is which
	// version of which skill each workspace pinned.
	skills   map[string]Imported
	attached map[string]map[string]int
	// systemAttached is which version of which skill the whole system pinned, so every workspace holds
	// it without an attachment of its own.
	systemAttached map[string]int
	// hooks is every revision of every hook the system holds, and hooksAttached and systemHooks are the
	// same two attachment levels again. Separate maps rather than one generic set: a skill and a
	// hook are different entities, and sharing the storage here would be the first step to sharing
	// the semantics.
	hooks         map[string]ImportedHook
	hooksAttached map[string]map[string]int
	systemHooks   map[string]int
	// execs is a session's history, oldest first, and execSeen is what makes writing the
	// same record twice harmless.
	execs    []*quaycrewv1.Exec
	execSeen map[string]bool
	// sessionEvents is what happened to each session, oldest first, and eventSeen does for them what
	// execSeen does for execs.
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

// SetProjectRepository records where a project's work lands, and what kind of repository it is.
func (m *Memory) SetProjectRepository(_ context.Context, project, repository, visibility string) (*quaycrewv1.Project, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	held, err := m.getProjectLocked(project)
	if err != nil {
		return nil, err
	}
	held.Repository, held.Visibility = repository, visibility
	m.projects[project] = held
	return clone(held), nil
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

// SetDeployTarget records where a project ships, and a zero target clears it.
func (m *Memory) SetDeployTarget(_ context.Context, project string, target deploy.Target) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.getProjectLocked(project); err != nil {
		return err
	}
	if target.IsZero() {
		m.projects[project].DeployTarget = nil
		return nil
	}
	m.projects[project].DeployTarget = &quaycrewv1.DeployTarget{
		Account:  target.Account,
		Region:   target.Region,
		Identity: target.Identity,
	}
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
		Title:          born.Title,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	m.sessions[made.GetId()] = made
	m.bySession[sessionKey(project, session)] = made.GetId()
	return clone(made), true, nil
}

// RecordExec stores the model conversation handle and status after an exec. An empty handle leaves
// the stored one alone, so a failed exec cannot erase the pointer to a live conversation.
func (m *Memory) RecordExec(_ context.Context, id, modelSessionID, status string) error {
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
	// An exec is running or has landed, so the session holds a container again and the stamp that said
	// the system took the last one back is no longer true.
	session.ReclaimedAt = nil
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

// ListSessions returns sessions, filtered to one project when set, else to one workspace when set,
// last moved first: see sortByLastMoved for the order and why it is that one.
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
	// The order is the store's to decide, not each surface's, so the console, the command line and
	// the web page cannot drift apart. See sortByLastMoved.
	sortByLastMoved(out)
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
	session.ReclaimedAt = nil
	session.UpdatedAt = timestamppb.New(time.Now().UTC())
	// Stopping closes the sandbox, and with it goes what it was born holding: the next one is born
	// with the current set, so a stopped session is never stale.
	delete(m.skillsBorn, id)
	return nil
}

// ReclaimSession records that the system took the session's container back. The stamp is its own,
// because the archive time is measured against how long the session has been reclaimed and
// UpdatedAt moves on every write.
func (m *Memory) ReclaimSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	now := timestamppb.New(time.Now().UTC())
	session.Status = StatusReclaimed
	session.ReclaimedAt = now
	session.UpdatedAt = now
	// The sandbox goes with the reclaim, so what it was born holding goes too: the next one is born
	// with the current set, and a reclaimed session is never stale.
	delete(m.skillsBorn, id)
	return nil
}

// IdleSandboxes is the sessions that still hold a container and nothing is holding open, oldest
// touched first: live, not running, not already reclaimed, and named by no job in a non terminal
// phase.
func (m *Memory) IdleSandboxes(_ context.Context, limit int) ([]*quaycrewv1.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settled(holdingStatuses(), limit, func(one *quaycrewv1.Session) time.Time {
		return one.GetUpdatedAt().AsTime()
	}), nil
}

// ReclaimedSessions is the sessions whose container has already gone, longest reclaimed first. The
// order is the reclaim stamp rather than updated_at, because that is what the archive time measures.
func (m *Memory) ReclaimedSessions(_ context.Context, limit int) ([]*quaycrewv1.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.settled([]string{StatusReclaimed}, limit, func(one *quaycrewv1.Session) time.Time {
		return one.GetReclaimedAt().AsTime()
	}), nil
}

// settled is the one query both of those are: live sessions in these statuses that no job in a non
// terminal phase names, oldest first by whichever stamp the caller measures against. Called under
// the read lock.
func (m *Memory) settled(statuses []string, limit int,
	oldest func(*quaycrewv1.Session) time.Time) []*quaycrewv1.Session {
	wanted := map[string]bool{}
	for _, status := range statuses {
		wanted[status] = true
	}

	out := make([]*quaycrewv1.Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		if session.GetArchivedAt() != nil || !wanted[session.GetStatus()] {
			continue
		}
		out = append(out, clone(session))
	}
	sort.Slice(out, func(i, j int) bool {
		left, right := oldest(out[i]), oldest(out[j])
		if left.Equal(right) {
			return out[i].GetId() < out[j].GetId()
		}
		return left.Before(right)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
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
	session.ReclaimedAt = nil
	session.UpdatedAt = timestamppb.New(time.Now().UTC())
	return nil
}

// SetPermissionMode records what a session's execs may do without asking.
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

// SetDescription records what the system observed a session to be.
func (m *Memory) SetDescription(_ context.Context, id, description string, atExec int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return ErrNotFound
	}
	session.Description = description
	session.DescribedAtExec = int32(atExec)
	session.UpdatedAt = timestamppb.New(time.Now().UTC())
	return nil
}

// CountExecs is how many execs a session has had.
func (m *Memory) CountExecs(_ context.Context, session string) (int, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, exec := range m.execs {
		if exec.GetSession() == session {
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

// GetDesign returns the project's design. A project with no design answers with a Design carrying
// only its identifier, which is the normal state and not an error.
func (m *Memory) GetDesign(_ context.Context, project string) (*quaycrewv1.Design, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, err := m.getProjectLocked(project); err != nil {
		return nil, err
	}
	held, ok := m.designs[project]
	if !ok {
		return &quaycrewv1.Design{Project: project}, nil
	}
	return copyDesign(held), nil
}

// SetProjectBrief records what a project is for, leaving the body and its writer alone.
func (m *Memory) SetProjectBrief(_ context.Context, project, brief string) (*quaycrewv1.Design, error) {
	return m.writeDesign(project, func(design *quaycrewv1.Design) {
		design.Brief = brief
	})
}

// SetProjectDesign records the design document whole and who wrote it, leaving the brief alone.
func (m *Memory) SetProjectDesign(_ context.Context, project, body, writtenBy string) (*quaycrewv1.Design, error) {
	return m.writeDesign(project, func(design *quaycrewv1.Design) {
		design.Body = body
		design.WrittenBy = writtenBy
	})
}

// writeDesign creates the row on first use, applies the change, and moves updated_at. Every design
// write goes through it so no caller can move one stamp and forget the other.
func (m *Memory) writeDesign(project string, change func(*quaycrewv1.Design)) (*quaycrewv1.Design, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.getProjectLocked(project); err != nil {
		return nil, err
	}
	if m.designs == nil {
		m.designs = make(map[string]*quaycrewv1.Design)
	}
	held, ok := m.designs[project]
	if !ok {
		held = &quaycrewv1.Design{Project: project}
		m.designs[project] = held
	}
	change(held)
	held.UpdatedAt = timestamppb.New(time.Now().UTC())
	return copyDesign(held), nil
}

// copyDesign hands the caller its own message. The map holds a pointer, and a caller that edited what
// it was given would be editing the store.
func copyDesign(design *quaycrewv1.Design) *quaycrewv1.Design {
	return proto.Clone(design).(*quaycrewv1.Design)
}

// ImportSkill takes a skill into the system, refusing a version that already exists carrying something
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

// AttachSkill gives a workspace a skill at the newest revision the system holds.
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

// AttachSystemSkill gives the whole system a skill at the newest revision it holds.
func (m *Memory) AttachSystemSkill(_ context.Context, name string) (Imported, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	newest, found := m.newestSkill(name)
	if !found {
		return Imported{}, ErrNotFound
	}
	if m.systemAttached == nil {
		m.systemAttached = make(map[string]int)
	}
	m.systemAttached[name] = newest.Version
	return newest, nil
}

// DetachSystemSkill takes a skill away from the system.
func (m *Memory) DetachSystemSkill(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, found := m.systemAttached[name]; !found {
		return ErrNotFound
	}
	delete(m.systemAttached, name)
	return nil
}

// SystemSkills returns what the system holds, at the versions it pinned.
func (m *Memory) SystemSkills(_ context.Context) ([]Imported, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Imported, 0, len(m.systemAttached))
	for name, version := range m.systemAttached {
		held, found := m.skills[skillKey(name, version)]
		if !found {
			continue
		}
		out = append(out, held)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// newestSkill is the highest version of a name the system holds. Callers hold the lock.
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

// AppendExec records an exec, ignoring one it has already seen.
func (m *Memory) AppendExec(_ context.Context, exec *quaycrewv1.Exec, _, _, _ string) error {
	if exec.GetId() == "" {
		return errors.New("store: an exec needs an id, so writing the same one twice leaves one exec")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.execSeen == nil {
		m.execSeen = make(map[string]bool)
	}
	if m.execSeen[exec.GetId()] {
		return nil
	}
	m.execSeen[exec.GetId()] = true
	m.execs = append(m.execs, clone(exec))
	return nil
}

// FinishExec closes the record an exec opened when it started.
func (m *Memory) FinishExec(_ context.Context, id, status, reply, failure string) error {
	if id == "" {
		return errors.New("store: an exec needs an id to be finished")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, exec := range m.execs {
		if exec.GetId() != id {
			continue
		}
		exec.Status, exec.Reply, exec.Failure = status, reply, failure
		return nil
	}
	return nil
}

// ListExecs returns a session's execs oldest first, capped at limit, keeping the most recent when
// there are more than the cap: the end of a conversation is the part somebody wants.
func (m *Memory) ListExecs(_ context.Context, session string, limit int) ([]*quaycrewv1.Exec, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*quaycrewv1.Exec, 0)
	for _, exec := range m.execs {
		if exec.GetSession() == session {
			out = append(out, clone(exec))
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].GetOccurredAt().AsTime().Before(out[j].GetOccurredAt().AsTime())
	})
	if capped := ExecLimit(limit); len(out) > capped {
		out = out[len(out)-capped:]
	}
	return out, nil
}

// AppendSessionEvent records one thing that happened to a session. Writing the same event twice
// leaves one, the way an exec does.
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

// ListSessionEvents returns a session's lifecycle oldest first, or the whole system's when no session
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
	if capped := ExecLimit(limit); len(out) > capped {
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
		// The driver acts for the operator rather than doing job of its own, and a driver that stops
		// to ask before every step describes the exec instead of doing it. What bounds it is the
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
