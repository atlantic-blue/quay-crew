package console

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// What one job holds, read in the console rather than in a shell.
//
// A person watching a job in the console can read a row of it, and the row is nine cells. Everything
// the job actually carries lives somewhere else: the brief somebody wrote, the questions it asked and
// the answers it got, the plan and whether anybody approved it, the record of what its verticals
// became, the evidence a person looked at before they said the value arrived, and the answer at the
// end. Today that reader leaves the console and runs `krewe job show`, and the console is the one
// place they were already looking.
//
// So these cases put a whole job on the screen and read the screen back. They assert on what is
// drawn, through the real model and the keys a person presses, rather than on the call a key
// produced: a view that fetched the job and drew none of it would pass every assertion made at the
// point of departure.

// TestTheJobViewShowsTheBriefWhole is the first of them because the brief is the only thing on this
// page a person wrote themselves, and the row can never hold it: the title is one line and the brief
// is what the session was actually asked to do.
func TestTheJobViewShowsTheBriefWhole(t *testing.T) {
	model := aConsoleOnOneJob(t, aJobWithItsWholeRecord())

	screen := drawnText(model)

	// Both ends of the brief, so a brief cut to the width of a cell fails here rather than reading as
	// a brief that was drawn.
	carries(t, screen, "Build the page that draws one conversation")
	carries(t, screen, "and nothing behind a sign in")
}

// Every question this job put to a person, with the answer that came back, in that order. A question
// with no answer under it leaves the reader looking for the decision, and an answer with no question
// over it says nothing at all.
func TestTheJobViewShowsEveryQuestionWithTheAnswerItGot(t *testing.T) {
	one := aJobWithItsWholeRecord()
	model := aConsoleOnOneJob(t, one)

	screen := drawnText(model)

	for _, pair := range []struct {
		asked    string
		answered string
	}{
		// What it understood before it planned, and what the person wrote about it.
		{asked: "I cannot tell who is allowed to open the address", answered: "Anybody holding the address may read it"},
		// What it asked once the verticals were built, and what the person said.
		{asked: "Look at the two pictures and say whether the value arrived", answered: "Yes, and it reads on a phone"},
	} {
		carries(t, screen, pair.asked)
		carries(t, screen, pair.answered)
		if at, said := strings.Index(screen, tidy(pair.asked)), strings.Index(screen, tidy(pair.answered)); said < at {
			t.Fatalf("the answer %q is drawn above the question %q it answers, so neither reads as the other's",
				pair.answered, pair.asked)
		}
	}
}

// The plan, with the numbers the work is accounted for by, and the fact that a person approved it.
func TestTheJobViewShowsThePlanAndThatAPersonApprovedIt(t *testing.T) {
	model := aConsoleOnOneJob(t, aJobWithItsWholeRecord())

	screen := drawnText(model)

	for _, step := range []string{
		"1. Write the failing tests for both requirements",
		"2. Build the page and turn them green",
		"3. Show each vertical working",
	} {
		carries(t, screen, step)
	}
	if !strings.Contains(strings.ToLower(screen), "approved") {
		t.Fatalf("the screen never says the plan was approved:\n%s", model.View())
	}
}

// And a plan nobody approved must not read as one that was. The two are days apart: an approved plan
// is work a person signed off, and an unapproved one is work waiting on them.
func TestTheJobViewDoesNotCallAPlanApprovedWhenNobodyHas(t *testing.T) {
	one := aJobWithItsWholeRecord()
	one.PlanApproved = false

	model := aConsoleOnOneJob(t, one)

	screen := strings.ToLower(drawnText(model))
	if strings.Contains(screen, "approved") && !strings.Contains(screen, "not approved") {
		t.Fatalf("a plan nobody approved is drawn as approved:\n%s", model.View())
	}
	if !strings.Contains(screen, "approv") {
		t.Fatalf("the screen says nothing about whether a person approved the plan:\n%s", model.View())
	}
}

// The record of what the verticals became: one line per vertical, and the picture each one was shown
// with. A record that lists the verticals and drops the pictures is the half a person cannot check.
func TestTheJobViewShowsTheBuildRecordWithAPictureForEachVertical(t *testing.T) {
	model := aConsoleOnOneJob(t, aJobWithItsWholeRecord())

	screen := drawnText(model)

	for _, line := range []string{
		"Vertical: 1", "Passing 1: TestPastingALinkPrintsTheTranscript", "Picture: vertical-1.png",
		"Vertical: 2", "Passing 1: TestTheTranscriptPageRendersAtItsAddress", "Picture: vertical-2.png",
	} {
		carries(t, screen, line)
	}
}

