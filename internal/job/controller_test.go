package job_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/job"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	// sessions are the conversations the crew holds, which is what a controller reads when a row
	// says nothing about which one its job ran in.
	sessions []*quaycrewv1.Session
	// refuse makes the next dispatch fail, which is a crew that could not make a sandbox.
	refuse error
	// store is the rows this crew writes through, the way the real control plane writes a reclaim or
	// an archive into the store rather than keeping it in the process. Nil leaves the two calls
	// recording what they were asked and changing nothing, which is enough for the tests that do not
	// read a session back.
	store *rows
	// reclaims and archives are what the controller asked for, in order.
	reclaims, archives []string
	// refuseReclaim makes the first reclaim fail, which is a session that moved between the query
	// that found it and the write that would have taken its container.
	refuseReclaim error
	// seen is the context the last dispatch arrived under, so a test can say which trace the task
	// ran in rather than assume the controller passed one on.
	seen context.Context
}

func newCrew() *crew { return &crew{tasks: map[string][]*quaycrewv1.Task{}} }

// ReclaimSession takes a session's container back, the way the control plane does.
func (c *crew) ReclaimSession(_ context.Context, req *quaycrewv1.ReclaimSessionRequest) (
	*quaycrewv1.ReclaimSessionResponse, error) {
	c.mu.Lock()
	if c.refuseReclaim != nil {
		refused := c.refuseReclaim
		c.refuseReclaim = nil
		c.mu.Unlock()
		return nil, refused
	}
	c.reclaims = append(c.reclaims, req.GetId())
	store := c.store
	c.mu.Unlock()
	if store != nil {
		store.reclaimSession(req.GetId())
	}
	return &quaycrewv1.ReclaimSessionResponse{}, nil
}

// ArchiveSession files a session away, the way the control plane does.
func (c *crew) ArchiveSession(_ context.Context, req *quaycrewv1.ArchiveSessionRequest) (
	*quaycrewv1.ArchiveSessionResponse, error) {
	c.mu.Lock()
	c.archives = append(c.archives, req.GetId())
	store := c.store
	c.mu.Unlock()
	if store != nil {
		store.archiveSession(req.GetId())
	}
	return &quaycrewv1.ArchiveSessionResponse{}, nil
}

func (c *crew) reclaimed() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.reclaims...)
}

func (c *crew) archived() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.archives...)
}

func (c *crew) Dispatch(ctx context.Context, req *quaycrewv1.DispatchRequest) (*quaycrewv1.DispatchResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = ctx
	if c.refuse != nil {
		refused := c.refuse
		c.refuse = nil
		return nil, refused
	}
	c.dispatched = append(c.dispatched, req)
	session := "session-" + req.GetHandle()
	c.sessions = append(c.sessions, &quaycrewv1.Session{Id: session, Handle: req.GetHandle(), Project: req.GetProject()})
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

// dispatchedInto is a task the crew is already running in a session, which is what a controller that
// died between the dispatch and writing the session onto the row left behind.
func (c *crew) dispatchedInto(handle, project, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dispatched = append(c.dispatched, &quaycrewv1.DispatchRequest{Project: project, Handle: handle, Text: text})
	session := "session-" + handle
	c.sessions = append(c.sessions, &quaycrewv1.Session{Id: session, Handle: handle, Project: project})
	c.tasks[session] = append(c.tasks[session], &quaycrewv1.Task{
		Id: "task-1", Session: session, Prompt: text, Status: "running",
	})
}

func (c *crew) ListSessions(_ context.Context, _ *quaycrewv1.ListSessionsRequest) (*quaycrewv1.ListSessionsResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return &quaycrewv1.ListSessionsResponse{Sessions: append([]*quaycrewv1.Session(nil), c.sessions...)}, nil
}

// lastContext is what the last dispatch arrived under.
func (c *crew) lastContext() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen == nil {
		return context.Background()
	}
	return c.seen
}

