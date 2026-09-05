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
	"fmt"
	"slices"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/deploy"
	"github.com/atlantic-blue/quay-krewe/internal/model"
	"github.com/atlantic-blue/quay-krewe/internal/skill"
	"github.com/atlantic-blue/quay-krewe/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
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

	t.Run("a probe writes, and says so again on the same row", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		for attempt := 1; attempt <= 3; attempt++ {
			if err := s.Probe(ctx); err != nil {
				t.Fatalf("Probe %d: %v", attempt, err)
			}
		}
	})

	t.Run("a probe under a context that is already over is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		over, stop := context.WithCancel(context.Background())
		stop()
		// The health check gives its probe a budget, so a store that cannot answer inside one comes
		// back rather than taking the caller with it.
		if err := s.Probe(over); err == nil {
			t.Fatal("a probe under a dead context answered, so a budget on it would mean nothing")
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

	// Both implementations, because the memory store writes the mode into a struct and postgres writes
	// it into a column with a default of its own. A fake that took the system's choice while the real one
	// quietly kept the column default would keep the suite green and run every real exec in the wrong
	// mode.
	t.Run("a session is born in the mode the system configured", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")

		born, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-planning", store.Birth{Mode: model.PermissionPlan})
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if born.GetPermissionMode() != model.PermissionPlan {
			t.Fatalf("a session born in a system configured for plan may do %q", born.GetPermissionMode())
		}

		// What a session may do is its own once it exists. A system whose configuration changed must not
		// widen a conversation that is already running.
		again, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-planning", store.Birth{Mode: model.PermissionBypass})
		if err != nil {
			t.Fatalf("FindOrCreateSession again: %v", err)
		}
		if again.GetPermissionMode() != model.PermissionPlan {
			t.Fatalf("a session that already existed was widened to %q by configuration", again.GetPermissionMode())
		}
	})

	// Both implementations, because the title is written in one place in each: a struct field in the
	// memory store and a column in postgres. A memory store that carried it while postgres dropped it
	// would keep every unit test green and leave the real listing blank, which is the defect this was
	// written for.
	t.Run("a session is born with the name it was dispatched with", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")

		born, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-billing",
			store.Birth{Title: "read the electricity bill"})
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if born.GetTitle() != "read the electricity bill" {
			t.Fatalf("a session dispatched with a title is called %q", born.GetTitle())
		}
		read, err := s.GetSession(ctx, born.GetId())
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if read.GetTitle() != "read the electricity bill" {
			t.Fatalf("the title did not survive being written: %q", read.GetTitle())
		}

		// What a session is called is its own once it exists. A dispatch that has to be made again
		// lands in the same conversation, and must not rename it under whoever is reading it.
		again, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-billing",
			store.Birth{Title: "something else entirely"})
		if err != nil {
			t.Fatalf("FindOrCreateSession again: %v", err)
		}
		if again.GetTitle() != "read the electricity bill" {
			t.Fatalf("a session that already existed was renamed to %q by a later dispatch", again.GetTitle())
		}
	})

	t.Run("a session nobody named has no title, rather than one nobody can explain", func(t *testing.T) {
		s := newDataset(t)(t)
		project := newProject(t, s, "acme", "house bills")

		born, _, err := s.FindOrCreateSession(context.Background(), project.GetId(), "session-quiet", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if born.GetTitle() != "" {
			t.Fatalf("a session nobody dispatched with a title is called %q", born.GetTitle())
		}
	})

	t.Run("a system that configured nothing gets the mode every session used to have", func(t *testing.T) {
		s := newDataset(t)(t)
		project := newProject(t, s, "acme", "house bills")

		born, _, err := s.FindOrCreateSession(context.Background(), project.GetId(), "session-quiet", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if born.GetPermissionMode() != model.PermissionAcceptEdits {
			t.Fatalf("a session in a system that configured nothing may do %q, want %q",
				born.GetPermissionMode(), model.PermissionAcceptEdits)
		}
	})

	// Both implementations, because a listing that shows a label the operator set on one store and an
	// identifier on the other is worse than no labels at all.
	t.Run("a session carries the name the operator gave it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if session.GetLabel() != "" {
			t.Fatalf("a new session is already called %q, and nobody has named it", session.GetLabel())
		}

		if err := s.SetLabel(ctx, session.GetId(), "the electricity bill"); err != nil {
			t.Fatalf("SetLabel: %v", err)
		}
		named, err := s.GetSession(ctx, session.GetId())
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if named.GetLabel() != "the electricity bill" {
			t.Fatalf("the session is called %q", named.GetLabel())
		}

		// It is in the listing too, which is the only place anybody reads it.
		listed, err := s.ListSessions(ctx, store.SessionFilter{Project: project.GetId()})
		if err != nil || len(listed) != 1 {
			t.Fatalf("ListSessions: %d sessions, %v", len(listed), err)
		}
		if listed[0].GetLabel() != "the electricity bill" {
			t.Fatalf("the listing calls it %q", listed[0].GetLabel())
		}

		// Clearing it puts the identifier back, which is the only way back and so has to work.
		if err := s.SetLabel(ctx, session.GetId(), ""); err != nil {
			t.Fatalf("clearing the label: %v", err)
		}
		cleared, err := s.GetSession(ctx, session.GetId())
		if err != nil {
			t.Fatalf("GetSession after clearing: %v", err)
		}
		if cleared.GetLabel() != "" {
			t.Fatalf("the label survived being cleared: %q", cleared.GetLabel())
		}
	})

	t.Run("a session keeps what the system observed it to be, and when", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}

		if err := s.SetDescription(ctx, session.GetId(), "the electricity bill", 3); err != nil {
			t.Fatalf("SetDescription: %v", err)
		}

		described, err := s.GetSession(ctx, session.GetId())
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if described.GetDescription() != "the electricity bill" {
			t.Fatalf("the system describes it as %q", described.GetDescription())
		}
		// The exec count travels with the text. Kept apart they drift, and a description that says it
		// is current when it is not is worse than one that admits it is old.
		if described.GetDescribedAtExec() != 3 {
			t.Fatalf("it was described at exec %d, want 3", described.GetDescribedAtExec())
		}
	})

	t.Run("how many execs a session has had", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		other, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-b", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession other: %v", err)
		}

		count, err := s.CountExecs(ctx, session.GetId())
		if err != nil || count != 0 {
			t.Fatalf("a session nobody has spoken in has %d execs (%v)", count, err)
		}

		for at := range 3 {
			exec := &quaycrewv1.Exec{
				Id: fmt.Sprintf("counted-exec-%d", at), Session: session.GetId(),
				Prompt: "hello", Reply: "ok", OccurredAt: timestamppb.Now(),
			}
			if err := s.AppendExec(ctx, exec, project.GetWorkspace(), project.GetId(), "session-a"); err != nil {
				t.Fatalf("AppendExec: %v", err)
			}
		}

		count, err = s.CountExecs(ctx, session.GetId())
		if err != nil || count != 3 {
			t.Fatalf("the session has %d execs (%v), want 3", count, err)
		}
		// Counted per session, not per system: a busy neighbour must not make this one look described.
		count, err = s.CountExecs(ctx, other.GetId())
		if err != nil || count != 0 {
			t.Fatalf("the other session has %d execs (%v), want 0", count, err)
		}
	})

	t.Run("naming a session that does not exist is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		if err := s.SetLabel(context.Background(), "ghost", "anything"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("SetLabel on a missing session returned %v, want ErrNotFound", err)
		}
	})

	t.Run("a session always lands in the same session", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")

		first, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if first.GetStatus() != "idle" {
			t.Fatalf("new session status is %q, want idle", first.GetStatus())
		}

		again, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession again: %v", err)
		}
		if again.GetId() != first.GetId() {
			t.Fatalf("the same session made two sessions: %q and %q", first.GetId(), again.GetId())
		}

		other, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-b", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession other session: %v", err)
		}
		if other.GetId() == first.GetId() {
			t.Fatal("two sessions share one session")
		}
	})

	t.Run("a session needs a live project", func(t *testing.T) {
		s := newDataset(t)(t)
		if _, _, err := s.FindOrCreateSession(context.Background(), "ghost", "session-a", store.Birth{}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("session on a missing project returned %v, want ErrNotFound", err)
		}
	})

	t.Run("an exec records the conversation handle", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		if err := s.RecordExec(ctx, session.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordExec: %v", err)
		}
		got, err := s.GetSession(ctx, session.GetId())
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.GetModelSessionId() != "conversation-1" {
			t.Fatalf("conversation handle is %q, want conversation-1", got.GetModelSessionId())
		}
	})

	t.Run("a failed exec does not erase the conversation handle", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		if err := s.RecordExec(ctx, session.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordExec: %v", err)
		}
		// A failed exec has no handle to report. The stored one points at a conversation that still
		// exists, so it must survive.
		if err := s.RecordExec(ctx, session.GetId(), "", "failed"); err != nil {
			t.Fatalf("RecordExec after failure: %v", err)
		}

		got, _ := s.GetSession(ctx, session.GetId())
		if got.GetModelSessionId() != "conversation-1" {
			t.Fatalf("a failed exec erased the handle: it is now %q", got.GetModelSessionId())
		}
		if got.GetStatus() != "failed" {
			t.Fatalf("status is %q, want failed", got.GetStatus())
		}
	})

	t.Run("an exec on a session that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		if err := s.RecordExec(context.Background(), "ghost", "conversation-1", "idle"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("RecordExec on a missing session returned %v, want ErrNotFound", err)
		}
	})

	t.Run("sessions list by workspace and in full", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		first := newProject(t, s, "acme", "house bills")
		second := newProject(t, s, "other", "gardening")

		if _, _, err := s.FindOrCreateSession(ctx, first.GetId(), "session-a", store.Birth{}); err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if _, _, err := s.FindOrCreateSession(ctx, first.GetId(), "session-b", store.Birth{}); err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if _, _, err := s.FindOrCreateSession(ctx, second.GetId(), "session-c", store.Birth{}); err != nil {
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
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

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

	t.Run("what a sandbox was born holding is recorded, and closing the sandbox clears it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		if fingerprint, err := s.SessionSkills(ctx, session.GetId()); err != nil || fingerprint != "" {
			t.Fatalf("a fresh session answers %q, %v; want empty and no error", fingerprint, err)
		}
		if err := s.SetSessionSkills(ctx, session.GetId(), "set-a"); err != nil {
			t.Fatalf("SetSessionSkills: %v", err)
		}
		if fingerprint, _ := s.SessionSkills(ctx, session.GetId()); fingerprint != "set-a" {
			t.Fatalf("read back %q, want set-a", fingerprint)
		}

		if err := s.StopSession(ctx, session.GetId()); err != nil {
			t.Fatalf("StopSession: %v", err)
		}
		if fingerprint, _ := s.SessionSkills(ctx, session.GetId()); fingerprint != "" {
			t.Fatalf("a stopped session still answers %q: its sandbox is gone, so the next one is born current", fingerprint)
		}

		if err := s.SetSessionSkills(ctx, session.GetId(), "set-b"); err != nil {
			t.Fatalf("SetSessionSkills after stop: %v", err)
		}
		if err := s.ArchiveSession(ctx, session.GetId()); err != nil {
			t.Fatalf("ArchiveSession: %v", err)
		}
		if fingerprint, _ := s.SessionSkills(ctx, session.GetId()); fingerprint != "" {
			t.Fatalf("an archived session still answers %q", fingerprint)
		}

		if err := s.SetSessionSkills(ctx, "ghost", "set-c"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("setting on a missing session returned %v, want ErrNotFound", err)
		}
		if _, err := s.SessionSkills(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("reading a missing session returned %v, want ErrNotFound", err)
		}
	})

	// The conversation handle is what makes this worth having: it is the only pointer to a
	// conversation the model keeps on its own disk, and a restart that lost it would leave the
	// conversation stranded and unreachable.
	t.Run("a stopped session restarts to idle and keeps its conversation", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		if err := s.RecordExec(ctx, session.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordExec: %v", err)
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
		kept, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		other, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-b", store.Birth{})
		if err := s.RecordExec(ctx, kept.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordExec: %v", err)
		}

		if err := s.ArchiveSession(ctx, kept.GetId()); err != nil {
			t.Fatalf("ArchiveSession: %v", err)
		}

		live, _ := s.ListSessions(ctx, store.SessionFilter{Project: project.GetId()})
		if len(live) != 1 || live[0].GetId() != other.GetId() {
			t.Fatalf("the default listing is %v, want only the live session", ids(live))
		}
		archived, _ := s.ListSessions(ctx, store.SessionFilter{Project: project.GetId(), Archived: true})
		if len(archived) != 1 || archived[0].GetId() != kept.GetId() {
			t.Fatalf("the archived listing is %v, want only the archived session", ids(archived))
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
			t.Fatalf("the default listing is %v, want both sessions back", ids(live))
		}

		for _, err := range []error{s.ArchiveSession(ctx, "ghost"), s.RestoreSession(ctx, "ghost")} {
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("acting on a missing session returned %v, want ErrNotFound", err)
			}
		}
	})

	// The listing's last column says how long ago a session moved, so the listing is ordered on that
	// same stamp. It used to be ordered on the created stamp instead, and a real listing of forty five
	// sessions then ran 1d, 1d, 1d, 7d, 7d, 7d, 1d, 7d down the age column.
	//
	// The gaps here are real waits rather than stamps written by hand, because the store writes its own
	// and there is no way to hand it one. Without them two sessions made in the same microsecond would
	// tie, and a tie is decided by the identifier, so the case would pass on whichever identifier the
	// store happened to mint first and prove nothing at all.
	t.Run("the listing is ordered by when a session last moved, not by when it was made", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")

		// made first, and then worked in last: the session the operator wants at the top.
		early, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-early", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		time.Sleep(orderingGap)
		late, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-late", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		time.Sleep(orderingGap)
		if err := s.RecordExec(ctx, early.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordExec: %v", err)
		}

		listed, err := s.ListSessions(ctx, store.SessionFilter{Project: project.GetId()})
		if err != nil {
			t.Fatalf("ListSessions: %v", err)
		}
		if len(listed) != 2 {
			t.Fatalf("the listing is %v, want both sessions", ids(listed))
		}
		// The two stamps disagree, which is the whole point of the case. Asserted rather than assumed,
		// because a sleep that did not separate them would leave this passing on the tiebreaker.
		if !listed[0].GetCreatedAt().AsTime().Before(listed[1].GetCreatedAt().AsTime()) {
			t.Fatalf("the sessions were made in the same moment, so this case cannot tell the two clocks apart")
		}
		if !listed[1].GetUpdatedAt().AsTime().Before(listed[0].GetUpdatedAt().AsTime()) {
			t.Fatalf("the sessions were touched in the same moment, so this case cannot tell the two clocks apart")
		}
		if got := ids(listed); got[0] != early.GetId() {
			t.Fatalf("the listing is %v, want the session touched last first: %s", got, early.GetId())
		}
		if got := ids(listed); got[1] != late.GetId() {
			t.Fatalf("the listing is %v, want the session made last second: %s", got, late.GetId())
		}
	})

	// An archived session is measured from when it was put away, which is a different stamp from the
	// one a live session is measured by. Writing to an archived row afterwards moves the touched stamp
	// and must not move the row: ordered on the touched stamp alone, this listing comes back reversed.
	t.Run("archived sessions are ordered by when they were put away", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")

		first, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-first", store.Birth{})
		second, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-second", store.Birth{})

		if err := s.ArchiveSession(ctx, first.GetId()); err != nil {
			t.Fatalf("ArchiveSession: %v", err)
		}
		time.Sleep(orderingGap)
		if err := s.ArchiveSession(ctx, second.GetId()); err != nil {
			t.Fatalf("ArchiveSession: %v", err)
		}
		time.Sleep(orderingGap)
		// Naming the one put away first is what pulls its touched stamp past the other's, so the two
		// clocks now say opposite things about this listing.
		if err := s.SetLabel(ctx, first.GetId(), "the bills"); err != nil {
			t.Fatalf("SetLabel: %v", err)
		}

		listed, err := s.ListSessions(ctx, store.SessionFilter{Project: project.GetId(), Archived: true})
		if err != nil {
			t.Fatalf("ListSessions archived: %v", err)
		}
		if len(listed) != 2 {
			t.Fatalf("the archived listing is %v, want both sessions", ids(listed))
		}
		if !listed[1].GetUpdatedAt().AsTime().After(listed[0].GetUpdatedAt().AsTime()) {
			t.Fatalf("naming the session did not move its touched stamp past the other's, so this case proves nothing")
		}
		if got := ids(listed); got[0] != second.GetId() {
			t.Fatalf("the archived listing is %v, want the one put away last first: %s", got, second.GetId())
		}
	})

	// The mode belongs to the session, so it has to survive everything the session survives. A session
	// started to plan something that quietly went back to editing files on the next exec would be
	// worse than never having the setting.
	t.Run("a session keeps the permission mode it was given", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		if got := session.GetPermissionMode(); got != "acceptEdits" {
			t.Fatalf("a new session runs as %q, want acceptEdits, which is what every exec has run as", got)
		}
		if err := s.SetPermissionMode(ctx, session.GetId(), "bypassPermissions"); err != nil {
			t.Fatalf("SetPermissionMode: %v", err)
		}

		got, _ := s.GetSession(ctx, session.GetId())
		if got.GetPermissionMode() != "bypassPermissions" {
			t.Fatalf("the session runs as %q, want bypassPermissions", got.GetPermissionMode())
		}
		// And the session it was set on, not every session in the project.
		other, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-b", store.Birth{})
		if other.GetPermissionMode() != "acceptEdits" {
			t.Fatalf("another session runs as %q, want it untouched", other.GetPermissionMode())
		}
		if err := s.SetPermissionMode(ctx, "ghost", "plan"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("setting the mode on a missing session returned %v, want ErrNotFound", err)
		}
	})

	t.Run("a session's execs come back in the order they happened", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		start := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
		for i, text := range []string{"first", "second", "third"} {
			exec := &quaycrewv1.Exec{
				Id:         fmt.Sprintf("exec-%d", i),
				Session:    session.GetId(),
				Prompt:     text,
				Reply:      "you said: " + text,
				Status:     "idle",
				OccurredAt: timestamppb.New(start.Add(time.Duration(i) * time.Minute)),
			}
			if err := s.AppendExec(ctx, exec, project.GetWorkspace(), project.GetId(), "session-a"); err != nil {
				t.Fatalf("AppendExec: %v", err)
			}
		}

		execs, err := s.ListExecs(ctx, session.GetId(), 0)
		if err != nil {
			t.Fatalf("ListExecs: %v", err)
		}
		if len(execs) != 3 {
			t.Fatalf("%d execs came back, want 3", len(execs))
		}
		for i, want := range []string{"first", "second", "third"} {
			if execs[i].GetPrompt() != want {
				t.Fatalf("exec %d says %q, want %q: the history is out of order", i, execs[i].GetPrompt(), want)
			}
		}
		if execs[0].GetReply() != "you said: first" || execs[0].GetStatus() != "idle" {
			t.Fatalf("the first exec came back as %+v, losing what it said", execs[0])
		}
	})

	t.Run("an exec written when it started is closed by what came of it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		running := &quaycrewv1.Exec{
			Id: "exec-open", Session: session.GetId(), Prompt: "read the repository",
			Status: "running", OccurredAt: timestamppb.New(time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)),
		}
		if err := s.AppendExec(ctx, running, project.GetWorkspace(), project.GetId(), "session-a"); err != nil {
			t.Fatalf("AppendExec: %v", err)
		}
		if err := s.FinishExec(ctx, "exec-open", "idle", "it is a control plane", ""); err != nil {
			t.Fatalf("FinishExec: %v", err)
		}

		execs, err := s.ListExecs(ctx, session.GetId(), 0)
		if err != nil {
			t.Fatalf("ListExecs: %v", err)
		}
		// One exec, not two: the same record, closed.
		if len(execs) != 1 {
			t.Fatalf("%d execs came back, want 1", len(execs))
		}
		if execs[0].GetStatus() != "idle" || execs[0].GetReply() != "it is a control plane" {
			t.Fatalf("the exec came back as %+v, so finishing it did not land", execs[0])
		}
		// What the operator was asked is still there. Closing an exec must not lose it.
		if execs[0].GetPrompt() != "read the repository" {
			t.Fatalf("the exec says %q was asked, want %q", execs[0].GetPrompt(), "read the repository")
		}
	})

	t.Run("finishing an exec the store does not hold is not an error", func(t *testing.T) {
		s := newDataset(t)(t)

		// The exec happened whatever the store holds, so this must not come back as a failure of it.
		if err := s.FinishExec(context.Background(), "exec-nobody-wrote", "idle", "done", ""); err != nil {
			t.Fatalf("FinishExec on an exec nobody wrote: %v", err)
		}
	})

	t.Run("the same exec delivered twice is stored once", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		// Delivery from the event log is at least once, so this is not a hypothetical.
		exec := &quaycrewv1.Exec{
			Id: "exec-once", Session: session.GetId(), Prompt: "hello",
			Status: "idle", OccurredAt: timestamppb.New(time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)),
		}
		for range 3 {
			if err := s.AppendExec(ctx, exec, project.GetWorkspace(), project.GetId(), "session-a"); err != nil {
				t.Fatalf("AppendExec: %v", err)
			}
		}

		execs, err := s.ListExecs(ctx, session.GetId(), 0)
		if err != nil {
			t.Fatalf("ListExecs: %v", err)
		}
		if len(execs) != 1 {
			t.Fatalf("%d execs came back, want 1: a replayed record was written again", len(execs))
		}
	})

	t.Run("an exec with no id is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		err := s.AppendExec(ctx, &quaycrewv1.Exec{Session: session.GetId(), Prompt: "hello"},
			project.GetWorkspace(), project.GetId(), "session-a")
		if err == nil {
			t.Fatal("an exec with no id was accepted, so nothing can recognise it on a replay")
		}
	})

	t.Run("a listing keeps the end of a long conversation", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		start := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
		for i := range 5 {
			exec := &quaycrewv1.Exec{
				Id: fmt.Sprintf("exec-%d", i), Session: session.GetId(),
				Prompt: fmt.Sprintf("message %d", i), Status: "idle",
				OccurredAt: timestamppb.New(start.Add(time.Duration(i) * time.Minute)),
			}
			if err := s.AppendExec(ctx, exec, project.GetWorkspace(), project.GetId(), "session-a"); err != nil {
				t.Fatalf("AppendExec: %v", err)
			}
		}

		execs, err := s.ListExecs(ctx, session.GetId(), 2)
		if err != nil {
			t.Fatalf("ListExecs: %v", err)
		}
		if len(execs) != 2 {
			t.Fatalf("%d execs came back, want 2", len(execs))
		}
		if execs[0].GetPrompt() != "message 3" || execs[1].GetPrompt() != "message 4" {
			t.Fatalf("the listing kept %q and %q, want the last two: a cap must keep the end", execs[0].GetPrompt(), execs[1].GetPrompt())
		}
	})

	t.Run("one session's execs are not another's", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		first, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		second, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-b", store.Birth{})

		now := timestamppb.New(time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC))
		if err := s.AppendExec(ctx, &quaycrewv1.Exec{Id: "a", Session: first.GetId(), Prompt: "mine", OccurredAt: now},
			project.GetWorkspace(), project.GetId(), "session-a"); err != nil {
			t.Fatalf("AppendExec: %v", err)
		}
		if err := s.AppendExec(ctx, &quaycrewv1.Exec{Id: "b", Session: second.GetId(), Prompt: "theirs", OccurredAt: now},
			project.GetWorkspace(), project.GetId(), "session-b"); err != nil {
			t.Fatalf("AppendExec: %v", err)
		}

		execs, err := s.ListExecs(ctx, first.GetId(), 0)
		if err != nil {
			t.Fatalf("ListExecs: %v", err)
		}
		if len(execs) != 1 || execs[0].GetPrompt() != "mine" {
			t.Fatalf("the first session's history came back as %d execs starting %q", len(execs), execs[0].GetPrompt())
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

	// Two stores, one rule. The driver was made in each of them separately, and only one of the two
	// said which mode it starts in, so a system on Postgres and a system on memory disagreed about
	// whether the session driving them could act at all.
	t.Run("the driver is created able to act, and is the same one every time", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "me", "house bills")

		driver, err := s.FindOrCreateDriver(ctx, project.GetId())
		if err != nil {
			t.Fatalf("FindOrCreateDriver: %v", err)
		}
		if !driver.GetDriver() {
			t.Fatal("the session made to drive the system is not marked as the driver")
		}
		if got := driver.GetPermissionMode(); got != model.PermissionBypass {
			t.Fatalf("the driver is created in %q, want %q: one that asks before every step describes "+
				"the exec instead of doing it", got, model.PermissionBypass)
		}

		again, err := s.FindOrCreateDriver(ctx, project.GetId())
		if err != nil {
			t.Fatalf("FindOrCreateDriver again: %v", err)
		}
		if again.GetId() != driver.GetId() {
			t.Fatalf("opening the driver twice gave %s then %s", driver.GetId(), again.GetId())
		}
	})

	// The record the operator used to be. A project that has not said carries nothing, and a project
	// that has said carries it on every read, so "where does this go" is answered by the row rather
	// than by asking somebody.
	t.Run("a project says where it deploys, and survives being reopened", func(t *testing.T) {
		open := newDataset(t)
		ctx := context.Background()

		before := open(t)
		project := newProject(t, before, "me", "house bills")
		if target := project.GetDeployTarget(); target != nil {
			t.Fatalf("a fresh project already deploys to %v", target)
		}

		want := deploy.Target{
			Account:  "123456789012",
			Region:   "eu-west-2",
			Identity: "arn:aws:iam::123456789012:role/krewe-deploy",
		}
		if err := before.SetDeployTarget(ctx, project.GetId(), want); err != nil {
			t.Fatalf("SetDeployTarget: %v", err)
		}
		before.Close()

		after := open(t)
		fetched, err := after.GetProject(ctx, project.GetId())
		if err != nil {
			t.Fatalf("GetProject: %v", err)
		}
		assertTarget(t, "the project it was set on", fetched.GetDeployTarget(), want)

		// A listing carries it too. A row a person reads is the whole point, and a listing that has to
		// fetch each project to say where it ships is a listing nobody puts it in.
		listed, err := after.ListProjects(ctx, project.GetWorkspace())
		if err != nil {
			t.Fatalf("ListProjects: %v", err)
		}
		if len(listed) != 1 {
			t.Fatalf("the workspace has %d projects, want 1", len(listed))
		}
		assertTarget(t, "the listed project", listed[0].GetDeployTarget(), want)
	})

	t.Run("a project that stops shipping somewhere says nothing again", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "me", "house bills")

		if err := s.SetDeployTarget(ctx, project.GetId(), deploy.Target{
			Account:  "123456789012",
			Region:   "eu-west-2",
			Identity: "arn:aws:iam::123456789012:role/krewe-deploy",
		}); err != nil {
			t.Fatalf("SetDeployTarget: %v", err)
		}
		if err := s.SetDeployTarget(ctx, project.GetId(), deploy.Target{}); err != nil {
			t.Fatalf("clearing the target: %v", err)
		}

		fetched, err := s.GetProject(ctx, project.GetId())
		if err != nil {
			t.Fatalf("GetProject: %v", err)
		}
		if target := fetched.GetDeployTarget(); target != nil {
			t.Fatalf("a cleared project still deploys to %v", target)
		}
	})

	t.Run("saying where a project that does not exist deploys is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		err := s.SetDeployTarget(context.Background(), "ghost", deploy.Target{
			Account:  "123456789012",
			Region:   "eu-west-2",
			Identity: "arn:aws:iam::123456789012:role/krewe-deploy",
		})
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("SetDeployTarget on a missing project returned %v, want ErrNotFound", err)
		}
	})

	// Where a project's work lands, and what kind of repository that is. Both stores or neither: a
	// system on memory that remembers the repository and a system on Postgres that forgets it is a system
	// whose jobs push nowhere on the one that matters.
	t.Run("a project's repository survives the round trip", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "me", "transcript")

		if project.GetRepository() != "" {
			t.Fatalf("a fresh project already works in %q", project.GetRepository())
		}
		recorded, err := s.SetProjectRepository(ctx, project.GetId(), "atlantic-blue/transcript", "public")
		if err != nil {
			t.Fatalf("SetProjectRepository: %v", err)
		}
		if recorded.GetRepository() != "atlantic-blue/transcript" || recorded.GetVisibility() != "public" {
			t.Fatalf("recorded %q %q, want atlantic-blue/transcript public",
				recorded.GetRepository(), recorded.GetVisibility())
		}

		fetched, err := s.GetProject(ctx, project.GetId())
		if err != nil {
			t.Fatalf("GetProject: %v", err)
		}
		if fetched.GetRepository() != "atlantic-blue/transcript" || fetched.GetVisibility() != "public" {
			t.Fatalf("read back %q %q, want atlantic-blue/transcript public",
				fetched.GetRepository(), fetched.GetVisibility())
		}

		listed, err := s.ListProjects(ctx, project.GetWorkspace())
		if err != nil {
			t.Fatalf("ListProjects: %v", err)
		}
		if len(listed) != 1 || listed[0].GetRepository() != "atlantic-blue/transcript" {
			t.Fatalf("the listing says %+v, want it to name the repository", listed)
		}
		// A listing is what the operator reads to answer "which project is that", so the kind has to
		// be in it too rather than only on the row a second call fetches.
		if listed[0].GetVisibility() != "public" {
			t.Fatalf("the listing says the repository is %q, want public", listed[0].GetVisibility())
		}
	})

	// A project that moved repository is corrected with the same command, so the second write has to
	// replace the first rather than sit beside it.
	t.Run("a project's repository is replaced by writing it again", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "me", "transcript")

		if _, err := s.SetProjectRepository(ctx, project.GetId(), "atlantic-blue/transcript", "public"); err != nil {
			t.Fatalf("SetProjectRepository: %v", err)
		}
		moved, err := s.SetProjectRepository(ctx, project.GetId(), "atlantic-blue/videos", "private")
		if err != nil {
			t.Fatalf("SetProjectRepository again: %v", err)
		}
		if moved.GetRepository() != "atlantic-blue/videos" || moved.GetVisibility() != "private" {
			t.Fatalf("after moving it works in %q %q, want atlantic-blue/videos private",
				moved.GetRepository(), moved.GetVisibility())
		}
	})

	t.Run("the repository of a project that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		_, err := s.SetProjectRepository(context.Background(), "ghost", "atlantic-blue/transcript", "public")
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("SetProjectRepository on a missing project returned %v, want ErrNotFound", err)
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

	t.Run("a deleted project is hidden and takes no new sessions", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "me", "house bills")

		if err := s.DeleteProject(ctx, project.GetId()); err != nil {
			t.Fatalf("DeleteProject: %v", err)
		}
		if _, err := s.GetProject(ctx, project.GetId()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetProject after delete returned %v, want ErrNotFound", err)
		}
		if _, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("a deleted project still took a session: %v", err)
		}
		if err := s.DeleteProject(ctx, project.GetId()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("deleting twice returned %v, want ErrNotFound", err)
		}
	})

	// A project cannot outlive the workspace it belongs to, or deleting a workspace would leave its
	// job reachable and dispatchable.
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

	// A session identifier only has to be unique inside its project, which is what lets two bodies of
	// job in one workspace both have a session the channel calls "general".
	t.Run("two projects in one workspace can share a session identifier", func(t *testing.T) {
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

		first, _, err := s.FindOrCreateSession(ctx, bills.GetId(), "general", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		second, _, err := s.FindOrCreateSession(ctx, garden.GetId(), "general", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession in the second project: %v", err)
		}
		if first.GetId() == second.GetId() {
			t.Fatal("the same session identifier in two projects landed in one session")
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
		session, _, err := before.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}
		if err := before.RecordExec(ctx, session.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordExec: %v", err)
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

		// The same session must still resolve to the same session, which is what lets the next exec
		// resume the conversation rather than start a new one.
		same, _, err := after.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession after reopening: %v", err)
		}
		if same.GetId() != session.GetId() {
			t.Fatalf("the session made a new session after reopening: %q, want %q", same.GetId(), session.GetId())
		}
		if same.GetModelSessionId() != "conversation-1" {
			t.Fatalf("the resumed session lost its conversation handle: %q", same.GetModelSessionId())
		}
	})

	// A flow run is carried by a job, and every step it takes is a job under
	// that one. Both stores have to write the run, the job and the record of it together, because a
	// step written without its movement is paid for twice and a movement written without its step is a
	// run waiting on something nobody declared.
	// A skill that needs nothing from the sandbox: no binary to check for, no secret to hand over.
	// Every skill shipped until the Simplified Technical English one declared at least one binary, so
	// nothing ever wrote an empty list, and the Postgres column is declared not null with a default
	// that an explicit NULL walks straight past. The memory store took it happily, which is exactly
	// how the two diverged without a test noticing.
	t.Run("a skill that asks for nothing is imported", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		asks := aSkill("ste", 1)
		asks.Binaries = nil
		asks.Secrets = nil
		if err := s.ImportSkill(ctx, asks); err != nil {
			t.Fatalf("ImportSkill for a skill declaring nothing: %v", err)
		}

		held, err := s.GetSkill(ctx, "ste", 1)
		if err != nil {
			t.Fatalf("GetSkill: %v", err)
		}
		if len(held.Binaries) != 0 {
			t.Errorf("binaries came back as %v, want none", held.Binaries)
		}
		if len(held.Secrets) != 0 {
			t.Errorf("secrets came back as %v, want none", held.Secrets)
		}
		// It has to reach a listing too, since that is what the operator and the wizard read.
		listed, err := s.ListSkills(ctx)
		if err != nil {
			t.Fatalf("ListSkills: %v", err)
		}
		found := false
		for _, one := range listed {
			if one.Name == "ste" {
				found = true
			}
		}
		if !found {
			t.Error("a skill that asks for nothing is missing from the listing")
		}
	})

	t.Run("a skill is imported with its files and comes back whole", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if err := s.ImportSkill(ctx, aSkill("github", 3)); err != nil {
			t.Fatalf("ImportSkill: %v", err)
		}

		held, err := s.GetSkill(ctx, "github", 3)
		if err != nil {
			t.Fatalf("GetSkill: %v", err)
		}
		if held.Summary != "Open pull requests." {
			t.Errorf("summary is %q", held.Summary)
		}
		if got := fmt.Sprint(held.Binaries); got != "[git gh]" {
			t.Errorf("binaries are %s, want [git gh]", got)
		}
		if len(held.Secrets) != 1 || held.Secrets["GH_TOKEN"] == "" {
			t.Fatalf("secrets are %+v, want GH_TOKEN with something saying what it is", held.Secrets)
		}
		if len(held.Files) != 3 {
			t.Fatalf("files came back as %d, want the 3 that went in", len(held.Files))
		}
		if held.ImportedAt.IsZero() {
			t.Error("the skill came back with no import time")
		}
		// The executable bit has to survive the round trip, or a setup script arrives unable to run and
		// the failure surfaces inside a container with nothing pointing back here.
		for _, file := range held.Files {
			if file.Path == "bin/setup" && !file.Executable {
				t.Error("bin/setup lost its executable bit in the store")
			}
			if file.Path == "SKILL.md" && file.Executable {
				t.Error("SKILL.md came back executable, so the bit is not stored per file")
			}
		}
	})

	t.Run("importing the same version twice is harmless, and importing a different skill as it is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if err := s.ImportSkill(ctx, aSkill("github", 3)); err != nil {
			t.Fatalf("ImportSkill: %v", err)
		}
		// The same skill again, which is what a pull that changed nothing produces.
		if err := s.ImportSkill(ctx, aSkill("github", 3)); err != nil {
			t.Fatalf("importing the same skill twice: %v", err)
		}

		changed := aSkill("github", 3)
		changed.Brief = "a different brief entirely"
		changed.Files[0].Body = []byte("a different brief entirely")
		if err := s.ImportSkill(ctx, changed); !errors.Is(err, store.ErrSkillChanged) {
			t.Fatalf("importing a changed skill at the same version returned %v, want ErrSkillChanged", err)
		}

		held, err := s.GetSkill(ctx, "github", 3)
		if err != nil {
			t.Fatalf("GetSkill: %v", err)
		}
		if held.Brief == "a different brief entirely" {
			t.Error("the refused import changed the skill anyway, so a session's pin means nothing")
		}
	})

	t.Run("a listing gives the newest revision of each skill and not its files", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		for _, one := range []store.Imported{aSkill("git", 1), aSkill("github", 1), aSkill("github", 2)} {
			if err := s.ImportSkill(ctx, one); err != nil {
				t.Fatalf("ImportSkill %s v%d: %v", one.Name, one.Version, err)
			}
		}

		list, err := s.ListSkills(ctx)
		if err != nil {
			t.Fatalf("ListSkills: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("ListSkills returned %d, want one row per name", len(list))
		}
		if list[0].Name != "git" || list[1].Name != "github" {
			t.Fatalf("ListSkills returned %s and %s, want them sorted", list[0].Name, list[1].Name)
		}
		if list[1].Version != 2 {
			t.Errorf("github came back at version %d, want the newest, 2", list[1].Version)
		}
		// A listing is the cheapest call and the files are the largest part of a skill.
		for _, held := range list {
			if len(held.Files) != 0 {
				t.Errorf("%s carried %d files into a listing", held.Name, len(held.Files))
			}
			if len(held.Secrets) == 0 {
				t.Errorf("%s carried no secrets, and a listing has to say what a skill needs", held.Name)
			}
		}
	})

	t.Run("a workspace holds the skills attached to it, pinned to a version", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}

		if held, err := s.WorkspaceSkills(ctx, workspace.GetId()); err != nil || len(held) != 0 {
			t.Fatalf("a new workspace holds %d skills (%v), want none", len(held), err)
		}
		if err := s.ImportSkill(ctx, aSkill("github", 1)); err != nil {
			t.Fatalf("ImportSkill: %v", err)
		}

		attached, err := s.AttachSkill(ctx, workspace.GetId(), "github")
		if err != nil {
			t.Fatalf("AttachSkill: %v", err)
		}
		if attached.Version != 1 {
			t.Errorf("attached version %d, want 1", attached.Version)
		}

		held, err := s.WorkspaceSkills(ctx, workspace.GetId())
		if err != nil {
			t.Fatalf("WorkspaceSkills: %v", err)
		}
		if len(held) != 1 || held[0].Name != "github" {
			t.Fatalf("the workspace holds %+v, want github", held)
		}
		// The files come with it, because this is the call a sandbox is built from.
		if len(held[0].Files) == 0 {
			t.Error("a workspace's skills came back without their files, so nothing could be mounted")
		}

		// A newer revision imported does not move a workspace that pinned the older one, which is what
		// stops a skill changing under a session already using it.
		if err := s.ImportSkill(ctx, aSkill("github", 2)); err != nil {
			t.Fatalf("ImportSkill v2: %v", err)
		}
		held, err = s.WorkspaceSkills(ctx, workspace.GetId())
		if err != nil {
			t.Fatalf("WorkspaceSkills after a new revision: %v", err)
		}
		if len(held) != 1 || held[0].Version != 1 {
			t.Fatalf("the workspace moved to %+v on its own, want it pinned at version 1", held)
		}

		// Attaching again is how it moves.
		if _, err := s.AttachSkill(ctx, workspace.GetId(), "github"); err != nil {
			t.Fatalf("AttachSkill again: %v", err)
		}
		held, err = s.WorkspaceSkills(ctx, workspace.GetId())
		if err != nil {
			t.Fatalf("WorkspaceSkills after re-attaching: %v", err)
		}
		if len(held) != 1 || held[0].Version != 2 {
			t.Fatalf("re-attaching left the workspace at %+v, want version 2", held)
		}

		if err := s.DetachSkill(ctx, workspace.GetId(), "github"); err != nil {
			t.Fatalf("DetachSkill: %v", err)
		}
		if held, err := s.WorkspaceSkills(ctx, workspace.GetId()); err != nil || len(held) != 0 {
			t.Fatalf("the workspace still holds %d skills (%v) after detaching", len(held), err)
		}
		// Detaching does not unimport: another workspace may hold it, and changing your mind should not
		// cost a re-import.
		if _, err := s.GetSkill(ctx, "github", 2); err != nil {
			t.Errorf("detaching removed the skill from the system: %v", err)
		}
	})

	t.Run("attaching what does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		if err := s.ImportSkill(ctx, aSkill("github", 1)); err != nil {
			t.Fatalf("ImportSkill: %v", err)
		}

		if _, err := s.AttachSkill(ctx, workspace.GetId(), "terraform"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("attaching a skill the system has not imported returned %v, want ErrNotFound", err)
		}
		if _, err := s.AttachSkill(ctx, "ghost", "github"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("attaching to a workspace that does not exist returned %v, want ErrNotFound", err)
		}
		if _, err := s.GetSkill(ctx, "github", 9); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("reading a version that was never imported returned %v, want ErrNotFound", err)
		}
		if err := s.DetachSkill(ctx, workspace.GetId(), "github"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("detaching a skill the workspace does not hold returned %v, want ErrNotFound", err)
		}
	})

	t.Run("the system holds a skill for every workspace, pinned to a version", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if held, err := s.SystemSkills(ctx); err != nil || len(held) != 0 {
			t.Fatalf("a fresh system holds %d skills (%v), want none", len(held), err)
		}
		if err := s.ImportSkill(ctx, aSkill("github", 1)); err != nil {
			t.Fatalf("ImportSkill: %v", err)
		}
		if _, err := s.AttachSystemSkill(ctx, "github"); err != nil {
			t.Fatalf("AttachSystemSkill: %v", err)
		}

		held, err := s.SystemSkills(ctx)
		if err != nil {
			t.Fatalf("SystemSkills: %v", err)
		}
		if len(held) != 1 || held[0].Name != "github" {
			t.Fatalf("the system holds %+v, want the github skill", held)
		}
		// The files come back, because a system skill is mounted into a sandbox exactly like a
		// workspace's, and a listing without them could mount nothing.
		if len(held[0].Files) == 0 {
			t.Error("the system's skills came back without their files, so nothing could be mounted")
		}
		if len(held[0].Secrets) == 0 {
			t.Error("the system's skills came back without the secrets they name")
		}

		// Pinned, and attaching again is how the system moves, the same as a workspace.
		if err := s.ImportSkill(ctx, aSkill("github", 2)); err != nil {
			t.Fatalf("ImportSkill v2: %v", err)
		}
		if held, err := s.SystemSkills(ctx); err != nil || len(held) != 1 || held[0].Version != 1 {
			t.Fatalf("the system moved to %+v on its own (%v), want it pinned at version 1", held, err)
		}
		if _, err := s.AttachSystemSkill(ctx, "github"); err != nil {
			t.Fatalf("AttachSystemSkill again: %v", err)
		}
		if held, err := s.SystemSkills(ctx); err != nil || len(held) != 1 || held[0].Version != 2 {
			t.Fatalf("re-attaching left the system at %+v (%v), want version 2", held, err)
		}

		if err := s.DetachSystemSkill(ctx, "github"); err != nil {
			t.Fatalf("DetachSystemSkill: %v", err)
		}
		if held, err := s.SystemSkills(ctx); err != nil || len(held) != 0 {
			t.Fatalf("the system still holds %d skills (%v) after detaching", len(held), err)
		}
		if _, err := s.GetSkill(ctx, "github", 2); err != nil {
			t.Errorf("detaching from the system removed the skill from the catalogue: %v", err)
		}
	})

	t.Run("the system's holding and a workspace's are separate statements", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		if err := s.ImportSkill(ctx, aSkill("github", 1)); err != nil {
			t.Fatalf("ImportSkill: %v", err)
		}
		if _, err := s.AttachSystemSkill(ctx, "github"); err != nil {
			t.Fatalf("AttachSystemSkill: %v", err)
		}
		if _, err := s.AttachSkill(ctx, workspace.GetId(), "github"); err != nil {
			t.Fatalf("AttachSkill: %v", err)
		}

		// Taking it off the system leaves the workspace's own attachment alone: the narrower statement
		// is not undone by the wider one.
		if err := s.DetachSystemSkill(ctx, "github"); err != nil {
			t.Fatalf("DetachSystemSkill: %v", err)
		}
		if held, err := s.WorkspaceSkills(ctx, workspace.GetId()); err != nil || len(held) != 1 {
			t.Fatalf("the workspace holds %d skills (%v) after the system let go, want 1", len(held), err)
		}
	})

	t.Run("attaching to the system what does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		if _, err := s.AttachSystemSkill(ctx, "terraform"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("attaching a skill the system has not imported returned %v, want ErrNotFound", err)
		}
		if err := s.DetachSystemSkill(ctx, "terraform"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("detaching a skill the system does not hold returned %v, want ErrNotFound", err)
		}
	})

	// A session coming into existence is an event, and only the store knows it happened. A caller
	// that found out by looking first would announce a session twice under a race.
	t.Run("finding or creating a session says which it did", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")

		if _, created, err := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{}); err != nil || !created {
			t.Fatalf("the first call created %v (%v), want it to say it made one", created, err)
		}
		if _, created, err := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{}); err != nil || created {
			t.Fatalf("the second call created %v (%v), want it to say it found one", created, err)
		}
	})

	t.Run("a session's lifecycle is kept, in the order it happened", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, err := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		if err != nil {
			t.Fatalf("FindOrCreateSession: %v", err)
		}

		at := time.Now().UTC().Add(-time.Hour)
		for i, kind := range []string{"session.created", "session.started", "session.completed"} {
			event := &quaycrewv1.SessionEvent{
				Id:         fmt.Sprintf("event-%d", i),
				Kind:       kind,
				Session:    session.GetId(),
				Workspace:  session.GetWorkspace(),
				Project:    session.GetProject(),
				Handle:     session.GetHandle(),
				Detail:     kind + " happened",
				OccurredAt: timestamppb.New(at.Add(time.Duration(i) * time.Minute)),
			}
			if err := s.AppendSessionEvent(ctx, event); err != nil {
				t.Fatalf("AppendSessionEvent %s: %v", kind, err)
			}
			// Written twice, because a caller that is unsure whether its write landed must be able to
			// send it again and leave one record.
			if err := s.AppendSessionEvent(ctx, event); err != nil {
				t.Fatalf("AppendSessionEvent %s again: %v", kind, err)
			}
		}

		kept, err := s.ListSessionEvents(ctx, session.GetId(), 0)
		if err != nil {
			t.Fatalf("ListSessionEvents: %v", err)
		}
		if len(kept) != 3 {
			t.Fatalf("the session holds %d events, want the three that happened once each", len(kept))
		}
		for i, want := range []string{"session.created", "session.started", "session.completed"} {
			if kept[i].GetKind() != want {
				t.Errorf("event %d is %q, want %q", i+1, kept[i].GetKind(), want)
			}
		}
		if kept[0].GetDetail() == "" || kept[0].GetHandle() != session.GetHandle() {
			t.Errorf("an event came back without what it carried: %+v", kept[0])
		}

		// No session named asks for the whole system's, which is what a view of what is going on reads.
		all, err := s.ListSessionEvents(ctx, "", 0)
		if err != nil {
			t.Fatalf("ListSessionEvents for the system: %v", err)
		}
		if len(all) != 3 {
			t.Fatalf("the system holds %d events, want 3", len(all))
		}

		// A session nobody has is empty rather than an error: nothing has happened to it yet.
		none, err := s.ListSessionEvents(ctx, "no-such-session", 0)
		if err != nil || len(none) != 0 {
			t.Fatalf("a session with no events answered %d (%v)", len(none), err)
		}
	})

	t.Run("an event with no id or no kind is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		if err := s.AppendSessionEvent(ctx, &quaycrewv1.SessionEvent{Kind: "session.created"}); err == nil {
			t.Error("an event with no id was accepted, so writing it twice would leave two")
		}
		if err := s.AppendSessionEvent(ctx, &quaycrewv1.SessionEvent{Id: "e1"}); err == nil {
			t.Error("an event with no kind was accepted, so a consumer would have nothing to switch on")
		}
	})

	runHookConformance(t, newDataset)
	runSessionLifecycleConformance(t, newDataset)
	runDesignConformance(t, newDataset)
	runPathConformance(t, newDataset)
	runTakeConformance(t, newDataset)
}

