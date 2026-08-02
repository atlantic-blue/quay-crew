// Package storetest holds the conformance suite every store.Store implementation must pass.
//
// It exists so the in memory store and the Postgres store cannot drift. A behaviour asserted here is
// asserted against both: the in memory one in the unit tier, the Postgres one against a real
// database in the integration tier. Anything that passes for one and not the other is a bug in the
// implementation, not a difference the control plane is allowed to care about.
package storetest

import (
	"context"
	"errors"
	"testing"

	"github.com/atlantic-blue/quay-crew/internal/store"
)

// Opener hands out handles to one isolated dataset. Calling it twice returns two independent handles
// to the same underlying data, which is how the durability check reopens the store.
type Opener func(t *testing.T) store.Store

// RunConformance runs the whole contract against an implementation. newDataset must return an Opener
// over data no other subtest can see.
func RunConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a project can be created, fetched and listed", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		created, err := s.CreateProject(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		if created.GetId() == "" {
			t.Fatal("created project has no id")
		}
		if created.GetName() != "acme" {
			t.Fatalf("name is %q, want acme", created.GetName())
		}
		if created.GetCreatedAt() == nil {
			t.Fatal("created project has no created_at")
		}

		fetched, err := s.GetProject(ctx, created.GetId())
		if err != nil {
			t.Fatalf("GetProject: %v", err)
		}
		if fetched.GetName() != "acme" {
			t.Fatalf("fetched name is %q, want acme", fetched.GetName())
		}

		list, err := s.ListProjects(ctx)
		if err != nil {
			t.Fatalf("ListProjects: %v", err)
		}
		if len(list) != 1 || list[0].GetId() != created.GetId() {
			t.Fatalf("ListProjects returned %d projects, want the one created", len(list))
		}
	})

	t.Run("a project that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		if _, err := s.GetProject(context.Background(), "ghost"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetProject on a missing project returned %v, want ErrNotFound", err)
		}
	})

	t.Run("a deleted project is hidden from every read", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		created, _ := s.CreateProject(ctx, "acme")
		if err := s.DeleteProject(ctx, created.GetId()); err != nil {
			t.Fatalf("DeleteProject: %v", err)
		}

		if _, err := s.GetProject(ctx, created.GetId()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetProject after delete returned %v, want ErrNotFound", err)
		}
		list, err := s.ListProjects(ctx)
		if err != nil {
			t.Fatalf("ListProjects: %v", err)
		}
		if len(list) != 0 {
			t.Fatalf("ListProjects returned %d projects after delete, want 0", len(list))
		}
		if err := s.DeleteProject(ctx, created.GetId()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("deleting twice returned %v, want ErrNotFound", err)
		}
	})

	t.Run("a channel attaches to a live project only", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		created, _ := s.CreateProject(ctx, "acme")
		channel, err := s.AttachChannel(ctx, created.GetId(), "family-chat", "telegram")
		if err != nil {
			t.Fatalf("AttachChannel: %v", err)
		}
		if channel.GetKind() != "telegram" || channel.GetId() != "family-chat" {
			t.Fatalf("attached channel is %+v", channel)
		}

		if _, err := s.AttachChannel(ctx, "ghost", "family-chat", "telegram"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("attaching to a missing project returned %v, want ErrNotFound", err)
		}
	})

	t.Run("a thread always lands in the same session", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project, _ := s.CreateProject(ctx, "acme")

		first, err := s.FindOrCreateSession(ctx, project.GetId(), "thread-a")
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if first.GetStatus() != "idle" {
			t.Fatalf("new session status is %q, want idle", first.GetStatus())
		}

		again, err := s.FindOrCreateSession(ctx, project.GetId(), "thread-a")
		if err != nil {
			t.Fatalf("FindOrCreateSession again: %v", err)
		}
		if again.GetId() != first.GetId() {
			t.Fatalf("the same thread made two sessions: %q and %q", first.GetId(), again.GetId())
		}

		other, err := s.FindOrCreateSession(ctx, project.GetId(), "thread-b")
		if err != nil {
			t.Fatalf("FindOrCreateSession other thread: %v", err)
		}
		if other.GetId() == first.GetId() {
			t.Fatal("two threads share one session")
		}
	})

	t.Run("a session needs a live project", func(t *testing.T) {
		s := newDataset(t)(t)
		if _, err := s.FindOrCreateSession(context.Background(), "ghost", "thread-a"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("session on a missing project returned %v, want ErrNotFound", err)
		}
	})

	t.Run("a turn records the conversation handle", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project, _ := s.CreateProject(ctx, "acme")
		session, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-a")

		if err := s.RecordTurn(ctx, session.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordTurn: %v", err)
		}
		got, err := s.GetSession(ctx, session.GetId())
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.GetModelSessionId() != "conversation-1" {
			t.Fatalf("conversation handle is %q, want conversation-1", got.GetModelSessionId())
		}
	})

	t.Run("a failed turn does not erase the conversation handle", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project, _ := s.CreateProject(ctx, "acme")
		session, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-a")

		if err := s.RecordTurn(ctx, session.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordTurn: %v", err)
		}
		// A failed turn has no handle to report. The stored one points at a conversation that still
		// exists, so it must survive.
		if err := s.RecordTurn(ctx, session.GetId(), "", "failed"); err != nil {
			t.Fatalf("RecordTurn after failure: %v", err)
		}

		got, _ := s.GetSession(ctx, session.GetId())
		if got.GetModelSessionId() != "conversation-1" {
			t.Fatalf("a failed turn erased the handle: it is now %q", got.GetModelSessionId())
		}
		if got.GetStatus() != "failed" {
			t.Fatalf("status is %q, want failed", got.GetStatus())
		}
	})

	t.Run("a turn on a session that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		if err := s.RecordTurn(context.Background(), "ghost", "conversation-1", "idle"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("RecordTurn on a missing session returned %v, want ErrNotFound", err)
		}
	})

	t.Run("sessions list by project and in full", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		first, _ := s.CreateProject(ctx, "acme")
		second, _ := s.CreateProject(ctx, "other")

		if _, err := s.FindOrCreateSession(ctx, first.GetId(), "thread-a"); err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if _, err := s.FindOrCreateSession(ctx, first.GetId(), "thread-b"); err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if _, err := s.FindOrCreateSession(ctx, second.GetId(), "thread-c"); err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}

		mine, err := s.ListSessions(ctx, first.GetId())
		if err != nil {
			t.Fatalf("ListSessions by project: %v", err)
		}
		if len(mine) != 2 {
			t.Fatalf("project has %d sessions, want 2", len(mine))
		}
		for _, session := range mine {
			if session.GetProject() != first.GetId() {
				t.Fatalf("ListSessions by project returned a session from %q", session.GetProject())
			}
		}

		all, err := s.ListSessions(ctx, "")
		if err != nil {
			t.Fatalf("ListSessions all: %v", err)
		}
		if len(all) != 3 {
			t.Fatalf("ListSessions returned %d sessions, want 3", len(all))
		}
	})

	t.Run("a session can be stopped", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project, _ := s.CreateProject(ctx, "acme")
		session, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-a")

		if err := s.StopSession(ctx, session.GetId()); err != nil {
			t.Fatalf("StopSession: %v", err)
		}
		got, _ := s.GetSession(ctx, session.GetId())
		if got.GetStatus() != "stopped" {
			t.Fatalf("status is %q, want stopped", got.GetStatus())
		}
		if err := s.StopSession(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("stopping a missing session returned %v, want ErrNotFound", err)
		}
	})

	t.Run("a session that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		if _, err := s.GetSession(context.Background(), "ghost"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetSession on a missing session returned %v, want ErrNotFound", err)
		}
	})

	// The point of the whole package. Everything above could be satisfied by a map in the process.
	t.Run("everything survives reopening the store", func(t *testing.T) {
		open := newDataset(t)
		ctx := context.Background()

		before := open(t)
		project, err := before.CreateProject(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		session, err := before.FindOrCreateSession(ctx, project.GetId(), "thread-a")
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if err := before.RecordTurn(ctx, session.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordTurn: %v", err)
		}
		before.Close()

		after := open(t)

		reopened, err := after.GetProject(ctx, project.GetId())
		if err != nil {
			t.Fatalf("the project did not survive reopening: %v", err)
		}
		if reopened.GetName() != "acme" {
			t.Fatalf("reopened project is named %q, want acme", reopened.GetName())
		}

		sessions, err := after.ListSessions(ctx, project.GetId())
		if err != nil {
			t.Fatalf("ListSessions after reopening: %v", err)
		}
		if len(sessions) != 1 {
			t.Fatalf("the project has %d sessions after reopening, want 1", len(sessions))
		}
		if sessions[0].GetModelSessionId() != "conversation-1" {
			t.Fatalf("the conversation handle did not survive: it is %q", sessions[0].GetModelSessionId())
		}

		// The same thread must still resolve to the same session, which is what lets the next turn
		// resume the conversation rather than start a new one.
		same, err := after.FindOrCreateSession(ctx, project.GetId(), "thread-a")
		if err != nil {
			t.Fatalf("FindOrCreateSession after reopening: %v", err)
		}
		if same.GetId() != session.GetId() {
			t.Fatalf("the thread made a new session after reopening: %q, want %q", same.GetId(), session.GetId())
		}
		if same.GetModelSessionId() != "conversation-1" {
			t.Fatalf("the resumed session lost its conversation handle: %q", same.GetModelSessionId())
		}
	})
}
