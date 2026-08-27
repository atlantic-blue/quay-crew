package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-crew/internal/store"
	"github.com/atlantic-blue/quay-crew/internal/work"
)

// runWorkConformance holds both stores to the work contract.
//
// Work is intent kept as a row, so what is proved here is that the row and the record of how it came
// to exist survive together, that a listing narrows the way a caller asks, and that work which
// already ended is not stopped a second time.
func runWorkConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a piece of work is written, read back whole, and outlives the caller", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
		declared := &work.Work{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "read the electricity bill", Brief: "open the bill and say when it is due",
			Role: "reader", RoleVersion: 2, Mode: "plan",
			ExpectFile: "notes/bill.md", ExpectContains: "due",
			After: []string{}, Deadline: &deadline, BudgetTokens: 5000,
			Labels: map[string]string{"owner": "house"}, Version: 1, Phase: work.PhasePending,
		}
		if err := s.CreateWork(ctx, declared, &work.Event{
			ID: store.NewID(), Kind: work.EventDeclared, Work: declared.ID,
			Workspace: workspace, Project: project, Detail: "read the electricity bill",
			OccurredAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("CreateWork: %v", err)
		}

		// Reopened, because intent that only exists in the caller's process is not intent that
		// survives the caller.
		found, err := open(t).GetWork(ctx, declared.ID)
		if err != nil {
			t.Fatalf("GetWork: %v", err)
		}
		if found.Title != declared.Title || found.Brief != declared.Brief {
			t.Fatalf("the work reads back as %q / %q", found.Title, found.Brief)
		}
		if found.Role != "reader" || found.RoleVersion != 2 {
			t.Fatalf("the role reads back as %q at version %d", found.Role, found.RoleVersion)
		}
		if found.Mode != "plan" || found.ExpectFile != "notes/bill.md" || found.ExpectContains != "due" {
			t.Fatalf("the claim reads back as %q %q %q", found.Mode, found.ExpectFile, found.ExpectContains)
		}
		if found.BudgetTokens != 5000 {
			t.Fatalf("the budget reads back as %d", found.BudgetTokens)
		}
		if found.Deadline == nil || !found.Deadline.Equal(deadline) {
			t.Fatalf("the deadline reads back as %v, want %v", found.Deadline, deadline)
		}
		if found.Labels["owner"] != "house" {
			t.Fatalf("the labels read back as %v", found.Labels)
		}
		if found.Phase != work.PhasePending {
			t.Fatalf("the work opens in phase %q, want pending", found.Phase)
		}
		if found.Parent != "" || found.Depth != 0 {
			t.Fatalf("the work has parent %q at depth %d, want a root", found.Parent, found.Depth)
		}
		if found.CreatedAt.IsZero() || found.UpdatedAt.IsZero() {
			t.Fatal("the work does not carry when it was declared")
		}
		if found.FinishedAt != nil || found.StartedAt != nil {
			t.Fatal("work nothing has run carries a start or a finish")
		}
	})

	t.Run("what a piece of work waits for is kept in order", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		first := declaredWork(t, s, workspace, project, "first")
		second := declaredWork(t, s, workspace, project, "second")

		third := &work.Work{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "third", Brief: "after both", After: []string{first, second},
			Version: 1, Phase: work.PhasePending,
		}
		if err := s.CreateWork(ctx, third, declaredEvent(third)); err != nil {
			t.Fatalf("CreateWork: %v", err)
		}

		found, err := s.GetWork(ctx, third.ID)
		if err != nil {
			t.Fatalf("GetWork: %v", err)
		}
		if len(found.After) != 2 || found.After[0] != first || found.After[1] != second {
			t.Fatalf("the work waits for %v, want %v in that order", found.After, []string{first, second})
		}
	})

	t.Run("the record of a declaration is written with the work", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		id := declaredWork(t, s, workspace, project, "read the electricity bill")

		events, err := s.ListWorkEvents(ctx, id)
		if err != nil {
			t.Fatalf("ListWorkEvents: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("%d records were written, want the one declaration", len(events))
		}
		if events[0].Kind != work.EventDeclared {
			t.Fatalf("the record is %q, want %q", events[0].Kind, work.EventDeclared)
		}
		if events[0].Work != id || events[0].Workspace != workspace || events[0].Project != project {
			t.Fatalf("the record names %s in %s / %s", events[0].Work, events[0].Workspace, events[0].Project)
		}
		if events[0].OccurredAt.IsZero() {
			t.Fatal("the record does not say when it happened")
		}
	})

	t.Run("the same record written twice leaves one", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		id := declaredWork(t, s, workspace, project, "read the electricity bill")
		events, _ := s.ListWorkEvents(ctx, id)
		again := *events[0]

		if _, err := s.StopWork(ctx, id, "changed my mind", &again); err != nil {
			t.Fatalf("StopWork: %v", err)
		}
		after, err := s.ListWorkEvents(ctx, id)
		if err != nil {
			t.Fatalf("ListWorkEvents: %v", err)
		}
		if len(after) != 1 {
			t.Fatalf("%d records exist, want the one that was written twice to leave one", len(after))
		}
	})

	t.Run("a listing narrows by project, by phase, by parent and by label", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		other, err := s.CreateProject(ctx, workspace, "other")
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}

		root := declaredWork(t, s, workspace, project, "the root")
		child := &work.Work{
			ID: store.NewID(), Workspace: workspace, Project: project, Title: "the child",
			Brief: "under the root", Parent: root, Depth: 1, Version: 1, Phase: work.PhasePending,
			Labels: map[string]string{"owner": "house"},
		}
		if err := s.CreateWork(ctx, child, declaredEvent(child)); err != nil {
			t.Fatalf("CreateWork: %v", err)
		}
		elsewhere := declaredWork(t, s, workspace, other.GetId(), "somewhere else")

		listed, err := s.ListWork(ctx, work.Filter{Project: project})
		if err != nil {
			t.Fatalf("ListWork: %v", err)
		}
		if len(listed) != 2 {
			t.Fatalf("the project holds %d pieces of work, want 2", len(listed))
		}
		if listed[0].ID != child.ID {
			t.Fatalf("the listing opens with %s, want the newest first", listed[0].Title)
		}

		if listed, _ = s.ListWork(ctx, work.Filter{Workspace: workspace}); len(listed) != 3 {
			t.Fatalf("the workspace holds %d pieces of work, want 3 including %s", len(listed), elsewhere)
		}
		if listed, _ = s.ListWork(ctx, work.Filter{Project: project, Parent: root}); len(listed) != 1 || listed[0].ID != child.ID {
			t.Fatalf("the children of the root are %d, want the one child", len(listed))
		}
		if listed, _ = s.ListWork(ctx, work.Filter{Project: project, Root: true}); len(listed) != 1 || listed[0].ID != root {
			t.Fatalf("the roots of the project are %d, want the one root", len(listed))
		}
		if listed, _ = s.ListWork(ctx, work.Filter{Project: project, LabelKey: "owner", LabelValue: "house"}); len(listed) != 1 || listed[0].ID != child.ID {
			t.Fatalf("the work labelled owner=house is %d rows, want the child", len(listed))
		}
		if listed, _ = s.ListWork(ctx, work.Filter{Project: project, LabelKey: "owner", LabelValue: "somebody else"}); len(listed) != 0 {
			t.Fatalf("a label value nothing carries matched %d rows", len(listed))
		}

		if _, err := s.StopWork(ctx, root, "not yet", stoppedEvent(root, workspace, project, "not yet")); err != nil {
			t.Fatalf("StopWork: %v", err)
		}
		if listed, _ = s.ListWork(ctx, work.Filter{Project: project, Phase: work.PhasePending}); len(listed) != 1 || listed[0].ID != child.ID {
			t.Fatalf("the pending work is %d rows, want the child alone", len(listed))
		}
	})

	t.Run("a listing carries no answers and reading one piece of work does", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		answered := &work.Work{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "read the electricity bill", Brief: "open it", Version: 1,
			Phase: work.PhaseDone, Answer: "the bill is due on the 14th",
		}
		if err := s.CreateWork(ctx, answered, declaredEvent(answered)); err != nil {
			t.Fatalf("CreateWork: %v", err)
		}

		listed, err := s.ListWork(ctx, work.Filter{Project: project})
		if err != nil {
			t.Fatalf("ListWork: %v", err)
		}
		if len(listed) != 1 {
			t.Fatalf("the project holds %d pieces of work, want 1", len(listed))
		}
		if listed[0].Answer != "" {
			t.Fatalf("the listing carries an answer: %q", listed[0].Answer)
		}
		if listed[0].Title == "" {
			t.Fatal("the listing carries no title, and the title is what a listing is for")
		}

		found, err := s.GetWork(ctx, answered.ID)
		if err != nil {
			t.Fatalf("GetWork: %v", err)
		}
		if found.Answer != "the bill is due on the 14th" {
			t.Fatalf("the answer reads back as %q", found.Answer)
		}
	})

	t.Run("work is stopped once, with the reason, and not stopped again", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		id := declaredWork(t, s, workspace, project, "read the electricity bill")

		stopped, err := s.StopWork(ctx, id, "the bill is not due yet",
			stoppedEvent(id, workspace, project, "the bill is not due yet"))
		if err != nil {
			t.Fatalf("StopWork: %v", err)
		}
		if stopped.Phase != work.PhaseStopped {
			t.Fatalf("the work is %q, want stopped", stopped.Phase)
		}
		if stopped.Reason != "the bill is not due yet" {
			t.Fatalf("the reason is %q", stopped.Reason)
		}
		if stopped.FinishedAt == nil {
			t.Fatal("stopped work does not carry when it finished")
		}

		if _, err := s.StopWork(ctx, id, "changed my mind",
			stoppedEvent(id, workspace, project, "changed my mind")); err == nil {
			t.Fatal("work that already ended was stopped again")
		}
		found, _ := s.GetWork(ctx, id)
		if found.Reason != "the bill is not due yet" {
			t.Fatalf("the second stop overwrote the reason: %q", found.Reason)
		}

		events, _ := s.ListWorkEvents(ctx, id)
		if len(events) != 2 {
			t.Fatalf("%d records exist, want the declaration and the one stop", len(events))
		}
		if events[1].Kind != work.EventStopped {
			t.Fatalf("the second record is %q, want %q", events[1].Kind, work.EventStopped)
		}
	})

	// Every field the store is handed comes back, status included. A store that keeps the declared
	// half and drops the rest passes every other case here while losing what a controller wrote.
	t.Run("the whole record survives a write, status and all", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		started := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
		finished := started.Add(time.Minute).Truncate(time.Second)
		declared := &work.Work{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "read the electricity bill", Brief: "open it", Version: 3,
			Phase: work.PhaseDone, Session: "session-1", Attempts: 2,
			Answer: "the bill is due on the 14th", Reason: "it answered", Question: "which bill",
			SpentTokens: 1234, ObservedVersion: 3, StartedAt: &started, FinishedAt: &finished,
		}
		if err := s.CreateWork(ctx, declared, declaredEvent(declared)); err != nil {
			t.Fatalf("CreateWork: %v", err)
		}

		found, err := s.GetWork(ctx, declared.ID)
		if err != nil {
			t.Fatalf("GetWork: %v", err)
		}
		for _, mismatch := range []struct {
			field     string
			got, want any
		}{
			{"phase", found.Phase, declared.Phase},
			{"session", found.Session, declared.Session},
			{"attempts", found.Attempts, declared.Attempts},
			{"answer", found.Answer, declared.Answer},
			{"reason", found.Reason, declared.Reason},
			{"question", found.Question, declared.Question},
			{"spent tokens", found.SpentTokens, declared.SpentTokens},
			{"version", found.Version, declared.Version},
			{"observed version", found.ObservedVersion, declared.ObservedVersion},
		} {
			if mismatch.got != mismatch.want {
				t.Errorf("the %s reads back as %v, want %v", mismatch.field, mismatch.got, mismatch.want)
			}
		}
		if found.StartedAt == nil || !found.StartedAt.Equal(started) {
			t.Errorf("the start reads back as %v, want %v", found.StartedAt, started)
		}
		if found.FinishedAt == nil || !found.FinishedAt.Equal(finished) {
			t.Errorf("the finish reads back as %v, want %v", found.FinishedAt, finished)
		}
	})

	t.Run("work that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if _, err := s.GetWork(ctx, "0123456789abcdef01234567"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetWork on missing work returned %v, want ErrNotFound", err)
		}
		if _, err := s.StopWork(ctx, "0123456789abcdef01234567", "why",
			stoppedEvent("0123456789abcdef01234567", "w", "p", "why")); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("StopWork on missing work returned %v, want ErrNotFound", err)
		}
	})
}

