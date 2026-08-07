package sandbox

import "fmt"

// Options configures the provider a composition root builds.
type Options struct {
	// Image is the container image sessions run in.
	Image string
	// Mounts are extra bind mounts applied to every sandbox, each "host:container[:ro]".
	Mounts []string
	// Storage is where a workspace's conversation store and a project's files are kept so they
	// outlive the container. Leave it empty and they do not.
	Storage Storage
	// Network is the container network sessions join. Empty leaves them where they cannot reach the
	// control plane, which is the default.
	Network string
}

// Kinds of sandbox. The default isolates each session in its own container.
const (
	// KindDocker gives each session a container of its own.
	KindDocker = "docker"
	// KindLocal runs on the host with no isolation. A stopgap, not a sandbox.
	KindLocal = "local"
)

// ResolveKind names the backend a kind selects, filling in the default for an empty one. Anything
// that reports the configuration reads it from here, so what is reported cannot drift from what is
// built.
func ResolveKind(kind string) (string, error) {
	switch kind {
	case "", KindDocker:
		return KindDocker, nil
	case KindLocal:
		return KindLocal, nil
	default:
		return "", fmt.Errorf("sandbox: unknown provider %q", kind)
	}
}

// NewProvider builds a Provider by kind. The default (empty or "docker") isolates each session in a
// container; "local" is the short term host backend. Other kinds are an error.
func NewProvider(kind string, opts Options) (Provider, error) {
	resolved, err := ResolveKind(kind)
	if err != nil {
		return nil, err
	}
	if resolved == KindLocal {
		return LocalProvider{}, nil
	}
	// The Docker backend is configured by exactly these options today, so it converts straight
	// across. A backend that needs something else gets its own fields here.
	return DockerProvider(opts), nil
}
