package console

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// A person watching one job answers its question there, on the job, with the key the listing already
// answers with, and the view they answer on is the view they are left on.
//
// The console has no view of one job yet, so every case here opens the job the way the operator does,
// with enter on its row, and then presses the key. What each one asserts is the screen the person is
// left looking at rather than the call the key produced.

// theAnswerTyped is what a person writes into the line. Short, because the assertion is that it is
// drawn on the view afterwards and a long answer would be cut to fit whatever holds it.
const theAnswerTyped = "the hall meter"

// answeringPlane is a control plane holding two jobs that both wait for a person, with runs of their
// own under them. Two jobs, because a key that answers the first row, or the row the cursor happens
// to be on, cannot pass against one.
type answeringPlane struct {
	quaycrewv1.ControlPlaneServiceClient

	jobs []*quaycrewv1.Job
	runs []*quaycrewv1.Execution
	told []toldOnce
}

// toldOnce is one answer the system was given, so a case can say which job it landed on and in whose
// words.
type toldOnce struct {
	id   string
	said string
}

func (a *answeringPlane) ListJobs(_ context.Context, req *quaycrewv1.ListJobsRequest, _ ...grpc.CallOption) (
	*quaycrewv1.ListJobsResponse, error) {
	matched := make([]*quaycrewv1.Job, 0, len(a.jobs))
	for _, one := range a.jobs {
		if req.GetProject() == "" || one.GetProject() == req.GetProject() {
			matched = append(matched, one)
		}
	}
	return &quaycrewv1.ListJobsResponse{Jobs: matched}, nil
}

// GetJob is one job, whole. A view of one job reads it here, and a job the system does not hold is an
// error rather than an empty job: a screen drawn from nothing says the job has no question.
func (a *answeringPlane) GetJob(_ context.Context, req *quaycrewv1.GetJobRequest, _ ...grpc.CallOption) (
	*quaycrewv1.GetJobResponse, error) {
	for _, one := range a.jobs {
		if one.GetId() == req.GetId() {
			return &quaycrewv1.GetJobResponse{Job: one}, nil
		}
	}
	return nil, errors.New("the system holds no job " + req.GetId())
}

// ListExecutions narrows by job the way the system narrows it, so a view of one job cannot pass by
// drawing every run in the crew. Every run here belongs to a job in the one project, so a listing
// asked for the project is all of them.
func (a *answeringPlane) ListExecutions(_ context.Context, req *quaycrewv1.ListExecutionsRequest, _ ...grpc.CallOption) (
	*quaycrewv1.ListExecutionsResponse, error) {
	matched := make([]*quaycrewv1.Execution, 0, len(a.runs))
	for _, run := range a.runs {
		if req.GetJob() != "" && run.GetJob() != req.GetJob() {
			continue
		}
		matched = append(matched, run)
	}
	return &quaycrewv1.ListExecutionsResponse{Executions: matched}, nil
}

// ListTasks answers with nothing. No case here opens what a session was asked; this is what keeps a
// view that reads the level below from failing on a call rather than on what it drew.
func (a *answeringPlane) ListTasks(_ context.Context, _ *quaycrewv1.ListTasksRequest, _ ...grpc.CallOption) (
	*quaycrewv1.ListTasksResponse, error) {
	return &quaycrewv1.ListTasksResponse{}, nil
}

// AttachSession answers because the console's deepest level offers a key that opens a container, and
// a call this double does not answer stops the whole run rather than the case that made it. Nothing
// here presses that key on purpose: the runtime is what starts such a command, and no case here has a
// terminal to give it.
func (a *answeringPlane) AttachSession(context.Context, *quaycrewv1.AttachSessionRequest, ...grpc.CallOption) (
	*quaycrewv1.AttachSessionResponse, error) {
	return &quaycrewv1.AttachSessionResponse{Sandbox: "krewe-s1", Argv: []string{"claude", "--resume", "c1"}}, nil
}

// AnswerJob is the movement the real one makes: the answer is trimmed, an answer with no words in it
// is refused, a job that is not asking is refused, and the job that was answered goes back in the
// queue carrying what it was told. A double that recorded the call and nothing else would let a view
// that answers the wrong job, or answers a job that asked nothing, pass.
func (a *answeringPlane) AnswerJob(_ context.Context, req *quaycrewv1.AnswerJobRequest, _ ...grpc.CallOption) (
	*quaycrewv1.AnswerJobResponse, error) {
	said := strings.TrimSpace(req.GetAnswer())
	if said == "" {
		return nil, errors.New("an answer needs words: the session is waiting to be told what to do")
	}
	for _, one := range a.jobs {
		if one.GetId() != req.GetId() {
			continue
		}
		if one.GetPhase() != job.PhaseAsking {
			return nil, errors.New("job " + one.GetId() + " is " + one.GetPhase() + " and asked nothing")
		}
		a.told = append(a.told, toldOnce{id: req.GetId(), said: said})
		one.Phase, one.Told = job.PhasePending, said
		return &quaycrewv1.AnswerJobResponse{Job: one}, nil
	}
	return nil, errors.New("the system holds no job " + req.GetId())
}

