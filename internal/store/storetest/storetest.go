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
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/deploy"
	"github.com/atlantic-blue/krewe/internal/flow"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/model"
	"github.com/atlantic-blue/krewe/internal/skill"
	"github.com/atlantic-blue/krewe/internal/store"
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
	// quietly kept the column default would keep the suite green and run every real task in the wrong
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
		// The task count travels with the text. Kept apart they drift, and a description that says it
		// is current when it is not is worse than one that admits it is old.
		if described.GetDescribedAtTask() != 3 {
			t.Fatalf("it was described at task %d, want 3", described.GetDescribedAtTask())
		}
	})

	t.Run("how many tasks a session has had", func(t *testing.T) {
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

		count, err := s.CountTasks(ctx, session.GetId())
		if err != nil || count != 0 {
			t.Fatalf("a session nobody has spoken in has %d tasks (%v)", count, err)
		}

		for at := range 3 {
			task := &quaycrewv1.Task{
				Id: fmt.Sprintf("counted-task-%d", at), Session: session.GetId(),
				Prompt: "hello", Reply: "ok", OccurredAt: timestamppb.Now(),
			}
			if err := s.AppendTask(ctx, task, project.GetWorkspace(), project.GetId(), "session-a"); err != nil {
				t.Fatalf("AppendTask: %v", err)
			}
		}

		count, err = s.CountTasks(ctx, session.GetId())
		if err != nil || count != 3 {
			t.Fatalf("the session has %d tasks (%v), want 3", count, err)
		}
		// Counted per session, not per system: a busy neighbour must not make this one look described.
		count, err = s.CountTasks(ctx, other.GetId())
		if err != nil || count != 0 {
			t.Fatalf("the other session has %d tasks (%v), want 0", count, err)
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

	t.Run("a task records the conversation handle", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		if err := s.RecordTask(ctx, session.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordTask: %v", err)
		}
		got, err := s.GetSession(ctx, session.GetId())
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.GetModelSessionId() != "conversation-1" {
			t.Fatalf("conversation handle is %q, want conversation-1", got.GetModelSessionId())
		}
	})

	t.Run("a failed task does not erase the conversation handle", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		if err := s.RecordTask(ctx, session.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordTask: %v", err)
		}
		// A failed task has no handle to report. The stored one points at a conversation that still
		// exists, so it must survive.
		if err := s.RecordTask(ctx, session.GetId(), "", "failed"); err != nil {
			t.Fatalf("RecordTask after failure: %v", err)
		}

		got, _ := s.GetSession(ctx, session.GetId())
		if got.GetModelSessionId() != "conversation-1" {
			t.Fatalf("a failed task erased the handle: it is now %q", got.GetModelSessionId())
		}
		if got.GetStatus() != "failed" {
			t.Fatalf("status is %q, want failed", got.GetStatus())
		}
	})

	t.Run("a task on a session that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		if err := s.RecordTask(context.Background(), "ghost", "conversation-1", "idle"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("RecordTask on a missing session returned %v, want ErrNotFound", err)
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
		if err := s.RecordTask(ctx, session.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordTask: %v", err)
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
		if err := s.RecordTask(ctx, kept.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordTask: %v", err)
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
		if err := s.RecordTask(ctx, early.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordTask: %v", err)
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
	// started to plan something that quietly went back to editing files on the next task would be
	// worse than never having the setting.
	t.Run("a session keeps the permission mode it was given", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		if got := session.GetPermissionMode(); got != "acceptEdits" {
			t.Fatalf("a new session runs as %q, want acceptEdits, which is what every task has run as", got)
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

	t.Run("a session's tasks come back in the order they happened", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		start := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
		for i, text := range []string{"first", "second", "third"} {
			task := &quaycrewv1.Task{
				Id:         fmt.Sprintf("task-%d", i),
				Session:    session.GetId(),
				Prompt:     text,
				Reply:      "you said: " + text,
				Status:     "idle",
				OccurredAt: timestamppb.New(start.Add(time.Duration(i) * time.Minute)),
			}
			if err := s.AppendTask(ctx, task, project.GetWorkspace(), project.GetId(), "session-a"); err != nil {
				t.Fatalf("AppendTask: %v", err)
			}
		}

		tasks, err := s.ListTasks(ctx, session.GetId(), 0)
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		if len(tasks) != 3 {
			t.Fatalf("%d tasks came back, want 3", len(tasks))
		}
		for i, want := range []string{"first", "second", "third"} {
			if tasks[i].GetPrompt() != want {
				t.Fatalf("task %d says %q, want %q: the history is out of order", i, tasks[i].GetPrompt(), want)
			}
		}
		if tasks[0].GetReply() != "you said: first" || tasks[0].GetStatus() != "idle" {
			t.Fatalf("the first task came back as %+v, losing what it said", tasks[0])
		}
	})

	t.Run("a task written when it started is closed by what came of it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		running := &quaycrewv1.Task{
			Id: "task-open", Session: session.GetId(), Prompt: "read the repository",
			Status: "running", OccurredAt: timestamppb.New(time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)),
		}
		if err := s.AppendTask(ctx, running, project.GetWorkspace(), project.GetId(), "session-a"); err != nil {
			t.Fatalf("AppendTask: %v", err)
		}
		if err := s.FinishTask(ctx, "task-open", "idle", "it is a control plane", ""); err != nil {
			t.Fatalf("FinishTask: %v", err)
		}

		tasks, err := s.ListTasks(ctx, session.GetId(), 0)
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		// One task, not two: the same record, closed.
		if len(tasks) != 1 {
			t.Fatalf("%d tasks came back, want 1", len(tasks))
		}
		if tasks[0].GetStatus() != "idle" || tasks[0].GetReply() != "it is a control plane" {
			t.Fatalf("the task came back as %+v, so finishing it did not land", tasks[0])
		}
		// What the operator was asked is still there. Closing a task must not lose it.
		if tasks[0].GetPrompt() != "read the repository" {
			t.Fatalf("the task says %q was asked, want %q", tasks[0].GetPrompt(), "read the repository")
		}
	})

	t.Run("finishing a task the store does not hold is not an error", func(t *testing.T) {
		s := newDataset(t)(t)

		// The task happened whatever the store holds, so this must not come back as a failure of it.
		if err := s.FinishTask(context.Background(), "task-nobody-wrote", "idle", "done", ""); err != nil {
			t.Fatalf("FinishTask on a task nobody wrote: %v", err)
		}
	})

	t.Run("the same task delivered twice is stored once", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		// Delivery from the event log is at least once, so this is not a hypothetical.
		task := &quaycrewv1.Task{
			Id: "task-once", Session: session.GetId(), Prompt: "hello",
			Status: "idle", OccurredAt: timestamppb.New(time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)),
		}
		for range 3 {
			if err := s.AppendTask(ctx, task, project.GetWorkspace(), project.GetId(), "session-a"); err != nil {
				t.Fatalf("AppendTask: %v", err)
			}
		}

		tasks, err := s.ListTasks(ctx, session.GetId(), 0)
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		if len(tasks) != 1 {
			t.Fatalf("%d tasks came back, want 1: a replayed record was written again", len(tasks))
		}
	})

	t.Run("a task with no id is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		err := s.AppendTask(ctx, &quaycrewv1.Task{Session: session.GetId(), Prompt: "hello"},
			project.GetWorkspace(), project.GetId(), "session-a")
		if err == nil {
			t.Fatal("a task with no id was accepted, so nothing can recognise it on a replay")
		}
	})

	t.Run("a listing keeps the end of a long conversation", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})

		start := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
		for i := range 5 {
			task := &quaycrewv1.Task{
				Id: fmt.Sprintf("task-%d", i), Session: session.GetId(),
				Prompt: fmt.Sprintf("message %d", i), Status: "idle",
				OccurredAt: timestamppb.New(start.Add(time.Duration(i) * time.Minute)),
			}
			if err := s.AppendTask(ctx, task, project.GetWorkspace(), project.GetId(), "session-a"); err != nil {
				t.Fatalf("AppendTask: %v", err)
			}
		}

		tasks, err := s.ListTasks(ctx, session.GetId(), 2)
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		if len(tasks) != 2 {
			t.Fatalf("%d tasks came back, want 2", len(tasks))
		}
		if tasks[0].GetPrompt() != "message 3" || tasks[1].GetPrompt() != "message 4" {
			t.Fatalf("the listing kept %q and %q, want the last two: a cap must keep the end", tasks[0].GetPrompt(), tasks[1].GetPrompt())
		}
	})

	t.Run("one session's tasks are not another's", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		first, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		second, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-b", store.Birth{})

		now := timestamppb.New(time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC))
		if err := s.AppendTask(ctx, &quaycrewv1.Task{Id: "a", Session: first.GetId(), Prompt: "mine", OccurredAt: now},
			project.GetWorkspace(), project.GetId(), "session-a"); err != nil {
			t.Fatalf("AppendTask: %v", err)
		}
		if err := s.AppendTask(ctx, &quaycrewv1.Task{Id: "b", Session: second.GetId(), Prompt: "theirs", OccurredAt: now},
			project.GetWorkspace(), project.GetId(), "session-b"); err != nil {
			t.Fatalf("AppendTask: %v", err)
		}

		tasks, err := s.ListTasks(ctx, first.GetId(), 0)
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		if len(tasks) != 1 || tasks[0].GetPrompt() != "mine" {
			t.Fatalf("the first session's history came back as %d tasks starting %q", len(tasks), tasks[0].GetPrompt())
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
				"the task instead of doing it", got, model.PermissionBypass)
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
		if err := before.RecordTask(ctx, session.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordTask: %v", err)
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

		// The same session must still resolve to the same session, which is what lets the next task
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

	t.Run("a flow graph is pinned per version and the newest is served", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		if err := s.ImportFlowGraph(ctx, "fix-red", 1, "version one"); err != nil {
			t.Fatalf("ImportFlowGraph: %v", err)
		}
		if err := s.ImportFlowGraph(ctx, "fix-red", 1, "changed"); err == nil {
			t.Fatal("a version was imported twice, so a run's pin can change under it")
		}
		if err := s.ImportFlowGraph(ctx, "fix-red", 2, "version two"); err != nil {
			t.Fatalf("ImportFlowGraph the second version: %v", err)
		}
		version, definition, err := s.LatestFlowGraph(ctx, "fix-red")
		if err != nil || version != 2 || definition != "version two" {
			t.Fatalf("LatestFlowGraph gave version %d %q (%v), want 2 %q", version, definition, err, "version two")
		}
		if _, _, err := s.LatestFlowGraph(ctx, "never-imported"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("a graph nobody imported answered %v, want not found", err)
		}
	})

	t.Run("a flow run moves with its record and its dispatch claim in one breath", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		project, err := s.CreateProject(ctx, workspace.GetId(), "house-bills")
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		if err := s.ImportFlowGraph(ctx, "fix-red", 1, "the definition"); err != nil {
			t.Fatalf("ImportFlowGraph: %v", err)
		}
		run := &flow.Run{
			ID: "run-1", Workspace: workspace.GetId(), Project: project.GetId(),
			GraphName: "fix-red", GraphVersion: 1, Node: "", Status: flow.StatusRunning,
			State: map[string]string{}, Attempts: map[string]int{},
		}
		carrier, records := carrierFor(run)
		if err := s.CreateFlowRun(ctx, run, carrier, records, ""); err != nil {
			t.Fatalf("CreateFlowRun: %v", err)
		}

		run.Node, run.Attempts = "fix", map[string]int{"fix": 1}
		dispatch := &flow.Command{Kind: flow.CommandDispatch, Node: "fix", Attempt: 1, Prompt: "fix it"}
		if err := s.AdvanceFlowRun(ctx, run, flow.Transition{Event: flow.EventStarted, Node: "fix", Dispatch: dispatch}); err != nil {
			t.Fatalf("AdvanceFlowRun: %v", err)
		}

		// The same key again refuses the whole movement: no second claim, no second record, and the
		// run row stays where it was, because a duplicate dispatch is a task paid for twice.
		moved := *run
		moved.Node = "somewhere-else"
		if err := s.AdvanceFlowRun(ctx, &moved, flow.Transition{Event: flow.EventStarted, Node: "somewhere-else", Dispatch: dispatch}); err == nil {
			t.Fatal("the same dispatch key was claimed twice")
		}
		kept, err := s.GetFlowRun(ctx, "run-1")
		if err != nil {
			t.Fatalf("GetFlowRun: %v", err)
		}
		if kept.Node != "fix" || kept.Status != flow.StatusRunning || kept.Attempts["fix"] != 1 {
			t.Fatalf("after the refused movement the run reads %+v, want it unmoved on fix", kept)
		}
		transitions, err := s.ListFlowTransitions(ctx, "run-1")
		if err != nil {
			t.Fatalf("ListFlowTransitions: %v", err)
		}
		if len(transitions) != 1 || transitions[0].Node != "fix" || transitions[0].Event != flow.EventStarted {
			t.Fatalf("the transitions read back as %+v, want exactly the one movement that happened", transitions)
		}
	})

	t.Run("a stopped flow run keeps what it spent and why it stopped", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		project, err := s.CreateProject(ctx, workspace.GetId(), "house-bills")
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		if err := s.ImportFlowGraph(ctx, "loop", 1, "the definition"); err != nil {
			t.Fatalf("ImportFlowGraph: %v", err)
		}
		run := &flow.Run{
			ID: "run-stopped", Workspace: workspace.GetId(), Project: project.GetId(),
			GraphName: "loop", GraphVersion: 1, Status: flow.StatusRunning,
			State: map[string]string{}, Attempts: map[string]int{},
		}
		carrier, records := carrierFor(run)
		if err := s.CreateFlowRun(ctx, run, carrier, records, ""); err != nil {
			t.Fatalf("CreateFlowRun: %v", err)
		}

		run.Status, run.Reason = flow.StatusStopped, "stopped after 5 transitions"
		run.Transitions, run.Spent = 5, 1_724_656
		if err := s.AdvanceFlowRun(ctx, run, flow.Transition{Event: flow.EventTaskFinished, Node: "more"}); err != nil {
			t.Fatalf("AdvanceFlowRun: %v", err)
		}

		// Read back through both roads: a run that reads as running in one and stopped in the other
		// would have the console and the command line disagreeing about whether job is happening.
		kept, err := s.GetFlowRun(ctx, "run-stopped")
		if err != nil {
			t.Fatalf("GetFlowRun: %v", err)
		}
		if kept.Status != flow.StatusStopped || kept.Reason == "" {
			t.Fatalf("the run reads back as %q saying %q, want stopped with its reason", kept.Status, kept.Reason)
		}
		if kept.Transitions != 5 || kept.Spent != 1_724_656 {
			t.Fatalf("the run reads back with %d transitions and %d spent, want 5 and 1724656", kept.Transitions, kept.Spent)
		}
		listed, err := s.ListFlowRuns(ctx, project.GetId())
		if err != nil || len(listed) != 1 {
			t.Fatalf("ListFlowRuns gave %d runs (%v), want the one", len(listed), err)
		}
		if listed[0].Status != flow.StatusStopped || listed[0].Reason != kept.Reason {
			t.Fatalf("the listing says %q %q while reading it says %q %q",
				listed[0].Status, listed[0].Reason, kept.Status, kept.Reason)
		}
	})

	t.Run("stopping a run halts it, and the engine's next movement is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		project, err := s.CreateProject(ctx, workspace.GetId(), "house-bills")
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		if err := s.ImportFlowGraph(ctx, "loop", 1, "the definition"); err != nil {
			t.Fatalf("ImportFlowGraph: %v", err)
		}
		run := &flow.Run{
			ID: "run-halt", Workspace: workspace.GetId(), Project: project.GetId(),
			GraphName: "loop", GraphVersion: 1, Node: "begin", Status: flow.StatusRunning,
			State: map[string]string{}, Attempts: map[string]int{},
		}
		carrier, records := carrierFor(run)
		if err := s.CreateFlowRun(ctx, run, carrier, records, ""); err != nil {
			t.Fatalf("CreateFlowRun: %v", err)
		}

		stopped, err := s.StopFlowRun(ctx, "run-halt", "the operator said so")
		if err != nil {
			t.Fatalf("StopFlowRun: %v", err)
		}
		if stopped.Status != flow.StatusStopped || stopped.Reason != "the operator said so" {
			t.Fatalf("the stopped run reads %q %q", stopped.Status, stopped.Reason)
		}

		// The engine was mid task and writes next. It must be refused rather than setting the run
		// back to running, which is the whole of what makes a stop take effect.
		run.Node = "again"
		if err := s.AdvanceFlowRun(ctx, run, flow.Transition{Event: flow.EventTaskFinished, Node: "again"}); !errors.Is(err, flow.ErrRunHalted) {
			t.Fatalf("moving a stopped run answered %v, want it refused as halted", err)
		}
		kept, err := s.GetFlowRun(ctx, "run-halt")
		if err != nil {
			t.Fatalf("GetFlowRun: %v", err)
		}
		if kept.Status != flow.StatusStopped || kept.Node != "begin" {
			t.Fatalf("after the refused movement the run reads %q on %q, want it stopped where it was", kept.Status, kept.Node)
		}

		// And a run that already ended is not stopped a second time: how it ended is the useful part.
		if _, err := s.StopFlowRun(ctx, "run-halt", "again"); err == nil {
			t.Fatal("a run that already ended was stopped again")
		}
		if _, err := s.StopFlowRun(ctx, "no-such-run", "whatever"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("stopping a run nobody started answered %v, want not found", err)
		}
	})

	t.Run("a waiting flow run is due, is carried on, and keeps its pinned version", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		project, err := s.CreateProject(ctx, workspace.GetId(), "house-bills")
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		if err := s.ImportFlowGraph(ctx, "patient", 1, "version one"); err != nil {
			t.Fatalf("ImportFlowGraph: %v", err)
		}
		if err := s.ImportFlowGraph(ctx, "patient", 2, "version two"); err != nil {
			t.Fatalf("ImportFlowGraph the second: %v", err)
		}
		// The version a run pinned is readable while a newer one exists, or a graph edited during
		// a wait would change what the run does when it wakes.
		if definition, err := s.FlowGraph(ctx, "patient", 1); err != nil || definition != "version one" {
			t.Fatalf("FlowGraph gave %q (%v), want the pinned version", definition, err)
		}

		run := &flow.Run{
			ID: "run-wait", Workspace: workspace.GetId(), Project: project.GetId(),
			GraphName: "patient", GraphVersion: 1, Node: "ask", Status: flow.StatusRunning,
			State: map[string]string{}, Attempts: map[string]int{},
		}
		carrier, records := carrierFor(run)
		if err := s.CreateFlowRun(ctx, run, carrier, records, ""); err != nil {
			t.Fatalf("CreateFlowRun: %v", err)
		}

		due := time.Now().UTC().Add(-time.Minute)
		run.Node, run.Status = "pause", flow.StatusWaiting
		if err := s.AdvanceFlowRun(ctx, run, flow.Transition{
			Event: flow.EventTaskFinished, Node: "pause", Due: &due,
		}); err != nil {
			t.Fatalf("AdvanceFlowRun into the wait: %v", err)
		}

		overdue, err := s.DueFlowRuns(ctx, time.Now().UTC())
		if err != nil {
			t.Fatalf("DueFlowRuns: %v", err)
		}
		if len(overdue) != 1 || overdue[0].ID != "run-wait" {
			t.Fatalf("%d runs are due, want the one waiting", len(overdue))
		}

		// A wait still ahead of us is not due, or every waiting run would be resumed at once.
		notYet, err := s.DueFlowRuns(ctx, time.Now().UTC().Add(-time.Hour))
		if err != nil {
			t.Fatalf("DueFlowRuns before it was due: %v", err)
		}
		if len(notYet) != 0 {
			t.Fatalf("%d runs are due an hour before their time", len(notYet))
		}

		// A waiting run is live, so the poller can carry it on. A store that refused this would
		// leave every wait stuck forever, which is a whole feature dead with a green suite.
		run.Node, run.Status = "check", flow.StatusRunning
		if err := s.AdvanceFlowRun(ctx, run, flow.Transition{Event: flow.EventDue, Node: "check"}); err != nil {
			t.Fatalf("carrying a waiting run on: %v", err)
		}
		kept, err := s.GetFlowRun(ctx, "run-wait")
		if err != nil {
			t.Fatalf("GetFlowRun: %v", err)
		}
		if kept.Status != flow.StatusRunning || kept.Node != "check" {
			t.Fatalf("the resumed run reads %q on %q", kept.Status, kept.Node)
		}
		if kept.DueAt != nil {
			t.Fatalf("the resumed run still carries a due time, so the poller would pick it up again")
		}
	})

	t.Run("a run waiting on a person is never due, so no timer answers a question", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		project, err := s.CreateProject(ctx, workspace.GetId(), "house-bills")
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		if err := s.ImportFlowGraph(ctx, "careful", 1, "the definition"); err != nil {
			t.Fatalf("ImportFlowGraph: %v", err)
		}
		run := &flow.Run{
			ID: "run-ask", Workspace: workspace.GetId(), Project: project.GetId(),
			GraphName: "careful", GraphVersion: 1, Node: "fix", Status: flow.StatusRunning,
			State: map[string]string{}, Attempts: map[string]int{},
		}
		carrier, records := carrierFor(run)
		if err := s.CreateFlowRun(ctx, run, carrier, records, ""); err != nil {
			t.Fatalf("CreateFlowRun: %v", err)
		}

		// An overdue time on an asking run, which is the arrangement that would go wrong: the
		// poller must pass it over on the status alone, or an automation nobody answered would
		// take silence for a yes and do the thing it was asking permission for.
		overdue := time.Now().UTC().Add(-time.Hour)
		run.Node, run.Status, run.Question = "permit", flow.StatusAsking, "push?"
		if err := s.AdvanceFlowRun(ctx, run, flow.Transition{
			Event: flow.EventTaskFinished, Node: "permit", Due: &overdue,
		}); err != nil {
			t.Fatalf("AdvanceFlowRun into the ask: %v", err)
		}

		due, err := s.DueFlowRuns(ctx, time.Now().UTC())
		if err != nil {
			t.Fatalf("DueFlowRuns: %v", err)
		}
		for _, one := range due {
			if one.ID == "run-ask" {
				t.Fatal("a run waiting on a person came back as due, so a timer would answer its question")
			}
		}

		// And the question survives the round trip, or the operator is asked nothing.
		kept, err := s.GetFlowRun(ctx, "run-ask")
		if err != nil {
			t.Fatalf("GetFlowRun: %v", err)
		}
		if kept.Status != flow.StatusAsking || kept.Question != "push?" {
			t.Fatalf("the asking run reads back as %q asking %q", kept.Status, kept.Question)
		}
	})

	// A flow run is carried by a job, and every step it takes is a job under
	// that one. Both stores have to write the run, the job and the record of it together, because a
	// step written without its movement is paid for twice and a movement written without its step is a
	// run waiting on something nobody declared.
	t.Run("a run hangs under a job, and its step is another under that", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		if err := s.ImportFlowGraph(ctx, "fix-red", 1, "the definition"); err != nil {
			t.Fatalf("ImportFlowGraph: %v", err)
		}
		run := &flow.Run{
			ID: "run-carried", Workspace: workspace, Project: project,
			GraphName: "fix-red", GraphVersion: 1, Status: flow.StatusRunning,
			State: map[string]string{}, Attempts: map[string]int{},
		}
		carrier, records := carrierFor(run)
		if err := s.CreateFlowRun(ctx, run, carrier, records, ""); err != nil {
			t.Fatalf("CreateFlowRun: %v", err)
		}

		// The run knows where it sits in the tree, off the row rather than out of a process.
		read, err := s.FlowRunCarrier(ctx, run.ID)
		if err != nil {
			t.Fatalf("FlowRunCarrier: %v", err)
		}
		if read != carrier.ID {
			t.Fatalf("the run is carried by %q, want %q", read, carrier.ID)
		}
		if _, err := s.GetJob(ctx, carrier.ID); err != nil {
			t.Fatalf("the job carrying the run is not there: %v", err)
		}
		// And the record of the run starting landed with it.
		history, err := s.ListJobEvents(ctx, carrier.ID)
		if err != nil || len(history) != len(records) {
			t.Fatalf("the run's own job has %d records (%v), want %d", len(history), err, len(records))
		}

		// One movement: the run goes to a dispatch node, the step is written down under it, and the
		// run's own job is moved to say it is out with something.
		step := &job.Job{
			ID: "step-fix", Workspace: workspace, Project: project,
			Title: "fix-red step fix", Brief: "fix the build",
			Parent: carrier.ID, Depth: carrier.Depth + 1, Version: 1, Phase: job.PhasePending,
			Labels: map[string]string{"flow.run": run.ID, "flow.node": "fix"},
		}
		run.Node, run.Status, run.Attempts = "fix", flow.StatusWorking, map[string]int{"fix": 1}
		if err := s.AdvanceFlowRun(ctx, run, flow.Transition{
			Event: flow.EventStarted, Node: "fix",
			Dispatch: &flow.Command{Kind: flow.CommandDispatch, Node: "fix", Attempt: 1, Prompt: "fix the build"},
			Job: flow.JobWrite{
				Declared: step,
				Carrier:  &flow.Carrier{Job: carrier.ID, Phase: job.PhaseWaiting},
				Records:  []*job.Event{declaredEvent(step)},
			},
		}); err != nil {
			t.Fatalf("AdvanceFlowRun: %v", err)
		}
		written, err := s.GetJob(ctx, step.ID)
		if err != nil {
			t.Fatalf("the step was not written with the movement: %v", err)
		}
		if written.Parent != carrier.ID || written.Depth != carrier.Depth+1 {
			t.Fatalf("the step hangs under %q at depth %d, want under the run one deeper", written.Parent, written.Depth)
		}

		// Nothing to carry on yet: the step has not ended.
		landed, err := s.LandedFlowSteps(ctx, 0)
		if err != nil {
			t.Fatalf("LandedFlowSteps: %v", err)
		}
		if len(landed) != 0 {
			t.Fatalf("%d runs read as having a step that ended while the step is pending", len(landed))
		}

		// The step ends, through the same calls a controller makes.
		if _, err := s.StartJob(ctx, step.ID, job.Lease{Owner: "a-controller", Until: time.Now().UTC().Add(time.Minute)},
			[]*job.Event{declaredEvent(step)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		if _, err := s.LandJob(ctx, step.ID, job.Landing{Phase: job.PhaseDone, Answer: "fixed it"},
			declaredEvent(step)); err != nil {
			t.Fatalf("LandJob: %v", err)
		}

		landed, err = s.LandedFlowSteps(ctx, 0)
		if err != nil {
			t.Fatalf("LandedFlowSteps: %v", err)
		}
		if len(landed) != 1 || landed[0].Run.ID != run.ID || landed[0].Step.ID != step.ID {
			t.Fatalf("the runs with a step that ended are %+v, want the one", landed)
		}
		if landed[0].Step.Answer != "fixed it" {
			t.Fatalf("the step comes back answering %q", landed[0].Step.Answer)
		}

		// The movement that answers a step applies only to a run still out with it, so two pollers
		// reading one landed step move the run once.
		run.Node, run.Status = "done", flow.StatusDone
		answering := flow.Transition{
			Event: flow.EventTaskFinished, Node: "done", Answers: step.ID,
			Job: flow.JobWrite{Carrier: &flow.Carrier{
				Job: carrier.ID, Phase: job.PhaseDone, Answer: "fixed it",
			}},
		}
		if err := s.AdvanceFlowRun(ctx, run, answering); err != nil {
			t.Fatalf("AdvanceFlowRun: %v", err)
		}
		if err := s.AdvanceFlowRun(ctx, run, answering); !errors.Is(err, flow.ErrRunHalted) {
			t.Fatalf("the same landed step moved the run twice: %v", err)
		}
		// And the run's own job says what the run came to, so a person reads one record.
		ended, err := s.GetJob(ctx, carrier.ID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if ended.Phase != job.PhaseDone || ended.Answer != "fixed it" {
			t.Fatalf("the run's own job is %q answering %q, want done with the run's answer", ended.Phase, ended.Answer)
		}
		if ended.FinishedAt == nil {
			t.Fatal("the run's own job ended and carries no finishing time")
		}
	})

	t.Run("a schedule comes due, moves on, and can be taken away", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, err := s.CreateWorkspace(ctx, "acme")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}
		project, err := s.CreateProject(ctx, workspace.GetId(), "house-bills")
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}

		// Due an hour ago, which is the arrangement the poller reads.
		overdue := time.Now().UTC().Add(-time.Hour)
		if err := s.ScheduleFlow(ctx, "nightly", project.GetId(), 24*time.Hour, overdue); err != nil {
			t.Fatalf("ScheduleFlow: %v", err)
		}
		due, err := s.DueFlowSchedules(ctx, time.Now().UTC())
		if err != nil {
			t.Fatalf("DueFlowSchedules: %v", err)
		}
		if len(due) != 1 || due[0].GraphName != "nightly" || due[0].Every != 24*time.Hour {
			t.Fatalf("%d schedules are due (%+v), want the one", len(due), due)
		}
		// The workspace travels with it, because starting a run needs both.
		if due[0].Workspace != workspace.GetId() {
			t.Fatalf("the due schedule names workspace %q, want %q", due[0].Workspace, workspace.GetId())
		}

		// Moved on, it is no longer due, which is what keeps one broken graph from firing on every
		// tick for as long as it stays broken.
		if err := s.MarkFlowScheduled(ctx, "nightly", project.GetId(), time.Now().UTC().Add(24*time.Hour)); err != nil {
			t.Fatalf("MarkFlowScheduled: %v", err)
		}
		if again, err := s.DueFlowSchedules(ctx, time.Now().UTC()); err != nil || len(again) != 0 {
			t.Fatalf("%d schedules are due after moving on (%v), want none", len(again), err)
		}

		// Scheduling the same pair again moves it rather than making a second one, so importing a
		// graph twice does not double the rate it runs at.
		if err := s.ScheduleFlow(ctx, "nightly", project.GetId(), 24*time.Hour, overdue); err != nil {
			t.Fatalf("ScheduleFlow again: %v", err)
		}
		if again, err := s.DueFlowSchedules(ctx, time.Now().UTC()); err != nil || len(again) != 1 {
			t.Fatalf("%d schedules are due after rescheduling (%v), want exactly one", len(again), err)
		}

		if err := s.UnscheduleFlow(ctx, "nightly", project.GetId()); err != nil {
			t.Fatalf("UnscheduleFlow: %v", err)
		}
		if gone, err := s.DueFlowSchedules(ctx, time.Now().UTC()); err != nil || len(gone) != 0 {
			t.Fatalf("%d schedules are due after unscheduling (%v), want none", len(gone), err)
		}
		if err := s.UnscheduleFlow(ctx, "nightly", project.GetId()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("unscheduling what is not scheduled answered %v, want not found", err)
		}
	})

	t.Run("a flow run that does not exist cannot move and cannot be read", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		if _, err := s.GetFlowRun(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("a run nobody made answered %v, want not found", err)
		}
		ghost := &flow.Run{ID: "ghost", Status: flow.StatusRunning, State: map[string]string{}, Attempts: map[string]int{}}
		if err := s.AdvanceFlowRun(ctx, ghost, flow.Transition{Event: flow.EventStarted, Node: "a"}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("moving a run nobody made answered %v, want not found", err)
		}
	})

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
	runRoleConformance(t, newDataset)
	runJobConformance(t, newDataset)
	runHistoryConformance(t, newDataset)
	runSteerConformance(t, newDataset)
	runJobControllerConformance(t, newDataset)
	runJobLeaseConformance(t, newDataset)
	runJobAskingConformance(t, newDataset)
	runJobClaimConformance(t, newDataset)
	runJobResumeConformance(t, newDataset)
	runJobHandoffConformance(t, newDataset)
	runWorkspaceLimitsConformance(t, newDataset)
	runSessionLifecycleConformance(t, newDataset)
	runTriggerConformance(t, newDataset)
}

// carrierFor is the job that carries a run, the way the flow engine writes one. A run
// hangs inside the job tree rather than beside it, so a store that writes a run without one is a
// store the engine cannot use.
func carrierFor(run *flow.Run) (*job.Job, []*job.Event) {
	carrier := &job.Job{
		ID: "carrier-" + run.ID, Workspace: run.Workspace, Project: run.Project,
		Title: "flow " + run.GraphName, Brief: "carries the run of " + run.GraphName,
		Version: 1, Phase: job.PhaseWaiting, TraceID: "trace-" + run.ID,
	}
	return carrier, []*job.Event{{
		ID: "declared-" + run.ID, Kind: job.EventDeclared, Job: carrier.ID,
		Workspace: run.Workspace, Project: run.Project, OccurredAt: time.Now().UTC(),
	}}
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
