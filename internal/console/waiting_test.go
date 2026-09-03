package console

import (
	"context"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The console is the surface that reaches somebody who is looking at something else, so these drive
// the whole of it: the answer arrives, the bell rings, and the line is on the screen. Stopping at
// "the model holds a count" would prove half of it.

// waits builds what the system says about one waiting job.
func waits(id, why, want string, seconds int64, over bool) *quaycrewv1.Waiting {
	return &quaycrewv1.Waiting{
		Job: id, Workspace: "acme", Project: "house-bills", Title: "choose where the transcripts are stored",
		Why: why, Want: want, WaitedSeconds: seconds, OverLimit: over,
		Since: timestamppb.Now(),
	}
}

// rings drives one answer into the console and says how many times it rang.
func rings(t *testing.T, model Model, answers ...waitingMsg) (Model, int) {
	t.Helper()
	rung := 0
	model = model.WithBell(func() { rung++ })
	for _, answer := range answers {
		next, cmd := model.Update(answer)
		model = next.(Model)
		// The command is run the way the runtime runs it, because a bell returned and never run is
		// a bell nobody heard.
		if cmd != nil {
			cmd()
		}
	}
	return model, rung
}

// The incident: a job enters asking, and the console says so without anybody navigating anywhere.
func TestAWaitingJobIsOnTheScreenWithoutBeingNavigatedTo(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))

	model, rung := rings(t, model, waitingMsg{waiting: []*quaycrewv1.Waiting{
		waits("f71415ba9c2e4d1a8b3c5d7e", job.WaitingAsking, "aurora or a key value store?", 30, false),
	}})

	if rung != 1 {
		t.Fatalf("the bell rang %d times when a job started waiting", rung)
	}
	screen := model.View()
	if !strings.Contains(screen, "1 job waits for you") {
		t.Fatalf("the console does not say a job waits:\n%s", screen)
	}
	if !strings.Contains(screen, "f71415ba") {
		t.Errorf("the console does not name the job that waits:\n%s", screen)
	}
	if !strings.Contains(screen, "aurora or a key value store?") {
		t.Errorf("the console does not say what the job wants:\n%s", screen)
	}
}

// The quiet case. A console that rang or drew a line while nothing waited would teach the operator
// to ignore the line, and the next one is the one that matters.
func TestAConsoleWithNothingWaitingSaysNothingAndStaysQuiet(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))

	model, rung := rings(t, model, waitingMsg{})

	if rung != 0 {
		t.Fatalf("the bell rang %d times with nothing waiting", rung)
	}
	if line := model.waitingLine(); line != "" {
		t.Fatalf("the console says %q with nothing waiting", line)
	}
	if strings.Contains(model.View(), "waits for you") {
		t.Fatalf("the console draws a telling with nothing waiting:\n%s", model.View())
	}
}

// One ring for each rise, never one for each poll. The console reloads every three seconds, and a
// bell every three seconds is a bell somebody turns off.
func TestTheBellRingsOnceForEachRiseRatherThanForEachRefresh(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	one := waits("f71415ba9c2e4d1a8b3c5d7e", job.WaitingAsking, "which store?", 30, false)
	two := waits("fe7bfea71c2e4d1a8b3c5d7e", job.WaitingBlocked, "the sandbox could not be made", 60, false)

	model, rung := rings(t, model,
		waitingMsg{waiting: []*quaycrewv1.Waiting{one}},
		waitingMsg{waiting: []*quaycrewv1.Waiting{one}},
		waitingMsg{waiting: []*quaycrewv1.Waiting{one}},
	)
	if rung != 1 {
		t.Fatalf("three refreshes of the same wait rang %d times", rung)
	}

	// A second job stops, which is news.
	model, rung = rings(t, model, waitingMsg{waiting: []*quaycrewv1.Waiting{one, two}})
	if rung != 1 {
		t.Fatalf("a second job waiting rang %d times", rung)
	}

	// And somebody answers one, which is not.
	_, rung = rings(t, model, waitingMsg{waiting: []*quaycrewv1.Waiting{two}})
	if rung != 0 {
		t.Fatalf("answering a question rang the bell %d times", rung)
	}
}

