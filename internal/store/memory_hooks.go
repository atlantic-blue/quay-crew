package store

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// The in memory store's hooks. The same six questions as skills, answered the same way, kept in
// their own file because the skill block in memory.go is already long enough to hide a bug in.

// ImportHook takes a hook into the crew at the version its manifest declares.
func (m *Memory) ImportHook(_ context.Context, imported ImportedHook) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hooks == nil {
		m.hooks = make(map[string]ImportedHook)
	}
	key := skillKey(imported.Name, imported.Version)
	if held, already := m.hooks[key]; already {
		if held.Fingerprint() != imported.Fingerprint() {
			return fmt.Errorf("%w: %s version %d", ErrHookChanged, imported.Name, imported.Version)
		}
		return nil
	}
	// Stamped here because Postgres stamps it in the table's default, and a store that leaves it
	// empty is a double that accepts what the real one would not.
	if imported.ImportedAt.IsZero() {
		imported.ImportedAt = time.Now().UTC()
	}
	m.hooks[key] = imported
	return nil
}

// GetHook returns one revision of a hook.
func (m *Memory) GetHook(_ context.Context, name string, version int) (ImportedHook, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	held, found := m.hooks[skillKey(name, version)]
	if !found {
		return ImportedHook{}, ErrNotFound
	}
	return held, nil
}

// ListHooks returns the newest revision of every hook, without their files.
func (m *Memory) ListHooks(_ context.Context) ([]ImportedHook, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	newest := make(map[string]ImportedHook, len(m.hooks))
	for _, held := range m.hooks {
		if seen, found := newest[held.Name]; !found || held.Version > seen.Version {
			newest[held.Name] = held
		}
	}
	out := make([]ImportedHook, 0, len(newest))
	for _, held := range newest {
		held.Files = nil
		out = append(out, held)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// AttachHook gives a workspace a hook at the newest revision the crew holds.
func (m *Memory) AttachHook(_ context.Context, workspace, name string) (ImportedHook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, found := m.workspaces[workspace]; !found || m.deleted[workspace] {
		return ImportedHook{}, ErrNotFound
	}
	newest, found := m.newestHook(name)
	if !found {
		return ImportedHook{}, ErrNotFound
	}
	if m.hooksAttached == nil {
		m.hooksAttached = make(map[string]map[string]int)
	}
	if m.hooksAttached[workspace] == nil {
		m.hooksAttached[workspace] = make(map[string]int)
	}
	m.hooksAttached[workspace][name] = newest.Version
	return newest, nil
}

// DetachHook takes a hook away from a workspace.
func (m *Memory) DetachHook(_ context.Context, workspace, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	held, found := m.hooksAttached[workspace]
	if !found {
		return ErrNotFound
	}
	if _, found := held[name]; !found {
		return ErrNotFound
	}
	delete(held, name)
	return nil
}

// WorkspaceHooks returns what a workspace holds, at the versions it pinned.
func (m *Memory) WorkspaceHooks(_ context.Context, workspace string) ([]ImportedHook, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ImportedHook, 0, len(m.hooksAttached[workspace]))
	for name, version := range m.hooksAttached[workspace] {
		held, found := m.hooks[skillKey(name, version)]
		if !found {
			continue
		}
		out = append(out, held)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// AttachCrewHook gives the whole crew a hook at the newest revision it holds.
func (m *Memory) AttachCrewHook(_ context.Context, name string) (ImportedHook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	newest, found := m.newestHook(name)
	if !found {
		return ImportedHook{}, ErrNotFound
	}
	if m.crewHooks == nil {
		m.crewHooks = make(map[string]int)
	}
	m.crewHooks[name] = newest.Version
	return newest, nil
}

// DetachCrewHook takes a hook away from the crew.
func (m *Memory) DetachCrewHook(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, found := m.crewHooks[name]; !found {
		return ErrNotFound
	}
	delete(m.crewHooks, name)
	return nil
}

// CrewHooks returns what the crew holds, at the versions it pinned.
func (m *Memory) CrewHooks(_ context.Context) ([]ImportedHook, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ImportedHook, 0, len(m.crewHooks))
	for name, version := range m.crewHooks {
		held, found := m.hooks[skillKey(name, version)]
		if !found {
			continue
		}
		out = append(out, held)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// newestHook is the highest version of a name the crew holds. Callers hold the lock.
func (m *Memory) newestHook(name string) (ImportedHook, bool) {
	var newest ImportedHook
	var found bool
	for _, held := range m.hooks {
		if held.Name != name {
			continue
		}
		if !found || held.Version > newest.Version {
			newest, found = held, true
		}
	}
	return newest, found
}
