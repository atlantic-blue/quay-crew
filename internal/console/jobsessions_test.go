package console

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A person opens a job in the console and sees every session running under it, one line each, and
// presses enter on a line to open that session.
//
// A job in its test stage is the case this is written against: it holds a session of its own and its
// stage fans out into one run for each requirement, each run in a session of its own. Six
// conversations belong to one job, and until now the only way to see them was to leave the console
// and read the command line, because the jobs listing spends enter on descending into what the job's
// own session ran.
//
// The lines are read off the screen, and the key is driven through the model, because what a listing
// holds and what a person sees are two claims and this requirement is about the second. A key that
// opened the same conversation for every line would satisfy a test that stopped at the call, so each
// line is pressed in turn and the assertion is on the conversation the operator is left beside.

// theJobBeingWatched is the job a person opens, and theSessionOfTheJob is the conversation the job
// itself runs in.
const (
	theJobBeingWatched = "1111111111111111aaaaaaaa"
	theSessionOfTheJob = "2222222222222222bbbbbbbb"
	// runsUnderTheJob is how many requirements the test stage fanned out into, so the job holds this
	// many run sessions beside its own.
	runsUnderTheJob = 5
)

// paddedID makes a whole identifier that differs from every other one in its first eight characters,
// which is the part a listing prints.
func paddedID(prefix string, at int) string {
	id := fmt.Sprintf("%s%d", prefix, at)
	return id + strings.Repeat("0", 24-len(id))
}

// theSessionOfRun is the conversation one run of the test stage works in.
func theSessionOfRun(at int) string { return paddedID("5555555", at) }

// theRun is one run of the test stage. A run is not a job: nobody declared it, and it is only ever
// read as one of the runs of one stage of one job.
func theRun(at int) string { return paddedID("6666666", at) }

// everySessionUnderTheJob is the job's own conversation and the conversation of each of its runs, in
// no order a person is promised.
func everySessionUnderTheJob() []string {
	sessions := []string{theSessionOfTheJob}
	for at := 1; at <= runsUnderTheJob; at++ {
		sessions = append(sessions, theSessionOfRun(at))
	}
	return sessions
}

// jobSessionsClient is a system holding one job in its test stage, that job's own session, and the
// five runs its stage fanned out into. Every call a view of one job can make is answered from the one
// set of rows, so two halves of the screen cannot disagree about what is running.
type jobSessionsClient struct {
	quaycrewv1.ControlPlaneServiceClient

	one      *quaycrewv1.Job
	runs     []*quaycrewv1.Execution
	sessions []*quaycrewv1.Session
}

// aJobWhoseTestStageFannedOut is that system: a job in the test stage, its own session, and one run
// for each of five requirements, each run working in a session of its own.
func aJobWhoseTestStageFannedOut() *jobSessionsClient {
	made := timestamppb.New(time.Now().Add(-9 * time.Minute))
	client := &jobSessionsClient{
		one: &quaycrewv1.Job{
			Id: theJobBeingWatched, Workspace: "w1", Project: "p1",
			Title: "let a person open a job in the console",
			Brief: "build the console view of one job",
			// The sentence, the answered reading and the accepted list are what put this job in the
			// test stage, which is the stage a job holds six conversations in.
			Product:        "A person opens a job in the console and sees every session running under it.",
			Ideation:       "it wants a view of one job",
			IdeationAnswer: "yes, that is it",
			Design:         "Vertical 1: a person opens a job",
			DesignAccepted: true,
			Role:           "backlog-clearer",
			Phase:          job.PhaseRunning, Session: theSessionOfTheJob,
			RunningExecutions: runsUnderTheJob,
			CreatedAt:         made,
		},
		sessions: []*quaycrewv1.Session{{
			Id: theSessionOfTheJob, Workspace: "w1", Project: "p1", Status: "working",
		}},
	}
	for at := 1; at <= runsUnderTheJob; at++ {
		client.runs = append(client.runs, &quaycrewv1.Execution{
			Id: theRun(at), Job: theJobBeingWatched, Stage: job.StageTest, Number: int32(at),
			Claim: fmt.Sprintf("requirement %d", at), Phase: job.PhaseRunning,
			Session: theSessionOfRun(at), CreatedAt: made,
		})
		client.sessions = append(client.sessions, &quaycrewv1.Session{
			Id: theSessionOfRun(at), Workspace: "w1", Project: "p1", Status: "working",
		})
	}
	return client
}

func (c *jobSessionsClient) GetJob(_ context.Context, req *quaycrewv1.GetJobRequest, _ ...grpc.CallOption) (
	*quaycrewv1.GetJobResponse, error) {
	if req.GetId() != c.one.GetId() {
		return nil, fmt.Errorf("no job %q", req.GetId())
	}
	return &quaycrewv1.GetJobResponse{Job: c.one}, nil
}