// aProject is a workspace and a project for work to live in.
func aProject(t *testing.T, s store.Store) (workspace, project string) {
	t.Helper()
	ctx := context.Background()
	made, err := s.CreateWorkspace(ctx, "acme")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	inside, err := s.CreateProject(ctx, made.GetId(), "house-bills")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return made.GetId(), inside.GetId()
}

// declaredWork writes one plain piece of work and answers with its identifier.
func declaredWork(t *testing.T, s store.Store, workspace, project, title string) string {
	t.Helper()
	declared := &work.Work{
		ID: store.NewID(), Workspace: workspace, Project: project,
		Title: title, Brief: "do it", Version: 1, Phase: work.PhasePending,
	}
	if err := s.CreateWork(context.Background(), declared, declaredEvent(declared)); err != nil {
		t.Fatalf("CreateWork: %v", err)
	}
	return declared.ID
}

func declaredEvent(declared *work.Work) *work.Event {
	return &work.Event{
		ID: store.NewID(), Kind: work.EventDeclared, Work: declared.ID,
		Workspace: declared.Workspace, Project: declared.Project, Parent: declared.Parent,
		Depth: declared.Depth, Detail: declared.Title, OccurredAt: time.Now().UTC(),
	}
}

func stoppedEvent(id, workspace, project, reason string) *work.Event {
	return &work.Event{
		ID: store.NewID(), Kind: work.EventStopped, Work: id,
		Workspace: workspace, Project: project, Detail: reason, OccurredAt: time.Now().UTC(),
	}
}

