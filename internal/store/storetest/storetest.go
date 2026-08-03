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

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/store"
)

// Opener hands out handles to one isolated dataset. Calling it twice returns two independent handles
// to the same underlying data, which is how the durability check reopens the store.
type Opener func(t *testing.T) store.Store

// RunConformance runs the whole contract against an implementation. newDataset must return an Opener
// over data no other subtest can see.
func RunConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a workspace can be created, fetched and listed", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		created, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		if created.GetId() == "" {
			t.Fatal("created workspace has no id")
		}
		if created.GetName() != "acme" {
			t.Fatalf("name is %q, want acme", created.GetName())
		}
		if created.GetCreatedAt() == nil {
			t.Fatal("created workspace has no created_at")
		}

		fetched, err := s.GetWorkspace(ctx, created.GetId())
		if err != nil {
			t.Fatalf("GetWorkspace: %v", err)
		}
		if fetched.GetName() != "acme" {
			t.Fatalf("fetched name is %q, want acme", fetched.GetName())
		}

		list, err := s.ListWorkspaces(ctx)
		if err != nil {
			t.Fatalf("ListWorkspaces: %v", err)
		}
		if len(list) != 1 || list[0].GetId() != created.GetId() {
			t.Fatalf("ListWorkspaces returned %d workspaces, want the one created", len(list))
		}
	})

	t.Run("a workspace that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		if _, err := s.GetWorkspace(context.Background(), "ghost"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetWorkspace on a missing workspace returned %v, want ErrNotFound", err)
		}
	})

	t.Run("a deleted workspace is hidden from every read", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		created, _ := s.CreateWorkspace(ctx, "acme")
		if err := s.DeleteWorkspace(ctx, created.GetId()); err != nil {
			t.Fatalf("DeleteWorkspace: %v", err)
		}

		if _, err := s.GetWorkspace(ctx, created.GetId()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetWorkspace after delete returned %v, want ErrNotFound", err)
		}
		list, err := s.ListWorkspaces(ctx)
		if err != nil {
			t.Fatalf("ListWorkspaces: %v", err)
		}
		if len(list) != 0 {
			t.Fatalf("ListWorkspaces returned %d workspaces after delete, want 0", len(list))
		}
		if err := s.DeleteWorkspace(ctx, created.GetId()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("deleting twice returned %v, want ErrNotFound", err)
		}
	})

	t.Run("a channel attaches to a live workspace only", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		created, _ := s.CreateWorkspace(ctx, "acme")
		channel, err := s.AttachChannel(ctx, created.GetId(), "family-chat", "telegram")
		if err != nil {
			t.Fatalf("AttachChannel: %v", err)
		}
		if channel.GetKind() != "telegram" || channel.GetId() != "family-chat" {
			t.Fatalf("attached channel is %+v", channel)
		}

		if _, err := s.AttachChannel(ctx, "ghost", "family-chat", "telegram"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("attaching to a missing workspace returned %v, want ErrNotFound", err)
		}
	})

	t.Run("a thread always lands in the same session", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")

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
		project := newProject(t, s, "acme", "house bills")
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
		project := newProject(t, s, "acme", "house bills")
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

	t.Run("sessions list by workspace and in full", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		first := newProject(t, s, "acme", "house bills")
		second := newProject(t, s, "other", "gardening")

		if _, err := s.FindOrCreateSession(ctx, first.GetId(), "thread-a"); err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if _, err := s.FindOrCreateSession(ctx, first.GetId(), "thread-b"); err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if _, err := s.FindOrCreateSession(ctx, second.GetId(), "thread-c"); err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}

		mine, err := s.ListSessions(ctx, store.SessionFilter{Project: first.GetId()})
		if err != nil {
			t.Fatalf("ListSessions by project: %v", err)
		}
		if len(mine) != 2 {
			t.Fatalf("workspace has %d sessions, want 2", len(mine))
		}
		for _, session := range mine {
			if session.GetProject() != first.GetId() {
				t.Fatalf("ListSessions by project returned a session from %q", session.GetProject())
			}
		}

		all, err := s.ListSessions(ctx, store.SessionFilter{})
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
		project := newProject(t, s, "acme", "house bills")
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

	// The conversation handle is what makes this worth having: it is the only pointer to a
	// conversation the model keeps on its own disk, and a restart that lost it would leave the
	// conversation stranded and unreachable.
	t.Run("a stopped session restarts to idle and keeps its conversation", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-a")
		if err := s.RecordTurn(ctx, session.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordTurn: %v", err)
		}
		if err := s.StopSession(ctx, session.GetId()); err != nil {
			t.Fatalf("StopSession: %v", err)
		}

		if err := s.RestartSession(ctx, session.GetId()); err != nil {
			t.Fatalf("RestartSession: %v", err)
		}
		got, _ := s.GetSession(ctx, session.GetId())
		if got.GetStatus() != "idle" {
			t.Fatalf("status is %q, want idle", got.GetStatus())
		}
		if got.GetModelSessionId() != "conversation-1" {
			t.Fatalf("the conversation handle is %q, want it untouched", got.GetModelSessionId())
		}
		if err := s.RestartSession(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("restarting a missing session returned %v, want ErrNotFound", err)
		}
	})

	// Archiving is a stamp, not a delete, which is the only reason restoring can exist at all. The
	// conversation handle is the thing worth checking: it is the only pointer to a conversation the
	// model keeps on its own disk.
	t.Run("an archived session leaves the default listing and comes back whole", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		kept, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-a")
		other, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-b")
		if err := s.RecordTurn(ctx, kept.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordTurn: %v", err)
		}

		if err := s.ArchiveSession(ctx, kept.GetId()); err != nil {
			t.Fatalf("ArchiveSession: %v", err)
		}

		live, _ := s.ListSessions(ctx, store.SessionFilter{Project: project.GetId()})
		if len(live) != 1 || live[0].GetId() != other.GetId() {
			t.Fatalf("the default listing is %v, want only the live thread", ids(live))
		}
		archived, _ := s.ListSessions(ctx, store.SessionFilter{Project: project.GetId(), Archived: true})
		if len(archived) != 1 || archived[0].GetId() != kept.GetId() {
			t.Fatalf("the archived listing is %v, want only the archived thread", ids(archived))
		}
		if archived[0].GetArchivedAt() == nil {
			t.Fatal("the archived session carries no archived_at, so nothing can say when it was put away")
		}
		// Still readable by id: it was hidden from a listing, not deleted.
		if _, err := s.GetSession(ctx, kept.GetId()); err != nil {
			t.Fatalf("an archived session cannot be fetched: %v", err)
		}

		if err := s.RestoreSession(ctx, kept.GetId()); err != nil {
			t.Fatalf("RestoreSession: %v", err)
		}
		back, _ := s.GetSession(ctx, kept.GetId())
		if back.GetArchivedAt() != nil {
			t.Fatal("the restored session is still stamped as archived")
		}
		if back.GetModelSessionId() != "conversation-1" {
			t.Fatalf("the conversation handle is %q, want it untouched", back.GetModelSessionId())
		}
		if live, _ := s.ListSessions(ctx, store.SessionFilter{Project: project.GetId()}); len(live) != 2 {
			t.Fatalf("the default listing is %v, want both threads back", ids(live))
		}

		for _, err := range []error{s.ArchiveSession(ctx, "ghost"), s.RestoreSession(ctx, "ghost")} {
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("acting on a missing session returned %v, want ErrNotFound", err)
			}
		}
	})

	t.Run("a session that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		if _, err := s.GetSession(context.Background(), "ghost"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetSession on a missing session returned %v, want ErrNotFound", err)
		}
	})

	t.Run("a project belongs to a workspace and is found within it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		workspace, _ := s.CreateWorkspace(ctx, "me")
		project, err := s.CreateProject(ctx, workspace.GetId(), "house bills")
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		if project.GetWorkspace() != workspace.GetId() {
			t.Fatalf("the project belongs to %q, want %q", project.GetWorkspace(), workspace.GetId())
		}

		fetched, err := s.GetProject(ctx, project.GetId())
		if err != nil {
			t.Fatalf("GetProject: %v", err)
		}
		if fetched.GetName() != "house bills" {
			t.Fatalf("fetched project is named %q", fetched.GetName())
		}

		within, err := s.ListProjects(ctx, workspace.GetId())
		if err != nil {
			t.Fatalf("ListProjects: %v", err)
		}
		if len(within) != 1 {
			t.Fatalf("the workspace has %d projects, want 1", len(within))
		}
	})

	t.Run("a project needs a live workspace", func(t *testing.T) {
		s := newDataset(t)(t)
		if _, err := s.CreateProject(context.Background(), "ghost", "house bills"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("a project on a missing workspace returned %v, want ErrNotFound", err)
		}
	})

	t.Run("one workspace's projects are not another's", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		mine := newProject(t, s, "me", "house bills")
		newProject(t, s, "someone else", "their bills")

		within, err := s.ListProjects(ctx, mine.GetWorkspace())
		if err != nil {
			t.Fatalf("ListProjects: %v", err)
		}
		if len(within) != 1 || within[0].GetId() != mine.GetId() {
			t.Fatalf("listing one workspace returned %d projects", len(within))
		}

		all, err := s.ListProjects(ctx, "")
		if err != nil {
			t.Fatalf("ListProjects all: %v", err)
		}
		if len(all) != 2 {
			t.Fatalf("listing every project returned %d, want 2", len(all))
		}
	})

	t.Run("a deleted project is hidden and takes no new threads", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "me", "house bills")

		if err := s.DeleteProject(ctx, project.GetId()); err != nil {
			t.Fatalf("DeleteProject: %v", err)
		}
		if _, err := s.GetProject(ctx, project.GetId()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetProject after delete returned %v, want ErrNotFound", err)
		}
		if _, err := s.FindOrCreateSession(ctx, project.GetId(), "thread-a"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("a deleted project still took a thread: %v", err)
		}
		if err := s.DeleteProject(ctx, project.GetId()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("deleting twice returned %v, want ErrNotFound", err)
		}
	})

	// A project cannot outlive the workspace it belongs to, or deleting a workspace would leave its
	// work reachable and dispatchable.
	t.Run("deleting a workspace hides its projects", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "me", "house bills")

		if err := s.DeleteWorkspace(ctx, project.GetWorkspace()); err != nil {
			t.Fatalf("DeleteWorkspace: %v", err)
		}
		if _, err := s.GetProject(ctx, project.GetId()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("the project outlived its workspace: %v", err)
		}
		listed, err := s.ListProjects(ctx, "")
		if err != nil {
			t.Fatalf("ListProjects: %v", err)
		}
		if len(listed) != 0 {
			t.Fatalf("%d projects survived their workspace", len(listed))
		}
	})

	// A thread identifier only has to be unique inside its project, which is what lets two bodies of
	// work in one workspace both have a thread the channel calls "general".
	t.Run("two projects in one workspace can share a thread identifier", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		workspace, _ := s.CreateWorkspace(ctx, "me")
		bills, err := s.CreateProject(ctx, workspace.GetId(), "house bills")
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		garden, err := s.CreateProject(ctx, workspace.GetId(), "gardening")
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}

		first, err := s.FindOrCreateSession(ctx, bills.GetId(), "general")
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		second, err := s.FindOrCreateSession(ctx, garden.GetId(), "general")
		if err != nil {
			t.Fatalf("FindOrCreateSession in the second project: %v", err)
		}
		if first.GetId() == second.GetId() {
			t.Fatal("the same thread identifier in two projects landed in one session")
		}
	})

	// The point of the whole package. Everything above could be satisfied by a map in the process.
	t.Run("everything survives reopening the store", func(t *testing.T) {
		open := newDataset(t)
		ctx := context.Background()

		before := open(t)
		workspace, err := before.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		project, err := before.CreateProject(ctx, workspace.GetId(), "house bills")
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

		reopened, err := after.GetWorkspace(ctx, workspace.GetId())
		if err != nil {
			t.Fatalf("the workspace did not survive reopening: %v", err)
		}
		if reopened.GetName() != "acme" {
			t.Fatalf("reopened workspace is named %q, want acme", reopened.GetName())
		}

		reopenedProject, err := after.GetProject(ctx, project.GetId())
		if err != nil {
			t.Fatalf("the project did not survive reopening: %v", err)
		}
		if reopenedProject.GetName() != "house bills" {
			t.Fatalf("reopened project is named %q, want house bills", reopenedProject.GetName())
		}

		sessions, err := after.ListSessions(ctx, store.SessionFilter{Project: project.GetId()})
		if err != nil {
			t.Fatalf("ListSessions after reopening: %v", err)
		}
		if len(sessions) != 1 {
			t.Fatalf("the workspace has %d sessions after reopening, want 1", len(sessions))
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

// newProject creates a workspace and a project inside it, which is the smallest setup a session
// needs now that threads live inside a project.
func newProject(t *testing.T, s store.Store, workspaceName, projectName string) *quaycrewv1.Project {
	t.Helper()
	ctx := context.Background()
	workspace, err := s.CreateWorkspace(ctx, workspaceName)
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	project, err := s.CreateProject(ctx, workspace.GetId(), projectName)
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return project
}

// ids names the sessions in a listing, so a failure says which threads came back rather than how
// many.
func ids(sessions []*quaycrewv1.Session) []string {
	out := make([]string, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, session.GetId())
	}
	return out
}