func (c *crew) sent() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.dispatched)
}

// rows is a store double: the smallest set of rows a controller reads and writes.
type rows struct {
	mu     sync.Mutex
	held   map[string]*job.Job
	events map[string][]*job.Event
	order  []string
	// refuseStart makes the claim fail, which is a database that went away mid tick.
	refuseStart error
	// beforeStart and beforeTakeOver run before each of those calls, so a test can put two callers
	// inside one at the same moment and make the conditional write answer the question it exists for.
	beforeStart    func()
	beforeTakeOver func()
	// limits is what each workspace allows, for the tests about a hold as long as the workspace says.
	limits map[string]job.Limits
	// sessions are the conversations the crew holds, which the fourth query reads.
	sessions map[string]*quaycrewv1.Session
	// sessionOrder keeps them in the order they were added, so a listing is stable.
	sessionOrder []string
}

func newRows() *rows {
	return &rows{
		held: map[string]*job.Job{}, events: map[string][]*job.Event{},
		sessions: map[string]*quaycrewv1.Session{},
	}
}

// allow sets what a workspace permits, which is where the reclaim and archive times come from.
func (r *rows) allow(limits job.Limits) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.limits == nil {
		r.limits = map[string]job.Limits{}
	}
	r.limits[limits.Workspace] = limits
}

// addSession puts a conversation in the store.
func (r *rows) addSession(session *quaycrewv1.Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.GetId()] = session
	r.sessionOrder = append(r.sessionOrder, session.GetId())
}

func (r *rows) sessionStatus(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions[id].GetStatus()
}

// reclaimSession is what the control plane writes when it takes a container back.
func (r *rows) reclaimSession(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := timestamppb.New(time.Now().UTC())
	if session, held := r.sessions[id]; held {
		session.Status, session.ReclaimedAt, session.UpdatedAt = job.StatusReclaimed, now, now
	}
}

// archiveSession is what the control plane writes when it files one away.
func (r *rows) archiveSession(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if session, held := r.sessions[id]; held {
		session.ArchivedAt = timestamppb.New(time.Now().UTC())
		session.Status = "stopped"
	}
}

// reclaimedAgo moves a session's reclaim stamp back, which is a later tick arriving.
func (r *rows) reclaimedAgo(id string, ago time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[id].ReclaimedAt = timestamppb.New(time.Now().UTC().Add(-ago))
}

// SettledSessions is the sessions nothing is holding open, oldest touched first. It is the same rule
// the real stores are held to: live, not running, and named by no job in a non terminal phase.
func (r *rows) SettledSessions(_ context.Context, limit int) ([]*quaycrewv1.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	open := map[string]bool{}
	for _, one := range r.held {
		if one.Session != "" && !job.Terminal(one.Phase) {
			open[one.Session] = true
		}
	}
	settled := map[string]bool{"idle": true, "failed": true, job.StatusReclaimed: true}

	out := []*quaycrewv1.Session{}
	for _, id := range r.sessionOrder {
		session := r.sessions[id]
		if session.GetArchivedAt() != nil || !settled[session.GetStatus()] || open[id] {
			continue
		}
		out = append(out, proto.Clone(session).(*quaycrewv1.Session))
		if limit > 0 && len(out) == limit {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].GetUpdatedAt().AsTime().Before(out[j].GetUpdatedAt().AsTime())
	})
	return out, nil
}

func (r *rows) add(one *job.Job) *job.Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := *one
	r.held[one.ID] = &kept
	r.order = append(r.order, one.ID)
	return &kept
}

// claim puts a lease on a row by hand, which is what a controller that then died left behind.
func (r *rows) claim(id, owner string, until time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	one := r.held[id]
	one.Phase, one.LeaseOwner, one.LeaseUntil = job.PhaseRunning, owner, &until
	one.Attempts = 1
}

// setSession puts a session on a row by hand.
func (r *rows) setSession(id, session string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.held[id].Session = session
}

