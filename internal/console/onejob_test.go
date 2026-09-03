package console

import (
	"context"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A person opens a job in the console and reads that job.
//
// Enter on a job row opened the conversation of the session under it, so the job itself had no screen
// at all. What a person wanted was the job: what it is, the sentence it serves, and which of the four
// stages it stands in. They left the console and read `krewe job show` instead.
//
// These scenarios drive the keyboard and read the screen the person is left looking at, because what
// the rows hold and what a person sees are two different claims and only the second one is the
// feature. They read the screen at two heights: a tall window shows everything at once, and a short
// window is where the fault lives, because the lines that say which sessions are working are the
// first thing a body of prose pushes off the bottom.

// oneJobPlane is a control plane double for a person reading one job. It embeds the generated
// interface, so a call this work is not supposed to make panics loudly rather than being quietly
// answered with a zero value.
type oneJobPlane struct {
	quaycrewv1.ControlPlaneServiceClient

	jobs  []*quaycrewv1.Job
	runs  []*quaycrewv1.Execution
	tasks []*quaycrewv1.Task
}

func (p *oneJobPlane) ListJobs(_ context.Context, req *quaycrewv1.ListJobsRequest, _ ...grpc.CallOption) (
	*quaycrewv1.ListJobsResponse, error) {
	matched := make([]*quaycrewv1.Job, 0, len(p.jobs))
	for _, one := range p.jobs {
		if req.GetProject() == "" || one.GetProject() == req.GetProject() {
			matched = append(matched, one)
		}
	}
	return &quaycrewv1.ListJobsResponse{Jobs: matched}, nil
}

// GetJob answers for the job it was asked about. A double that answers with one job whatever it is
// asked cannot tell the right job from the wrong one, and a view that always draws the first job
// passes against it.
func (p *oneJobPlane) GetJob(_ context.Context, req *quaycrewv1.GetJobRequest, _ ...grpc.CallOption) (
	*quaycrewv1.GetJobResponse, error) {
	for _, one := range p.jobs {
		if one.GetId() == req.GetId() {
			return &quaycrewv1.GetJobResponse{Job: one}, nil
		}
	}
	return nil, status.Error(codes.NotFound, "no such job")
}

// ListExecutions is narrowed the way the system narrows it: the runs of one job where a job is named,
// and the runs of a project where one is. A double that answered every question with every run would
// let a view draw another job's sessions under this one.
func (p *oneJobPlane) ListExecutions(_ context.Context, req *quaycrewv1.ListExecutionsRequest, _ ...grpc.CallOption) (
	*quaycrewv1.ListExecutionsResponse, error) {
	matched := make([]*quaycrewv1.Execution, 0, len(p.runs))
	for _, run := range p.runs {
		switch {
		case req.GetJob() != "":
			if run.GetJob() == req.GetJob() {
				matched = append(matched, run)
			}
		default:
			matched = append(matched, run)
		}
	}
	return &quaycrewv1.ListExecutionsResponse{Executions: matched}, nil
}

func (p *oneJobPlane) ListTasks(_ context.Context, req *quaycrewv1.ListTasksRequest, _ ...grpc.CallOption) (
	*quaycrewv1.ListTasksResponse, error) {
	matched := make([]*quaycrewv1.Task, 0, len(p.tasks))
	for _, task := range p.tasks {
		if task.GetSession() == req.GetSession() {
			matched = append(matched, task)
		}
	}
	return &quaycrewv1.ListTasksResponse{Tasks: matched}, nil
}

// The sentence this job serves, in a person's words. It is what the design and the plan are held
// against, and it is the line a person reads to know what the work is for.
const theSentenceOfTheJob = "a person opens a job in the console, reads what it asked, answers it " +
	"there, and sees every session running under it"

// The identifiers of the two jobs and of the session working on the second one. They are written out
// rather than generated, so a failure names the row it is about.
const (
	theOtherJobID   = "1111111111111111aaaaaaaa"
	theJobID        = "2222222222222222bbbbbbbb"
	theJobSessionID = "3333333333333333cccccccc"
)

// aJobInDesign is the job of the picture: it states its sentence, a person answered what it
// understood, and nobody has accepted the list yet, which is the second of the four stages. One
// session is working on it.
func aJobInDesign(sentence string) *quaycrewv1.Job {
	return &quaycrewv1.Job{
		Id: theJobID, Workspace: "w1", Project: "p1",
		Title: "read the electricity bill", Role: "backlog-clearer",
		Phase: job.PhaseRunning, Session: theJobSessionID, Attempts: 1,
		Product:        sentence,
		IdeationAnswer: "yes, that is the work, and the standing charge is the part that matters",
		CreatedAt:      timestamppb.New(time.Now().Add(-90 * time.Second)),
	}
}

// theOtherJob is a second job in the same project, so a scenario can put the cursor on one row and
// prove the screen it opens is about that row.
func theOtherJob() *quaycrewv1.Job {
	return &quaycrewv1.Job{
		Id: theOtherJobID, Workspace: "w1", Project: "p1",
		Title: "check the meter reading", Phase: job.PhasePending,
		Product:   "a person reads the meter and knows what the quarter will cost",
		CreatedAt: timestamppb.New(time.Now().Add(-120 * time.Second)),
	}
}

// openedOnTheJobs is the console as a person meets it, standing on the listing of jobs, at the window
// size the scenario is about. The first listing has landed, so what is read is a screen rather than
// an intention.
func openedOnTheJobs(t *testing.T, plane quaycrewv1.ControlPlaneServiceClient, wide, tall int) Model {
	t.Helper()
	registry, err := NewDefaultRegistry(plane)
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	model, err := New(registry, "jobs", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model.width, model.height = wide, tall
	return settle(t, model, listCmd(model.active, model.parent))
}

// standingOnTheJob moves the cursor onto the job under test and says so, because a scenario that
// indexes into a listing whose order is nobody's contract passes by accident.
func standingOnTheJob(t *testing.T, model Model, want string) Model {
	t.Helper()
	for range len(model.Listed()) {
		row, found := model.Selected()
		if found && row.ID == want {
			return model
		}
		model = walk(t, model, runes("j"))
	}
	row, _ := model.Selected()
	t.Fatalf("the cursor is on %q after moving through the listing, want the job %q", row.ID, want)
	return model
}

// prose is what the screen says with the frame taken off and the rows run together, so a sentence
// drawn over several rows reads as the sentence it is. Only what is drawn is in it, so a line that
// scrolled off the screen is not.
func prose(model Model) string { return drawnText(model) }

// saysInProse fails when the screen does not carry every phrase, naming what it drew instead.
func saysInProse(t *testing.T, model Model, want ...string) {
	t.Helper()
	drawn := prose(model)
	for _, one := range want {
		if !strings.Contains(drawn, one) {
			t.Fatalf("the screen does not say %q:\n%s", one, model.View())
		}
	}
}

func doesNotSayInProse(t *testing.T, model Model, unwanted string) {
	t.Helper()
	if strings.Contains(prose(model), unwanted) {
		t.Fatalf("the screen says %q, and this screen is about one job:\n%s", unwanted, model.View())
	}
}

// Enter on a job row reads that job. It used to open the conversation of the session under it, which
// is one level past the thing a person pointed at.
func TestEnterOnAJobRowOpensThatJobRatherThanTheSessionUnderIt(t *testing.T) {
	plane := &oneJobPlane{
		jobs: []*quaycrewv1.Job{theOtherJob(), aJobInDesign(theSentenceOfTheJob)},
		tasks: []*quaycrewv1.Task{{
			Id: "4444444444444444dddddddd", Session: theJobSessionID, Status: "idle",
			Prompt: "say what you understood", Reply: "the standing charge moved in March",
			OccurredAt: timestamppb.New(time.Now()),
		}},
	}
	model := openedOnTheJobs(t, plane, 120, 60)
	model = standingOnTheJob(t, model, theJobID)

	model = walk(t, model, enter())

	if model.Reported() != nil {
		t.Fatalf("enter on a job reported %v, and reading a job is not a fault", model.Reported())
	}
	// What it is, the sentence it serves, and where it stands in the four stages.
	saysInProse(t, model, "read the electricity bill", theSentenceOfTheJob, "stage 2 of 4: design")
	// And the session working on it, so a person watching knows something is happening.
	saysInProse(t, model, display.ShortID(theJobSessionID))
	// Its own screen, so the job beside it in the listing is not on it.
	doesNotSayInProse(t, model, "check the meter reading")
}

// The same four things on a short window. This is the picture a person actually works from: a laptop
// pane is about twenty four rows, and everything above has to survive it.
func TestOneJobKeepsItsTitleSentenceAndStageOnAShortScreen(t *testing.T) {
	plane := &oneJobPlane{jobs: []*quaycrewv1.Job{theOtherJob(), aJobInDesign(theSentenceOfTheJob)}}
	model := openedOnTheJobs(t, plane, 120, 24)
	model = standingOnTheJob(t, model, theJobID)

	model = walk(t, model, enter())

	if model.Reported() != nil {
		t.Fatalf("enter on a job reported %v, and reading a job is not a fault", model.Reported())
	}
	saysInProse(t, model, "read the electricity bill", theSentenceOfTheJob, "stage 2 of 4: design",
		display.ShortID(theJobSessionID))
}

// A sentence longer than the window is the case the short screen is about: the prose scrolls, and the
// lines that name the sessions stay where they are. A person watching two sessions build must not
// lose them by reading.
func TestTheSessionLinesStayWhileTheProseScrolls(t *testing.T) {
	// One sentence a person wrote at length. It is longer than the window on purpose: a screen that
	// holds everything at once proves nothing about scrolling.
	long := "a person opens a job in the console and reads the whole of what it is working on, " +
		strings.Repeat("which is the sentence it serves and every word of it, kept whole rather than "+
			"cut at the border of the panel, ", 12) +
		"and the last words of it are these ones"
	one := aJobInDesign(long)
	one.Session, one.RunningExecutions = "", 2
	one.DesignAccepted, one.Tests = true, "requirement 1: 3 tests"
	first := "5555555555555555eeeeeeee"
	second := "6666666666666666ffffffff"
	plane := &oneJobPlane{
		jobs: []*quaycrewv1.Job{theOtherJob(), one},
		runs: []*quaycrewv1.Execution{
			{
				Id: "7777777777777777aaaaaaaa", Job: theJobID, Stage: job.StageBuild, Number: 1,
				Phase: job.PhaseRunning, Session: first, Attempts: 1,
				CreatedAt: timestamppb.New(time.Now().Add(-60 * time.Second)),
			},
			{
				Id: "8888888888888888bbbbbbbb", Job: theJobID, Stage: job.StageBuild, Number: 2,
				Phase: job.PhaseRunning, Session: second, Attempts: 1,
				CreatedAt: timestamppb.New(time.Now().Add(-60 * time.Second)),
			},
		},
	}
	model := openedOnTheJobs(t, plane, 80, 24)
	model = standingOnTheJob(t, model, theJobID)

	model = walk(t, model, enter())

	// Both sessions, and the sentence starting, before anybody scrolls anything.
	saysInProse(t, model, display.ShortID(first), display.ShortID(second),
		"a person opens a job in the console and reads the whole of what it is working on")
	// The window is genuinely too short for the prose, which is what makes the rest of this a test.
	const theLastWords = "and the last words of it are these ones"
	if strings.Contains(prose(model), theLastWords) {
		t.Fatalf("the whole sentence fits on a window of 24 rows, so nothing here is about scrolling:\n%s",
			model.View())
	}

	// Read to the end of it.
	for range 60 {
		model = walk(t, model, runes("j"))
	}

	saysInProse(t, model, theLastWords)
	// And the sessions are still there, which is the whole point of the short screen.
	saysInProse(t, model, display.ShortID(first), display.ShortID(second))
}

// A job that has reached no session is the normal state of a pending job, and it is exactly the job
// somebody is watching. Enter used to refuse it, because there was nothing under it to open. There is
// something to read now: the job.
func TestEnterOpensAJobThatHasNoSessionYet(t *testing.T) {
	waiting := aJobInDesign(theSentenceOfTheJob)
	waiting.Phase, waiting.Session, waiting.IdeationAnswer = job.PhasePending, "", ""
	plane := &oneJobPlane{jobs: []*quaycrewv1.Job{theOtherJob(), waiting}}
	model := openedOnTheJobs(t, plane, 120, 60)
	model = standingOnTheJob(t, model, theJobID)

	model = walk(t, model, enter())

	if model.Reported() != nil {
		t.Fatalf("enter on a job with no session reported %v, and a job waiting its turn is not a fault",
			model.Reported())
	}
	// The first of the four stages, because nobody has answered what it understood.
	saysInProse(t, model, "read the electricity bill", theSentenceOfTheJob, "stage 1 of 4: ideation")
}

var _ tea.Model = Model{}
