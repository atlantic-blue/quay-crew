package work_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/work"
)

// A controller reads what is declared, compares it against the world, and closes the gap. These
// tests drive one tick at a time against doubles for the two things it touches: the rows, and the
// one interface every other caller of the crew uses.

// crew is a control plane double. It records what it was asked to run and answers with a session,
// the way a dispatch that lets go of its task answers.
type crew struct {
	mu sync.Mutex
	// dispatched is every task the controller asked the crew to run.
	dispatched []*quaycrewv1.DispatchRequest
	// tasks is the history each session has, which is what the controller reads an answer off.
	tasks map[string][]*quaycrewv1.Task
	// refuse makes the next dispatch fail, which is a crew that could not make a sandbox.
	refuse error
}

func newCrew() *crew { return &crew{tasks: map[string][]*quaycrewv1.Task{}} }

func (c *crew) Dispatch(_ context.Context, req *quaycrewv1.DispatchRequest) (*quaycrewv1.DispatchResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refuse != nil {
		refused := c.refuse
		c.refuse = nil
		return nil, refused
	}
	c.dispatched = append(c.dispatched, req)
	session := "session-" + req.GetHandle()
	// A task is written when it starts, so the history carries an open row before any answer.
	c.tasks[session] = append(c.tasks[session], &quaycrewv1.Task{
		Id: fmt.Sprintf("task-%d", len(c.tasks[session])+1), Session: session,
		Prompt: req.GetText(), Status: "running",
	})
	return &quaycrewv1.DispatchResponse{Id: session, Handle: req.GetHandle()}, nil
}

func (c *crew) ListTasks(_ context.Context, req *quaycrewv1.ListTasksRequest) (*quaycrewv1.ListTasksResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return &quaycrewv1.ListTasksResponse{Tasks: c.tasks[req.GetSession()]}, nil
}

// lands closes the open task of the session the controller started, the way a model answering does.
func (c *crew) lands(reply string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, tasks := range c.tasks {
		for _, task := range tasks {
			if task.Status == "running" {
				task.Status, task.Reply = "idle", reply
			}
		}
	}
}

// fails closes the open task as the model refusing it.
func (c *crew) fails(why string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, tasks := range c.tasks {
		for _, task := range tasks {
			if task.Status == "running" {
				task.Status, task.Failure = "failed", why
			}
		}
	}
}

func (c *crew) sent() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.dispatched)
}

// rows is a store double: the smallest set of rows a controller reads and writes.
type rows struct {
	mu     sync.Mutex
	held   map[string]*work.Work
	events map[string][]*work.Event
	order  []string
	// refuseStart makes the claim fail, which is a database that went away mid tick.
	refuseStart error
	// beforeStart runs before each claim, so a test can put two callers inside it at the same moment.
	beforeStart func()
}

func newRows() *rows {
	return &rows{held: map[string]*work.Work{}, events: map[string][]*work.Event{}}
}

func (r *rows) add(one *work.Work) *work.Work {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := *one
	r.held[one.ID] = &kept
	r.order = append(r.order, one.ID)
	return &kept
}