// runDesignConformance holds both stores to the same answers about what a project is for and what
// was designed for it.
func runDesignConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a project with no design answers with its identifier and nothing else", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		design, err := s.GetDesign(ctx, project.GetId())
		if err != nil {
			t.Fatalf("GetDesign on a project with no design: %v", err)
		}
		if design.GetProject() != project.GetId() {
			t.Fatalf("the design names project %q, want %q", design.GetProject(), project.GetId())
		}
		if design.GetBrief() != "" || design.GetBody() != "" || design.GetWrittenBy() != "" {
			t.Fatalf("a project nobody designed answered brief %q, body %q, written by %q",
				design.GetBrief(), design.GetBody(), design.GetWrittenBy())
		}
		if design.GetApproved() {
			t.Error("a project nobody designed answered approved")
		}
	})

	t.Run("a design is read back for a project that does not exist as not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if _, err := s.GetDesign(ctx, "no-such-project"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetDesign on a missing project answered %v, want ErrNotFound", err)
		}
		if _, err := s.SetProjectBrief(ctx, "no-such-project", "anything"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("SetProjectBrief on a missing project answered %v, want ErrNotFound", err)
		}
		if _, err := s.SetProjectDesign(ctx, "no-such-project", "anything", ""); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("SetProjectDesign on a missing project answered %v, want ErrNotFound", err)
		}
	})

	t.Run("a brief is written, read back and cleared", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		written, err := s.SetProjectBrief(ctx, project.GetId(), "pay the water bill first")
		if err != nil {
			t.Fatalf("SetProjectBrief: %v", err)
		}
		if written.GetBrief() != "pay the water bill first" {
			t.Fatalf("the write answered brief %q", written.GetBrief())
		}
		if written.GetUpdatedAt() == nil {
			t.Fatal("the write answered no updated_at, so nothing says when the brief was set")
		}

		read, err := s.GetDesign(ctx, project.GetId())
		if err != nil {
			t.Fatalf("GetDesign: %v", err)
		}
		if read.GetBrief() != "pay the water bill first" {
			t.Fatalf("the brief reads back as %q", read.GetBrief())
		}

		// An empty brief is a value, not an absence: clearing one is the only way back.
		cleared, err := s.SetProjectBrief(ctx, project.GetId(), "")
		if err != nil {
			t.Fatalf("clearing the brief: %v", err)
		}
		if cleared.GetBrief() != "" {
			t.Fatalf("the cleared brief reads %q, want empty", cleared.GetBrief())
		}
	})

	// A design body is the largest text in the system, and a body read short is a body read wrong.
	t.Run("a design body is kept whole, line breaks and all", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		body := "# The design\n\nOne paragraph.\n\n- a point\n- another point\n\n\tan indented line\n"
		if _, err := s.SetProjectDesign(ctx, project.GetId(), body, "sess-1"); err != nil {
			t.Fatalf("SetProjectDesign: %v", err)
		}
		read, err := s.GetDesign(ctx, project.GetId())
		if err != nil {
			t.Fatalf("GetDesign: %v", err)
		}
		if read.GetBody() != body {
			t.Fatalf("the body reads back as %q, want %q", read.GetBody(), body)
		}
		if read.GetWrittenBy() != "sess-1" {
			t.Fatalf("the design says it was written by %q, want sess-1", read.GetWrittenBy())
		}
	})

	// The operator writes a design too, and then nobody wrote it. Empty is what the caller sent, so
	// the store keeps it rather than treating it as a field nobody filled in.
	t.Run("a design written by the operator records nobody", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetProjectDesign(ctx, project.GetId(), "the body", "sess-1"); err != nil {
			t.Fatalf("SetProjectDesign as a session: %v", err)
		}
		written, err := s.SetProjectDesign(ctx, project.GetId(), "the body again", "")
		if err != nil {
			t.Fatalf("SetProjectDesign as the operator: %v", err)
		}
		if written.GetWrittenBy() != "" {
			t.Fatalf("the design still says it was written by %q, want nobody", written.GetWrittenBy())
		}
	})

	// The two are separate statements about a project. Writing one over the other is the shape that
	// loses a brief nobody meant to touch.
	t.Run("a brief and a body do not overwrite each other", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetProjectBrief(ctx, project.GetId(), "pay the water bill first"); err != nil {
			t.Fatalf("SetProjectBrief: %v", err)
		}
		afterBody, err := s.SetProjectDesign(ctx, project.GetId(), "the whole design", "")
		if err != nil {
			t.Fatalf("SetProjectDesign: %v", err)
		}
		if afterBody.GetBrief() != "pay the water bill first" {
			t.Fatalf("writing the body left the brief as %q", afterBody.GetBrief())
		}
		afterBrief, err := s.SetProjectBrief(ctx, project.GetId(), "and the electricity bill")
		if err != nil {
			t.Fatalf("SetProjectBrief again: %v", err)
		}
		if afterBrief.GetBody() != "the whole design" {
			t.Fatalf("writing the brief left the body as %q", afterBrief.GetBody())
		}
	})

	// The operator's word is a statement about one text, and this is the pair of writes that proves
	// it: approve, then write a body over it, and the word is gone.
	t.Run("writing a design clears the approval", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetProjectDesign(ctx, project.GetId(), "the whole design", ""); err != nil {
			t.Fatalf("SetProjectDesign: %v", err)
		}
		approved, err := s.ApproveProjectDesign(ctx, project.GetId())
		if err != nil {
			t.Fatalf("ApproveProjectDesign: %v", err)
		}
		if !approved.GetApproved() {
			t.Fatal("the approval answered a design that is not approved")
		}
		if approved.GetApprovedAt() == nil {
			t.Fatal("the approval answered no moment, so nothing says when the word was given")
		}

		rewritten, err := s.SetProjectDesign(ctx, project.GetId(), "the whole design, again", "")
		if err != nil {
			t.Fatalf("SetProjectDesign over an approved design: %v", err)
		}
		if rewritten.GetApproved() || rewritten.GetApprovedAt() != nil {
			t.Fatalf("a design rewritten over an approved one reads approved at %v", rewritten.GetApprovedAt())
		}
		read, err := s.GetDesign(ctx, project.GetId())
		if err != nil {
			t.Fatalf("GetDesign: %v", err)
		}
		if read.GetApproved() || read.GetApprovedAt() != nil {
			t.Fatalf("the rewritten design reads back approved at %v", read.GetApprovedAt())
		}
	})

	// A brief says nothing about what was designed, so it cannot take the word away.
	t.Run("writing a brief leaves the approval alone", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetProjectDesign(ctx, project.GetId(), "the whole design", ""); err != nil {
			t.Fatalf("SetProjectDesign: %v", err)
		}
		if _, err := s.ApproveProjectDesign(ctx, project.GetId()); err != nil {
			t.Fatalf("ApproveProjectDesign: %v", err)
		}
		afterBrief, err := s.SetProjectBrief(ctx, project.GetId(), "keep the household bills paid")
		if err != nil {
			t.Fatalf("SetProjectBrief: %v", err)
		}
		if !afterBrief.GetApproved() {
			t.Fatal("writing a brief took the approval away, and a brief says nothing about the design")
		}
	})

	// The check on the body and the write are one statement, so only the store can hold this rule.
	t.Run("approving a design that says nothing is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		// A project with no design row at all.
		if _, err := s.ApproveProjectDesign(ctx, project.GetId()); !errors.Is(err, store.ErrNothingToApprove) {
			t.Fatalf("approving a project with no design answered %v, want ErrNothingToApprove", err)
		}
		// And one that holds a brief and no body, which is a row with nothing to agree to.
		if _, err := s.SetProjectBrief(ctx, project.GetId(), "keep the household bills paid"); err != nil {
			t.Fatalf("SetProjectBrief: %v", err)
		}
		if _, err := s.ApproveProjectDesign(ctx, project.GetId()); !errors.Is(err, store.ErrNothingToApprove) {
			t.Fatalf("approving a design with no body answered %v, want ErrNothingToApprove", err)
		}
		read, err := s.GetDesign(ctx, project.GetId())
		if err != nil {
			t.Fatalf("GetDesign: %v", err)
		}
		if read.GetApproved() {
			t.Fatal("the refused approval was written anyway")
		}
	})

	t.Run("approving the design of a project that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if _, err := s.ApproveProjectDesign(ctx, "no-such-project"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("ApproveProjectDesign on a missing project answered %v, want ErrNotFound", err)
		}
	})

	// The word is about the text, so saying it twice is not a mistake to refuse. The moment moves,
	// which is what an operator who approved the same design again would expect to read.
	t.Run("approving an approved design moves the moment", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetProjectDesign(ctx, project.GetId(), "the whole design", ""); err != nil {
			t.Fatalf("SetProjectDesign: %v", err)
		}
		first, err := s.ApproveProjectDesign(ctx, project.GetId())
		if err != nil {
			t.Fatalf("ApproveProjectDesign: %v", err)
		}
		time.Sleep(orderingGap)
		again, err := s.ApproveProjectDesign(ctx, project.GetId())
		if err != nil {
			t.Fatalf("ApproveProjectDesign again: %v", err)
		}
		if !again.GetApprovedAt().AsTime().After(first.GetApprovedAt().AsTime()) {
			t.Fatalf("the second approval reads %v, and the first reads %v",
				again.GetApprovedAt().AsTime(), first.GetApprovedAt().AsTime())
		}
	})

	// The design row hangs off the project, so deleting the project takes it. Postgres does this with
	// the cascade and the in memory store has to agree, or a deleted project would keep answering.
	t.Run("deleting a project takes its design with it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetProjectDesign(ctx, project.GetId(), "the whole design", ""); err != nil {
			t.Fatalf("SetProjectDesign: %v", err)
		}
		if err := s.DeleteProject(ctx, project.GetId()); err != nil {
			t.Fatalf("DeleteProject: %v", err)
		}
		if _, err := s.GetDesign(ctx, project.GetId()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("a deleted project answered %v for its design, want ErrNotFound", err)
		}
	})
}