func (c *jobSessionsClient) ListJobs(_ context.Context, _ *quaycrewv1.ListJobsRequest, _ ...grpc.CallOption) (
	*quaycrewv1.ListJobsResponse, error) {
	return &quaycrewv1.ListJobsResponse{Jobs: []*quaycrewv1.Job{c.one}}, nil
}

// Narrowed by job, the way the system narrows it, so a view that asked for one job's runs and drew
// every run in the project cannot pass.
func (c *jobSessionsClient) ListExecutions(_ context.Context, req *quaycrewv1.ListExecutionsRequest, _ ...grpc.CallOption) (
	*quaycrewv1.ListExecutionsResponse, error) {
	matched := make([]*quaycrewv1.Execution, 0, len(c.runs))
	for _, run := range c.runs {
		if req.GetJob() != "" && run.GetJob() != req.GetJob() {
			continue
		}
		matched = append(matched, run)
	}
	return &quaycrewv1.ListExecutionsResponse{Executions: matched}, nil
}

func (c *jobSessionsClient) ListSessions(_ context.Context, _ *quaycrewv1.ListSessionsRequest, _ ...grpc.CallOption) (
	*quaycrewv1.ListSessionsResponse, error) {
	return &quaycrewv1.ListSessionsResponse{Sessions: c.sessions}, nil
}

// This job's sessions have answered nothing yet, and none of the lines this requirement is about is
// a task.
func (c *jobSessionsClient) ListTasks(_ context.Context, _ *quaycrewv1.ListTasksRequest, _ ...grpc.CallOption) (
	*quaycrewv1.ListTasksResponse, error) {
	return &quaycrewv1.ListTasksResponse{}, nil
}

// Each session attaches to a sandbox of its own. A double answering one sandbox for every identifier
// cannot tell the right conversation from the wrong one, which is the whole of what this key has to
// get right.
func (c *jobSessionsClient) AttachSession(_ context.Context, req *quaycrewv1.AttachSessionRequest, _ ...grpc.CallOption) (
	*quaycrewv1.AttachSessionResponse, error) {
	return &quaycrewv1.AttachSessionResponse{
		Sandbox: "krewe-" + req.GetId(), Argv: []string{"claude", "--resume", req.GetId()},
	}, nil
}