func (r *rows) RunnableWork(_ context.Context, limit int) ([]*work.Work, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*work.Work{}
	for _, id := range r.order {
		one := r.held[id]
		if one.Phase != work.PhasePending || one.Parent != "" || one.Role != "" || len(one.After) > 0 {
			continue
		}
		kept := *one
		out = append(out, &kept)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

func (r *rows) StartedWork(_ context.Context, limit int) ([]*work.Work, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*work.Work{}
	for _, id := range r.order {
		one := r.held[id]
		if one.Phase != work.PhaseRunning || one.Session == "" {
			continue
		}
		kept := *one
		out = append(out, &kept)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

func (r *rows) StartWork(_ context.Context, id string, event *work.Event) (*work.Work, error) {
	// Before the lock, so a test can hold two callers inside this call at once and make the claim
	// answer the question it exists for.
	if r.beforeStart != nil {
		r.beforeStart()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refuseStart != nil {
		return nil, r.refuseStart
	}
	one, held := r.held[id]
	if !held {
		return nil, errors.New("no such work")
	}
	// The claim is conditional, which is the whole of the idempotency: a second tick over the same
	// row finds it is no longer pending and does nothing.
	if one.Phase != work.PhasePending {
		return nil, work.ErrNotPending
	}
	now := time.Now().UTC()
	one.Phase, one.Attempts = work.PhaseRunning, one.Attempts+1
	one.StartedAt, one.UpdatedAt = &now, now
	r.events[id] = append(r.events[id], event)
	kept := *one
	return &kept, nil
}

func (r *rows) RecordWorkSession(_ context.Context, id, session string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	one, held := r.held[id]
	if !held {
		return errors.New("no such work")
	}
	one.Session = session
	return nil
}

func (r *rows) LandWork(_ context.Context, id string, landed work.Landing, event *work.Event) (*work.Work, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	one, held := r.held[id]
	if !held {
		return nil, errors.New("no such work")
	}
	if one.Phase != work.PhaseRunning {
		return nil, work.ErrNotRunning
	}
	now := time.Now().UTC()
	one.Phase, one.Answer, one.Reason = landed.Phase, landed.Answer, landed.Reason
	one.SpentTokens, one.ObservedVersion = landed.SpentTokens, one.Version
	one.FinishedAt, one.UpdatedAt = &now, now
	r.events[id] = append(r.events[id], event)
	kept := *one
	return &kept, nil
}

func (r *rows) get(id string) *work.Work {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := *r.held[id]
	return &kept
}

func (r *rows) kinds(id string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.events[id]))
	for _, event := range r.events[id] {
		out = append(out, event.Kind)
	}
	return out
}

// declared is one piece of root work, pending, the way CreateWork leaves it.
func declaredWork(title string) *work.Work {
	return &work.Work{
		ID: "work-1", Workspace: "workspace-1", Project: "project-1",
		Title: title, Brief: "open the bill and say when it is due",
		Version: 1, Phase: work.PhasePending,
	}
}

// aController is a controller over the two doubles.
func aController(t *testing.T) (*work.Controller, *rows, *crew) {
	t.Helper()
	kept, plane := newRows(), newCrew()
	return work.NewController(kept, plane, nil, nil, nil), kept, plane
}

// The whole of what this slice buys: declared work runs, and the answer lands on the record.
func TestDeclaredWorkRunsAndTheAnswerLandsOnTheRecord(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks, want 1", plane.sent())
	}
	if got := kept.get(one.ID); got.Phase != work.PhaseRunning || got.Session == "" {
		t.Fatalf("the work is %q in session %q, want running in a session", got.Phase, got.Session)
	}

	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != work.PhaseDone {
		t.Fatalf("the work is %q, want done", got.Phase)
	}
	if got.Answer != "the bill is due on the 14th" {
		t.Fatalf("the answer is %q", got.Answer)
	}
	if got.StartedAt == nil || got.FinishedAt == nil {
		t.Fatal("the work does not carry when it started and when it finished")
	}
	if got.ObservedVersion != got.Version {
		t.Fatalf("the status describes version %d of a declaration at version %d", got.ObservedVersion, got.Version)
	}
}

// The brief is what the session is asked to do, and the session is named after the work, so a second
// controller can find it again without being told.
func TestTheTaskCarriesTheBriefIntoASessionNamedAfterTheWork(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredWork("read the electricity bill"))

	controller.Tick(context.Background())

	sent := plane.dispatched[0]
	if sent.GetText() != one.Brief {
		t.Fatalf("the task says %q, want the brief", sent.GetText())
	}
	if sent.GetProject() != one.Project {
		t.Fatalf("the task runs in project %q, want %q", sent.GetProject(), one.Project)
	}
	if !strings.Contains(sent.GetHandle(), one.ID) {
		t.Fatalf("the session is called %q, want it named after the work", sent.GetHandle())
	}
	if !sent.GetDetach() {
		t.Fatal("the controller waited for the task, and a controller that waits on a model stops controlling")
	}
}

// Ticking again must not send the task again. Work is paid for, so twice is money.
func TestTickingAgainNeverSendsTheTaskTwice(t *testing.T) {
	controller, kept, plane := aController(t)
	kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()

	for range 3 {
		controller.Tick(ctx)
	}

	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks, want 1 however often the loop ticks", plane.sent())
	}
}

// Two controllers over the same row is the same question asked concurrently, and the claim is what
// answers it: one wins the row, the other finds it no longer pending.
func TestTwoControllersOverOneRowSendOneTask(t *testing.T) {
	kept, plane := newRows(), newCrew()
	first := work.NewController(kept, plane, nil, nil, nil)
	second := work.NewController(kept, plane, nil, nil, nil)
	kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()

	// Both controllers are held inside the claim until the other arrives, so this is the race the
	// claim exists to answer rather than two ticks that happened to take turns.
	var arrived sync.WaitGroup
	arrived.Add(2)
	kept.beforeStart = func() {
		arrived.Done()
		arrived.Wait()
	}

	var waiting sync.WaitGroup
	waiting.Add(2)
	go func() { defer waiting.Done(); first.Tick(ctx) }()
	go func() { defer waiting.Done(); second.Tick(ctx) }()
	waiting.Wait()

	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks, want 1", plane.sent())
	}
}

