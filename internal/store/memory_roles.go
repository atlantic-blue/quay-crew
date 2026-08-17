package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// The roles a crew holds, in memory. The same nine calls the Postgres store answers, held to the
// same conformance suite, so a behaviour proven against one is proven against the other.

// ImportRole takes a role into the crew, refusing a version that already exists carrying something
// different.
func (m *Memory) ImportRole(_ context.Context, imported ImportedRole) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.roles == nil {
		m.roles = make(map[string]ImportedRole)
	}
	key := roleKey(imported.Name, imported.Version)
	if held, already := m.roles[key]; already {
		if held.Fingerprint() != imported.Fingerprint() {
			return fmt.Errorf("%w: %s version %d", ErrRoleChanged, imported.Name, imported.Version)
		}
		return nil
	}
	// Stamped here because Postgres stamps it in the table's default, and a store that leaves it
	// empty is a double that accepts what the real one would not.
	if imported.ImportedAt.IsZero() {
		imported.ImportedAt = time.Now().UTC()
	}
	m.roles[key] = imported
	return nil
}

// GetRole returns one revision of a role.
func (m *Memory) GetRole(_ context.Context, name string, version int) (ImportedRole, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	held, found := m.roles[roleKey(name, version)]
	if !found {
		return ImportedRole{}, ErrNotFound
	}
	return held, nil
}

// ListRoles returns the newest revision of every role.
func (m *Memory) ListRoles(_ context.Context) ([]ImportedRole, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	newest := make(map[string]ImportedRole, len(m.roles))
	for _, held := range m.roles {
		if seen, found := newest[held.Name]; !found || held.Version > seen.Version {
			newest[held.Name] = held
		}
	}
	out := make([]ImportedRole, 0, len(newest))
	for _, held := range newest {
		out = append(out, held)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// AttachRole gives a workspace a role at the newest revision the crew holds.
func (m *Memory) AttachRole(_ context.Context, workspace, name string) (ImportedRole, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, found := m.workspaces[workspace]; !found || m.deleted[workspace] {
		return ImportedRole{}, ErrNotFound
	}
	newest, found := m.newestRole(name)
	if !found {
		return ImportedRole{}, ErrNotFound
	}
	if m.rolesAttached == nil {
		m.rolesAttached = make(map[string]map[string]int)
	}
	if m.rolesAttached[workspace] == nil {
		m.rolesAttached[workspace] = make(map[string]int)
	}
	m.rolesAttached[workspace][name] = newest.Version
	return newest, nil
}

// DetachRole takes a role away from a workspace.
func (m *Memory) DetachRole(_ context.Context, workspace, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	held, found := m.rolesAttached[workspace]
	if !found {
		return ErrNotFound
	}
	if _, found := held[name]; !found {
		return ErrNotFound
	}
	delete(held, name)
	return nil
}

// WorkspaceRoles returns what a workspace holds, at the versions it pinned.
func (m *Memory) WorkspaceRoles(_ context.Context, workspace string) ([]ImportedRole, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ImportedRole, 0, len(m.rolesAttached[workspace]))
	for name, version := range m.rolesAttached[workspace] {
		held, found := m.roles[roleKey(name, version)]
		if !found {
			continue
		}
		out = append(out, held)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// AttachCrewRole gives the whole crew a role at the newest revision it holds.
func (m *Memory) AttachCrewRole(_ context.Context, name string) (ImportedRole, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	newest, found := m.newestRole(name)
	if !found {
		return ImportedRole{}, ErrNotFound
	}
	if m.crewRoles == nil {
		m.crewRoles = make(map[string]int)
	}
	m.crewRoles[name] = newest.Version
	return newest, nil
}

// DetachCrewRole takes a role away from the crew.
func (m *Memory) DetachCrewRole(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, found := m.crewRoles[name]; !found {
		return ErrNotFound
	}
	delete(m.crewRoles, name)
	return nil
}

// CrewRoles returns what the crew holds, at the versions it pinned.
func (m *Memory) CrewRoles(_ context.Context) ([]ImportedRole, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ImportedRole, 0, len(m.crewRoles))
	for name, version := range m.crewRoles {
		held, found := m.roles[roleKey(name, version)]
		if !found {
			continue
		}
		out = append(out, held)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// newestRole is the highest version of a name the crew holds. Callers hold the lock.
func (m *Memory) newestRole(name string) (ImportedRole, bool) {
	var newest ImportedRole
	var found bool
	for _, held := range m.roles {
		if held.Name != name {
			continue
		}
		if !found || held.Version > newest.Version {
			newest, found = held, true
		}
	}
	return newest, found
}

func roleKey(name string, version int) string { return name + "\x00" + strconv.Itoa(version) }