// twoJobsWaitingOnAPerson is a project with two jobs in it, both stopped for an answer and both
// asking about a meter, so a case can say which of the two the answer reached. The second is the one
// every case opens: a key that answers whichever job the listing drew first cannot pass.
func twoJobsWaitingOnAPerson() *answeringPlane {
	made := timestamppb.New(time.Now().Add(-4 * time.Minute))
	return &answeringPlane{
		jobs: []*quaycrewv1.Job{
			{
				Id: "1111111111111111aaaaaaaa", Workspace: "w1", Project: "p1",
				Title: "read the gas bill", Phase: job.PhaseAsking,
				Question: "which meter?", Session: "3333333333333333cccccccc", CreatedAt: made,
			},
			{
				Id: "2222222222222222bbbbbbbb", Workspace: "w1", Project: "p1",
				Title: "read the water bill", Phase: job.PhaseAsking,
				Question: "which meter, the one in the hall or the one outside?",
				Session:  "4444444444444444dddddddd", CreatedAt: made,
			},
		},
		runs: []*quaycrewv1.Execution{
			{
				Id: "5555555555555555eeeeeeee", Job: "2222222222222222bbbbbbbb",
				Stage: job.StageBuild, Number: 1, Phase: job.PhaseDone,
				Session: "6666666666666666ffffffff", Outcome: job.OutcomeProved, CreatedAt: made,
			},
			{
				Id: "7777777777777777aaaaaaaa", Job: "2222222222222222bbbbbbbb",
				Stage: job.StageBuild, Number: 2, Phase: job.PhaseDone,
				Session: "8888888888888888bbbbbbbb", Outcome: job.OutcomeProved, CreatedAt: made,
			},
		},
	}
}