// A task still open is work still running. Reading it must not move anything.
func TestWorkWhoseTaskIsStillOpenIsLeftAlone(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != work.PhaseRunning {
		t.Fatalf("the work is %q, want running while its task is open", got.Phase)
	}
	if got.Answer != "" || got.FinishedAt != nil {
		t.Fatal("work whose task is still open carries an answer or a finish")
	}
	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks", plane.sent())
	}
}

func TestATaskThatFailedLeavesTheWorkFailedSayingWhy(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.fails("the model refused this task")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != work.PhaseFailed {
		t.Fatalf("the work is %q, want failed", got.Phase)
	}
	if !strings.Contains(got.Reason, "the model refused this task") {
		t.Fatalf("the reason is %q, want what the model said", got.Reason)
	}
}

// A dispatch the crew refuses is work that cannot run. It is failed with the reason rather than left
// running forever with nothing behind it.
func TestADispatchTheCrewRefusesFailsTheWorkWithTheReason(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredWork("read the electricity bill"))
	plane.refuse = errors.New("no sandbox could be made")

	controller.Tick(context.Background())

	got := kept.get(one.ID)
	if got.Phase != work.PhaseFailed {
		t.Fatalf("the work is %q, want failed", got.Phase)
	}
	if !strings.Contains(got.Reason, "no sandbox could be made") {
		t.Fatalf("the reason is %q, want what the crew said", got.Reason)
	}
}

// The claim is checked by the crew rather than believed from the model, which is what it is for.
func TestAnAnswerThatDoesNotCarryWhatWasClaimedStopsTheWork(t *testing.T) {
	controller, kept, plane := aController(t)
	declared := declaredWork("read the electricity bill")
	declared.ExpectContains = "paid"
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != work.PhaseStopped {
		t.Fatalf("the work is %q, want stopped", got.Phase)
	}
	if !strings.Contains(got.Reason, "paid") {
		t.Fatalf("the reason is %q, want it to name what was claimed", got.Reason)
	}
	// The answer stays, because what the model said is how somebody works out why the claim failed.
	if got.Answer != "the bill is due on the 14th" {
		t.Fatalf("the answer is %q, want what the model said", got.Answer)
	}
}

func TestAnAnswerThatCarriesWhatWasClaimedIsDone(t *testing.T) {
	controller, kept, plane := aController(t)
	declared := declaredWork("pay the electricity bill")
	declared.ExpectContains = "paid"
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is paid")
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.Phase != work.PhaseDone {
		t.Fatalf("the work is %q, want done", got.Phase)
	}
}

// A file the work said would be there is asked about, not believed.
func TestAFileTheWorkClaimedIsCheckedAndItsAbsenceStopsTheWork(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := work.NewController(kept, plane, nil, holds(false), nil)
	declared := declaredWork("read the electricity bill")
	declared.ExpectFile = "notes/bill.md"
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I wrote the notes")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != work.PhaseStopped {
		t.Fatalf("the work is %q, want stopped", got.Phase)
	}
	if !strings.Contains(got.Reason, "notes/bill.md") {
		t.Fatalf("the reason is %q, want it to name the file", got.Reason)
	}
}

// A crew that cannot answer the question stops the work rather than passing it. A check that
// quietly passes when it could not be run is the same false green as no check at all.
func TestAClaimAboutAFileThatCannotBeCheckedStopsTheWork(t *testing.T) {
	controller, kept, plane := aController(t)
	declared := declaredWork("read the electricity bill")
	declared.ExpectFile = "notes/bill.md"
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I wrote the notes")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != work.PhaseStopped {
		t.Fatalf("the work is %q, want stopped when the claim cannot be checked", got.Phase)
	}
	if !strings.Contains(got.Reason, "notes/bill.md") {
		t.Fatalf("the reason is %q, want it to name the file", got.Reason)
	}
}

func TestAFileThatIsThereLeavesTheWorkDone(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := work.NewController(kept, plane, nil, holds(true), nil)
	declared := declaredWork("read the electricity bill")
	declared.ExpectFile = "notes/bill.md"
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I wrote the notes")
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.Phase != work.PhaseDone {
		t.Fatalf("the work is %q, want done", got.Phase)
	}
}

