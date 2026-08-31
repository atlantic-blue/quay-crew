package storetest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/store"
)

// runJobConformance holds both stores to the job contract.
//
// Job is intent kept as a row, so what is proved here is that the row and the record of how it came
// to exist survive together, that a listing narrows the way a caller asks, and that job which
// already ended is not stopped a second time.
func runJobConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a job is written, read back whole, and outlives the caller", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
		declared := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "read the electricity bill", Brief: "open the bill and say when it is due",
			Role: "reader", RoleVersion: 2, Mode: "plan",
			ExpectFile: "notes/bill.md", ExpectContains: "due",
			After: []string{}, Deadline: &deadline, BudgetTokens: 5000,
			Labels: map[string]string{"owner": "house"}, Repository: "atlantic-blue/quay-crew",
			Product: "paste a link and get the text back",
			Version: 1, Phase: job.PhasePending,
		}
		if err := s.CreateJob(ctx, declared, &job.Event{
			ID: store.NewID(), Kind: job.EventDeclared, Job: declared.ID,
			Workspace: workspace, Project: project, Detail: "read the electricity bill",
			OccurredAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}

		// Reopened, because intent that only exists in the caller's process is not intent that
		// survives the caller.
		found, err := open(t).GetJob(ctx, declared.ID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.Title != declared.Title || found.Brief != declared.Brief {
			t.Fatalf("the job reads back as %q / %q", found.Title, found.Brief)
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
		if found.Repository != "atlantic-blue/quay-crew" {
			t.Fatalf("the repository reads back as %q", found.Repository)
		}
		// The one sentence the job serves. A tree is measured against it, so a store that loses it
		// leaves every job under this one building against a design and nothing else.
		if found.Product != "paste a link and get the text back" {
			t.Fatalf("the sentence reads back as %q", found.Product)
		}
		// Nothing has answered yet, so nothing says where the work went.
		if found.PullRequest != "" {
			t.Fatalf("a job nobody has run says its pull request is %q", found.PullRequest)
		}
		if found.Phase != job.PhasePending {
			t.Fatalf("the job opens in phase %q, want pending", found.Phase)
		}
		if found.Parent != "" || found.Depth != 0 {
			t.Fatalf("the job has parent %q at depth %d, want a root", found.Parent, found.Depth)
		}
		if found.CreatedAt.IsZero() || found.UpdatedAt.IsZero() {
			t.Fatal("the job does not carry when it was declared")
		}
		if found.FinishedAt != nil || found.StartedAt != nil {
			t.Fatal("job nothing has run carries a start or a finish")
		}
	})

	t.Run("the trace a job belongs to survives the process that declared it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		declared := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "read the electricity bill", Brief: "open it", Version: 1, Phase: job.PhasePending,
			TraceID:      "4bf92f3577b34da6a3ce929d0e0e4736",
			ParentSpanID: "00f067aa0ba902b7",
		}
		if err := s.CreateJob(ctx, declared, &job.Event{
			ID: store.NewID(), Kind: job.EventDeclared, Job: declared.ID,
			Workspace: workspace, Project: project, Detail: "read the electricity bill",
			TraceID: declared.TraceID, OccurredAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}

		found, err := s.GetJob(ctx, declared.ID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.TraceID != declared.TraceID || found.ParentSpanID != declared.ParentSpanID {
			t.Fatalf("the trace reads back as %q / %q, and a trace held in a process is a trace lost "+
				"with the controller that died", found.TraceID, found.ParentSpanID)
		}

		events, err := s.ListJobEvents(ctx, declared.ID)
		if err != nil {
			t.Fatalf("ListJobEvents: %v", err)
		}
		if len(events) != 1 || events[0].TraceID != declared.TraceID {
			t.Fatalf("%d records came back, the first tracing %q", len(events), events[0].TraceID)
		}
	})

	t.Run("a job nothing was tracing reads back with no trace rather than a made up one", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		declared := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "read it", Brief: "open it", Version: 1, Phase: job.PhasePending,
		}
		if err := s.CreateJob(ctx, declared, &job.Event{
			ID: store.NewID(), Kind: job.EventDeclared, Job: declared.ID,
			Workspace: workspace, Project: project, OccurredAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}
		found, err := s.GetJob(ctx, declared.ID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.TraceID != "" || found.ParentSpanID != "" {
			t.Fatalf("the store invented a trace: %q / %q", found.TraceID, found.ParentSpanID)
		}
	})

	t.Run("what a job waits for is kept in order", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		first := declaredJob(t, s, workspace, project, "first")
		second := declaredJob(t, s, workspace, project, "second")

		third := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "third", Brief: "after both", After: []string{first, second},
			Version: 1, Phase: job.PhasePending,
		}
		if err := s.CreateJob(ctx, third, declaredEvent(third)); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}

		found, err := s.GetJob(ctx, third.ID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(found.After) != 2 || found.After[0] != first || found.After[1] != second {
			t.Fatalf("the job waits for %v, want %v in that order", found.After, []string{first, second})
		}
	})

	t.Run("the record of a declaration is written with the job", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		id := declaredJob(t, s, workspace, project, "read the electricity bill")

		events, err := s.ListJobEvents(ctx, id)
		if err != nil {
			t.Fatalf("ListJobEvents: %v", err)
		}
		if len(events) != 1 {
			t.Fatalf("%d records were written, want the one declaration", len(events))
		}
		if events[0].Kind != job.EventDeclared {
			t.Fatalf("the record is %q, want %q", events[0].Kind, job.EventDeclared)
		}
		if events[0].Job != id || events[0].Workspace != workspace || events[0].Project != project {
			t.Fatalf("the record names %s in %s / %s", events[0].Job, events[0].Workspace, events[0].Project)
		}
		if events[0].OccurredAt.IsZero() {
			t.Fatal("the record does not say when it happened")
		}
	})

	t.Run("the same record written twice leaves one", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		events, _ := s.ListJobEvents(ctx, id)
		again := *events[0]

		if _, err := s.StopJob(ctx, id, "changed my mind", &again); err != nil {
			t.Fatalf("StopJob: %v", err)
		}
		after, err := s.ListJobEvents(ctx, id)
		if err != nil {
			t.Fatalf("ListJobEvents: %v", err)
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

		root := declaredJob(t, s, workspace, project, "the root")
		child := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project, Title: "the child",
			Brief: "under the root", Parent: root, Depth: 1, Version: 1, Phase: job.PhasePending,
			Labels: map[string]string{"owner": "house"},
		}
		if err := s.CreateJob(ctx, child, declaredEvent(child)); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}
		elsewhere := declaredJob(t, s, workspace, other.GetId(), "somewhere else")

		listed, err := s.ListJobs(ctx, job.Filter{Project: project})
		if err != nil {
			t.Fatalf("ListJob: %v", err)
		}
		if len(listed) != 2 {
			t.Fatalf("the project holds %d jobs, want 2", len(listed))
		}
		if listed[0].ID != child.ID {
			t.Fatalf("the listing opens with %s, want the newest first", listed[0].Title)
		}

		if listed, _ = s.ListJobs(ctx, job.Filter{Workspace: workspace}); len(listed) != 3 {
			t.Fatalf("the workspace holds %d jobs, want 3 including %s", len(listed), elsewhere)
		}
		if listed, _ = s.ListJobs(ctx, job.Filter{Project: project, Parent: root}); len(listed) != 1 || listed[0].ID != child.ID {
			t.Fatalf("the children of the root are %d, want the one child", len(listed))
		}
		if listed, _ = s.ListJobs(ctx, job.Filter{Project: project, Root: true}); len(listed) != 1 || listed[0].ID != root {
			t.Fatalf("the roots of the project are %d, want the one root", len(listed))
		}
		if listed, _ = s.ListJobs(ctx, job.Filter{Project: project, LabelKey: "owner", LabelValue: "house"}); len(listed) != 1 || listed[0].ID != child.ID {
			t.Fatalf("the job labelled owner=house is %d rows, want the child", len(listed))
		}
		if listed, _ = s.ListJobs(ctx, job.Filter{Project: project, LabelKey: "owner", LabelValue: "somebody else"}); len(listed) != 0 {
			t.Fatalf("a label value nothing carries matched %d rows", len(listed))
		}

		if _, err := s.StopJob(ctx, root, "not yet", stoppedEvent(root, workspace, project, "not yet")); err != nil {
			t.Fatalf("StopJob: %v", err)
		}
		if listed, _ = s.ListJobs(ctx, job.Filter{Project: project, Phase: job.PhasePending}); len(listed) != 1 || listed[0].ID != child.ID {
			t.Fatalf("the pending job is %d rows, want the child alone", len(listed))
		}
	})

	// The filter the phase cannot be. Two jobs are done, one of them could not do its work, and a
	// reader that had to open both to tell them apart is the reading this exists to end.
	t.Run("a listing narrows by outcome", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		proved := landedWith(t, s, workspace, project, "read the electricity bill", job.OutcomeProved)
		blocked := landedWith(t, s, workspace, project, "read the water bill", job.OutcomeBlocked)

		for _, one := range []struct {
			outcome string
			want    string
		}{{job.OutcomeProved, proved}, {job.OutcomeBlocked, blocked}} {
			listed, err := s.ListJobs(ctx, job.Filter{Project: project, Outcome: one.outcome})
			if err != nil {
				t.Fatalf("ListJobs: %v", err)
			}
			if len(listed) != 1 || listed[0].ID != one.want {
				t.Fatalf("the jobs that ended %q are %d rows, want the one", one.outcome, len(listed))
			}
		}
		listed, err := s.ListJobs(ctx, job.Filter{Project: project, Outcome: job.OutcomeDecide})
		if err != nil {
			t.Fatalf("ListJobs: %v", err)
		}
		if len(listed) != 0 {
			t.Fatalf("an outcome nothing ended with matched %d rows", len(listed))
		}
		// Both are done, which is the point: the phase cannot tell them apart.
		if listed, _ = s.ListJobs(ctx, job.Filter{Project: project, Phase: job.PhaseDone}); len(listed) != 2 {
			t.Fatalf("the done jobs are %d rows, want both", len(listed))
		}
	})

	t.Run("a listing carries no answers and reading one job does", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		answered := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "read the electricity bill", Brief: "open it", Version: 1,
			Phase: job.PhaseDone, Answer: "the bill is due on the 14th",
		}
		if err := s.CreateJob(ctx, answered, declaredEvent(answered)); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}

		listed, err := s.ListJobs(ctx, job.Filter{Project: project})
		if err != nil {
			t.Fatalf("ListJob: %v", err)
		}
		if len(listed) != 1 {
			t.Fatalf("the project holds %d jobs, want 1", len(listed))
		}
		if listed[0].Answer != "" {
			t.Fatalf("the listing carries an answer: %q", listed[0].Answer)
		}
		if listed[0].Title == "" {
			t.Fatal("the listing carries no title, and the title is what a listing is for")
		}

		found, err := s.GetJob(ctx, answered.ID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.Answer != "the bill is due on the 14th" {
			t.Fatalf("the answer reads back as %q", found.Answer)
		}
	})

	t.Run("job is stopped once, with the reason, and not stopped again", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		id := declaredJob(t, s, workspace, project, "read the electricity bill")

		stopped, err := s.StopJob(ctx, id, "the bill is not due yet",
			stoppedEvent(id, workspace, project, "the bill is not due yet"))
		if err != nil {
			t.Fatalf("StopJob: %v", err)
		}
		if stopped.Phase != job.PhaseStopped {
			t.Fatalf("the job is %q, want stopped", stopped.Phase)
		}
		if stopped.Reason != "the bill is not due yet" {
			t.Fatalf("the reason is %q", stopped.Reason)
		}
		if stopped.FinishedAt == nil {
			t.Fatal("stopped job does not carry when it finished")
		}

		if _, err := s.StopJob(ctx, id, "changed my mind",
			stoppedEvent(id, workspace, project, "changed my mind")); err == nil {
			t.Fatal("job that already ended was stopped again")
		}
		found, _ := s.GetJob(ctx, id)
		if found.Reason != "the bill is not due yet" {
			t.Fatalf("the second stop overwrote the reason: %q", found.Reason)
		}

		events, _ := s.ListJobEvents(ctx, id)
		if len(events) != 2 {
			t.Fatalf("%d records exist, want the declaration and the one stop", len(events))
		}
		if events[1].Kind != job.EventStopped {
			t.Fatalf("the second record is %q, want %q", events[1].Kind, job.EventStopped)
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
		declared := &job.Job{
			ID: store.NewID(), Workspace: workspace, Project: project,
			Title: "read the electricity bill", Brief: "open it", Version: 3,
			Phase: job.PhaseDone, Session: "session-1", Attempts: 2,
			Answer: "the bill is due on the 14th", Outcome: job.OutcomeProved,
			Reason: "it answered", Question: "which bill",
			Told:        "the electricity one",
			SpentTokens: 1234, ObservedVersion: 3, StartedAt: &started, FinishedAt: &finished,
		}
		if err := s.CreateJob(ctx, declared, declaredEvent(declared)); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}

		found, err := s.GetJob(ctx, declared.ID)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		for _, mismatch := range []struct {
			field     string
			got, want any
		}{
			{"phase", found.Phase, declared.Phase},
			{"session", found.Session, declared.Session},
			{"attempts", found.Attempts, declared.Attempts},
			{"answer", found.Answer, declared.Answer},
			{"outcome", found.Outcome, declared.Outcome},
			{"reason", found.Reason, declared.Reason},
			{"question", found.Question, declared.Question},
			{"told", found.Told, declared.Told},
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

	t.Run("job that does not exist is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		if _, err := s.GetJob(ctx, "0123456789abcdef01234567"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetJob on missing job returned %v, want ErrNotFound", err)
		}
		if _, err := s.StopJob(ctx, "0123456789abcdef01234567", "why",
			stoppedEvent("0123456789abcdef01234567", "w", "p", "why")); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("StopJob on missing job returned %v, want ErrNotFound", err)
		}
	})
}

