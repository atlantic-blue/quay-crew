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
}

// NewProvider builds a Provider by kind. The default (empty or "docker") isolates each session in a
// container; "local" is the short term host backend. Other kinds are an error.
func NewProvider(kind string, opts Options) (Provider, error) {
	switch kind {
	case "", "docker":
		// The Docker backend is configured by exactly these options today, so it converts straight
		// across. A backend that needs something else gets its own fields here.
		return DockerProvider(opts), nil
	case "local":
		return LocalProvider{}, nil
	default:
		return nil, fmt.Errorf("sandbox: unknown provider %q", kind)
	}
}