// Past the limit the line names how long. A person deciding what to look at first needs to know
// which of these has been sitting there for an hour.
func TestTheLineNamesTheAgeOfAWaitPastTheLimit(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))

	model, _ = rings(t, model, waitingMsg{waiting: []*quaycrewv1.Waiting{
		waits("f71415ba9c2e4d1a8b3c5d7e", job.WaitingAsking, "which store?", 3840, true),
	}})

	line := model.waitingLine()
	if !strings.Contains(line, "1 hour 4 minutes") {
		t.Fatalf("the line does not say how long the job has waited: %q", line)
	}
}

// The telling is drawn over every view, because a job waiting on somebody is not a property of the
// rows they happen to have open.
func TestTheTellingIsDrawnWhicheverViewIsOpen(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model, _ = rings(t, model, waitingMsg{waiting: []*quaycrewv1.Waiting{
		waits("f71415ba9c2e4d1a8b3c5d7e", job.WaitingAsking, "which store?", 30, false),
	}})

	for _, view := range []struct {
		name string
		mode mode
	}{
		{"the listing", modeBrowse},
		{"the help panel", modeHelp},
		{"the output of a command", modeReading},
	} {
		t.Run(view.name, func(t *testing.T) {
			drawn := model
			drawn.mode = view.mode
			if !strings.Contains(drawn.View(), "1 job waits for you") {
				t.Fatalf("the telling is not on this screen:\n%s", drawn.View())
			}
		})
	}
}

// A console nobody gave a bell to still draws the line. Quieter, never blind.
func TestAConsoleWithNoBellStillSaysIt(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))

	next, cmd := model.Update(waitingMsg{waiting: []*quaycrewv1.Waiting{
		waits("f71415ba9c2e4d1a8b3c5d7e", job.WaitingAsking, "which store?", 30, false),
	}})
	model = next.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			t.Fatalf("a console with no bell returned something to run: %v", msg)
		}
	}
	if !strings.Contains(model.View(), "1 job waits for you") {
		t.Fatalf("a console with no bell drew nothing:\n%s", model.View())
	}
}

// The refresh asks for it. Without this the console would answer the question once, when it opened,
// and never again, which is a page that looks live and is not.
func TestEveryRefreshAsksWhatWaits(t *testing.T) {
	model := newTestModel(t, staticResource("sessions"))
	model.client = &waitingClient{}

	_, cmd := model.Update(tickMsg{})
	if cmd == nil {
		t.Fatal("a refresh asks for nothing at all")
	}
	if !asksWhatWaits(t, cmd) {
		t.Fatal("a refresh does not ask what waits for a person, so the console answers it once and never again")
	}
}

// asksWhatWaits runs a batch of commands and says whether one of them read what waits.
func asksWhatWaits(t *testing.T, cmd tea.Cmd) bool {
	t.Helper()
	msg := cmd()
	batch, batched := msg.(tea.BatchMsg)
	if !batched {
		_, waiting := msg.(waitingMsg)
		return waiting
	}
	for _, one := range batch {
		if one == nil {
			continue
		}
		if _, waiting := one().(waitingMsg); waiting {
			return true
		}
	}
	return false
}

// waitingClient answers the one call this asks for, and panics on anything else through the embedded
// interface: a double that answered more than the real thing would hide a console asking for it.
type waitingClient struct {
	quaycrewv1.ControlPlaneServiceClient
	waiting []*quaycrewv1.Waiting
}

func (w *waitingClient) GetWaiting(_ context.Context, _ *quaycrewv1.GetWaitingRequest, _ ...grpc.CallOption) (
	*quaycrewv1.GetWaitingResponse, error) {
	return &quaycrewv1.GetWaitingResponse{Waiting: w.waiting}, nil
}