// aProject is a workspace and a project for jobs to live in.
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

// declaredJob writes one plain job and answers with its identifier.
func declaredJob(t *testing.T, s store.Store, workspace, project, title string) string {
	t.Helper()
	declared := &job.Job{
		ID: store.NewID(), Workspace: workspace, Project: project,
		Title: title, Brief: "do it", Version: 1, Phase: job.PhasePending,
	}
	if err := s.CreateJob(context.Background(), declared, declaredEvent(declared)); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return declared.ID
}

// landedWith is a job that ran and ended on one word, written the way the controller writes it: the
// row is declared, claimed and landed, so the outcome comes off a landing rather than being seeded.
// A test that seeded it would pass against a store that never writes the column.
func landedWith(t *testing.T, s store.Store, workspace, project, title, outcome string) string {
	t.Helper()
	ctx := context.Background()
	id := declaredJob(t, s, workspace, project, title)
	if _, err := s.StartJob(ctx, id, aLease("controller-a"),
		[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if _, err := s.LandJob(ctx, id, job.Landing{
		Phase: job.PhaseDone, Answer: "it is done", Outcome: outcome,
	}, answeredEvent(id, workspace, project)); err != nil {
		t.Fatalf("LandJob: %v", err)
	}
	return id
}

func declaredEvent(declared *job.Job) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventDeclared, Job: declared.ID,
		Workspace: declared.Workspace, Project: declared.Project, Parent: declared.Parent,
		Depth: declared.Depth, Detail: declared.Title, OccurredAt: time.Now().UTC(),
	}
}