// runWorkControllerConformance holds both stores to what a controller needs of them.
//
// A controller reads what it may start, claims it once however many controllers are asking, and
// writes what came of it. The claim is the interesting one: it is conditional in the store, so two
// callers cannot both win, and neither store is allowed to be the lenient one.
func runWorkControllerConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("the work a controller may run is root work that is pending", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		root := declaredWork(t, s, workspace, project, "the root")
		waiting := workShaped(t, s, workspace, project, "waits for the root", func(w *work.Work) {
			w.After = []string{root}
		})
		inRole := workShaped(t, s, workspace, project, "runs as a role", func(w *work.Work) {
			w.Role, w.RoleVersion = "backlog-clearer", 1
		})
		child := workShaped(t, s, workspace, project, "under the root", func(w *work.Work) {
			w.Parent, w.Depth = root, 1
		})
		stopped := declaredWork(t, s, workspace, project, "stopped by a person")
		if _, err := s.StopWork(ctx, stopped, "not yet", stoppedEvent(stopped, workspace, project, "not yet")); err != nil {
			t.Fatalf("StopWork: %v", err)
		}

		runnable, err := s.RunnableWork(ctx, 0)
		if err != nil {
			t.Fatalf("RunnableWork: %v", err)
		}
		if len(runnable) != 1 || runnable[0].ID != root {
			t.Fatalf("the runnable work is %v, want the one root", titlesOf(runnable))
		}
		_ = []string{waiting, inRole, child}
	})

	t.Run("the oldest declared work is offered first", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		first := declaredWork(t, s, workspace, project, "first")
		second := declaredWork(t, s, workspace, project, "second")

		runnable, err := s.RunnableWork(ctx, 0)
		if err != nil {
			t.Fatalf("RunnableWork: %v", err)
		}
		if len(runnable) != 2 {
			t.Fatalf("%d pieces of work are runnable, want 2", len(runnable))
		}
		if runnable[0].ID != first || runnable[1].ID != second {
			t.Fatalf("the work is offered as %v, want the oldest declared first", titlesOf(runnable))
		}
		// And a limit takes the oldest, rather than an arbitrary one.
		capped, err := s.RunnableWork(ctx, 1)
		if err != nil {
			t.Fatalf("RunnableWork: %v", err)
		}
		if len(capped) != 1 || capped[0].ID != first {
			t.Fatalf("a limit of one offered %v, want the oldest", titlesOf(capped))
		}
	})

	t.Run("work is claimed once, and a second claim is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredWork(t, s, workspace, project, "read the electricity bill")

		claimed, err := s.StartWork(ctx, id, startedEvent(id, workspace, project))
		if err != nil {
			t.Fatalf("StartWork: %v", err)
		}
		if claimed.Phase != work.PhaseRunning {
			t.Fatalf("claimed work is %q, want running", claimed.Phase)
		}
		if claimed.Attempts != 1 {
			t.Fatalf("claimed work is on attempt %d, want 1", claimed.Attempts)
		}
		if claimed.StartedAt == nil {
			t.Fatal("claimed work does not carry when it started")
		}

		if _, err := s.StartWork(ctx, id, startedEvent(id, workspace, project)); !errors.Is(err, work.ErrNotPending) {
			t.Fatalf("the second claim answered %v, want ErrNotPending", err)
		}
		// And the refused claim wrote no record, or a listing would say the work started twice.
		events, err := s.ListWorkEvents(ctx, id)
		if err != nil {
			t.Fatalf("ListWorkEvents: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("%d records exist, want the declaration and the one start", len(events))
		}
	})

	t.Run("claiming work the crew does not hold is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		if _, err := s.StartWork(context.Background(), "0123456789abcdef01234567",
			startedEvent("0123456789abcdef01234567", "w", "p")); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("claiming missing work answered %v, want ErrNotFound", err)
		}
		_ = ctx
	})

	t.Run("the session the work runs in is written onto the row", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredWork(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartWork(ctx, id, startedEvent(id, workspace, project)); err != nil {
			t.Fatalf("StartWork: %v", err)
		}

		if err := s.RecordWorkSession(ctx, id, "session-1"); err != nil {
			t.Fatalf("RecordWorkSession: %v", err)
		}

		found, err := s.GetWork(ctx, id)
		if err != nil {
			t.Fatalf("GetWork: %v", err)
		}
		if found.Session != "session-1" {
			t.Fatalf("the work says session %q", found.Session)
		}
		// Started work is what a controller comes back to, and only once it has a session: without
		// one there is no task to read.
		started, err := s.StartedWork(ctx, 0)
		if err != nil {
			t.Fatalf("StartedWork: %v", err)
		}
		if len(started) != 1 || started[0].ID != id {
			t.Fatalf("the started work is %v, want the one that is running", titlesOf(started))
		}
	})

	t.Run("work that is running with no session yet is not offered to be read back", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredWork(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartWork(ctx, id, startedEvent(id, workspace, project)); err != nil {
			t.Fatalf("StartWork: %v", err)
		}

		started, err := s.StartedWork(ctx, 0)
		if err != nil {
			t.Fatalf("StartedWork: %v", err)
		}
		if len(started) != 0 {
			t.Fatalf("%d pieces of work were offered with no session behind them", len(started))
		}
	})

	t.Run("what came of the work is written with its record, once", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredWork(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartWork(ctx, id, startedEvent(id, workspace, project)); err != nil {
			t.Fatalf("StartWork: %v", err)
		}

		landed, err := s.LandWork(ctx, id, work.Landing{
			Phase: work.PhaseDone, Answer: "the bill is due on the 14th", SpentTokens: 1234,
		}, answeredEvent(id, workspace, project))
		if err != nil {
			t.Fatalf("LandWork: %v", err)
		}
		if landed.Phase != work.PhaseDone || landed.Answer != "the bill is due on the 14th" {
			t.Fatalf("the work landed as %q saying %q", landed.Phase, landed.Answer)
		}
		if landed.SpentTokens != 1234 {
			t.Fatalf("the work spent %d tokens", landed.SpentTokens)
		}
		if landed.FinishedAt == nil {
			t.Fatal("landed work does not carry when it finished")
		}
		if landed.ObservedVersion != landed.Version {
			t.Fatalf("the status describes version %d of a declaration at version %d",
				landed.ObservedVersion, landed.Version)
		}

		// A second landing is refused: the work has ended, and what it ended as is the useful part.
		if _, err := s.LandWork(ctx, id, work.Landing{Phase: work.PhaseFailed, Reason: "no"},
			answeredEvent(id, workspace, project)); !errors.Is(err, work.ErrNotRunning) {
			t.Fatalf("the second landing answered %v, want ErrNotRunning", err)
		}
		found, _ := s.GetWork(ctx, id)
		if found.Phase != work.PhaseDone {
			t.Fatalf("the second landing moved the work to %q", found.Phase)
		}
	})

	t.Run("landing work that never started is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredWork(t, s, workspace, project, "read the electricity bill")

		if _, err := s.LandWork(ctx, id, work.Landing{Phase: work.PhaseDone, Answer: "done"},
			answeredEvent(id, workspace, project)); !errors.Is(err, work.ErrNotRunning) {
			t.Fatalf("landing work that never started answered %v, want ErrNotRunning", err)
		}
	})
}

