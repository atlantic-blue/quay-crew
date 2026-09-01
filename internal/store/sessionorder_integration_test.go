//go:build integration

package store_test

import (
	"context"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The order of a session listing, against the real database.
//
// The conformance suite already runs the ordering cases here, so what is left is the one thing no
// sequence of store calls can set up: two sessions that share a moment exactly. The store stamps its
// own rows and takes no stamp from a caller, and every write is its own transaction, so `now()` moves
// between any two of them. One statement over both rows is the only way to make a tie, and a tie is
// what the identifier in the order clause exists for.
//
// Without it the two rows sit in whatever order the database returns them, which is stable enough to
// pass a test and not stable enough for an operator: a listing that reshuffles between two refreshes
// is a listing nobody can point at.
func TestTwoSessionsSharingAMomentAreOrderedByIdentifier(t *testing.T) {
	ctx := context.Background()
	truncate(t)

	s, err := store.NewPostgres(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(s.Close)

	workspace, err := s.CreateWorkspace(ctx, "acme")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := s.CreateProject(ctx, workspace.GetId(), "house bills")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	for _, handle := range []string{"session-a", "session-b", "session-c"} {
		if _, _, err := s.FindOrCreateSession(ctx, project.GetId(), handle, store.Birth{}); err != nil {
			t.Fatalf("FindOrCreateSession %s: %v", handle, err)
		}
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to stamp the rows: %v", err)
	}
	defer pool.Close()
	// One statement, so `now()` is the transaction's own clock and every row takes the same value.
	if _, err := pool.Exec(ctx, `update sessions set updated_at = now() where project = $1`, project.GetId()); err != nil {
		t.Fatalf("stamp the rows with one moment: %v", err)
	}

	listed, err := s.ListSessions(ctx, store.SessionFilter{Project: project.GetId()})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("the listing holds %d sessions, want 3", len(listed))
	}
	// The tie is the setup, so it is asserted rather than assumed. Three rows a microsecond apart
	// would order by the stamp and this case would report the identifier working when it never ran.
	first := listed[0].GetUpdatedAt().AsTime()
	for _, session := range listed {
		if !session.GetUpdatedAt().AsTime().Equal(first) {
			t.Fatalf("the rows do not share a moment, so nothing here reaches the tiebreaker")
		}
	}
	for i := 1; i < len(listed); i++ {
		if listed[i-1].GetId() >= listed[i].GetId() {
			t.Fatalf("sessions sharing a moment came back as %v, want them by identifier", idsOf(listed))
		}
	}
}

// idsOf names a listing, so a failure says which sessions came back and in what order.
func idsOf(sessions []*quaycrewv1.Session) []string {
	out := make([]string, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, session.GetId())
	}
	return out
}
