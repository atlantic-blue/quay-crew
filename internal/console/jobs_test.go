package console

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/job"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// jobClient is a control plane double for the jobs view. It embeds the generated interface so a call
// this view is not supposed to make panics loudly rather than being silently satisfied.
type jobClient struct {
	quaycrewv1.ControlPlaneServiceClient

	jobs  []*quaycrewv1.Job
	tasks []*quaycrewv1.Task

	listedFor  string
	listedFrom string
	stopped    []string
	listErr    error
}

func (j *jobClient) ListJobs(_ context.Context, req *quaycrewv1.ListJobsRequest, _ ...grpc.CallOption) (*quaycrewv1.ListJobsResponse, error) {
	if j.listErr != nil {
		return nil, j.listErr
	}
	j.listedFor = req.GetProject()
	if req.GetProject() == "" {
		return &quaycrewv1.ListJobsResponse{Jobs: j.jobs}, nil
	}
	matched := make([]*quaycrewv1.Job, 0, len(j.jobs))
	for _, one := range j.jobs {
		if one.GetProject() == req.GetProject() {
			matched = append(matched, one)
		}
	}
	return &quaycrewv1.ListJobsResponse{Jobs: matched}, nil
}

func (j *jobClient) StopJob(_ context.Context, req *quaycrewv1.StopJobRequest, _ ...grpc.CallOption) (*quaycrewv1.StopJobResponse, error) {
	j.stopped = append(j.stopped, req.GetId())
	return &quaycrewv1.StopJobResponse{}, nil
}

func (j *jobClient) ListTasks(_ context.Context, req *quaycrewv1.ListTasksRequest, _ ...grpc.CallOption) (*quaycrewv1.ListTasksResponse, error) {
	j.listedFrom = req.GetSession()
	return &quaycrewv1.ListTasksResponse{Tasks: j.tasks}, nil
}

// aJob is a declared job with the fields a row is built from, so each case below says only what it
// is about.
func aJob(id, phase string, fill func(*quaycrewv1.Job)) *quaycrewv1.Job {
	one := &quaycrewv1.Job{
		Id: id, Workspace: "w1", Project: "p1",
		Title: "read the electricity bill", Phase: phase,
		CreatedAt: timestamppb.New(time.Now().Add(-90 * time.Second)),
	}
	if fill != nil {
		fill(one)
	}
	return one
}

func TestAJobRowCarriesTheWholeStoryOfOneJob(t *testing.T) {
	running := aJob("1111111111111111aaaaaaaa", job.PhaseRunning, func(one *quaycrewv1.Job) {
		one.Role, one.RoleVersion = "backlog-clearer", 2
		one.Session, one.Attempts = "2222222222222222bbbbbbbb", 1
	})

	got := jobRow(running)

	// The identifiers stay whole: stopping the job and descending into what it did both use them.
	if got.ID != running.GetId() {
		t.Fatalf("the row carries %q as the job, want the whole identifier", got.ID)
	}
	if got.Parent != running.GetSession() {
		t.Fatalf("the row carries %q as the session, want the whole identifier", got.Parent)
	}
	want := []string{"11111111", "running", "-", "backlog-clearer", "read the electricity bill",
		"22222222", "1", "1m"}
	if len(got.Cells) != len(want) {
		t.Fatalf("the row has %d cells, want %d: %q", len(got.Cells), len(want), got.Cells)
	}
	for at, cell := range want {
		if got.Cells[at] != cell {
			t.Fatalf("cell %d is %q, want %q (whole row %q)", at, got.Cells[at], cell, got.Cells)
		}
	}
	if got.State != StateBusy {
		t.Fatalf("a running job is drawn as %v, want busy", got.State)
	}
	// The breadcrumb reads this, so descending into a job says what it is about rather than eight
	// characters of hexadecimal.
	if got.Label != running.GetTitle() {
		t.Fatalf("the row is called %q, want its title", got.Label)
	}
}

// A pending job has no session, which is the normal state rather than a fault. An empty cell reads as
// something missing, so the row says which it is.
func TestAJobWithNoSessionYetSaysSoRatherThanLeavingTheCellEmpty(t *testing.T) {
	got := jobRow(aJob("1111111111111111aaaaaaaa", job.PhasePending, nil))

	if got.Parent != "" {
		t.Fatalf("a pending job carries %q as its session, want none", got.Parent)
	}
	// The literal rather than the constant: a case that reads the constant passes against a
	// constant emptied out, which is the one mistake this is here to catch.
	if got.Cells[5] != "not yet" {
		t.Fatalf(`the session cell says %q, want "not yet"`, got.Cells[5])
	}
	if got.State != StateUnknown {
		t.Fatalf("a pending job is drawn as %v, want no claim at all", got.State)
	}
}