// aSkill is a skill to put in the store, whole enough that the round trip is worth asserting on: two
// binaries, a named secret, and a setup script that has to stay executable.
func aSkill(name string, version int) store.Imported {
	return store.Imported{Skill: skill.Skill{
		Name:     name,
		Version:  version,
		Summary:  "Open pull requests.",
		Binaries: []string{"git", "gh"},
		Secrets:  map[string]string{"GH_TOKEN": "a token with repo scope"},
		Brief:    "how it is done here",
		Files: []skill.File{
			{Path: "SKILL.md", Body: []byte("how it is done here")},
			{Path: "bin/setup", Body: []byte("#!/bin/sh\n"), Executable: true},
			{Path: "skill.yaml", Body: []byte("name: " + name + "\n")},
		},
	}}
}

// newProject creates a workspace and a project inside it, which is the smallest setup a session
// needs now that sessions live inside a project.
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

// ids names the sessions in a listing, so a failure says which sessions came back rather than how
// many.
func ids(sessions []*quaycrewv1.Session) []string {
	out := make([]string, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, session.GetId())
	}
	return out
}

// orderingGap is how long the ordering cases wait between two writes so the stamps they compare are
// genuinely apart. The store writes its own stamps and takes none, so a wait is the only way to put a
// gap between them. Ten milliseconds is far above what either store's clock resolves and is paid four
// times in the whole suite.
const orderingGap = 10 * time.Millisecond

