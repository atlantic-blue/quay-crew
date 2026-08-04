// Package secrets is the pluggable store for per workspace credentials. Secrets are set at runtime
// through the control plane and read by reference; the value never appears in the repo, and the
// event log records only a reference, never the value. This package provides the interface and an
// in memory backend for development; a managed backend implements the same interface in the cloud.
package secrets

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

// ErrNotFound is returned when a secret does not exist.
var ErrNotFound = errors.New("secrets: not found")

// Store sets and reads workspace scoped secrets. Set is called from the API; Get is called by
// services that need the value at runtime.
type Store interface {
	Set(ctx context.Context, workspace, key, value string) error
	Get(ctx context.Context, workspace, key string) (string, error)
	// Names lists what a workspace has set, and never what any of it says. A value the backend holds
	// must not become readable because a client asked politely, so there is no call that returns one:
	// the only reader is the crew itself, at the moment a turn needs it.
	Names(ctx context.Context, workspace string) ([]string, error)
}

// Memory is an in memory Store for development and tests.
type Memory struct {
	mu   sync.RWMutex
	data map[string]string
}

// compile time check.
var _ Store = (*Memory)(nil)

// NewMemory returns an empty in memory store.
func NewMemory() *Memory { return &Memory{data: make(map[string]string)} }

func key(workspace, name string) string { return workspace + "\x00" + name }

// Set stores a secret value for a workspace.
func (m *Memory) Set(_ context.Context, workspace, name, value string) error {
	if workspace == "" || name == "" {
		return errors.New("secrets: workspace and key are required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key(workspace, name)] = value
	return nil
}

// Get reads a secret value for a workspace.
// Names lists what a workspace has set, sorted, and never what any of it says.
func (m *Memory) Names(_ context.Context, workspace string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	prefix := key(workspace, "")
	names := make([]string, 0, len(m.data))
	for stored := range m.data {
		if strings.HasPrefix(stored, prefix) {
			names = append(names, strings.TrimPrefix(stored, prefix))
		}
	}
	sort.Strings(names)
	return names, nil
}

func (m *Memory) Get(_ context.Context, workspace, name string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.data[key(workspace, name)]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}