func stoppedEvent(id, workspace, project, reason string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventStopped, Job: id,
		Workspace: workspace, Project: project, Detail: reason, OccurredAt: time.Now().UTC(),
	}
}

// runJobControllerConformance holds both stores to what a controller needs of them.
//
// A controller reads what it may start, claims it once however many controllers are asking, and
// writes what came of it. The claim is the interesting one: it is conditional in the store, so two
// callers cannot both win, and neither store is allowed to be the lenient one.
func runJobControllerConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("the job a controller may run is pending job with nothing outstanding", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		root := declaredJob(t, s, workspace, project, "the root")
		waiting := jobShaped(t, s, workspace, project, "waits for the root", func(w *job.Job) {
			w.After = []string{root}
		})
		// Job in a role is offered, because the controller runs it as that role. The two left out
		// are ordering and depth, which are each a later slice.
		inRole := jobShaped(t, s, workspace, project, "runs as a role", func(w *job.Job) {
			w.Role, w.RoleVersion = "backlog-clearer", 1
		})
		child := jobShaped(t, s, workspace, project, "under the root", func(w *job.Job) {
			w.Parent, w.Depth = root, 1
		})
		stopped := declaredJob(t, s, workspace, project, "stopped by a person")
		if _, err := s.StopJob(ctx, stopped, "not yet", stoppedEvent(stopped, workspace, project, "not yet")); err != nil {
			t.Fatalf("StopJob: %v", err)
		}

		runnable, err := s.RunnableJob(ctx, 0)
		if err != nil {
			t.Fatalf("RunnableJob: %v", err)
		}
		// A job under a parent and a job in a role both run: a flow declares every step under the run
		// and a step may name a role. Job that waits for something is the one thing left out,
		// because nothing honours ordering yet, and a job a person stopped is not pending.
		offered := map[string]bool{}
		for _, one := range runnable {
			offered[one.ID] = true
		}
		if len(runnable) != 3 || !offered[root] || !offered[inRole] || !offered[child] {
			t.Fatalf("the runnable job is %v, want the root, the role and the child", titlesOf(runnable))
		}
		if offered[waiting] {
			t.Errorf("job that waits for something else was offered to a controller that cannot order it")
		}
		if offered[stopped] {
			t.Errorf("job a person stopped was offered to a controller")
		}
	})

	// What a job requires survives the store, which is the whole of the boundary the
	// controller checks: a field that came back empty would refuse nothing and look exactly like a
	// boundary that held.
	t.Run("what a job requires is kept", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		demanding := jobShaped(t, s, workspace, project, "needs the system's context", func(w *job.Job) {
			w.Role, w.RoleVersion = "backlog-clearer", 1
			w.Requires = []string{"context", "skills"}
		})
		bare := declaredJob(t, s, workspace, project, "requires nothing")

		kept, err := s.GetJob(ctx, demanding)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(kept.Requires) != 2 || kept.Requires[0] != "context" || kept.Requires[1] != "skills" {
			t.Fatalf("the job requires %v, want context and skills", kept.Requires)
		}
		plain, err := s.GetJob(ctx, bare)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if len(plain.Requires) != 0 {
			t.Fatalf("job that requires nothing requires %v, want nothing", plain.Requires)
		}
	})

	t.Run("the oldest declared job is offered first", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)

		first := declaredJob(t, s, workspace, project, "first")
		second := declaredJob(t, s, workspace, project, "second")

		runnable, err := s.RunnableJob(ctx, 0)
		if err != nil {
			t.Fatalf("RunnableJob: %v", err)
		}
		if len(runnable) != 2 {
			t.Fatalf("%d jobs are runnable, want 2", len(runnable))
		}
		if runnable[0].ID != first || runnable[1].ID != second {
			t.Fatalf("the job is offered as %v, want the oldest declared first", titlesOf(runnable))
		}
		// And a limit takes the oldest, rather than an arbitrary one.
		capped, err := s.RunnableJob(ctx, 1)
		if err != nil {
			t.Fatalf("RunnableJob: %v", err)
		}
		if len(capped) != 1 || capped[0].ID != first {
			t.Fatalf("a limit of one offered %v, want the oldest", titlesOf(capped))
		}
	})

	t.Run("job is claimed once, and a second claim is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")

		claimed, err := s.StartJob(ctx, id, aLease("controller-a"), []*job.Event{startedEvent(id, workspace, project)})
		if err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		if claimed.Phase != job.PhaseRunning {
			t.Fatalf("claimed job is %q, want running", claimed.Phase)
		}
		if claimed.Attempts != 1 {
			t.Fatalf("claimed job is on attempt %d, want 1", claimed.Attempts)
		}
		if claimed.StartedAt == nil {
			t.Fatal("claimed job does not carry when it started")
		}

		if _, err := s.StartJob(ctx, id, aLease("controller-a"), []*job.Event{startedEvent(id, workspace, project)}); !errors.Is(err, job.ErrNotPending) {
			t.Fatalf("the second claim answered %v, want ErrNotPending", err)
		}
		// And the refused claim wrote no record, or a listing would say the job started twice.
		events, err := s.ListJobEvents(ctx, id)
		if err != nil {
			t.Fatalf("ListJobEvents: %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("%d records exist, want the declaration and the one start", len(events))
		}
	})

	// A job the machine has no room for is held rather than moved. The phase stays pending, because
	// a machine that is full now has room in ten minutes and the job is still the next thing to run,
	// and the reason is the difference between a system that is full and a system that has stalled.
	t.Run("a pending job is held with a reason, and the reason goes when it starts", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")

		reason := "there is not enough memory for this job's sandbox: it asks for 1536 MiB, " +
			"512 MiB of 5605 MiB is unallocated"
		held, err := s.HoldJob(ctx, id, reason, heldEvent(id, workspace, project, reason))
		if err != nil {
			t.Fatalf("HoldJob: %!v(MISSING)", err)
		}
		if held.Phase != job.PhasePending {
			t.Fatalf("a held job is %q, want pending", held.Phase)
		}
		if held.Reason != reason {
			t.Fatalf("a held job says %q, want the reason it was given", held.Reason)
		}
		if held.Attempts != 0 {
			t.Fatalf("a held job is on attempt %d, and it never started", held.Attempts)
		}

		// It is still runnable, so the next tick with room on the machine picks it up.
		runnable, err := s.RunnableJob(ctx, 10)
		if err != nil {
			t.Fatalf("RunnableJob: %v", err)
		}
		if len(runnable) != 1 || runnable[0].ID != id {
			t.Fatalf("%d jobs are runnable, want the held one", len(runnable))
		}

		// And the reason goes with the wait it described, in the same statement that starts the job.
		started, err := s.StartJob(ctx, id, aLease("controller-a"),
			[]*job.Event{startedEvent(id, workspace, project)})
		if err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		if started.Reason != "" {
			t.Fatalf("a running job still says %q, which described the wait it is out of", started.Reason)
		}
	})

	// A hold can never overwrite how a job ended: what came of it is the useful part.
	t.Run("a job that already ended is not held", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, aLease("controller-a"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		if _, err := s.HoldJob(ctx, id, "there is not enough memory", nil); !errors.Is(err, job.ErrNotPending) {
			t.Fatalf("holding a running job answered %v, want ErrNotPending", err)
		}
	})

	t.Run("claiming job the system does not hold is not found", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		if _, err := s.StartJob(context.Background(), "0123456789abcdef01234567", aLease("controller-a"),
			[]*job.Event{startedEvent("0123456789abcdef01234567", "w", "p")}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("claiming missing job answered %v, want ErrNotFound", err)
		}
		_ = ctx
	})

	t.Run("the session the job runs in is written onto the row", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, aLease("controller-a"), []*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		if err := s.RecordJobSession(ctx, id, "session-1"); err != nil {
			t.Fatalf("RecordJobSession: %v", err)
		}

		found, err := s.GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if found.Session != "session-1" {
			t.Fatalf("the job says session %q", found.Session)
		}
		// Started job is what a controller comes back to, and only once it has a session: without
		// one there is no task to read.
		started, err := s.HeldJob(ctx, "controller-a", 0)
		if err != nil {
			t.Fatalf("StartedJob: %v", err)
		}
		if len(started) != 1 || started[0].ID != id {
			t.Fatalf("the started job is %v, want the one that is running", titlesOf(started))
		}
	})

	t.Run("job that is running with no session yet is not offered to be read back", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, aLease("controller-a"), []*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		started, err := s.HeldJob(ctx, "controller-a", 0)
		if err != nil {
			t.Fatalf("StartedJob: %v", err)
		}
		if len(started) != 0 {
			t.Fatalf("%d jobs were offered with no session behind them", len(started))
		}
	})

	t.Run("what came of the job is written with its record, once", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, aLease("controller-a"), []*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		const address = "https://github.com/atlantic-blue/quay-crew/pull/454"
		landed, err := s.LandJob(ctx, id, job.Landing{
			Phase: job.PhaseDone, Answer: "the bill is due on the 14th", SpentTokens: 1234,
			PullRequest: address, Outcome: job.OutcomeProved,
		}, answeredEvent(id, workspace, project))
		if err != nil {
			t.Fatalf("LandJob: %v", err)
		}
		if landed.Phase != job.PhaseDone || landed.Answer != "the bill is due on the 14th" {
			t.Fatalf("the job landed as %q saying %q", landed.Phase, landed.Answer)
		}
		// The word the job ended on, which is what a flow branches on and a listing filters by. The
		// answer beside it is the explanation rather than the signal.
		if landed.Outcome != job.OutcomeProved {
			t.Fatalf("the job landed with the outcome %q, want %q", landed.Outcome, job.OutcomeProved)
		}
		if reread, err := s.GetJob(ctx, id); err != nil || reread.Outcome != job.OutcomeProved {
			t.Fatalf("the job reads back with the outcome %v (%v), want %q", reread, err, job.OutcomeProved)
		}
		// Where the work went is on the row, so a reader finds it without opening the answer.
		if landed.PullRequest != address {
			t.Fatalf("the job landed saying its pull request is %q, want %s", landed.PullRequest, address)
		}
		if reread, err := s.GetJob(ctx, id); err != nil || reread.PullRequest != address {
			t.Fatalf("the job reads back saying its pull request is %v (%v), want %s", reread, err, address)
		}
		if landed.SpentTokens != 1234 {
			t.Fatalf("the job spent %d tokens", landed.SpentTokens)
		}
		if landed.FinishedAt == nil {
			t.Fatal("landed job does not carry when it finished")
		}
		if landed.ObservedVersion != landed.Version {
			t.Fatalf("the status describes version %d of a declaration at version %d",
				landed.ObservedVersion, landed.Version)
		}

		// A second landing is refused: the job has ended, and what it ended as is the useful part.
		if _, err := s.LandJob(ctx, id, job.Landing{Phase: job.PhaseFailed, Reason: "no"},
			answeredEvent(id, workspace, project)); !errors.Is(err, job.ErrNotRunning) {
			t.Fatalf("the second landing answered %v, want ErrNotRunning", err)
		}
		found, _ := s.GetJob(ctx, id)
		if found.Phase != job.PhaseDone {
			t.Fatalf("the second landing moved the job to %q", found.Phase)
		}
	})

	t.Run("landing job that never started is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")

		if _, err := s.LandJob(ctx, id, job.Landing{Phase: job.PhaseDone, Answer: "done"},
			answeredEvent(id, workspace, project)); !errors.Is(err, job.ErrNotRunning) {
			t.Fatalf("landing job that never started answered %v, want ErrNotRunning", err)
		}
	})
}

