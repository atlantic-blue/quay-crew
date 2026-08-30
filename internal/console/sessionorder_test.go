package console

import (
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The console shows the same listing the command line and the web page show, and the order is the
// control plane's to decide. Sorting here as well would be a second order to keep in step with the
// other two, and it was: the view sorted by the session column while the system answered last moved
// first, so the age column arrived in order and was shuffled before anybody saw it.

func movedAt(id, project string, hoursAgo int, archived bool) *quaycrewv1.Session {
	stamp := timestamppb.New(time.Now().UTC().Add(-time.Duration(hoursAgo) * time.Hour))
	session := &quaycrewv1.Session{
		Id: id, Project: project, Workspace: "w", Handle: id, Status: "idle", UpdatedAt: stamp,
	}
	if archived {
		session.ArchivedAt = stamp
	}
	return session
}

// listedBy drives a resource's own List, the way the console does on a refresh, and hands back what
// the operator is left looking at rather than what the system answered.
func listedBy(t *testing.T, resource Resource, client *fakeClient) []string {
	t.Helper()
	rows, err := resource.List(context.Background(), "")
	if err != nil {
		t.Fatalf("list %s: %v", resource.Name, err)
	}
	model := newTestModel(t, resource)
	model, _ = update(t, model, rowsFor(model, rows...))

	visible := model.visibleRows()
	out := make([]string, 0, len(visible))
	for _, row := range visible {
		out = append(out, row.ID)
	}
	return out
}

func sameOrder(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the view lists %v, want %v", got, want)
	}
}

// The identifiers here run backwards against the order the system answered in, so a view that sorts by
// the session column comes back exactly reversed.
func TestTheSessionsViewKeepsTheOrderTheSystemAnsweredIn(t *testing.T) {
	client := &fakeClient{sessions: []*quaycrewv1.Session{
		movedAt("ccc", "p", 1, false),
		movedAt("bbb", "p", 24, false),
		movedAt("aaa", "p", 24*7, false),
	}}

	sameOrder(t, listedBy(t, Sessions(client), client), []string{"ccc", "bbb", "aaa"})
}

// The archived view is the same listing read from the other shelf, so it keeps the order too.
func TestTheArchivedViewKeepsTheOrderTheSystemAnsweredIn(t *testing.T) {
	client := &fakeClient{sessions: []*quaycrewv1.Session{
		movedAt("zzz", "p", 2, true),
		movedAt("mmm", "p", 48, true),
		movedAt("aaa", "p", 24*30, true),
	}}

	sameOrder(t, listedBy(t, Archived(client), client), []string{"zzz", "mmm", "aaa"})
}

// A view that orders rows itself marks the column it ordered by, so an order the operator cannot see
// is never claimed. Neither session view orders anything now, so neither may claim to.
func TestNeitherSessionViewClaimsToHaveOrderedAColumn(t *testing.T) {
	client := &fakeClient{sessions: []*quaycrewv1.Session{movedAt("aaa", "p", 1, false)}}
	for _, resource := range []Resource{Sessions(client), Archived(client)} {
		model := newTestModel(t, resource)
		model, _ = update(t, model, rowsFor(model, Row{ID: "aaa", Cells: []string{"aaa"}}))
		if view := model.View(); strings.Contains(view, "↑") {
			t.Fatalf("the %s view marks a column as sorted while the system decides the order:\n%s", resource.Name, view)
		}
	}
}
