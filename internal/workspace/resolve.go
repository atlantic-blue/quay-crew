// Package workspace tasks what an operator typed into a workspace id.
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
	"github.com/atlantic-blue/quay-crew/internal/name"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ErrNotFound is the sentinel for "that address does not exist", at any level. What the operator
// reads is a NotFoundError, which knows which level failed; this stays so errors.Is keeps working.
var ErrNotFound = errors.New("no such address")

// NotFoundError says which level of an address did not resolve, where it was looked for, and what is
// actually there.
//
// One message used to serve all three levels, and it named the wrong one: a missing project came out
// as "workspace: no workspace with that id or name: project \"nope\"", which sent the operator to
// check the one part of the address that was correct.
type NotFoundError struct {
	// What is the level: workspace, project or session.
	What string
	// Name is what was typed.
	Name string
	// In is the address it was looked for inside, empty at the top level.
	In string
	// Have is what does exist there, in the words a listing prints them in. Empty means nothing
	// exists at that level yet, which is a different sentence: the operator has nothing to pick.
	Have []string
	// Make is the command that creates one, for when there is nothing to pick.
	Make string
}

func (e *NotFoundError) Error() string {
	where := "this system"
	if e.In != "" {
		where = e.In
	}
	if len(e.Have) == 0 {
		if e.Make == "" {
			return fmt.Sprintf("%s has no %ss", where, e.What)
		}
		return fmt.Sprintf("%s has no %ss yet: %s", where, e.What, e.Make)
	}
	return fmt.Sprintf("%s has no %s %q. it has: %s", where, e.What, e.Name, strings.Join(e.Have, ", "))
}

// Is makes errors.Is(err, ErrNotFound) answer for every level, so callers that only care whether
// something was missing keep working.
func (e *NotFoundError) Is(target error) bool { return target == ErrNotFound }

// AmbiguousError is returned when a name belongs to more than one thing. It carries the candidate
// ids so the operator can pick one rather than guess which the tool chose.
type AmbiguousError struct {
	// What is the level the name was read at, for example "workspaces".
	What string
	Name string
	IDs  []string
}

func (e *AmbiguousError) Error() string {
	what := e.What
	if what == "" {
		what = "workspaces"
	}
	return fmt.Sprintf("workspace: %q matches %d %s, use one of these ids: %s",
		e.Name, len(e.IDs), what, strings.Join(e.IDs, ", "))
}

// Resolve tasks a reference into a workspace id.
//
// An id wins. Only when the reference is not an id is it matched against workspace names, so a workspace
// whose name happens to be another workspace's id still resolves to itself.
func Resolve(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, reference string) (string, error) {
	if strings.TrimSpace(reference) == "" {
		return "", fmt.Errorf("workspace: a workspace id or name is required")
	}
	// A listing that takes the level word takes it before this, so anything arriving here saying the
	// old one is somebody typing what used to work.
	if err := name.RefuseRetired(reference); err != nil {
		return "", err
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
		return "", &NotFoundError{
			What: "workspace", Name: reference,
			Have: namesOf(resp.GetWorkspaces(), func(i int) string { return resp.GetWorkspaces()[i].GetName() }),
			Make: "make one with quay workspace create <name>",
		}
	case 1:
		return matches[0], nil
	default:
		sort.Strings(matches)
		return "", &AmbiguousError{What: "workspaces", Name: reference, IDs: matches}
	}
}

// namesOf pulls the names out of a listing, sorted, so a refusal offers them in the order a listing
// would. Taken by index and accessor because the three listings are three unrelated message types.
func namesOf[T any](items []T, name func(int) string) []string {
	names := make([]string, 0, len(items))
	for index := range items {
		if got := name(index); got != "" {
			names = append(names, got)
		}
	}
	sort.Strings(names)
	return names
}

// ResolveProject tasks a reference into a project id, the same way Resolve does for a workspace.
//
// A workspace reference narrows the search, which is what makes short project names usable: two
// workspaces may each have a project called "notes" without either being ambiguous.
func ResolveProject(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, workspaceRef, projectRef string) (string, error) {
	if strings.TrimSpace(projectRef) == "" {
		return "", fmt.Errorf("workspace: a project id or name is required")
	}

	if _, err := client.GetProject(ctx, &quaycrewv1.GetProjectRequest{Id: projectRef}); err == nil {
		return projectRef, nil
	} else if status.Code(err) != codes.NotFound {
		return "", fmt.Errorf("workspace: look up project %q: %w", projectRef, err)
	}

	scope := ""
	if strings.TrimSpace(workspaceRef) != "" {
		resolved, err := Resolve(ctx, client, workspaceRef)
		if err != nil {
			return "", err
		}
		scope = resolved
	}

	resp, err := client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{Workspace: scope})
	if err != nil {
		return "", fmt.Errorf("workspace: list projects: %w", err)
	}

	matches := make([]string, 0, 1)
	for _, candidate := range resp.GetProjects() {
		if candidate.GetName() == projectRef {
			matches = append(matches, candidate.GetId())
		}
	}

	switch len(matches) {
	case 0:
		return "", &NotFoundError{
			What: "project", Name: projectRef, In: strings.TrimSpace(workspaceRef),
			Have: namesOf(resp.GetProjects(), func(i int) string { return resp.GetProjects()[i].GetName() }),
			Make: "make one with quay project create <name>",
		}
	case 1:
		return matches[0], nil
	default:
		sort.Strings(matches)
		return "", &AmbiguousError{What: "projects", Name: projectRef, IDs: matches}
	}
}