// The evidence itself, which is the label under each picture: where it came from and what it takes to
// get it again. A picture with no label is decoration, and this is the page a person accepts the work
// from.
func TestTheJobViewShowsWhereEachPictureCameFrom(t *testing.T) {
	model := aConsoleOnOneJob(t, aJobWithItsWholeRecord())

	screen := drawnText(model)

	carries(t, screen, "Taken: the page at http://localhost:3000, drawn with krewe render while the server was up")
	carries(t, screen, "Taken: the same page at a second address, drawn the same way")
}

// That a person looked at that evidence and said the value arrived. It is the only road into done for
// a job whose verticals are built, so a reader of the page has to be able to see it happened.
func TestTheJobViewSaysAPersonAcceptedWhatWasBuilt(t *testing.T) {
	model := aConsoleOnOneJob(t, aJobWithItsWholeRecord())

	if !strings.Contains(strings.ToLower(drawnText(model)), "accepted") {
		t.Fatalf("the screen never says a person accepted the build:\n%s", model.View())
	}
}

// And a build nobody has looked at yet must not read as accepted, for the reason the plan must not:
// the record is the same either way, and the one word is the whole difference.
func TestTheJobViewDoesNotSayAcceptedBeforeAnybodyHas(t *testing.T) {
	one := aJobWithItsWholeRecord()
	one.Accepted = false

	model := aConsoleOnOneJob(t, one)

	screen := strings.ToLower(drawnText(model))
	if strings.Contains(screen, "accepted") && !strings.Contains(screen, "not accepted") {
		t.Fatalf("a build nobody accepted is drawn as accepted:\n%s", model.View())
	}
	if !strings.Contains(screen, "accept") {
		t.Fatalf("the screen says nothing about a build waiting to be accepted:\n%s", model.View())
	}
}

// The answer, whole. It is what the job came back with, and a reader who has to open a sandbox or run
// a command to see the end of it has been given a summary rather than the answer.
func TestTheJobViewShowsTheAnswerWhole(t *testing.T) {
	model := aConsoleOnOneJob(t, aJobWithItsWholeRecord())

	screen := drawnText(model)

	carries(t, screen, "Both verticals are built and the suite is green.")
	carries(t, screen, "The work is open at https://github.com/atlantic-blue/quay-krewe/pull/999")
	// The last sentence as well as the first, because an answer cut at the panel's edge carries the
	// claim and drops the cost, and the cost is the half a reader came for.
	carries(t, screen, "I did not check it against a screen reader.")
}

// ---------- the job, the console it is read in, and the double under both ----------

// aJobWithItsWholeRecord is one job at the end of its road: it asked two questions and got two
// answers, it holds the brief it was declared with, the plan a person approved, the record of the two
// verticals it built with a picture each, the word that says a person accepted them, and the answer.
//
// Every field here is one a job carries today. Nothing is invented for the sake of the view.
func aJobWithItsWholeRecord() *quaycrewv1.Job {
	return &quaycrewv1.Job{
		Id: "1111111111111111aaaaaaaa", Workspace: "w1", Project: "p1",
		Title:   "put a conversation on a page",
		Product: "A person pastes a link to a conversation and reads it as a page.",
		Brief: "Build the page that draws one conversation from the store, oldest message first, " +
			"at an address anybody holding it can open and nothing behind a sign in.",
		Role: "builder", RoleVersion: 2,
		Phase: job.PhaseDone, Outcome: "proved",
		Session: "2222222222222222bbbbbbbb", Attempts: 1,
		Ideation: "I read this as one page for one conversation, read only, drawn from the store. " +
			"I cannot tell who is allowed to open the address.",
		IdeationAnswer: "Anybody holding the address may read it. Do not put it behind a sign in.",
		Design: "Vertical 1: a person pastes a link and reads the conversation.\n" +
			"Shown 1: a picture of the page with the messages on it.",
		DesignAccepted: true,
		Plan: "1. Write the failing tests for both requirements\n" +
			"2. Build the page and turn them green\n" +
			"3. Show each vertical working",
		PlanApproved: true,
		Build: "I built both verticals and ran the suite.\n\n" +
			"Vertical: 1\nRan: 14\nRed: 0\n" +
			"Passing 1: TestPastingALinkPrintsTheTranscript\n" +
			"Changed 1: internal/transcript/page.go\n" +
			"Picture: vertical-1.png\n" +
			"Taken: the page at http://localhost:3000, drawn with krewe render while the server was up\n\n" +
			"Vertical: 2\nRan: 14\nRed: 0\n" +
			"Passing 1: TestTheTranscriptPageRendersAtItsAddress\n" +
			"Changed 1: internal/transcript/route.go\n" +
			"Picture: vertical-2.png\n" +
			"Taken: the same page at a second address, drawn the same way",
		Accepted: true,
		Question: "The two verticals are built. Look at the two pictures and say whether the value arrived.",
		Told:     "Yes, and it reads on a phone as well.",
		Answer: "Both verticals are built and the suite is green. " +
			"The work is open at https://github.com/atlantic-blue/quay-krewe/pull/999. " +
			"I did not check it against a screen reader.",
		PullRequest: "https://github.com/atlantic-blue/quay-krewe/pull/999",
		Reviewed:    true, Tested: true,
		CreatedAt: timestamppb.New(time.Now().Add(-2 * time.Hour)),
	}
}

