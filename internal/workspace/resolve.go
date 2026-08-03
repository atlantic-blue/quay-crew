// Package workspace turns what an operator typed into a workspace id.
//
// Commands take a workspace id, a twenty four character hex string that is printed once at creation
// and is not worth carrying in your head. This package lets the same flag take a name instead. It
// lives here rather than in the command so the behaviour scenarios can drive it against a real
// control plane, the way the console does.
package workspace

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrNotFound is returned when a reference matches no workspace, by id or by name.
var ErrNotFound = errors.New("workspace: no workspace with that id or name")

// AmbiguousError is returned when a name belongs to more than one workspace. It carries the candidate
// ids so the operator can pick one rather than guess which the tool chose.
type AmbiguousError struct {
	Name string
	IDs  []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("workspace: %q matches %d workspaces, use one of these ids: %s",
		e.Name, len(e.IDs), strings.Join(e.IDs, ", "))
}

// Resolve turns a reference into a workspace id.
//
// An id wins. Only when the reference is not an id is it matched against workspace names, so a workspace
// whose name happens to be another workspace's id still resolves to itself.
func Resolve(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, reference string) (string, error) {
	if strings.TrimSpace(reference) == "" {
		return "", fmt.Errorf("workspace: a workspace id or name is required")
	}

	if _, err := client.GetWorkspace(ctx, &quaycrewv1.GetWorkspaceRequest{Id: reference}); err == nil {
		return reference, nil
	} else if status.Code(err) != codes.NotFound {
		return "", fmt.Errorf("workspace: look up %q: %w", reference, err)
	}

	resp, err := client.ListWorkspaces(ctx, &quaycrewv1.ListWorkspacesRequest{})
	if err != nil {
		return "", fmt.Errorf("workspace: list workspaces: %w", err)
	}

	matches := make([]string, 0, 1)
	for _, candidate := range resp.GetWorkspaces() {
		if candidate.GetName() == reference {
			matches = append(matches, candidate.GetId())
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("%w: %q", ErrNotFound, reference)
	case 1:
		return matches[0], nil
	default:
		sort.Strings(matches)
		return "", &AmbiguousError{Name: reference, IDs: matches}
	}
}
