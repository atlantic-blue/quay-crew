package session

import "fmt"

// New builds a Runtime by kind. The default (empty or "docker") is Docker isolation; "local" is the
// short term host backend. Other kinds are an error.
func New(kind, image string, mounts []string) (Runtime, error) {
	switch kind {
	case "", "docker":
		return Docker{Image: image, Mounts: mounts}, nil
	case "local":
		return Local{}, nil
	default:
		return nil, fmt.Errorf("session: unknown runtime %q", kind)
	}
}
