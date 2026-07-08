package sandbox

import "fmt"

// NewProvider builds a Provider by kind. The default (empty or "docker") isolates each session in a
// container; "local" is the short term host backend. Other kinds are an error.
func NewProvider(kind, image string, mounts []string) (Provider, error) {
	switch kind {
	case "", "docker":
		return DockerProvider{Image: image, Mounts: mounts}, nil
	case "local":
		return LocalProvider{}, nil
	default:
		return nil, fmt.Errorf("sandbox: unknown provider %q", kind)
	}
}