// theJobsListing is the console an operator meets: every view this build has, opened on the jobs the
// system holds, with the first listing landed.
func theJobsListing(t *testing.T, client quaycrewv1.ControlPlaneServiceClient) Model {
	t.Helper()
	registry, err := NewDefaultRegistry(client)
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	model, err := New(registry, "jobs", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model.width, model.height = 120, 30
	return settle(t, model, listCmd(model.active, model.parent))
}

// theJobOpened is the screen a person is left on after they open the job under the cursor. Enter is
// the key, because it is the key the jobs listing already spends on going into a job.
func theJobOpened(t *testing.T, model Model) Model {
	t.Helper()
	on, found := model.Selected()
	if !found {
		t.Fatal("the jobs listing draws no row, so there is no job to open")
	}
	if on.ID != theJobBeingWatched {
		t.Fatalf("the cursor is on %q, want the job a person declared", on.ID)
	}
	opened := walk(t, model, enter())
	if opened.Reported() != nil {
		t.Fatalf("opening the job was refused: %v", opened.Reported())
	}
	return opened
}

// namesTheSession says whether a line of the listing is the line for one session. The identifier is
// looked for wherever a row can carry it, because which cell holds it is the view's own business and
// this requirement is about the line being there.
func namesTheSession(row Row, session string) bool {
	if row.ID == session || row.Parent == session {
		return true
	}
	short := display.ShortID(session)
	for _, cell := range row.Cells {
		if strings.Contains(cell, short) {
			return true
		}
	}
	return strings.Contains(row.Detail, short)
}

// linesNaming is how many lines of the drawn screen carry this session.
func linesNaming(model Model, session string) int {
	found := 0
	for _, line := range strings.Split(model.View(), "\n") {
		if strings.Contains(stripped(line), display.ShortID(session)) {
			found++
		}
	}
	return found
}

// Every conversation belonging to the job is on the screen: the one the job runs in, and the one each
// of its five runs works in. A person watching a fan out is looking straight at this screen, and
// until now five of the six were only on the command line.
func TestOpeningAJobShowsEverySessionRunningUnderIt(t *testing.T) {
	model := theJobOpened(t, theJobsListing(t, aJobWhoseTestStageFannedOut()))

	for _, session := range everySessionUnderTheJob() {
		if linesNaming(model, session) == 0 {
			t.Fatalf("no line of the job says session %s is running under it:\n%s",
				display.ShortID(session), model.View())
		}
	}
}

// One line each. Six conversations on six lines is what a person can read; the same conversation on
// two lines is a listing that has counted something twice, and a person then cannot tell how many
// sessions the job is running.
func TestEverySessionUnderAJobIsOnOneLineOfItsOwn(t *testing.T) {
	model := theJobOpened(t, theJobsListing(t, aJobWhoseTestStageFannedOut()))

	rows := model.Listed()
	sessionLines := 0
	for _, row := range rows {
		for _, session := range everySessionUnderTheJob() {
			if namesTheSession(row, session) {
				sessionLines++
				break
			}
		}
	}
	for _, session := range everySessionUnderTheJob() {
		on := 0
		for _, row := range rows {
			if namesTheSession(row, session) {
				on++
			}
		}
		if on != 1 {
			t.Fatalf("session %s is on %d lines of the job, want one:\n%s",
				display.ShortID(session), on, model.View())
		}
	}
	if want := len(everySessionUnderTheJob()); sessionLines != want {
		t.Fatalf("the job draws %d lines about sessions, want %d, one for each session running under it:\n%s",
			sessionLines, want, model.View())
	}
}

// theLineFor puts the cursor on the line for one session, the way a person does: to the top, then
// down. It walks rather than indexing, because which line a session is drawn on is not something this
// view promises, and a case that indexed into the order would pass by accident.
func theLineFor(t *testing.T, model Model, session string) Model {
	t.Helper()
	rows := model.Listed()
	want := -1
	for at, row := range rows {
		if namesTheSession(row, session) {
			want = at
			break
		}
	}
	if want < 0 {
		t.Fatalf("the job has no line for session %s, so there is nothing to press enter on:\n%s",
			display.ShortID(session), model.View())
	}
	at := walk(t, walk(t, model, runes("g")), runes("g"))
	for range want {
		at = walk(t, at, runes("j"))
	}
	on, found := at.Selected()
	if !found || !namesTheSession(on, session) {
		t.Fatalf("the cursor cannot be put on the line for session %s: it landed on %v",
			display.ShortID(session), on.Cells)
	}
	return at
}

// Enter on a line opens that session, and it is pressed on every one of the six in turn. A key that
// opened one conversation whatever the cursor was on would pass a case that pressed it once, and that
// is the defect this whole view is watched for: the assertion is on the conversation the operator is
// beside afterwards rather than on the call the key made.
func TestEnterOnASessionLineOpensThatSessionEveryTime(t *testing.T) {
	t.Setenv("TMUX_PANE", "%0")
	panes := aPanelWithAConversationBeside([]string{"krewe", "attach", "the-driver"})
	model := theJobsListing(t, aJobWhoseTestStageFannedOut()).
		Beside(theConversationOf).WithPanes(panes)
	model = theJobOpened(t, model)

	for _, session := range everySessionUnderTheJob() {
		at := theLineFor(t, model, session)

		at = walk(t, at, enter())

		if at.Reported() != nil {
			t.Fatalf("enter on the line for %s was refused: %v", display.ShortID(session), at.Reported())
		}
		if open, found := at.OpenBeside(); !found || open != session {
			t.Fatalf("the cursor is on %s and the console says it is beside %q (%v)",
				display.ShortID(session), open, found)
		}
		if showing := panes.showing(); !strings.Contains(showing, session) {
			t.Fatalf("the cursor is on %s and the conversation beside the console is %q",
				display.ShortID(session), showing)
		}
	}
}

// The other half of that key. A session that cannot be opened is refused by name, and the
// conversation already beside the console is left alone: replacing it with somebody else's is the
// fault this key is watched for, and it would be worse arriving through the refusal.
func TestASessionLineThatCannotBeOpenedIsRefusedAndTheConversationBesideStays(t *testing.T) {
	t.Setenv("TMUX_PANE", "%0")
	refused := theSessionOfRun(3)
	panes := aPanelWithAConversationBeside([]string{"krewe", "attach", theSessionOfTheJob})
	model := theJobsListing(t, aJobWhoseTestStageFannedOut()).
		Beside(func(selected string) ([]string, error) {
			if selected == refused {
				return nil, fmt.Errorf("attach: session %s has no conversation behind it", display.ShortID(selected))
			}
			return []string{"krewe", "attach", selected}, nil
		}).WithPanes(panes)
	model = theJobOpened(t, model)

	at := walk(t, theLineFor(t, model, refused), enter())

	if at.Reported() == nil {
		t.Fatalf("enter on a session with no conversation said nothing:\n%s", at.View())
	}
	if !strings.Contains(at.Reported().Error(), display.ShortID(refused)) {
		t.Fatalf("the console said %v, want it to name the session it could not open", at.Reported())
	}
	if showing := panes.showing(); !strings.Contains(showing, theSessionOfTheJob) {
		t.Fatalf("the conversation beside the console is now %q, want the one that was already there", showing)
	}
}
