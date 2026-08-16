// Package secrets is the pluggable store for per workspace credentials. Secrets are set at runtime
// through the control plane and read by reference; the value never appears in the repo, and the
// event log records only a reference, never the value. This package provides the interface and an
// in memory backend for development; a managed backend implements the same interface in the cloud.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// ErrNotFound is returned when a secret does not exist.
var ErrNotFound = errors.New("secrets: not found")

// Projection is how a secret reaches a sandbox.
//
// The store holds bytes under a name and knows nothing else about them. Whether those bytes become
// an environment variable or a file is a separate choice, which is the shape Kubernetes and Docker
// both settled on: a Kubernetes Secret is read through secretKeyRef or mounted through a secret
// volume, and a Docker secret is given a target and lands as a file. Neither writes the presentation
// into the store.
type Projection string

const (
	// Env makes the value an environment variable named after the secret. The default, and what every
	// secret set before this existed is.
	Env Projection = "env"
	// File makes the value a file, and keeps it out of the environment entirely. A credential a tool
	// opens by path needs this, and it is also the safer of the two: the environment of a container is
	// readable through docker inspect for the life of that container, and a file is not.
	File Projection = "file"
)

// Or answers with this projection, or with the default when there is none. A secret stored before
// projections existed has no answer of its own, and the answer for one is the environment.
func (p Projection) Or() Projection {
	if p == File {
		return File
	}
	return Env
}

// Secret is what an operator sets: a name, bytes, and how they should reach a sandbox.
type Secret struct {
	Name  string
	Value string
	// Projection is how it reaches a sandbox. Empty reads as Env.
	Projection Projection
}

// Ref names a secret a workspace has set and says how it reaches a sandbox. It carries no value:
// there is no call that returns one, because a value the backend holds must not become readable
// because a client asked politely. The only reader is the crew itself, at the moment a task needs it.
type Ref struct {
	Name       string
	Projection Projection
}

// Validate refuses a secret the store should not hold.
//
// A file projected name becomes a file name inside the sandbox, so it is checked here rather than at
// the moment of writing: a name that escapes its directory must not reach the store at all, and a
// second reader is a second chance to get the check wrong.
func (s Secret) Validate() error {
	if s.Name == "" {
		return errors.New("secrets: a secret needs a name")
	}
	if s.Projection.Or() != File {
		return nil
	}
	if s.Name == "." || s.Name == ".." || strings.ContainsAny(s.Name, `/\`) {
		return fmt.Errorf("secrets: %q cannot be a file name, so it cannot be mounted", s.Name)
	}
	return nil
}

// Store sets and reads workspace scoped secrets. Set is called from the API; Get is called by
// services that need the value at runtime.
type Store interface {
	Set(ctx context.Context, workspace string, secret Secret) error
	Get(ctx context.Context, workspace, name string) (string, error)
	// List says what a workspace has set and how each one reaches a sandbox, sorted by name, and
	// never what any of it says.
	List(ctx context.Context, workspace string) ([]Ref, error)
}

// Memory is an in memory Store for development and tests.
type Memory struct {
	mu   sync.RWMutex
	data map[string]Secret
}

// compile time check.
var _ Store = (*Memory)(nil)

// NewMemory returns an empty in memory store.
func NewMemory() *Memory { return &Memory{data: make(map[string]Secret)} }

func key(workspace, name string) string { return workspace + "\x00" + name }

// Set stores a secret for a workspace.
func (m *Memory) Set(_ context.Context, workspace string, secret Secret) error {
	if workspace == "" {
		return errors.New("secrets: a secret needs a workspace")
	}
	if err := secret.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	secret.Projection = secret.Projection.Or()
	m.data[key(workspace, secret.Name)] = secret
	return nil
}

// List says what a workspace has set and how each one reaches a sandbox, sorted, and never what any
// of it says.
func (m *Memory) List(_ context.Context, workspace string) ([]Ref, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	prefix := key(workspace, "")
	refs := make([]Ref, 0, len(m.data))
	for stored, secret := range m.data {
		if !strings.HasPrefix(stored, prefix) {
			continue
		}
		refs = append(refs, Ref{Name: secret.Name, Projection: secret.Projection.Or()})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs, nil
}

// Get reads a secret value for a workspace.
func (m *Memory) Get(_ context.Context, workspace, name string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	secret, ok := m.data[key(workspace, name)]
	if !ok {
		return "", ErrNotFound
	}
	return secret.Value, nil
}
