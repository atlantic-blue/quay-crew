package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/store"
)

// runSessionLifecycleConformance holds both stores to what reclaiming a session means, and to the
// fourth query a controller reads sessions with.
//
// Both implementations or neither. A memory store that answered this query more loosely than Postgres
// would make a suite green over a system that reclaims containers the real one leaves alone.
func runSessionLifecycleConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a reclaimed session keeps everything but its container", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		if err := s.RecordTask(ctx, session.GetId(), "conversation-1", "idle"); err != nil {
			t.Fatalf("RecordTask: %v", err)
		}

		if err := s.ReclaimSession(ctx, session.GetId()); err != nil {
			t.Fatalf("ReclaimSession: %v", err)
		}

		// Reopened, because a session that only reads reclaimed in the process that reclaimed it is a
		// session the next control plane will start a second container for.
		got, err := open(t).GetSession(ctx, session.GetId())
		if err != nil {
			t.Fatalf("GetSession: %v", err)
		}
		if got.GetStatus() != store.StatusReclaimed {
			t.Fatalf("status is %q, want reclaimed", got.GetStatus())
		}
		if got.GetReclaimedAt() == nil {
			t.Fatal("a reclaimed session carries no stamp, so nothing can measure how long it has been one")
		}
		if got.GetModelSessionId() != "conversation-1" {
			t.Fatalf("the conversation handle reads %q, and a reclaim must not touch it: "+
				"it is the only pointer to the transcript", got.GetModelSessionId())
		}
		if got.GetArchivedAt() != nil {
			t.Fatal("reclaiming filed the session away, and those are two separate decisions")
		}
	})

	t.Run("reclaiming clears what the sandbox was born holding", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		if err := s.SetSessionSkills(ctx, session.GetId(), "fingerprint-1"); err != nil {
			t.Fatalf("SetSessionSkills: %v", err)
		}

		if err := s.ReclaimSession(ctx, session.GetId()); err != nil {
			t.Fatalf("ReclaimSession: %v", err)
		}

		born, err := s.SessionSkills(ctx, session.GetId())
		if err != nil {
			t.Fatalf("SessionSkills: %v", err)
		}
		if born != "" {
			t.Fatalf("the session still reads as born holding %q, and its container has gone: "+
				"the next one is born with the current set, so it can never be stale", born)
		}
	})

	t.Run("a task clears the reclaim stamp, so the next dispatch undoes it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		if err := s.ReclaimSession(ctx, session.GetId()); err != nil {
			t.Fatalf("ReclaimSession: %v", err)
		}

		if err := s.RecordTask(ctx, session.GetId(), "conversation-1", "running"); err != nil {
			t.Fatalf("RecordTask: %v", err)
		}

		got, _ := s.GetSession(ctx, session.GetId())
		if got.GetReclaimedAt() != nil {
			t.Fatal("the reclaim stamp survived a task, so the archive rule would go on measuring " +
				"against a reclaim that a dispatch already undid")
		}
		if got.GetStatus() != "running" {
			t.Fatalf("status is %q, want running", got.GetStatus())
		}
	})

	t.Run("stopping and restarting both clear the reclaim stamp", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		for _, clearing := range []struct {
			name  string
			apply func(id string) error
			want  string
		}{
			{"stopping", func(id string) error { return s.StopSession(ctx, id) }, "stopped"},
			{"restarting", func(id string) error { return s.RestartSession(ctx, id) }, "idle"},
		} {
			session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-"+clearing.name, store.Birth{})
			if err := s.ReclaimSession(ctx, session.GetId()); err != nil {
				t.Fatalf("ReclaimSession: %v", err)
			}
			if err := clearing.apply(session.GetId()); err != nil {
				t.Fatalf("%s: %v", clearing.name, err)
			}
			got, _ := s.GetSession(ctx, session.GetId())
			if got.GetReclaimedAt() != nil {
				t.Fatalf("%s left the reclaim stamp behind", clearing.name)
			}
			if got.GetStatus() != clearing.want {
				t.Fatalf("%s left the status %q, want %s", clearing.name, got.GetStatus(), clearing.want)
			}
		}
	})

	t.Run("reclaiming a session nobody has is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		if err := s.ReclaimSession(context.Background(), "ghost"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("reclaiming a missing session returned %v, want ErrNotFound", err)
		}
	})

	t.Run("the settled sessions are the live ones nothing is running in", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")

		waiting, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "waiting", store.Birth{})
		working, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "working", store.Birth{})
		broken, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "broken", store.Birth{})
		halted, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "halted", store.Birth{})
		filed, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "filed", store.Birth{})
		if err := s.RecordTask(ctx, working.GetId(), "", "running"); err != nil {
			t.Fatalf("RecordTask: %v", err)
		}
		if err := s.RecordTask(ctx, broken.GetId(), "", "failed"); err != nil {
			t.Fatalf("RecordTask: %v", err)
		}
		if err := s.StopSession(ctx, halted.GetId()); err != nil {
			t.Fatalf("StopSession: %v", err)
		}
		if err := s.ArchiveSession(ctx, filed.GetId()); err != nil {
			t.Fatalf("ArchiveSession: %v", err)
		}

		settled, err := s.SettledSessions(ctx, 0)
		if err != nil {
			t.Fatalf("SettledSessions: %v", err)
		}
		if got := idsOf(settled); !holds(got, waiting.GetId()) || !holds(got, broken.GetId()) {
			t.Fatalf("the settled sessions are %v, and both the waiting one and the one whose last "+
				"task failed are settled: nothing is running in either", got)
		}
		for _, absent := range []struct {
			id  string
			why string
		}{
			{working.GetId(), "a task is under way in it"},
			{halted.GetId(), "an operator stopped it, and filing away what somebody halted overwrites a decision"},
			{filed.GetId(), "it is already archived"},
		} {
			if holds(idsOf(settled), absent.id) {
				t.Fatalf("the settled sessions carry the one where %s", absent.why)
			}
		}
	})

	t.Run("a reclaimed session is still settled, so it can be filed away later", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), "session-a", store.Birth{})
		if err := s.ReclaimSession(ctx, session.GetId()); err != nil {
			t.Fatalf("ReclaimSession: %v", err)
		}

		settled, err := s.SettledSessions(ctx, 0)
		if err != nil {
			t.Fatalf("SettledSessions: %v", err)
		}
		if !holds(idsOf(settled), session.GetId()) {
			t.Fatalf("the settled sessions are %v, and a reclaimed one has to stay in them or "+
				"nothing would ever archive it", idsOf(settled))
		}
	})

	t.Run("job still open holds its session out of the settled ones", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		busy, _, _ := s.FindOrCreateSession(ctx, project, "busy", store.Birth{})
		finished, _, _ := s.FindOrCreateSession(ctx, project, "finished", store.Birth{})

		open := declaredJob(t, s, workspace, project, "still going")
		if err := s.RecordJobSession(ctx, open, busy.GetId()); err != nil {
			t.Fatalf("RecordJobSession: %v", err)
		}
		if _, err := s.StartJob(ctx, open, aLease("controller-1"), nil); err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		ended := declaredJob(t, s, workspace, project, "over")
		if err := s.RecordJobSession(ctx, ended, finished.GetId()); err != nil {
			t.Fatalf("RecordJobSession: %v", err)
		}
		if _, err := s.StartJob(ctx, ended, aLease("controller-1"), nil); err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		if _, err := s.LandJob(ctx, ended, job.Landing{Phase: job.PhaseDone, Answer: "done"},
			answeredEvent(ended, workspace, project)); err != nil {
			t.Fatalf("LandJob: %v", err)
		}

		settled, err := s.SettledSessions(ctx, 0)
		if err != nil {
			t.Fatalf("SettledSessions: %v", err)
		}
		if holds(idsOf(settled), busy.GetId()) {
			t.Fatalf("the settled sessions carry one a job is still running in: %v", idsOf(settled))
		}
		if !holds(idsOf(settled), finished.GetId()) {
			t.Fatalf("the settled sessions are %v, and the one whose job is done is nothing's to hold",
				idsOf(settled))
		}
	})

	t.Run("the settled sessions come back oldest touched first, and capped", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		project := newProject(t, s, "acme", "house bills")
		for _, handle := range []string{"first", "second", "third"} {
			session, _, _ := s.FindOrCreateSession(ctx, project.GetId(), handle, store.Birth{})
			if err := s.RecordTask(ctx, session.GetId(), "", "idle"); err != nil {
				t.Fatalf("RecordTask: %v", err)
			}
			// Distinct stamps, so the ordering is a fact rather than a tie broken by chance.
			time.Sleep(2 * time.Millisecond)
		}

		settled, err := s.SettledSessions(ctx, 2)
		if err != nil {
			t.Fatalf("SettledSessions: %v", err)
		}
		if len(settled) != 2 {
			t.Fatalf("asking for 2 settled sessions returned %d", len(settled))
		}
		if settled[0].GetHandle() != "first" || settled[1].GetHandle() != "second" {
			t.Fatalf("the settled sessions read %v, want the two touched longest ago, oldest first",
				handlesOf(settled))
		}
	})
}

// idsOf is the identifiers of a list of sessions.
func idsOf(sessions []*quaycrewv1.Session) []string {
	out := make([]string, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, session.GetId())
	}
	return out
}

// handlesOf is what an operator would call each of them, for a failure that has to be readable.
func handlesOf(sessions []*quaycrewv1.Session) []string {
	out := make([]string, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, session.GetHandle())
	}
	return out
}

func holds(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