// assertTarget says a project ships where it was told to, naming the read that disagreed.
func assertTarget(t *testing.T, where string, got *quaycrewv1.DeployTarget, want deploy.Target) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s says it deploys nowhere, want %v", where, want)
	}
	read := deploy.Target{
		Account:  got.GetAccount(),
		Region:   got.GetRegion(),
		Identity: got.GetIdentity(),
	}
	if read != want {
		t.Fatalf("%s deploys to %+v, want %+v", where, read, want)
	}
}

// runPathConformance holds both stores to the same answers about the steps a design was broken into.
//
// The store keeps what it is given: whether a number is unique and whether `after` names a step that
// exists are the control plane's questions, because the document is where a person can be told which
// line is wrong. What is proved here is order, replacement, and what a fresh step reads as.
func runPathConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a project with no path answers with nothing, and it is not an error", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		steps, err := s.ListSteps(ctx, project.GetId())
		if err != nil {
			t.Fatalf("ListSteps on a project with no path: %v", err)
		}
		if len(steps) != 0 {
			t.Fatalf("a project nobody gave a path answered %d steps", len(steps))
		}
	})

	// Number order is what a caller is promised, whatever order the steps were written in.
	t.Run("a path is written whole and read back in number order", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		written, err := s.SetPath(ctx, project.GetId(), []store.Step{
			{Number: 3, Title: "the third", After: 2},
			{Number: 1, Title: "the first"},
			{Number: 2, Title: "the second", After: 1},
		})
		if err != nil {
			t.Fatalf("SetPath: %v", err)
		}
		if got := numbersOf(written); !slices.Equal(got, []int32{1, 2, 3}) {
			t.Fatalf("the write answered steps %v, want 1, 2, 3 in order", got)
		}
		read, err := s.ListSteps(ctx, project.GetId())
		if err != nil {
			t.Fatalf("ListSteps: %v", err)
		}
		if got := numbersOf(read); !slices.Equal(got, []int32{1, 2, 3}) {
			t.Fatalf("the path reads back as steps %v, want 1, 2, 3 in order", got)
		}
	})

	// Numbers need not run without gaps, so a path may go 1, 2, 5.
	t.Run("a path with gaps in its numbers is kept as it is", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetPath(ctx, project.GetId(), []store.Step{
			{Number: 1, Title: "the first"},
			{Number: 2, Title: "the second", After: 1},
			{Number: 5, Title: "the fifth", After: 2},
		}); err != nil {
			t.Fatalf("SetPath: %v", err)
		}
		read, err := s.ListSteps(ctx, project.GetId())
		if err != nil {
			t.Fatalf("ListSteps: %v", err)
		}
		if got := numbersOf(read); !slices.Equal(got, []int32{1, 2, 5}) {
			t.Fatalf("the path reads back as steps %v, want 1, 2, 5", got)
		}
	})

	// Everything a caller may set travels, and everything the system owns reads as a step nobody has
	// touched. A column added to one store and not the other reads as a zero rather than a failure,
	// so every field is named here.
	t.Run("a step keeps what it was given and is born ready", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		written, err := s.SetPath(ctx, project.GetId(), []store.Step{{
			Number:        1,
			Title:         "the store holds a project's brief",
			Intention:     "The design has nowhere to live.",
			Touches:       "internal/store/store.go\ninternal/store/postgres.go",
			Proof:         "The operator sets a brief and reads it back.",
			ProofScenario: "a project carries a brief",
			After:         0,
		}})
		if err != nil {
			t.Fatalf("SetPath: %v", err)
		}
		if len(written) != 1 {
			t.Fatalf("the write answered %d steps, want 1", len(written))
		}
		step := written[0]
		if step.GetProject() != project.GetId() {
			t.Errorf("the step names project %q, want %q", step.GetProject(), project.GetId())
		}
		if step.GetTitle() != "the store holds a project's brief" {
			t.Errorf("the step is titled %q", step.GetTitle())
		}
		if step.GetIntention() != "The design has nowhere to live." {
			t.Errorf("the step's intention reads %q", step.GetIntention())
		}
		// Line breaks and all: the take reads this field line by line.
		if step.GetTouches() != "internal/store/store.go\ninternal/store/postgres.go" {
			t.Errorf("the step touches %q", step.GetTouches())
		}
		if step.GetProof() != "The operator sets a brief and reads it back." {
			t.Errorf("the step's proof reads %q", step.GetProof())
		}
		if step.GetProofScenario() != "a project carries a brief" {
			t.Errorf("the step names scenario %q", step.GetProofScenario())
		}
		if step.GetState() != store.StepReady {
			t.Errorf("a fresh step reads as %q, want %q", step.GetState(), store.StepReady)
		}
		if step.GetSession() != "" || step.GetResult() != "" {
			t.Errorf("a fresh step reads session %q and result %q, want both empty",
				step.GetSession(), step.GetResult())
		}
		if step.GetTakenAt() != nil || step.GetFinishedAt() != nil {
			t.Errorf("a fresh step carries the stamps %v and %v, and nobody has touched it",
				step.GetTakenAt(), step.GetFinishedAt())
		}
	})

	// Writing a path replaces it. A step the new path does not name is gone, which is the whole of
	// what this slice of SetPath does.
	t.Run("writing a path again replaces the one that was there", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetPath(ctx, project.GetId(), []store.Step{
			{Number: 1, Title: "the first"},
			{Number: 2, Title: "the second", After: 1},
			{Number: 3, Title: "the third", After: 2},
		}); err != nil {
			t.Fatalf("SetPath: %v", err)
		}
		written, err := s.SetPath(ctx, project.GetId(), []store.Step{
			{Number: 1, Title: "the first, rewritten"},
			{Number: 3, Title: "the third", After: 1},
		})
		if err != nil {
			t.Fatalf("SetPath again: %v", err)
		}
		if got := numbersOf(written); !slices.Equal(got, []int32{1, 3}) {
			t.Fatalf("the rewritten path holds steps %v, want 1 and 3", got)
		}
		if written[0].GetTitle() != "the first, rewritten" {
			t.Errorf("step 1 is titled %q, so the rewrite did not reach it", written[0].GetTitle())
		}
		if written[1].GetAfter() != 1 {
			t.Errorf("step 3 waits for step %d, want 1", written[1].GetAfter())
		}
	})

	t.Run("one project's path is not another's", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		bills := newProject(t, s, "acme", "house-bills")
		garden := newProject(t, s, "acme", "the-garden")

		if _, err := s.SetPath(ctx, bills.GetId(), []store.Step{{Number: 1, Title: "pay the water"}}); err != nil {
			t.Fatalf("SetPath for house-bills: %v", err)
		}
		if _, err := s.SetPath(ctx, garden.GetId(), []store.Step{
			{Number: 1, Title: "cut the grass"},
			{Number: 2, Title: "plant the beds", After: 1},
		}); err != nil {
			t.Fatalf("SetPath for the-garden: %v", err)
		}

		read, err := s.ListSteps(ctx, bills.GetId())
		if err != nil {
			t.Fatalf("ListSteps: %v", err)
		}
		if len(read) != 1 || read[0].GetTitle() != "pay the water" {
			t.Fatalf("house-bills holds %d steps, and the first is %q", len(read), read[0].GetTitle())
		}

		// Every project at once, ordered by project and then by number.
		every, err := s.ListSteps(ctx, "")
		if err != nil {
			t.Fatalf("ListSteps for every project: %v", err)
		}
		if len(every) != 3 {
			t.Fatalf("every project holds %d steps, want 3", len(every))
		}
		for at := 1; at < len(every); at++ {
			before, now := every[at-1], every[at]
			if before.GetProject() > now.GetProject() {
				t.Fatalf("the listing goes from project %q to %q, so it is not in project order",
					before.GetProject(), now.GetProject())
			}
			if before.GetProject() == now.GetProject() && before.GetNumber() >= now.GetNumber() {
				t.Fatalf("one project's steps read %d then %d", before.GetNumber(), now.GetNumber())
			}
		}
	})

	// A project here is deleted by a stamp rather than by removing the row, so the foreign key
	// cascade never fires for one. Both stores have to hide its path anyway, or a listing answers
	// with the path of a project nobody can reach.
	t.Run("a deleted project's path leaves every listing", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetPath(ctx, project.GetId(), []store.Step{{Number: 1, Title: "pay the water"}}); err != nil {
			t.Fatalf("SetPath: %v", err)
		}
		if err := s.DeleteProject(ctx, project.GetId()); err != nil {
			t.Fatalf("DeleteProject: %v", err)
		}
		if _, err := s.ListSteps(ctx, project.GetId()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("ListSteps on a deleted project answered %v, want ErrNotFound", err)
		}
		every, err := s.ListSteps(ctx, "")
		if err != nil {
			t.Fatalf("ListSteps for every project: %v", err)
		}
		if len(every) != 0 {
			t.Fatalf("a deleted project left %d steps in the listing", len(every))
		}
	})

	t.Run("a path written for a project that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if _, err := s.SetPath(ctx, "no-such-project", []store.Step{{Number: 1, Title: "the first"}}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("SetPath on a missing project answered %v, want ErrNotFound", err)
		}
		if _, err := s.ListSteps(ctx, "no-such-project"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("ListSteps on a missing project answered %v, want ErrNotFound", err)
		}
	})
}