// workShaped writes one piece of work with a shape a controller must not pick up.
func workShaped(t *testing.T, s store.Store, workspace, project, title string, shape func(*work.Work)) string {
	t.Helper()
	declared := &work.Work{
		ID: store.NewID(), Workspace: workspace, Project: project,
		Title: title, Brief: "do it", Version: 1, Phase: work.PhasePending,
	}
	shape(declared)
	if err := s.CreateWork(context.Background(), declared, declaredEvent(declared)); err != nil {
		t.Fatalf("CreateWork: %v", err)
	}
	return declared.ID
}

func titlesOf(listed []*work.Work) []string {
	titles := make([]string, 0, len(listed))
	for _, one := range listed {
		titles = append(titles, one.Title)
	}
	return titles
}

func startedEvent(id, workspace, project string) *work.Event {
	return &work.Event{
		ID: store.NewID(), Kind: work.EventStarted, Work: id,
		Workspace: workspace, Project: project, Detail: "attempt 1", OccurredAt: time.Now().UTC(),
	}
}

func answeredEvent(id, workspace, project string) *work.Event {
	return &work.Event{
		ID: store.NewID(), Kind: work.EventAnswered, Work: id,
		Workspace: workspace, Project: project, Detail: "1234 tokens", OccurredAt: time.Now().UTC(),
	}
}