// WorkspaceLimits is what the workspace allows. A test that says nothing gets the defaults, which
// grant nothing, so a scenario about a limit has to set one.
func (r *rows) WorkspaceLimits(_ context.Context, workspace string) (job.Limits, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if held, set := r.limits[workspace]; set {
		return held, nil
	}
	return job.Limits{Workspace: workspace}, nil
}

func (r *rows) RunnableJob(_ context.Context, limit int) ([]*job.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// The same shape the real stores offer, job in a role included: a double that offered less than
	// the store does would hide every behaviour that only a job in a role reaches.
	return r.matching(limit, func(one *job.Job) bool {
		return one.Phase == job.PhasePending && len(one.After) == 0
	}), nil
}

func (r *rows) HeldJob(_ context.Context, owner string, limit int) ([]*job.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.matching(limit, func(one *job.Job) bool {
		return one.Phase == job.PhaseRunning && one.Session != "" &&
			one.LeaseOwner == owner && one.LeaseUntil != nil && one.LeaseUntil.After(time.Now())
	}), nil
}

func (r *rows) ExpiredJob(_ context.Context, limit int) ([]*job.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.matching(limit, func(one *job.Job) bool {
		return one.Phase == job.PhaseRunning &&
			(one.LeaseUntil == nil || !one.LeaseUntil.After(time.Now()))
	}), nil
}

// matching is every row that matches, capped. The caller holds the lock.
func (r *rows) matching(limit int, matches func(*job.Job) bool) []*job.Job {
	out := []*job.Job{}
	for _, id := range r.order {
		one := r.held[id]
		if !matches(one) {
			continue
		}
		kept := *one
		out = append(out, &kept)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out
}

func (r *rows) StartJob(_ context.Context, id string, lease job.Lease, events []*job.Event) (*job.Job, error) {
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
		return nil, errors.New("no such job")
	}
	// The claim is conditional, which is the whole of the idempotency: a second tick over the same
	// row finds it is no longer pending and does nothing.
	if one.Phase != job.PhasePending {
		return nil, job.ErrNotPending
	}
	now := time.Now().UTC()
	one.Phase, one.Attempts = job.PhaseRunning, one.Attempts+1
	one.LeaseOwner, one.LeaseUntil = lease.Owner, &lease.Until
	one.StartedAt, one.UpdatedAt = &now, now
	r.record(id, events)
	kept := *one
	return &kept, nil
}

func (r *rows) TakeOverJob(_ context.Context, id string, lease job.Lease, events []*job.Event) (*job.Job, error) {
	if r.beforeTakeOver != nil {
		r.beforeTakeOver()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	one, held := r.held[id]
	if !held {
		return nil, errors.New("no such job")
	}
	// Only job nobody is holding. A lease that still runs belongs to whoever wrote it.
	if one.Phase != job.PhaseRunning || (one.LeaseUntil != nil && one.LeaseUntil.After(time.Now())) {
		return nil, job.ErrHeld
	}
	one.LeaseOwner, one.LeaseUntil = lease.Owner, &lease.Until
	one.UpdatedAt = time.Now().UTC()
	r.record(id, events)
	kept := *one
	return &kept, nil
}

func (r *rows) ReleaseJob(_ context.Context, id string, events []*job.Event) (*job.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	one, held := r.held[id]
	if !held {
		return nil, errors.New("no such job")
	}
	if one.Phase != job.PhaseRunning || one.Session != "" ||
		(one.LeaseUntil != nil && one.LeaseUntil.After(time.Now())) {
		return nil, job.ErrHeld
	}
	one.Phase, one.LeaseOwner, one.LeaseUntil = job.PhasePending, "", nil
	one.StartedAt, one.UpdatedAt = nil, time.Now().UTC()
	r.record(id, events)
	kept := *one
	return &kept, nil
}

// RequeueJob puts a running job back to pending, the way a store does when the controller holding it
// could not start it. Only where this controller still holds the lease.
func (r *rows) RequeueJob(_ context.Context, id string, back job.Requeue, events []*job.Event) (*job.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	one, held := r.held[id]
	if !held {
		return nil, errors.New("no such job")
	}
	if one.Phase != job.PhaseRunning || one.LeaseOwner != back.Owner {
		return nil, job.ErrHeld
	}
	one.Phase, one.Reason = job.PhasePending, back.Reason
	one.LeaseOwner, one.LeaseUntil = "", nil
	one.StartedAt, one.UpdatedAt = nil, time.Now().UTC()
	r.record(id, events)
	kept := *one
	return &kept, nil
}

func (r *rows) RenewLease(_ context.Context, id string, lease job.Lease) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	one, held := r.held[id]
	if !held {
		return errors.New("no such job")
	}
	// Only the holder renews. A controller that lost the row must not take it back by renewing.
	if one.LeaseOwner != lease.Owner {
		return job.ErrHeld
	}
	one.LeaseUntil, one.UpdatedAt = &lease.Until, time.Now().UTC()
	return nil
}