// jobShaped writes one job with a shape a controller must not pick up.
func jobShaped(t *testing.T, s store.Store, workspace, project, title string, shape func(*job.Job)) string {
	t.Helper()
	declared := &job.Job{
		ID: store.NewID(), Workspace: workspace, Project: project,
		Title: title, Brief: "do it", Version: 1, Phase: job.PhasePending,
	}
	shape(declared)
	if err := s.CreateJob(context.Background(), declared, declaredEvent(declared)); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return declared.ID
}

func titlesOf(listed []*job.Job) []string {
	titles := make([]string, 0, len(listed))
	for _, one := range listed {
		titles = append(titles, one.Title)
	}
	return titles
}

func eventKindsOf(events []*job.Event) []string {
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	return kinds
}

func startedEvent(id, workspace, project string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventStarted, Job: id,
		Workspace: workspace, Project: project, Detail: "attempt 1", OccurredAt: time.Now().UTC(),
	}
}

func answeredEvent(id, workspace, project string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventAnswered, Job: id,
		Workspace: workspace, Project: project, Detail: "1234 tokens", OccurredAt: time.Now().UTC(),
	}
}

// aLease is a hold long enough that no test outlives it by accident.
func aLease(owner string) job.Lease {
	return job.Lease{Owner: owner, Until: time.Now().UTC().Add(time.Minute)}
}

