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

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/flow"
	"github.com/atlantic-blue/quay-crew/internal/model"
	"github.com/atlantic-blue/quay-crew/internal/skill"
	"github.com/atlantic-blue/quay-crew/internal/store"
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

	t.Run("what a sandbox was born holding is recorded, and closing the sandbox clears it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-a")

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

	// The mode belongs to the thread, so it has to survive everything the thread survives. A thread
	// started to plan something that quietly went back to editing files on the next turn would be
	// worse than never having the setting.
	t.Run("a thread keeps the permission mode it was given", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-a")

		if got := session.GetPermissionMode(); got != "acceptEdits" {
			t.Fatalf("a new thread runs as %q, want acceptEdits, which is what every turn has run as", got)
		}
		if err := s.SetPermissionMode(ctx, session.GetId(), "bypassPermissions"); err != nil {
			t.Fatalf("SetPermissionMode: %v", err)
		}

		got, _ := s.GetSession(ctx, session.GetId())
		if got.GetPermissionMode() != "bypassPermissions" {
			t.Fatalf("the thread runs as %q, want bypassPermissions", got.GetPermissionMode())
		}
		// And the thread it was set on, not every thread in the project.
		other, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-b")
		if other.GetPermissionMode() != "acceptEdits" {
			t.Fatalf("another thread runs as %q, want it untouched", other.GetPermissionMode())
		}
		if err := s.SetPermissionMode(ctx, "ghost", "plan"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("setting the mode on a missing session returned %v, want ErrNotFound", err)
		}
	})

	t.Run("a session's turns come back in the order they happened", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-a")

		start := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
		for i, text := range []string{"first", "second", "third"} {
			turn := &quaycrewv1.Turn{
				Id:         fmt.Sprintf("turn-%d", i),
				Thread:     session.GetId(),
				Prompt:     text,
				Reply:      "you said: " + text,
				Status:     "idle",
				OccurredAt: timestamppb.New(start.Add(time.Duration(i) * time.Minute)),
			}
			if err := s.AppendTurn(ctx, turn, project.GetWorkspace(), project.GetId(), "thread-a"); err != nil {
				t.Fatalf("AppendTurn: %v", err)
			}
		}

		turns, err := s.ListTurns(ctx, session.GetId(), 0)
		if err != nil {
			t.Fatalf("ListTurns: %v", err)
		}
		if len(turns) != 3 {
			t.Fatalf("%d turns came back, want 3", len(turns))
		}
		for i, want := range []string{"first", "second", "third"} {
			if turns[i].GetPrompt() != want {
				t.Fatalf("turn %d says %q, want %q: the history is out of order", i, turns[i].GetPrompt(), want)
			}
		}
		if turns[0].GetReply() != "you said: first" || turns[0].GetStatus() != "idle" {
			t.Fatalf("the first turn came back as %+v, losing what it said", turns[0])
		}
	})

	t.Run("the same turn delivered twice is stored once", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-a")

		// Delivery from the event log is at least once, so this is not a hypothetical.
		turn := &quaycrewv1.Turn{
			Id: "turn-once", Thread: session.GetId(), Prompt: "hello",
			Status: "idle", OccurredAt: timestamppb.New(time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)),
		}
		for range 3 {
			if err := s.AppendTurn(ctx, turn, project.GetWorkspace(), project.GetId(), "thread-a"); err != nil {
				t.Fatalf("AppendTurn: %v", err)
			}
		}

		turns, err := s.ListTurns(ctx, session.GetId(), 0)
		if err != nil {
			t.Fatalf("ListTurns: %v", err)
		}
		if len(turns) != 1 {
			t.Fatalf("%d turns came back, want 1: a replayed record was written again", len(turns))
		}
	})

	t.Run("a turn with no id is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-a")

		err := s.AppendTurn(ctx, &quaycrewv1.Turn{Thread: session.GetId(), Prompt: "hello"},
			project.GetWorkspace(), project.GetId(), "thread-a")
		if err == nil {
			t.Fatal("a turn with no id was accepted, so nothing can recognise it on a replay")
		}
	})

	t.Run("a listing keeps the end of a long conversation", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-a")

		start := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
		for i := range 5 {
			turn := &quaycrewv1.Turn{
				Id: fmt.Sprintf("turn-%d", i), Thread: session.GetId(),
				Prompt: fmt.Sprintf("message %d", i), Status: "idle",
				OccurredAt: timestamppb.New(start.Add(time.Duration(i) * time.Minute)),
			}
			if err := s.AppendTurn(ctx, turn, project.GetWorkspace(), project.GetId(), "thread-a"); err != nil {
				t.Fatalf("AppendTurn: %v", err)
			}
		}

		turns, err := s.ListTurns(ctx, session.GetId(), 2)
		if err != nil {
			t.Fatalf("ListTurns: %v", err)
		}
		if len(turns) != 2 {
			t.Fatalf("%d turns came back, want 2", len(turns))
		}
		if turns[0].GetPrompt() != "message 3" || turns[1].GetPrompt() != "message 4" {
			t.Fatalf("the listing kept %q and %q, want the last two: a cap must keep the end", turns[0].GetPrompt(), turns[1].GetPrompt())
		}
	})

	t.Run("one session's turns are not another's", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		first, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-a")
		second, _ := s.FindOrCreateSession(ctx, project.GetId(), "thread-b")

		now := timestamppb.New(time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC))
		if err := s.AppendTurn(ctx, &quaycrewv1.Turn{Id: "a", Thread: first.GetId(), Prompt: "mine", OccurredAt: now},
			project.GetWorkspace(), project.GetId(), "thread-a"); err != nil {
			t.Fatalf("AppendTurn: %v", err)
		}
		if err := s.AppendTurn(ctx, &quaycrewv1.Turn{Id: "b", Thread: second.GetId(), Prompt: "theirs", OccurredAt: now},
			project.GetWorkspace(), project.GetId(), "thread-b"); err != nil {
			t.Fatalf("AppendTurn: %v", err)
		}

		turns, err := s.ListTurns(ctx, first.GetId(), 0)
		if err != nil {
			t.Fatalf("ListTurns: %v", err)
		}
		if len(turns) != 1 || turns[0].GetPrompt() != "mine" {
			t.Fatalf("the first session's history came back as %d turns starting %q", len(turns), turns[0].GetPrompt())
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
	// said which mode it starts in, so a crew on Postgres and a crew on memory disagreed about
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
			t.Fatal("the session made to drive the crew is not marked as the driver")
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
		if err := s.CreateFlowRun(ctx, run); err != nil {
			t.Fatalf("CreateFlowRun: %v", err)
		}

		run.Node, run.Attempts = "fix", map[string]int{"fix": 1}
		dispatch := &flow.Command{Kind: flow.CommandDispatch, Node: "fix", Attempt: 1, Prompt: "fix it"}
		if err := s.AdvanceFlowRun(ctx, run, flow.Transition{Event: flow.EventStarted, Node: "fix", Dispatch: dispatch}); err != nil {
			t.Fatalf("AdvanceFlowRun: %v", err)
		}

		// The same key again refuses the whole movement: no second claim, no second record, and the
		// run row stays where it was, because a duplicate dispatch is a turn paid for twice.
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
		if err := s.CreateFlowRun(ctx, run); err != nil {
			t.Fatalf("CreateFlowRun: %v", err)
		}

		run.Status, run.Reason = flow.StatusStopped, "stopped after 5 transitions"
		run.Transitions, run.Spent = 5, 1_724_656
		if err := s.AdvanceFlowRun(ctx, run, flow.Transition{Event: flow.EventTurnFinished, Node: "more"}); err != nil {
			t.Fatalf("AdvanceFlowRun: %v", err)
		}

		// Read back through both roads: a run that reads as running in one and stopped in the other
		// would have the console and the command line disagreeing about whether work is happening.
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
		if err := s.CreateFlowRun(ctx, run); err != nil {
			t.Fatalf("CreateFlowRun: %v", err)
		}

		stopped, err := s.StopFlowRun(ctx, "run-halt", "the operator said so")
		if err != nil {
			t.Fatalf("StopFlowRun: %v", err)
		}
		if stopped.Status != flow.StatusStopped || stopped.Reason != "the operator said so" {
			t.Fatalf("the stopped run reads %q %q", stopped.Status, stopped.Reason)
		}

		// The engine was mid turn and writes next. It must be refused rather than setting the run
		// back to running, which is the whole of what makes a stop take effect.
		run.Node = "again"
		if err := s.AdvanceFlowRun(ctx, run, flow.Transition{Event: flow.EventTurnFinished, Node: "again"}); !errors.Is(err, flow.ErrRunHalted) {
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
		if err := s.CreateFlowRun(ctx, run); err != nil {
			t.Fatalf("CreateFlowRun: %v", err)
		}

		due := time.Now().UTC().Add(-time.Minute)
		run.Node, run.Status = "pause", flow.StatusWaiting
		if err := s.AdvanceFlowRun(ctx, run, flow.Transition{
			Event: flow.EventTurnFinished, Node: "pause", Due: &due,
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
		if err := s.CreateFlowRun(ctx, run); err != nil {
			t.Fatalf("CreateFlowRun: %v", err)
		}

		// An overdue time on an asking run, which is the arrangement that would go wrong: the
		// poller must pass it over on the status alone, or an automation nobody answered would
		// take silence for a yes and do the thing it was asking permission for.
		overdue := time.Now().UTC().Add(-time.Hour)
		run.Node, run.Status, run.Question = "permit", flow.StatusAsking, "push?"
		if err := s.AdvanceFlowRun(ctx, run, flow.Transition{
			Event: flow.EventTurnFinished, Node: "permit", Due: &overdue,
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
			t.Errorf("detaching removed the skill from the crew: %v", err)
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
			t.Errorf("attaching a skill the crew has not imported returned %v, want ErrNotFound", err)
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
func ids(sessions []*quaycrewv1.Thread) []string {
	out := make([]string, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, session.GetId())
	}
	return out
}