func (r *rows) RecordJobSession(_ context.Context, id, session string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	one, held := r.held[id]
	if !held {
		return errors.New("no such job")
	}
	one.Session = session
	return nil
}

func (r *rows) LandJob(_ context.Context, id string, landed job.Landing, event *job.Event) (*job.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	one, held := r.held[id]
	if !held {
		return nil, errors.New("no such job")
	}
	if one.Phase != job.PhaseRunning {
		return nil, job.ErrNotRunning
	}
	now := time.Now().UTC()
	one.Phase, one.Answer, one.Reason = landed.Phase, landed.Answer, landed.Reason
	one.SpentTokens, one.ObservedVersion = landed.SpentTokens, one.Version
	one.LeaseOwner, one.LeaseUntil = "", nil
	one.FinishedAt, one.UpdatedAt = &now, now
	r.record(id, []*job.Event{event})
	kept := *one
	return &kept, nil
}

// record appends what happened, the way a store writes the events in the same transaction.
func (r *rows) record(id string, events []*job.Event) {
	for _, event := range events {
		if event != nil {
			r.events[id] = append(r.events[id], event)
		}
	}
}

func (r *rows) get(id string) *job.Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := *r.held[id]
	return &kept
}

func (r *rows) recorded(id string) []*job.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*job.Event(nil), r.events[id]...)
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

// declared is one piece of root job, pending, the way CreateJob leaves it.
func declaredJob(title string) *job.Job {
	return &job.Job{
		ID: "job-1", Workspace: "workspace-1", Project: "project-1",
		Title: title, Brief: "open the bill and say when it is due",
		Version: 1, Phase: job.PhasePending,
	}
}

// aController is a controller over the two doubles.
func aController(t *testing.T) (*job.Controller, *rows, *crew) {
	t.Helper()
	kept, plane := newRows(), newCrew()
	return job.NewController(kept, plane, nil, nil, nil), kept, plane
}

// The whole of what this slice buys: declared job runs, and the answer lands on the record.
func TestDeclaredJobRunsAndTheAnswerLandsOnTheRecord(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks, want 1", plane.sent())
	}
	if got := kept.get(one.ID); got.Phase != job.PhaseRunning || got.Session == "" {
		t.Fatalf("the job is %q in session %q, want running in a session", got.Phase, got.Session)
	}

	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q, want done", got.Phase)
	}
	if got.Answer != "the bill is due on the 14th" {
		t.Fatalf("the answer is %q", got.Answer)
	}
	if got.StartedAt == nil || got.FinishedAt == nil {
		t.Fatal("the job does not carry when it started and when it finished")
	}
	if got.ObservedVersion != got.Version {
		t.Fatalf("the status describes version %d of a declaration at version %d", got.ObservedVersion, got.Version)
	}
}