// What a piece of work cost is read from the conversation rather than from what the model said about
// itself.
func TestWhatTheWorkSpentIsWrittenOntoTheRecord(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := work.NewController(kept, plane, spent(1234), nil, nil)
	one := kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.SpentTokens != 1234 {
		t.Fatalf("the work spent %d tokens on the record, want 1234", got.SpentTokens)
	}
}

// Root work only in this slice. Everything else is a later one, and picking it up early would run
// work whose ordering, role or budget nothing here honours.
func TestOnlyRootWorkIsRun(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shape func(*work.Work)
	}{
		{"work that waits for something", func(w *work.Work) { w.After = []string{"work-0"} }},
		{"work in a role", func(w *work.Work) { w.Role, w.RoleVersion = "backlog-clearer", 1 }},
		{"work under a parent", func(w *work.Work) { w.Parent, w.Depth = "work-0", 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controller, kept, plane := aController(t)
			declared := declaredWork("read the electricity bill")
			tc.shape(declared)
			one := kept.add(declared)

			controller.Tick(context.Background())

			if plane.sent() != 0 {
				t.Fatalf("the crew was asked to run %d tasks, want none", plane.sent())
			}
			if got := kept.get(one.ID); got.Phase != work.PhasePending {
				t.Fatalf("the work is %q, want left pending", got.Phase)
			}
		})
	}
}

// Work in any phase but pending is not started, which covers the one a person stopped.
func TestWorkThatIsNotPendingIsNeverStarted(t *testing.T) {
	for _, phase := range []string{work.PhaseStopped, work.PhaseDone, work.PhaseFailed, work.PhaseWaiting, work.PhaseAsking} {
		controller, kept, plane := aController(t)
		declared := declaredWork("read the electricity bill")
		declared.Phase = phase
		kept.add(declared)

		controller.Tick(context.Background())

		if plane.sent() != 0 {
			t.Errorf("%s work was sent to the crew", phase)
		}
	}
}

// Every movement writes its own record, in the order it happened.
func TestEveryMovementIsOnTheRecord(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	if got := strings.Join(kept.kinds(one.ID), ","); got != work.EventStarted+","+work.EventAnswered {
		t.Fatalf("the records read %q, want the start then the answer", got)
	}
}

func TestWorkThatFailedRecordsThatItFailed(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredWork("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.fails("the model refused this task")
	controller.Tick(ctx)

	if got := strings.Join(kept.kinds(one.ID), ","); got != work.EventStarted+","+work.EventFailed {
		t.Fatalf("the records read %q, want the start then the failure", got)
	}
}

// One piece of work that cannot be claimed must not stop the others. A tick that gave up on the
// first row would leave a crew where one bad row stops every good one.
func TestWorkThatCannotBeClaimedDoesNotStopTheRest(t *testing.T) {
	controller, kept, plane := aController(t)
	kept.refuseStart = errors.New("the database went away")
	kept.add(declaredWork("read the electricity bill"))

	controller.Tick(context.Background())

	if plane.sent() != 0 {
		t.Fatalf("a task was sent for work that was never claimed")
	}
	// And the next tick, with the store back, runs it.
	kept.refuseStart = nil
	controller.Tick(context.Background())
	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks after the store came back, want 1", plane.sent())
	}
}

// A tick over an empty crew asks for nothing and writes nothing.
func TestATickWithNothingToDoDoesNothing(t *testing.T) {
	controller, _, plane := aController(t)

	controller.Tick(context.Background())

	if plane.sent() != 0 {
		t.Fatalf("the crew was asked to run %d tasks over an empty crew", plane.sent())
	}
}

// holds is a prover that answers the same way every time.
func holds(answer bool) work.Prover {
	return proverFunc(func(context.Context, string, string) (bool, error) { return answer, nil })
}

type proverFunc func(ctx context.Context, session, path string) (bool, error)

func (f proverFunc) SessionHolds(ctx context.Context, session, path string) (bool, error) {
	return f(ctx, session, path)
}

// spent is a reader that answers the same number every time.
func spent(tokens int64) work.Spend {
	return spendFunc(func(context.Context, string) int64 { return tokens })
}

type spendFunc func(ctx context.Context, session string) int64

func (f spendFunc) SessionTokens(ctx context.Context, session string) int64 { return f(ctx, session) }
