package workspace

import (
	"context"
	"fmt"
	"sort"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
)

// Separator divides the levels of an address.
const Separator = "/"

// Path is an address into the crew: a workspace, optionally a project inside it, optionally a thread
// inside that. "me", "me/house-bills" and "me/house-bills/3cb04bf5" are all paths.
//
// It says what the operator typed, not what it points at. Turning it into identifiers is Resolve's
// job, because that needs the control plane.
type Path struct {
	Workspace string
	Project   string
	Thread    string
}

// ParsePath reads an address. Each segment is an id or a name, and a thread may be typed as the
// shortened identifier a listing prints.
func ParsePath(value string) (Path, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return Path{}, fmt.Errorf("workspace: an address is required, for example me/house-bills")
	}
	segments := strings.Split(trimmed, Separator)
	if len(segments) > 3 {
		return Path{}, fmt.Errorf("workspace: %q has %d levels: an address is workspace/project/thread at most",
			trimmed, len(segments))
	}
	for _, segment := range segments {
		if strings.TrimSpace(segment) == "" {
			return Path{}, fmt.Errorf("workspace: %q has an empty level in it", trimmed)
		}
	}

	parsed := Path{Workspace: strings.TrimSpace(segments[0])}
	if len(segments) > 1 {
		parsed.Project = strings.TrimSpace(segments[1])
	}
	if len(segments) > 2 {
		parsed.Thread = strings.TrimSpace(segments[2])
	}
	return parsed, nil
}

// String renders the address back the way it was typed.
func (p Path) String() string {
	parts := make([]string, 0, 3)
	for _, segment := range []string{p.Workspace, p.Project, p.Thread} {
		if segment == "" {
			break
		}
		parts = append(parts, segment)
	}
	return strings.Join(parts, Separator)
}

// IsZero reports whether the path names nothing at all.
func (p Path) IsZero() bool { return p.Workspace == "" }

// Location is a resolved address: the identifiers the control plane works in.
type Location struct {
	Path        Path
	WorkspaceID string
	ProjectID   string
	ThreadID    string
}

// HasProject reports whether the location reaches a project, which is what a turn needs.
func (l Location) HasProject() bool { return l.ProjectID != "" }

// ResolvePath turns an address into identifiers, one level at a time, so a failure names the level
// that failed rather than the whole address.
//
// The workspace narrows the project, which is what makes short project names usable: two workspaces
// may each hold a project called "notes" without either being ambiguous. A thread may be given as
// the shortened identifier a listing prints, and is expanded within its project.
func ResolvePath(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, path Path) (Location, error) {
	if path.IsZero() {
		return Location{}, fmt.Errorf("workspace: an address is required, for example me/house-bills")
	}

	located := Location{Path: path}
	workspaceID, err := Resolve(ctx, client, path.Workspace)
	if err != nil {
		return Location{}, err
	}
	located.WorkspaceID = workspaceID

	if path.Project == "" {
		return located, nil
	}
	projectID, err := ResolveProject(ctx, client, path.Workspace, path.Project)
	if err != nil {
		return Location{}, err
	}
	located.ProjectID = projectID

	if path.Thread == "" {
		return located, nil
	}
	threadID, err := resolveThread(ctx, client, projectID, path.Thread)
	if err != nil {
		return Location{}, err
	}
	located.ThreadID = threadID
	return located, nil
}

// resolveThread turns a thread reference into a thread id within one project. Listings shorten
// identifiers, so the thing on the operator's screen is a prefix and typing it back has to work.
func resolveThread(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, projectID, reference string) (string, error) {
	resp, err := client.ListThreads(ctx, &quaycrewv1.ListThreadsRequest{Project: projectID})
	if err != nil {
		return "", fmt.Errorf("workspace: list threads: %w", err)
	}

	matches := make([]string, 0, 1)
	for _, session := range resp.GetThreads() {
		if session.GetHandle() == reference {
			return reference, nil
		}
		if strings.HasPrefix(session.GetHandle(), reference) {
			matches = append(matches, session.GetHandle())
		}
	}

	switch len(matches) {
	case 0:
		// Shortened, because a listing prints them shortened and that is what gets typed back.
		return "", &NotFoundError{
			What: "thread", Name: reference,
			Have: namesOf(resp.GetThreads(), func(i int) string {
				return display.ShortID(resp.GetThreads()[i].GetHandle())
			}),
			Make: `start one with quay dispatch "..."`,
		}
	case 1:
		return matches[0], nil
	default:
		sort.Strings(matches)
		return "", &AmbiguousError{What: "threads", Name: reference, IDs: matches}
	}
}