// The brief is what the session is asked to do, and the session is named after the job, so a second
// controller can find it again without being told.
func TestTheTaskCarriesTheBriefIntoASessionNamedAfterTheJob(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	sent := plane.dispatched[0]
	if sent.GetText() != one.Brief {
		t.Fatalf("the task says %q, want the brief", sent.GetText())
	}
	if sent.GetProject() != one.Project {
		t.Fatalf("the task runs in project %q, want %q", sent.GetProject(), one.Project)
	}
	if !strings.Contains(sent.GetHandle(), one.ID) {
		t.Fatalf("the session is called %q, want it named after the job", sent.GetHandle())
	}
	if !sent.GetDetach() {
		t.Fatal("the controller waited for the task, and a controller that waits on a model stops controlling")
	}
}

// Ticking again must not send the task again. A job is paid for, so twice is money.
func TestTickingAgainNeverSendsTheTaskTwice(t *testing.T) {
	controller, kept, plane := aController(t)
	kept.add(declaredJob("read the electricity bill"))
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
	first := job.NewController(kept, plane, nil, nil, nil)
	second := job.NewController(kept, plane, nil, nil, nil)
	kept.add(declaredJob("read the electricity bill"))
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

// A task still open is job still running. Reading it must not move anything.
func TestJobWhoseTaskIsStillOpenIsLeftAlone(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q, want running while its task is open", got.Phase)
	}
	if got.Answer != "" || got.FinishedAt != nil {
		t.Fatal("job whose task is still open carries an answer or a finish")
	}
	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks", plane.sent())
	}
}

func TestATaskThatFailedLeavesTheJobFailedSayingWhy(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.fails("the model refused this task")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseFailed {
		t.Fatalf("the job is %q, want failed", got.Phase)
	}
	if !strings.Contains(got.Reason, "the model refused this task") {
		t.Fatalf("the reason is %q, want what the model said", got.Reason)
	}
}

// The failure this whole slice exists for. A machine with no room to make a container is not a job
// that was wrong, so the job goes back to pending and a later tick tries it again.
//
// It used to be failed, which lost the work: nothing raised it, and the operator had one word in a
// listing that reads the same as a job that ran and did not work. See issue 465.
func TestAJobTheCrewCouldNotGiveASandboxGoesBackToPending(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.fails(job.NoSandbox + ": rpc error: code = DeadlineExceeded desc = " +
		"waited 2m7s for the sandbox to be created and gave up")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhasePending {
		t.Fatalf("the job is %q saying %q, want pending", got.Phase, got.Reason)
	}
	if got.FinishedAt != nil {
		t.Fatal("a job that never started carries the moment it finished")
	}
	if !strings.Contains(got.Reason, "waits for room") {
		t.Fatalf("the reason is %q, and it does not say the job is waiting", got.Reason)
	}
	if got.LeaseOwner != "" || got.LeaseUntil != nil {
		t.Fatalf("the job is still held by %q, so no controller may pick it up", got.LeaseOwner)
	}
	// The record says it was given up rather than that it failed, so a reader is never told the job
	// was wrong.
	kinds := kept.kinds(one.ID)
	if kinds[len(kinds)-1] != job.EventReleased {
		t.Fatalf("the records read %v, want the last one to say the job was given up", kinds)
	}
}

// Pending is only the right answer if something comes back for it, so this is the half that proves
// the work is not lost: a later tick starts it again, and the answer lands on the row.
func TestAJobPutBackForWantOfASandboxRunsOnALaterTick(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.fails(job.NoSandbox + ": the daemon had no room")
	controller.Tick(ctx)
	controller.Tick(ctx)

	if plane.sent() != 2 {
		t.Fatalf("the crew was asked to run %d tasks, want the first and the one after the machine had room",
			plane.sent())
	}
	if got := kept.get(one.ID); got.Attempts != 2 {
		t.Fatalf("the job has been tried %d times, want 2", got.Attempts)
	}

	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q saying %q, want done", got.Phase, got.Reason)
	}
	if got.Answer != "the bill is due on the 14th" {
		t.Fatalf("the answer is %q", got.Answer)
	}
	// The reason the wait was written under is gone, so nothing says the job is waiting for a machine
	// it has already run on.
	if got.Reason != "" {
		t.Fatalf("the finished job still says %q", got.Reason)
	}
}

