// Package project turns what an operator typed into a project id.
//
// Commands take a project id, a twenty four character hex string that is printed once at creation
// and is not worth carrying in your head. This package lets the same flag take a name instead. It
// lives here rather than in the command so the behaviour scenarios can drive it against a real
// control plane, the way the console does.
package project

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

// ErrNotFound is returned when a reference matches no project, by id or by name.
var ErrNotFound = errors.New("project: no project with that id or name")

// AmbiguousError is returned when a name belongs to more than one project. It carries the candidate
// ids so the operator can pick one rather than guess which the tool chose.
type AmbiguousError struct {
	Name string
	IDs  []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("project: %q matches %d projects, use one of these ids: %s",
		e.Name, len(e.IDs), strings.Join(e.IDs, ", "))
}

// Resolve turns a reference into a project id.
//
// An id wins. Only when the reference is not an id is it matched against project names, so a project
// whose name happens to be another project's id still resolves to itself.
func Resolve(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, reference string) (string, error) {
	if strings.TrimSpace(reference) == "" {
		return "", fmt.Errorf("project: a project id or name is required")
	}

	if _, err := client.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: reference}); err == nil {
		return reference, nil
	} else if status.Code(err) != codes.NotFound {
		return "", fmt.Errorf("project: look up %q: %w", reference, err)
	}

	resp, err := client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{})
	if err != nil {
		return "", fmt.Errorf("project: list projects: %w", err)
	}

	matches := make([]string, 0, 1)
	for _, candidate := range resp.GetProjects() {
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