func TestAFailedJobIsMarkedForAttention(t *testing.T) {
	got := jobRow(aJob("1111111111111111aaaaaaaa", job.PhaseFailed, func(one *quaycrewv1.Job) {
		one.Session, one.Attempts = "2222222222222222bbbbbbbb", 3
	}))

	if got.State != StateFailed {
		t.Fatalf("a failed job is drawn as %v, want failed", got.State)
	}
	if got.Cells[1] != job.PhaseFailed {
		t.Fatalf("the phase cell says %q, want %q", got.Cells[1], job.PhaseFailed)
	}
	// Three tries is the number that says this is not a one off, and it is the reason the column is
	// there at all.
	if got.Cells[6] != "3" {
		t.Fatalf("the attempts cell says %q, want 3", got.Cells[6])
	}
}

func TestEveryPhaseIsColouredOrLeftAlone(t *testing.T) {
	tests := map[string]State{
		job.PhasePending: StateUnknown,
		job.PhaseWaiting: StateUnknown,
		job.PhaseRunning: StateBusy,
		job.PhaseAsking:  StateBusy,
		job.PhaseDone:    StateReady,
		job.PhaseFailed:  StateFailed,
		job.PhaseStopped: StateStopped,
		// A phase neither this nor the job package knows stays uncoloured rather than being dressed
		// as finished.
		"reticulating": StateUnknown,
	}
	for phase, want := range tests {
		t.Run(phase, func(t *testing.T) {
			if got := stateOfPhase(phase); got != want {
				t.Fatalf("stateOfPhase(%q) = %v, want %v", phase, got, want)
			}
		})
	}
	// The colour rides on the phase cell rather than the whole line, so the role and the title keep
	// theirs.
	if colourOfPhase(job.PhaseFailed) != ansiRedCode {
		t.Fatal("a failed phase is not drawn in red, so the cell that says how the job is doing says nothing")
	}
	if colourOfPhase("reticulating") != "" {
		t.Fatal("an unknown phase is coloured, which is a claim about a word the console does not know")
	}
}

// The row an operator is looking at is the one that gets stopped, whole identifier and all, and the
// key asks first the way every destructive key in this console does.
func TestStoppingAJobAsksFirstAndUsesTheWholeIdentifier(t *testing.T) {
	client := &jobClient{}
	action, found := actionNamed(Jobs(client), "Stop")
	if !found {
		t.Fatal("the jobs view has no Stop action")
	}
	if !action.Confirm {
		t.Fatal("stopping a job does not ask first, and there is no way back from the wrong row")
	}
	if !action.Bound("backspace") || !action.Bound("x") {
		t.Fatalf("Stop answers to %v, want the keys the sessions view already stops on", action.Keys())
	}

	row := jobRow(aJob("1111111111111111aaaaaaaa", job.PhaseRunning, nil))
	if err := action.Run(context.Background(), row); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if len(client.stopped) != 1 || client.stopped[0] != "1111111111111111aaaaaaaa" {
		t.Fatalf("the system was asked to stop %v, want the whole identifier", client.stopped)
	}
}

func TestStoppingWithNothingSelectedSaysSo(t *testing.T) {
	action, _ := actionNamed(Jobs(&jobClient{}), "Stop")
	if err := action.Run(context.Background(), Row{}); err == nil {
		t.Fatal("stopping nothing succeeded, so an empty listing looks like a job that was halted")
	}
}

func TestTheJobsViewNarrowsToOneProject(t *testing.T) {
	client := &jobClient{jobs: []*quaycrewv1.Job{
		aJob("1111111111111111aaaaaaaa", job.PhaseDone, nil),
		aJob("3333333333333333cccccccc", job.PhaseDone, func(one *quaycrewv1.Job) { one.Project = "p2" }),
	}}

	rows, err := Jobs(client).List(context.Background(), "p1")
	if err != nil {
		t.Fatalf("listing jobs: %v", err)
	}
	if client.listedFor != "p1" {
		t.Fatalf("the system was asked for project %q, want p1", client.listedFor)
	}
	if len(rows) != 1 {
		t.Fatalf("the view lists %d jobs, want the one job in that project", len(rows))
	}
}

func TestJobListingSurfacesTheControlPlaneError(t *testing.T) {
	client := &jobClient{listErr: errors.New("unavailable")}

	if _, err := Jobs(client).List(context.Background(), ""); err == nil {
		t.Fatal("want the control plane error surfaced, not swallowed")
	}
}