// A dispatch the crew refuses is job that cannot run. It is failed with the reason rather than left
// running forever with nothing behind it.
func TestADispatchTheCrewRefusesFailsTheJobWithTheReason(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	plane.refuse = errors.New("no sandbox could be made")

	controller.Tick(context.Background())

	got := kept.get(one.ID)
	if got.Phase != job.PhaseFailed {
		t.Fatalf("the job is %q, want failed", got.Phase)
	}
	if !strings.Contains(got.Reason, "no sandbox could be made") {
		t.Fatalf("the reason is %q, want what the crew said", got.Reason)
	}
}

// The claim is checked by the crew rather than believed from the model, which is what it is for.
func TestAnAnswerThatDoesNotCarryWhatWasClaimedStopsTheJob(t *testing.T) {
	controller, kept, plane := aController(t)
	declared := declaredJob("read the electricity bill")
	declared.ExpectContains = "paid"
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", got.Phase)
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
	declared := declaredJob("pay the electricity bill")
	declared.ExpectContains = "paid"
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is paid")
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q, want done", got.Phase)
	}
}

// A file the job said would be there is asked about, not believed.
func TestAFileTheJobClaimedIsCheckedAndItsAbsenceStopsTheJob(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := job.NewController(kept, plane, nil, holds(false), nil)
	declared := declaredJob("read the electricity bill")
	declared.ExpectFile = "notes/bill.md"
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I wrote the notes")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped", got.Phase)
	}
	if !strings.Contains(got.Reason, "notes/bill.md") {
		t.Fatalf("the reason is %q, want it to name the file", got.Reason)
	}
}

// A crew that cannot answer the question stops the job rather than passing it. A check that
// quietly passes when it could not be run is the same false green as no check at all.
func TestAClaimAboutAFileThatCannotBeCheckedStopsTheJob(t *testing.T) {
	controller, kept, plane := aController(t)
	declared := declaredJob("read the electricity bill")
	declared.ExpectFile = "notes/bill.md"
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I wrote the notes")
	controller.Tick(ctx)

	got := kept.get(one.ID)
	if got.Phase != job.PhaseStopped {
		t.Fatalf("the job is %q, want stopped when the claim cannot be checked", got.Phase)
	}
	if !strings.Contains(got.Reason, "notes/bill.md") {
		t.Fatalf("the reason is %q, want it to name the file", got.Reason)
	}
}

func TestAFileThatIsThereLeavesTheJobDone(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := job.NewController(kept, plane, nil, holds(true), nil)
	declared := declaredJob("read the electricity bill")
	declared.ExpectFile = "notes/bill.md"
	one := kept.add(declared)
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("I wrote the notes")
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.Phase != job.PhaseDone {
		t.Fatalf("the job is %q, want done", got.Phase)
	}
}

// What a job cost is read from the conversation rather than from what the model said about
// itself.
func TestWhatTheJobSpentIsWrittenOntoTheRecord(t *testing.T) {
	kept, plane := newRows(), newCrew()
	controller := job.NewController(kept, plane, spent(1234), nil, nil)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	if got := kept.get(one.ID); got.SpentTokens != 1234 {
		t.Fatalf("the job spent %d tokens on the record, want 1234", got.SpentTokens)
	}
}