// aConsoleOnOneJob is a person standing in the jobs listing with the cursor on that job, who then
// opens it.
//
// The window is taller than any operator's, so a piece of the record that is missing from this screen
// is missing rather than one line below the fold. What the view does when the record is taller than
// the window is the scrolling every reading panel in this console already does, and it is not what
// these cases are about.
func aConsoleOnOneJob(t *testing.T, one *quaycrewv1.Job) Model {
	t.Helper()
	client := &wholeRecordClient{jobs: []*quaycrewv1.Job{one}}
	model := newTestModel(t, Jobs(client), Exec(client))
	model.width, model.height = 120, 200
	model = model.WithClient(client)
	model, _ = update(t, model, rowsFor(model, jobRow(one)))
	return openTheJob(t, model)
}

// openTheJob presses the key that descends from a row into what it is about, and then runs whatever
// that key asked for and feeds it back the way the runtime does, so what the cases read is the screen
// the person is left looking at rather than the intent behind it.
func openTheJob(t *testing.T, model Model) Model {
	t.Helper()
	model, cmd := update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	for depth := 0; cmd != nil && depth < 4; depth++ {
		msg := cmd()
		if msg == nil {
			break
		}
		if batch, isBatch := msg.(tea.BatchMsg); isBatch {
			cmd = nil
			for _, each := range batch {
				if each == nil {
					continue
				}
				if produced := each(); produced != nil {
					model, _ = update(t, model, produced)
				}
			}
			continue
		}
		model, cmd = update(t, model, msg)
	}
	if model.err != nil {
		t.Fatalf("opening the job refused: %v", model.err)
	}
	return model
}

// carries says the screen holds a piece of what the job holds, and prints the screen when it does not,
// because a reader of the failure needs to see what was drawn instead.
func carries(t *testing.T, screen, want string) {
	t.Helper()
	if !strings.Contains(screen, tidy(want)) {
		t.Fatalf("the screen does not carry %q. What it says:\n%s", want, screen)
	}
}

// tidy runs the words together the way the drawn screen does, so a sentence wrapped over two rows is
// looked for as the sentence it is.
func tidy(text string) string { return strings.Join(strings.Fields(text), " ") }

// wholeRecordClient answers the calls a view of one job makes. It embeds the generated interface, so
// a call nothing here answers panics loudly rather than being quietly satisfied.
type wholeRecordClient struct {
	quaycrewv1.ControlPlaneServiceClient

	jobs []*quaycrewv1.Job
}

func (w *wholeRecordClient) GetJob(_ context.Context, req *quaycrewv1.GetJobRequest, _ ...grpc.CallOption) (*quaycrewv1.GetJobResponse, error) {
	for _, one := range w.jobs {
		if one.GetId() == req.GetId() {
			return &quaycrewv1.GetJobResponse{Job: one}, nil
		}
	}
	return nil, fmt.Errorf("no job %q", req.GetId())
}

func (w *wholeRecordClient) ListJobs(_ context.Context, _ *quaycrewv1.ListJobsRequest, _ ...grpc.CallOption) (*quaycrewv1.ListJobsResponse, error) {
	return &quaycrewv1.ListJobsResponse{Jobs: w.jobs}, nil
}

func (w *wholeRecordClient) ListExecutions(_ context.Context, _ *quaycrewv1.ListExecutionsRequest, _ ...grpc.CallOption) (*quaycrewv1.ListExecutionsResponse, error) {
	return &quaycrewv1.ListExecutionsResponse{}, nil
}

func (w *wholeRecordClient) ListTasks(_ context.Context, _ *quaycrewv1.ListTasksRequest, _ ...grpc.CallOption) (*quaycrewv1.ListTasksResponse, error) {
	return &quaycrewv1.ListTasksResponse{}, nil
}