// Enter carried through to what the operator is left looking at: the tasks of the job's session, with
// the rows in front of them, rather than the command the key produced.
func TestEnterOnAJobOpensWhatItDid(t *testing.T) {
	client := &jobClient{
		jobs: []*quaycrewv1.Job{aJob("1111111111111111aaaaaaaa", job.PhaseDone, func(one *quaycrewv1.Job) {
			one.Session = "2222222222222222bbbbbbbb"
		})},
		tasks: []*quaycrewv1.Task{{
			Id: "4444444444444444dddddddd", Session: "2222222222222222bbbbbbbb",
			Status: "done", Prompt: "read the electricity bill", Reply: "it is due on the 14th",
			OccurredAt: timestamppb.New(time.Now()),
		}},
	}
	model := newTestModel(t, Jobs(client), Tasks(client))
	model, _ = update(t, model, rowsFor(model, jobRow(client.jobs[0])))

	model, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if model.err != nil {
		t.Fatalf("enter on a job refused: %v", model.err)
	}
	if model.active.Name != "tasks" {
		t.Fatalf("enter left the operator in %q, want the job's tasks", model.active.Name)
	}
	if model.parent != "2222222222222222bbbbbbbb" {
		t.Fatalf("the tasks view is scoped to %q, want the job's session", model.parent)
	}
	// The listing the key asked for, run and fed back the way the runtime does it, so what is
	// asserted is the screen rather than the intent.
	if cmd == nil {
		t.Fatal("enter asked for no listing, so the tasks view would draw empty")
	}
	model, _ = update(t, model, cmd())
	if len(model.rows) != 1 {
		t.Fatalf("the tasks view shows %d rows, want the one task the session ran", len(model.rows))
	}
	if client.listedFrom != "2222222222222222bbbbbbbb" {
		t.Fatalf("the tasks were read from %q, want the job's session", client.listedFrom)
	}
	if !strings.Contains(strings.Join(model.rows[0].Cells, " "), "read the electricity bill") {
		t.Fatalf("the task row says %q, want what the job was asked", model.rows[0].Cells)
	}
}

// A job that has not reached a session yet is the case enter has to refuse, and the refusal names the
// phase so it says why rather than only that it will not.
func TestEnterOnAJobWithNoSessionYetSaysWhy(t *testing.T) {
	client := &jobClient{jobs: []*quaycrewv1.Job{aJob("1111111111111111aaaaaaaa", job.PhasePending, nil)}}
	model := newTestModel(t, Jobs(client), Tasks(client))
	model, _ = update(t, model, rowsFor(model, jobRow(client.jobs[0])))

	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if model.active.Name != "jobs" {
		t.Fatalf("enter opened %q on a job with no session, want to stay where it was", model.active.Name)
	}
	if model.err == nil {
		t.Fatal("enter did nothing and said nothing, which reads as a console that stopped answering")
	}
	said := model.err.Error()
	if !strings.Contains(said, job.PhasePending) || !strings.Contains(said, "11111111") {
		t.Fatalf("the refusal says %q, want it to name the job and the phase it is in", said)
	}
}

func TestTheJobsViewIsRegisteredAndAnswersToWhatFingersType(t *testing.T) {
	registry, err := NewDefaultRegistry(&jobClient{})
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	for _, typed := range []string{"jobs", "job", "j"} {
		resource, found := registry.Resolve(typed)
		if !found {
			t.Fatalf("typing %q opens nothing", typed)
		}
		if resource.Name != "jobs" {
			t.Fatalf("typing %q opens %q, want jobs", typed, resource.Name)
		}
	}
}

// actionNamed is the action a view binds under a label, so a case says which key it is about without
// counting positions in a slice.
func actionNamed(resource Resource, label string) (Action, bool) {
	for _, action := range resource.Actions {
		if action.Label == label {
			return action, true
		}
	}
	return Action{}, false
}

// The column the phase cannot be. Two rows read "done" and one of them could not do its work.
func TestTheOutcomeCellSaysWhatTheJobEndedOn(t *testing.T) {
	done := jobRow(aJob("1111111111111111aaaaaaaa", job.PhaseDone, func(one *quaycrewv1.Job) {
		one.Outcome = job.OutcomeBlocked
	}))
	if done.Cells[outcomeColumn] != job.OutcomeBlocked {
		t.Fatalf("the outcome cell says %q, want %q", done.Cells[outcomeColumn], job.OutcomeBlocked)
	}
	// A job that has not ended on a word says so rather than leaving a hole in the row. The literal
	// rather than the constant, because a case reading the constant passes against it emptied out.
	running := jobRow(aJob("1111111111111111aaaaaaaa", job.PhaseRunning, nil))
	if running.Cells[outcomeColumn] != "-" {
		t.Fatalf(`the outcome cell of a running job says %q, want "-"`, running.Cells[outcomeColumn])
	}
}

func TestEveryOutcomeIsColouredOrLeftAlone(t *testing.T) {
	for outcome, want := range map[string]string{
		job.OutcomeProved:   ansiGreenCode,
		job.OutcomeUnproved: "",
		job.OutcomeBlocked:  ansiRedCode,
		job.OutcomeDecide:   ansiYellowCode,
		"-":                 "",
		"reticulating":      "",
	} {
		if got := colourOfOutcome(outcome); got != want {
			t.Errorf("colourOfOutcome(%q) = %q, want %q", outcome, got, want)
		}
	}
	// Every word the job package hands out is answered here, so a fifth outcome cannot arrive
	// uncoloured without this saying so.
	for _, known := range job.Outcomes() {
		if _, answered := map[string]bool{
			job.OutcomeProved: true, job.OutcomeUnproved: true,
			job.OutcomeBlocked: true, job.OutcomeDecide: true,
		}[known]; !answered {
			t.Fatalf("the console says nothing about the outcome %q", known)
		}
	}
}
