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
// because a client asked politely. The only reader is the system itself, at the moment an exec needs it.
type Ref struct {
	Name       string
	Projection Projection
	// System says this one came from the system's level rather than from the workspace, so a listing can
	// say where it came from. A workspace that set nothing and has three is otherwise a puzzle.
	//
	// A backend never sets it. Levels sets it when it merges the two levels, and a caller reading one
	// level knows which it asked for, so there is one place that decides and it is the place that can
	// be wrong.
	System bool
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

// Store sets and reads secrets at two levels: one workspace's own, and the system's. Set is called
// from the API; Get is called by services that need the value at runtime.
//
// The system's level is what a token every workspace needs belongs to. A subscription token, a forge
// token, a credential file: set once, read by every workspace, including the ones made after it. It
// is a separate set of calls rather than a reserved workspace identifier because the system is not a
// workspace: it holds no projects, no sessions and no channels. That is the shape the system's skills,
// hooks and roles already have.
//
// Neither Get nor List merges the two levels. Levels does that, once, so there is one answer to
// which level wins.
type Store interface {
	Set(ctx context.Context, workspace string, secret Secret) error
	Get(ctx context.Context, workspace, name string) (string, error)
	// List says what a workspace has set and how each one reaches a sandbox, sorted by name, and
	// never what any of it says.
	List(ctx context.Context, workspace string) ([]Ref, error)

	// SetSystem stores a secret at the system's level, where every workspace reads it.
	SetSystem(ctx context.Context, secret Secret) error
	// GetSystem reads a value the system holds.
	GetSystem(ctx context.Context, name string) (string, error)
	// ListSystem says what the system holds, sorted by name, and never what any of it says.
	ListSystem(ctx context.Context) ([]Ref, error)
}

// Levels reads a workspace's secrets over the system's.
//
// A workspace wins on a name, which is the rule the system's skills already use: the system's level is
// what every workspace gets, and a workspace that says something different about a name means it.
// Without that, a shared token could not be overridden for the one workspace that needs a different
// one, and the system's level would be a floor rather than a default.
//
// The merge lives here, in one place, rather than in each backend. A second reader is a second
// chance to get the rule wrong, and the two backends would then disagree about which level wins
// while both passing their own tests.
type Levels struct{ Store }

// compile time check: a merged reader is still a Store, so nothing downstream knows about this.
var _ Store = Levels{}

// Get reads what the workspace holds, and what the system holds when the workspace holds nothing under
// that name.
func (l Levels) Get(ctx context.Context, workspace, name string) (string, error) {
	value, err := l.Store.Get(ctx, workspace, name)
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	return l.GetSystem(ctx, name)
}

// List says everything that reaches this workspace's sandboxes: what it set itself, and what the
// system holds under a name it did not set. Sorted by name, so a caller reading two levels reads one
// listing.
func (l Levels) List(ctx context.Context, workspace string) ([]Ref, error) {
	own, err := l.Store.List(ctx, workspace)
	if err != nil {
		return nil, err
	}
	system, err := l.ListSystem(ctx)
	if err != nil {
		return nil, err
	}
	held := make(map[string]bool, len(own))
	for _, ref := range own {
		held[ref.Name] = true
	}
	merged := own
	for _, ref := range system {
		if held[ref.Name] {
			continue
		}
		ref.System = true
		merged = append(merged, ref)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Name < merged[j].Name })
	return merged, nil
}

// Memory is an in memory Store for development and tests.
type Memory struct {
	mu     sync.RWMutex
	data   map[string]Secret
	system map[string]Secret
}

// compile time check.
var _ Store = (*Memory)(nil)

// NewMemory returns an empty in memory store.
func NewMemory() *Memory {
	return &Memory{data: make(map[string]Secret), system: make(map[string]Secret)}
}

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

// SetSystem stores a secret at the system's level.
func (m *Memory) SetSystem(_ context.Context, secret Secret) error {
	if err := secret.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	secret.Projection = secret.Projection.Or()
	m.system[secret.Name] = secret
	return nil
}

// GetSystem reads a value the system holds.
func (m *Memory) GetSystem(_ context.Context, name string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	secret, ok := m.system[name]
	if !ok {
		return "", ErrNotFound
	}
	return secret.Value, nil
}

// ListSystem says what the system holds, sorted, and never what any of it says.
func (m *Memory) ListSystem(_ context.Context) ([]Ref, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	refs := make([]Ref, 0, len(m.system))
	for _, secret := range m.system {
		refs = append(refs, Ref{Name: secret.Name, Projection: secret.Projection.Or()})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	return refs, nil
}