// anExpiredLease is a hold that has already run out, which is what a controller that went away left
// behind.
func anExpiredLease(owner string) job.Lease {
	return job.Lease{Owner: owner, Until: time.Now().UTC().Add(-time.Second)}
}

// runJobLeaseConformance holds both stores to what a lease means.
//
// A controller is disposable and the job is not. What is proved here is the compare and set: a
// claim applies only where the lease is free, a take over applies only where it has run out, and a
// renewal belongs to the holder alone. Neither store is allowed to be the lenient one.
func runJobLeaseConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a claim writes who holds the job and until when", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")

		lease := aLease("controller-a")
		claimed, err := s.StartJob(ctx, id, lease, []*job.Event{startedEvent(id, workspace, project)})
		if err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		if claimed.LeaseOwner != "controller-a" {
			t.Fatalf("the job is held by %q", claimed.LeaseOwner)
		}
		if claimed.LeaseUntil == nil || !claimed.LeaseUntil.After(time.Now()) {
			t.Fatalf("the lease runs to %v, want a moment still ahead", claimed.LeaseUntil)
		}
	})

	t.Run("job under a lease that still runs is nobody else's to take", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, aLease("controller-a"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		if _, err := s.TakeOverJob(ctx, id, aLease("controller-b"),
			[]*job.Event{claimedEvent(id, workspace, project)}); !errors.Is(err, job.ErrHeld) {
			t.Fatalf("taking over held job answered %v, want ErrHeld", err)
		}
		found, _ := s.GetJob(ctx, id)
		if found.LeaseOwner != "controller-a" {
			t.Fatalf("the job is now held by %q, and the lease had not run out", found.LeaseOwner)
		}
		// And it is not offered to another controller as abandoned.
		expired, err := s.ExpiredJob(ctx, 0)
		if err != nil {
			t.Fatalf("ExpiredJob: %v", err)
		}
		if len(expired) != 0 {
			t.Fatalf("%d jobs are offered as abandoned while their lease runs", len(expired))
		}
	})

	t.Run("job whose lease has run out is offered, taken over once, and only once", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, anExpiredLease("controller-a"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		expired, err := s.ExpiredJob(ctx, 0)
		if err != nil {
			t.Fatalf("ExpiredJob: %v", err)
		}
		if len(expired) != 1 || expired[0].ID != id {
			t.Fatalf("the abandoned job is %v, want the one whose lease ran out", titlesOf(expired))
		}

		taken, err := s.TakeOverJob(ctx, id, aLease("controller-b"),
			[]*job.Event{claimedEvent(id, workspace, project)})
		if err != nil {
			t.Fatalf("TakeOverJob: %v", err)
		}
		if taken.LeaseOwner != "controller-b" {
			t.Fatalf("the job is held by %q after the take over", taken.LeaseOwner)
		}
		if taken.Phase != job.PhaseRunning {
			t.Fatalf("taking over moved the job to %q, and a take over moves nothing but the lease", taken.Phase)
		}
		// A third controller finds a lease that runs, so it leaves it alone.
		if _, err := s.TakeOverJob(ctx, id, aLease("controller-c"),
			[]*job.Event{claimedEvent(id, workspace, project)}); !errors.Is(err, job.ErrHeld) {
			t.Fatalf("a second take over answered %v, want ErrHeld", err)
		}
	})

	t.Run("the job a controller holds is its own, and no other controller is offered it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, aLease("controller-a"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		if err := s.RecordJobSession(ctx, id, "session-1"); err != nil {
			t.Fatalf("RecordJobSession: %v", err)
		}

		mine, err := s.HeldJob(ctx, "controller-a", 0)
		if err != nil {
			t.Fatalf("HeldJob: %v", err)
		}
		if len(mine) != 1 || mine[0].ID != id {
			t.Fatalf("the holder is offered %v, want its own job", titlesOf(mine))
		}

		theirs, err := s.HeldJob(ctx, "controller-b", 0)
		if err != nil {
			t.Fatalf("HeldJob: %v", err)
		}
		if len(theirs) != 0 {
			t.Fatalf("a controller holding nothing is offered %v, which is another controller's job",
				titlesOf(theirs))
		}
	})

	t.Run("only the holder renews, and a renewal moves the hold on", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		claimed, err := s.StartJob(ctx, id, job.Lease{
			Owner: "controller-a", Until: time.Now().UTC().Add(10 * time.Second),
		}, []*job.Event{startedEvent(id, workspace, project)})
		if err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		if err := s.RenewLease(ctx, id, aLease("controller-a")); err != nil {
			t.Fatalf("RenewLease: %v", err)
		}
		found, _ := s.GetJob(ctx, id)
		if !found.LeaseUntil.After(*claimed.LeaseUntil) {
			t.Fatalf("the lease still runs to %v, want it moved on from %v", found.LeaseUntil, claimed.LeaseUntil)
		}

		if err := s.RenewLease(ctx, id, aLease("controller-b")); !errors.Is(err, job.ErrHeld) {
			t.Fatalf("a controller that holds nothing renewed and got %v, want ErrHeld", err)
		}
		after, _ := s.GetJob(ctx, id)
		if after.LeaseOwner != "controller-a" {
			t.Fatalf("the job is held by %q after somebody else renewed", after.LeaseOwner)
		}
	})

	t.Run("job whose holder went away before dispatching goes back to pending", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, anExpiredLease("controller-a"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		released, err := s.ReleaseJob(ctx, id, []*job.Event{releasedEvent(id, workspace, project)})
		if err != nil {
			t.Fatalf("ReleaseJob: %v", err)
		}
		if released.Phase != job.PhasePending {
			t.Fatalf("the released job is %q, want pending", released.Phase)
		}
		if released.LeaseOwner != "" || released.LeaseUntil != nil {
			t.Fatalf("the released job is still held by %q until %v", released.LeaseOwner, released.LeaseUntil)
		}
		// And it is offered to be started again, because nothing was ever paid for.
		runnable, _ := s.RunnableJob(ctx, 0)
		if len(runnable) != 1 || runnable[0].ID != id {
			t.Fatalf("the runnable job is %v, want the one that was released", titlesOf(runnable))
		}
	})

	// The holder giving up an attempt it made, which is a different movement from a release: the job
	// has a session and a live lease, and the controller that holds it is the one putting it back.
	t.Run("a job the system could not start goes back to pending, and is offered again", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, aLease("controller-a"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		if err := s.RecordJobSession(ctx, id, "session-1"); err != nil {
			t.Fatalf("RecordJobSession: %v", err)
		}

		back := job.Requeue{Owner: "controller-a", Reason: "it waits for room"}
		put, err := s.RequeueJob(ctx, id, back, []*job.Event{releasedEvent(id, workspace, project)})
		if err != nil {
			t.Fatalf("RequeueJob: %v", err)
		}
		if put.Phase != job.PhasePending {
			t.Fatalf("the job is %q, want pending", put.Phase)
		}
		if put.Reason != "it waits for room" {
			t.Fatalf("the job says %q, want why it is waiting", put.Reason)
		}
		if put.LeaseOwner != "" || put.LeaseUntil != nil {
			t.Fatalf("the job is still held by %q until %v", put.LeaseOwner, put.LeaseUntil)
		}
		if put.FinishedAt != nil {
			t.Fatalf("a job that never started carries a finish at %v", put.FinishedAt)
		}
		// What it has already cost stays on the row: attempts is how many times it was tried.
		if put.Attempts != 1 {
			t.Fatalf("the job has been tried %d times, want 1", put.Attempts)
		}
		// And the record of the movement landed with it, in the same transaction.
		events, err := s.ListJobEvents(ctx, id)
		if err != nil {
			t.Fatalf("ListJobEvents: %v", err)
		}
		if kinds := eventKindsOf(events); kinds[len(kinds)-1] != job.EventReleased {
			t.Fatalf("the records read %v, want the last to say the job was given up", kinds)
		}

		runnable, _ := s.RunnableJob(ctx, 0)
		if len(runnable) != 1 || runnable[0].ID != id {
			t.Fatalf("the runnable job is %v, want the one that was put back", titlesOf(runnable))
		}
	})

	t.Run("a job another controller holds is not one this controller may put back", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, aLease("controller-a"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		back := job.Requeue{Owner: "controller-b", Reason: "it waits for room"}
		if _, err := s.RequeueJob(ctx, id, back,
			[]*job.Event{releasedEvent(id, workspace, project)}); !errors.Is(err, job.ErrHeld) {
			t.Fatalf("a controller put another controller's job back and got %v, want ErrHeld", err)
		}
		found, _ := s.GetJob(ctx, id)
		if found.Phase != job.PhaseRunning || found.LeaseOwner != "controller-a" {
			t.Fatalf("the job is %q held by %q, want running and still held", found.Phase, found.LeaseOwner)
		}
	})

	t.Run("a job that already ended is never put back", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, aLease("controller-a"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		if _, err := s.LandJob(ctx, id, job.Landing{Phase: job.PhaseDone, Answer: "the 14th"},
			answeredEvent(id, workspace, project)); err != nil {
			t.Fatalf("LandJob: %v", err)
		}

		back := job.Requeue{Owner: "controller-a", Reason: "it waits for room"}
		if _, err := s.RequeueJob(ctx, id, back,
			[]*job.Event{releasedEvent(id, workspace, project)}); !errors.Is(err, job.ErrHeld) {
			t.Fatalf("a job that had already ended was put back and got %v, want ErrHeld", err)
		}
		found, _ := s.GetJob(ctx, id)
		if found.Phase != job.PhaseDone || found.Answer != "the 14th" {
			t.Fatalf("the job is %q answering %q, want the answer it ended with", found.Phase, found.Answer)
		}
	})

	t.Run("job with a session is never released, because its task was paid for", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, anExpiredLease("controller-a"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}
		if err := s.RecordJobSession(ctx, id, "session-1"); err != nil {
			t.Fatalf("RecordJobSession: %v", err)
		}

		if _, err := s.ReleaseJob(ctx, id, []*job.Event{releasedEvent(id, workspace, project)}); !errors.Is(err, job.ErrHeld) {
			t.Fatalf("job with a task behind it was released and got %v, want ErrHeld", err)
		}
	})

	t.Run("job that ended holds no lease", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, aLease("controller-a"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		landed, err := s.LandJob(ctx, id, job.Landing{Phase: job.PhaseDone, Answer: "done"},
			answeredEvent(id, workspace, project))
		if err != nil {
			t.Fatalf("LandJob: %v", err)
		}
		if landed.LeaseOwner != "" || landed.LeaseUntil != nil {
			t.Fatalf("job that ended is held by %q until %v", landed.LeaseOwner, landed.LeaseUntil)
		}
		// And nothing offers it as abandoned, however long ago its lease would have run out.
		expired, _ := s.ExpiredJob(ctx, 0)
		if len(expired) != 0 {
			t.Fatalf("%d pieces of finished job are offered as abandoned", len(expired))
		}
	})

	t.Run("job a person stopped holds no lease either", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, aLease("controller-a"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		stopped, err := s.StopJob(ctx, id, "the bill is not due yet",
			stoppedEvent(id, workspace, project, "the bill is not due yet"))
		if err != nil {
			t.Fatalf("StopJob: %v", err)
		}
		if stopped.LeaseOwner != "" || stopped.LeaseUntil != nil {
			t.Fatalf("stopped job is held by %q until %v", stopped.LeaseOwner, stopped.LeaseUntil)
		}
	})

	t.Run("the record of a take over is written with it", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, project := aProject(t, s)
		id := declaredJob(t, s, workspace, project, "read the electricity bill")
		if _, err := s.StartJob(ctx, id, anExpiredLease("controller-a"),
			[]*job.Event{startedEvent(id, workspace, project)}); err != nil {
			t.Fatalf("StartJob: %v", err)
		}

		if _, err := s.TakeOverJob(ctx, id, aLease("controller-b"), []*job.Event{
			releasedEvent(id, workspace, project), claimedEvent(id, workspace, project),
		}); err != nil {
			t.Fatalf("TakeOverJob: %v", err)
		}

		events, err := s.ListJobEvents(ctx, id)
		if err != nil {
			t.Fatalf("ListJobEvents: %v", err)
		}
		want := []string{job.EventDeclared, job.EventStarted, job.EventReleased, job.EventClaimed}
		got := make([]string, 0, len(events))
		for _, event := range events {
			got = append(got, event.Kind)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("the records read %v, want %v", got, want)
		}
	})
}