// numbersOf is a path's step numbers, in the order the store answered with.
func numbersOf(steps []*quaycrewv1.Step) []int32 {
	numbers := make([]int32, 0, len(steps))
	for _, step := range steps {
		numbers = append(numbers, step.GetNumber())
	}
	return numbers
}

// runTakeConformance holds both stores to the same answers about giving one step to a session.
func runTakeConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("one step of a path is read back whole", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetPath(ctx, project.GetId(), []store.Step{
			{Number: 1, Title: "the first"},
			{Number: 2, Title: "the second", Intention: "The second thing.",
				Touches: "internal/store/store.go", Proof: "It reads back.",
				ProofScenario: "a project carries a brief", After: 1},
		}); err != nil {
			t.Fatalf("SetPath: %v", err)
		}
		step, err := s.GetStep(ctx, project.GetId(), 2)
		if err != nil {
			t.Fatalf("GetStep: %v", err)
		}
		if step.GetNumber() != 2 || step.GetTitle() != "the second" {
			t.Fatalf("the read answered step %d titled %q", step.GetNumber(), step.GetTitle())
		}
		if step.GetIntention() != "The second thing." || step.GetTouches() != "internal/store/store.go" {
			t.Errorf("the step reads intention %q and touches %q",
				step.GetIntention(), step.GetTouches())
		}
		if step.GetProof() != "It reads back." || step.GetProofScenario() != "a project carries a brief" {
			t.Errorf("the step reads proof %q and scenario %q",
				step.GetProof(), step.GetProofScenario())
		}
		if step.GetAfter() != 1 || step.GetState() != store.StepReady {
			t.Errorf("the step waits for %d and reads as %q", step.GetAfter(), step.GetState())
		}
	})

	// A number nobody wrote and a project nobody made both answer the same way: neither is the step
	// that was asked for.
	t.Run("a step the path does not hold is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetPath(ctx, project.GetId(), []store.Step{{Number: 1, Title: "the first"}}); err != nil {
			t.Fatalf("SetPath: %v", err)
		}
		if _, err := s.GetStep(ctx, project.GetId(), 7); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetStep on a step nobody wrote answered %v, want ErrNotFound", err)
		}
		if _, err := s.GetStep(ctx, "no-such-project", 1); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetStep on a missing project answered %v, want ErrNotFound", err)
		}
	})

	t.Run("taking a ready step records the session and the moment", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetPath(ctx, project.GetId(), []store.Step{{Number: 1, Title: "the first"}}); err != nil {
			t.Fatalf("SetPath: %v", err)
		}
		taken, err := s.TakeStep(ctx, project.GetId(), 1, "session-one")
		if err != nil {
			t.Fatalf("TakeStep: %v", err)
		}
		if taken.GetState() != store.StepTaken {
			t.Errorf("the taken step reads as %q, want %q", taken.GetState(), store.StepTaken)
		}
		if taken.GetSession() != "session-one" {
			t.Errorf("the taken step names session %q, want session-one", taken.GetSession())
		}
		if taken.GetTakenAt() == nil {
			t.Error("the taken step carries no moment, so nothing records when it was taken")
		}
		// Read again, because a write that answered well and stored nothing reads the same to its
		// caller and to nobody else.
		read, err := s.GetStep(ctx, project.GetId(), 1)
		if err != nil {
			t.Fatalf("GetStep after the take: %v", err)
		}
		if read.GetState() != store.StepTaken || read.GetSession() != "session-one" {
			t.Fatalf("the step reads back as %q held by %q", read.GetState(), read.GetSession())
		}
	})

	// The refusal is what makes one step one session's. Two takes that both passed would put two
	// sessions on one change.
	t.Run("taking a step somebody already holds is refused, and the holder keeps it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetPath(ctx, project.GetId(), []store.Step{{Number: 1, Title: "the first"}}); err != nil {
			t.Fatalf("SetPath: %v", err)
		}
		if _, err := s.TakeStep(ctx, project.GetId(), 1, "session-one"); err != nil {
			t.Fatalf("TakeStep: %v", err)
		}
		if _, err := s.TakeStep(ctx, project.GetId(), 1, "session-two"); !errors.Is(err, store.ErrStepNotReady) {
			t.Fatalf("taking a step twice answered %v, want ErrStepNotReady", err)
		}
		read, err := s.GetStep(ctx, project.GetId(), 1)
		if err != nil {
			t.Fatalf("GetStep after the refusal: %v", err)
		}
		if read.GetSession() != "session-one" {
			t.Fatalf("the refused take moved the step to session %q", read.GetSession())
		}
	})

	// Several steps may be in flight. Nothing here refuses a second take on a different step.
	t.Run("two steps of one path are taken at once", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetPath(ctx, project.GetId(), []store.Step{
			{Number: 1, Title: "the first"},
			{Number: 2, Title: "the second", After: 1},
		}); err != nil {
			t.Fatalf("SetPath: %v", err)
		}
		if _, err := s.TakeStep(ctx, project.GetId(), 1, "session-one"); err != nil {
			t.Fatalf("TakeStep on step 1: %v", err)
		}
		if _, err := s.TakeStep(ctx, project.GetId(), 2, "session-two"); err != nil {
			t.Fatalf("TakeStep on step 2: %v", err)
		}
		read, err := s.ListSteps(ctx, project.GetId())
		if err != nil {
			t.Fatalf("ListSteps: %v", err)
		}
		for at, want := range []string{"session-one", "session-two"} {
			if read[at].GetState() != store.StepTaken || read[at].GetSession() != want {
				t.Errorf("step %d reads as %q held by %q, want taken by %q",
					read[at].GetNumber(), read[at].GetState(), read[at].GetSession(), want)
			}
		}
	})

	t.Run("taking a step nothing holds is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house-bills")

		if _, err := s.SetPath(ctx, project.GetId(), []store.Step{{Number: 1, Title: "the first"}}); err != nil {
			t.Fatalf("SetPath: %v", err)
		}
		if _, err := s.TakeStep(ctx, project.GetId(), 7, "session-one"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("TakeStep on a step nobody wrote answered %v, want ErrNotFound", err)
		}
		if _, err := s.TakeStep(ctx, "no-such-project", 1, "session-one"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("TakeStep on a missing project answered %v, want ErrNotFound", err)
		}
	})
}