// Ordering is the one thing a controller still honours nothing of, so job that waits is the one
// thing left alone. A job in a role runs, and so does a job under a parent: a flow declares every step
// under its own run.
func TestJobThatWaitsForSomethingIsLeftAlone(t *testing.T) {
	controller, kept, plane := aController(t)
	declared := declaredJob("read the electricity bill")
	declared.After = []string{"job-0"}
	one := kept.add(declared)

	controller.Tick(context.Background())

	if plane.sent() != 0 {
		t.Fatalf("the crew was asked to run %d tasks, want none", plane.sent())
	}
	if got := kept.get(one.ID); got.Phase != job.PhasePending {
		t.Fatalf("the job is %q, want left pending", got.Phase)
	}
}

// Job under a parent runs. It has to: a flow run declares every step under the run's own job, so a
// controller that started roots only would leave every step of every automation pending forever.
func TestJobUnderAParentIsRun(t *testing.T) {
	controller, kept, plane := aController(t)
	declared := declaredJob("write the tests")
	declared.Parent, declared.Depth = "job-0", 1
	one := kept.add(declared)

	controller.Tick(context.Background())

	if plane.sent() != 1 {
		t.Fatalf("the crew was asked to run %d tasks, want one", plane.sent())
	}
	if got := kept.get(one.ID); got.Phase != job.PhaseRunning {
		t.Fatalf("the job is %q, want it running", got.Phase)
	}
}

// Job in any phase but pending is not started, which covers the one a person stopped.
func TestJobThatIsNotPendingIsNeverStarted(t *testing.T) {
	for _, phase := range []string{job.PhaseStopped, job.PhaseDone, job.PhaseFailed, job.PhaseWaiting, job.PhaseAsking} {
		controller, kept, plane := aController(t)
		declared := declaredJob("read the electricity bill")
		declared.Phase = phase
		kept.add(declared)

		controller.Tick(context.Background())

		if plane.sent() != 0 {
			t.Errorf("%s job was sent to the crew", phase)
		}
	}
}

// Every movement writes its own record, in the order it happened.
func TestEveryMovementIsOnTheRecord(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.lands("the bill is due on the 14th")
	controller.Tick(ctx)

	// The claim comes before the start, because a controller takes the job in hand before it sends
	// anything, and the record says so in that order.
	want := strings.Join([]string{job.EventClaimed, job.EventStarted, job.EventAnswered}, ",")
	if got := strings.Join(kept.kinds(one.ID), ","); got != want {
		t.Fatalf("the records read %q, want %q", got, want)
	}
}

func TestJobThatFailedRecordsThatItFailed(t *testing.T) {
	controller, kept, plane := aController(t)
	one := kept.add(declaredJob("read the electricity bill"))
	ctx := context.Background()

	controller.Tick(ctx)
	plane.fails("the model refused this task")
	controller.Tick(ctx)

	want := strings.Join([]string{job.EventClaimed, job.EventStarted, job.EventFailed}, ",")
	if got := strings.Join(kept.kinds(one.ID), ","); got != want {
		t.Fatalf("the records read %q, want %q", got, want)
	}
}

// One job that cannot be claimed must not stop the others. A tick that gave up on the
// first row would leave a crew where one bad row stops every good one.
func TestJobThatCannotBeClaimedDoesNotStopTheRest(t *testing.T) {
	controller, kept, plane := aController(t)
	kept.refuseStart = errors.New("the database went away")
	kept.add(declaredJob("read the electricity bill"))

	controller.Tick(context.Background())

	if plane.sent() != 0 {
		t.Fatalf("a task was sent for a job that was never claimed")
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
func holds(answer bool) job.Prover {
	return proverFunc(func(context.Context, string, string) (bool, error) { return answer, nil })
}

type proverFunc func(ctx context.Context, session, path string) (bool, error)

func (f proverFunc) SessionHolds(ctx context.Context, session, path string) (bool, error) {
	return f(ctx, session, path)
}

// spent is a reader that answers the same number every time.
func spent(tokens int64) job.Spend {
	return spendFunc(func(context.Context, string) int64 { return tokens })
}

type spendFunc func(ctx context.Context, session string) int64

func (f spendFunc) SessionTokens(ctx context.Context, session string) int64 { return f(ctx, session) }