func claimedEvent(id, workspace, project string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventClaimed, Job: id,
		Workspace: workspace, Project: project, Detail: "lease_owner controller-b",
		OccurredAt: time.Now().UTC(),
	}
}

func releasedEvent(id, workspace, project string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventReleased, Job: id,
		Workspace: workspace, Project: project, Detail: "previous owner controller-a, phase found running",
		OccurredAt: time.Now().UTC(),
	}
}

// runWorkspaceLimitsConformance holds both stores to what a ceiling means.
//
// The one that matters is the default: a workspace nobody configured must answer with a depth of
// zero, because that is what stops a session declaring job until an operator says otherwise. A
// store that answered "not found" instead would make a system that grants everything until it is
// configured, which is the wrong direction to fail in.
func runWorkspaceLimitsConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	t.Run("a workspace nobody configured allows no depth at all", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, _ := aProject(t, s)

		limits, err := s.WorkspaceLimits(ctx, workspace)
		if err != nil {
			t.Fatalf("WorkspaceLimits: %v", err)
		}
		if limits.MaxDepth != 0 {
			t.Fatalf("a workspace nobody configured allows depth %d, want 0", limits.MaxDepth)
		}
		if limits.MaxRunning != 0 || limits.BudgetTokens != 0 || limits.LeaseSeconds != 0 {
			t.Fatalf("a workspace nobody configured carries %+v, want every limit unset", limits)
		}
		// The rule this slice ships on. Both times are absent, and absent means the controller takes
		// no container back and files nothing away. A default written here would be a number nobody
		// measured deciding how long an operator may leave a conversation open.
		if limits.ReclaimSeconds != 0 || limits.ArchiveSeconds != 0 {
			t.Fatalf("a workspace nobody configured reclaims after %ds and archives after %ds, "+
				"and both ship unset because no measurement has set either",
				limits.ReclaimSeconds, limits.ArchiveSeconds)
		}
		if limits.Reclaim() != 0 || limits.Archive() != 0 {
			t.Fatalf("the times read as %s and %s, and unset has to read as zero so the controller "+
				"does nothing", limits.Reclaim(), limits.Archive())
		}
	})

	t.Run("the reclaim and archive times are written whole and read back", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		workspace, _ := aProject(t, s)

		if _, err := s.SetWorkspaceLimits(ctx, job.Limits{
			Workspace: workspace, ReclaimSeconds: 900, ArchiveSeconds: 86400,
		}); err != nil {
			t.Fatalf("SetWorkspaceLimits: %v", err)
		}

		read, err := open(t).WorkspaceLimits(ctx, workspace)
		if err != nil {
			t.Fatalf("WorkspaceLimits: %v", err)
		}
		if read.Reclaim() != 15*time.Minute {
			t.Fatalf("the reclaim time reads back as %s, want the 900 seconds that were written",
				read.Reclaim())
		}
		if read.Archive() != 24*time.Hour {
			t.Fatalf("the archive time reads back as %s, want the 86400 seconds that were written",
				read.Archive())
		}
	})

	t.Run("a ceiling is written whole and read back", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		workspace, _ := aProject(t, s)

		written, err := s.SetWorkspaceLimits(ctx, job.Limits{
			Workspace: workspace, MaxDepth: 2, MaxRunning: 4, BudgetTokens: 5000, LeaseSeconds: 90,
		})
		if err != nil {
			t.Fatalf("SetWorkspaceLimits: %v", err)
		}
		if written.MaxDepth != 2 || written.MaxRunning != 4 || written.BudgetTokens != 5000 ||
			written.LeaseSeconds != 90 {
			t.Fatalf("the ceiling was written as %+v", written)
		}

		// Reopened, because a ceiling that only exists in the process that set it is not a ceiling.
		read, err := open(t).WorkspaceLimits(ctx, workspace)
		if err != nil {
			t.Fatalf("WorkspaceLimits: %v", err)
		}
		if read.MaxDepth != 2 || read.MaxRunning != 4 || read.BudgetTokens != 5000 || read.LeaseSeconds != 90 {
			t.Fatalf("the ceiling reads back as %+v", read)
		}
	})

	t.Run("writing a ceiling again replaces it rather than adding one", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, _ := aProject(t, s)

		if _, err := s.SetWorkspaceLimits(ctx, job.Limits{Workspace: workspace, MaxDepth: 3, MaxRunning: 9}); err != nil {
			t.Fatalf("SetWorkspaceLimits: %v", err)
		}
		written, err := s.SetWorkspaceLimits(ctx, job.Limits{Workspace: workspace, MaxDepth: 1})
		if err != nil {
			t.Fatalf("SetWorkspaceLimits: %v", err)
		}

		if written.MaxDepth != 1 {
			t.Fatalf("the depth is %d, want the one just written", written.MaxDepth)
		}
		if written.MaxRunning != 0 {
			t.Fatalf("the concurrency is %d, and the whole row is written, so it should be back to unset",
				written.MaxRunning)
		}
	})

	t.Run("one workspace's ceiling is not another's", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, _ := aProject(t, s)
		other, err := s.CreateWorkspace(ctx, "other")
		if err != nil {
			t.Fatalf("CreateWorkspace: %v", err)
		}

		if _, err := s.SetWorkspaceLimits(ctx, job.Limits{Workspace: workspace, MaxDepth: 2}); err != nil {
			t.Fatalf("SetWorkspaceLimits: %v", err)
		}

		theirs, err := s.WorkspaceLimits(ctx, other.GetId())
		if err != nil {
			t.Fatalf("WorkspaceLimits: %v", err)
		}
		if theirs.MaxDepth != 0 {
			t.Fatalf("the other workspace allows depth %d, and nobody raised it", theirs.MaxDepth)
		}
	})

	t.Run("the lease a workspace names is what a controller holds job for", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()
		workspace, _ := aProject(t, s)

		if _, err := s.SetWorkspaceLimits(ctx, job.Limits{Workspace: workspace, LeaseSeconds: 90}); err != nil {
			t.Fatalf("SetWorkspaceLimits: %v", err)
		}
		limits, err := s.WorkspaceLimits(ctx, workspace)
		if err != nil {
			t.Fatalf("WorkspaceLimits: %v", err)
		}

		if got := limits.Lease(time.Minute); got != 90*time.Second {
			t.Fatalf("a hold here lasts %s, want the 90 seconds the workspace named", got)
		}
		unset, _ := s.WorkspaceLimits(ctx, "nobody")
		if got := unset.Lease(time.Minute); got != time.Minute {
			t.Fatalf("a hold where nothing is named lasts %s, want the system's own", got)
		}
	})
}

// heldEvent is the record the system writes when it will not start a job yet.
func heldEvent(id, workspace, project, reason string) *job.Event {
	return &job.Event{
		ID: store.NewID(), Kind: job.EventHeld, Job: id,
		Workspace: workspace, Project: project, Detail: reason, OccurredAt: time.Now().UTC(),
	}
}