// openedOnTheAskingJob is the console as the operator meets it, taken to the view of one job: the
// jobs listing with its rows landed, the cursor moved onto the job at that position, and enter.
//
// Enter is how a person opens a job. A console that is still on the listing afterwards has no view of
// one job to answer on, and the case says that rather than going on to press a key at the listing.
func openedOnTheAskingJob(t *testing.T, client *answeringPlane, at int) Model {
	t.Helper()
	registry, err := NewDefaultRegistry(client)
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	model, err := New(registry, "jobs", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	model.width, model.height = 140, 40
	model = settle(t, model, listCmd(model.active, model.parent))
	for range at {
		model = step(t, model, runes("j"))
	}
	selected, standing := model.Selected()
	if !standing || selected.ID != client.jobs[at].GetId() {
		t.Fatalf("the cursor is on %q, want the job %q:\n%s", selected.ID, client.jobs[at].GetId(), model.View())
	}
	model = step(t, model, enter())
	if model.active.Name == "jobs" {
		t.Fatalf("enter on the job stayed on the listing (%v), so there is no view of one job to answer on:\n%s",
			model.Reported(), model.View())
	}
	return model
}

// boundOnTheView is the action a key runs on a view, so a case can say which of them a letter reaches.
func boundOnTheView(resource Resource, key string) (Action, bool) {
	for _, action := range resource.Actions {
		if action.Bound(key) {
			return action, true
		}
	}
	return Action{}, false
}

// The whole of it, driven the way an operator drives it: open the job that is asking, press the key
// the listing answers with, type the answer, and read the screen it leaves behind.
func TestTheAnswerKeyOnAJobsOwnViewTellsTheSystemAndStaysOnTheJob(t *testing.T) {
	client := twoJobsWaitingOnAPerson()
	model := openedOnTheAskingJob(t, client, 1)
	view, scope := model.active.Name, model.parent

	// The view before the answer. Nobody has written anything, so the words are on no screen yet.
	screenDoesNotSay(t, model, theAnswerTyped)

	model = step(t, model, runes("a"))
	if model.mode != modeType {
		t.Fatalf("a on the job opened mode %v, want the line that asks for an answer (%v):\n%s",
			model.mode, model.Reported(), model.View())
	}
	model = typeAll(t, model, theAnswerTyped)
	model = step(t, model, enter())

	if model.Reported() != nil {
		t.Fatalf("answering the job refused: %v", model.Reported())
	}
	if len(client.told) != 1 {
		t.Fatalf("the system was answered %d times, want once: %+v", len(client.told), client.told)
	}
	if client.told[0].id != client.jobs[1].GetId() {
		t.Fatalf("the answer went to %q, want the job the view was opened on, %q",
			client.told[0].id, client.jobs[1].GetId())
	}
	if client.told[0].said != theAnswerTyped {
		t.Fatalf("the system was told %q, want the words that were typed, %q", client.told[0].said, theAnswerTyped)
	}
	// Where the person is standing afterwards: the same view of the same job. A console that went back
	// to the listing took them off the job they were reading.
	if model.active.Name != view || model.parent != scope {
		t.Fatalf("answering left the operator on %q scoped to %q, want the job view %q scoped to %q",
			model.active.Name, model.parent, view, scope)
	}
	screenDoesNotSay(t, model, "read the gas bill")
	// And the view after the answer: redrawn in place, with the answer on it.
	screenSays(t, model, theAnswerTyped)
}

// The answer belongs to the job the view is open on. The rows here are the work running under that
// job, so a key that reads the row under the cursor answers a run, and a run has no question.
func TestTheAnswerOnAJobsViewGoesToTheJobAndNotToTheLineUnderTheCursor(t *testing.T) {
	client := twoJobsWaitingOnAPerson()
	model := openedOnTheAskingJob(t, client, 1)
	// Down one, onto the work under the job, which is where a person watching it is standing.
	model = step(t, model, runes("j"))

	model = step(t, model, runes("a"))
	if model.mode != modeType {
		t.Fatalf("a opened mode %v with the cursor on the work under the job, want the line that asks "+
			"for an answer (%v):\n%s", model.mode, model.Reported(), model.View())
	}
	model = typeAll(t, model, theAnswerTyped)
	model = step(t, model, enter())

	if len(client.told) != 1 {
		t.Fatalf("the system was answered %d times, want once: %+v", len(client.told), client.told)
	}
	if client.told[0].id != client.jobs[1].GetId() {
		t.Fatalf("the answer went to %q, want the job %q rather than the line under the cursor",
			client.told[0].id, client.jobs[1].GetId())
	}
}

// An empty line is a cancel rather than an answer. A job started again with nothing new to go on, and
// a person who believes they answered it, is worse than a key that refuses.
func TestAnEmptyAnswerOnAJobsViewIsRefusedAndTheJobKeepsAsking(t *testing.T) {
	client := twoJobsWaitingOnAPerson()
	model := openedOnTheAskingJob(t, client, 1)
	view := model.active.Name

	model = step(t, model, runes("a"))
	if model.mode != modeType {
		t.Fatalf("a on the job opened mode %v, want the line that asks for an answer (%v):\n%s",
			model.mode, model.Reported(), model.View())
	}
	model = step(t, model, enter())

	if len(client.told) != 0 {
		t.Fatalf("an empty line was sent as an answer: %+v", client.told)
	}
	if model.Reported() == nil {
		t.Fatal("an empty answer was swallowed, which reads as the job having been answered")
	}
	if client.jobs[1].GetPhase() != job.PhaseAsking {
		t.Fatalf("the job is %q, want it still asking", client.jobs[1].GetPhase())
	}
	if model.active.Name != view {
		t.Fatalf("a refused answer left the operator on %q, want the job view %q", model.active.Name, view)
	}
}

// Most jobs are not asking anything. The key says so rather than opening a line and taking an answer
// that nothing is waiting for, which is what the listing already does with it.
func TestTheAnswerKeyOnAJobThatIsNotAskingSaysSoRatherThanTakingAnAnswer(t *testing.T) {
	client := twoJobsWaitingOnAPerson()
	client.jobs[1].Phase, client.jobs[1].Question = job.PhaseRunning, ""

	model := openedOnTheAskingJob(t, client, 1)
	model = step(t, model, runes("a"))

	if model.mode == modeType {
		t.Fatalf("a opened a line on a job that asked nothing, so the answer would be thrown away:\n%s",
			model.View())
	}
	if len(client.told) != 0 {
		t.Fatalf("a job that asked nothing was answered: %+v", client.told)
	}
	if model.Reported() == nil {
		t.Fatal("a did nothing and said nothing, which reads as a console that stopped answering")
	}
}

// The same key on both views. A person who answers from the listing and a person who answers from the
// job press the same letter, and a second letter for the same thing is a letter somebody has to learn.
func TestAJobsOwnViewAnswersWithTheSameKeyTheListingUses(t *testing.T) {
	client := twoJobsWaitingOnAPerson()
	listing, found := actionNamed(Jobs(client), "Answer")
	if !found {
		t.Fatal("the jobs listing has no key that answers a job, so there is nothing to be the same as")
	}

	model := openedOnTheAskingJob(t, client, 1)
	answer, bound := boundOnTheView(model.active, listing.Key)
	if !bound {
		t.Fatalf("%q does nothing on the job view, and it is the key the listing answers with:\n%s",
			listing.Key, model.View())
	}
	if answer.Asks == "" || answer.RunTyped == nil {
		t.Fatalf("%q is %q on the job view, which takes no line of text, and the listing answers with it",
			listing.Key, answer.Label)
	}
}
