package console

import (
	"context"
	"errors"
	"strings"
	"testing"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/flow"
	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/grpc"
)

// flowClient is a control plane double for the flows view. It embeds the generated interface so a
// call this view is not supposed to make panics loudly rather than being silently satisfied.
type flowClient struct {
	quaycrewv1.ControlPlaneServiceClient

	runs      []*quaycrewv1.FlowRun
	listedFor string
	stopped   []string
	reasons   []string
	listErr   error
}

func (f *flowClient) ListFlowRuns(_ context.Context, req *quaycrewv1.ListFlowRunsRequest, _ ...grpc.CallOption) (*quaycrewv1.ListFlowRunsResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	f.listedFor = req.GetProject()
	if req.GetProject() == "" {
		return &quaycrewv1.ListFlowRunsResponse{Runs: f.runs}, nil
	}
	matched := make([]*quaycrewv1.FlowRun, 0, len(f.runs))
	for _, run := range f.runs {
		if run.GetProject() == req.GetProject() {
			matched = append(matched, run)
		}
	}
	return &quaycrewv1.ListFlowRunsResponse{Runs: matched}, nil
}

func (f *flowClient) StopFlowRun(_ context.Context, req *quaycrewv1.StopFlowRunRequest, _ ...grpc.CallOption) (*quaycrewv1.StopFlowRunResponse, error) {
	for _, run := range f.runs {
		if run.GetId() != req.GetId() {
			continue
		}
		f.stopped, f.reasons = append(f.stopped, req.GetId()), append(f.reasons, req.GetReason())
		run.Status, run.Reason = flow.StatusStopped, req.GetReason()
		return &quaycrewv1.StopFlowRunResponse{Run: run}, nil
	}
	return nil, errors.New("no such flow")
}

func aRun(id, status string, fill func(*quaycrewv1.FlowRun)) *quaycrewv1.FlowRun {
	run := &quaycrewv1.FlowRun{
		Id: id, Workspace: "w1", Project: "p1",
		GraphName: "fix-red-pull-request", GraphVersion: 2,
		Node: "read", Status: status, Transitions: 3,
		// No moment it began: `asFlowRun` puts none on the wire and `flow.Run` holds none to put
		// there. A double looser than the real thing is how a listing passes here and draws a dash in
		// front of an operator.
	}
	if fill != nil {
		fill(run)
	}
	return run
}

func TestTheFlowsViewListsWhatTheSystemIsRunningOnItsOwn(t *testing.T) {
	client := &flowClient{runs: []*quaycrewv1.FlowRun{aRun("1111111111111111aaaaaaaa", flow.StatusRunning, nil)}}

	rows, err := Flows(client).List(context.Background(), "")
	if err != nil {
		t.Fatalf("listing flows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the view lists %d flows, want the one the system holds", len(rows))
	}
	// The identifier stays whole: stopping uses it. Only the cell shortens.
	if rows[0].ID != "1111111111111111aaaaaaaa" {
		t.Fatalf("the row carries %q as the flow, want the whole identifier", rows[0].ID)
	}
	want := []string{"11111111", "fix-red-pull-request v2", flow.StatusRunning, "read", "3"}
	if len(rows[0].Cells) != len(want) {
		t.Fatalf("the row has %d cells, want %d: %q", len(rows[0].Cells), len(want), rows[0].Cells)
	}
	for at, cell := range want {
		if rows[0].Cells[at] != cell {
			t.Fatalf("cell %d is %q, want %q (whole row %q)", at, rows[0].Cells[at], cell, rows[0].Cells)
		}
	}
}

func TestTheFlowsViewNarrowsToOneProject(t *testing.T) {
	client := &flowClient{runs: []*quaycrewv1.FlowRun{
		aRun("1111111111111111aaaaaaaa", flow.StatusRunning, nil),
		aRun("2222222222222222bbbbbbbb", flow.StatusRunning, func(run *quaycrewv1.FlowRun) { run.Project = "p2" }),
	}}

	rows, err := Flows(client).List(context.Background(), "p1")
	if err != nil {
		t.Fatalf("listing flows: %v", err)
	}
	if client.listedFor != "p1" {
		t.Fatalf("the system was asked for project %q, want p1", client.listedFor)
	}
	if len(rows) != 1 {
		t.Fatalf("the view lists %d flows, want the one in that project", len(rows))
	}
}

// A system running no automation is the ordinary state, and the screen says so rather than leaving a
// panel that reads as a console that failed to draw.
func TestAFlowsViewWithNothingRunningSaysSo(t *testing.T) {
	model := newTestModel(t, Flows(&flowClient{}))

	model, _ = update(t, model, rowsFor(model))

	if !strings.Contains(model.View(), "nothing here") {
		t.Fatalf("an empty flows view says nothing about being empty:\n%s", model.View())
	}
}

func TestFlowListingSurfacesTheControlPlaneError(t *testing.T) {
	client := &flowClient{listErr: errors.New("unavailable")}

	if _, err := Flows(client).List(context.Background(), ""); err == nil {
		t.Fatal("want the control plane error surfaced, not swallowed")
	}
}

// A flow waiting on a person shows the question where the node would be, because that is the answer
// to what it is doing. A stopped one shows why it stopped, for the same reason.
func TestAFlowSaysWhatItIsWaitingOnAndWhyItStopped(t *testing.T) {
	asking := flowRow(aRun("1111111111111111aaaaaaaa", flow.StatusAsking, func(run *quaycrewv1.FlowRun) {
		run.Question = "the change is larger than it said, carry on?"
	}))
	if !strings.Contains(asking.Cells[3], "carry on?") {
		t.Fatalf("an asking flow reads %q, want the question it is waiting on", asking.Cells[3])
	}
	if asking.State != StateBusy {
		t.Fatalf("an asking flow is drawn as %v, want work in flight rather than a failure", asking.State)
	}
	halted := flowRow(aRun("1111111111111111aaaaaaaa", flow.StatusStopped, func(run *quaycrewv1.FlowRun) {
		run.Reason = "it went round the same node four times"
	}))
	if !strings.Contains(halted.Cells[3], "four times") {
		t.Fatalf("a stopped flow reads %q, want why it stopped", halted.Cells[3])
	}
}

func TestEveryFlowStatusIsColouredOrLeftAlone(t *testing.T) {
	for status, want := range map[string]State{
		flow.StatusRunning: StateBusy,
		flow.StatusWorking: StateBusy,
		flow.StatusAsking:  StateBusy,
		flow.StatusWaiting: StateUnknown,
		flow.StatusDone:    StateReady,
		flow.StatusFailed:  StateFailed,
		flow.StatusStopped: StateStopped,
		// A status neither this nor the flow package knows stays uncoloured rather than being
		// dressed as finished.
		"reticulating": StateUnknown,
	} {
		if got := stateOfFlow(status); got != want {
			t.Errorf("stateOfFlow(%q) = %v, want %v", status, got, want)
		}
	}
	if colourOfFlowStatus(flow.StatusFailed) != ansiRedCode {
		t.Fatal("a failed flow is not drawn in red, so the cell that says how it is doing says nothing")
	}
	if colourOfFlowStatus("reticulating") != "" {
		t.Fatal("an unknown status is coloured, which is a claim about a word the console does not know")
	}
}

// The row an operator is looking at is the one that gets stopped, and the key asks first the way
// every destructive key in this console does. Driven through the console, so what is asserted is the
// screen they are left with.
func TestStoppingAFlowAsksFirstAndHaltsTheOneUnderTheCursor(t *testing.T) {
	client := &flowClient{runs: []*quaycrewv1.FlowRun{
		aRun("1111111111111111aaaaaaaa", flow.StatusRunning, nil),
		aRun("2222222222222222bbbbbbbb", flow.StatusRunning, func(run *quaycrewv1.FlowRun) {
			run.GraphName = "nightly-backlog"
		}),
		aRun("3333333333333333cccccccc", flow.StatusRunning, func(run *quaycrewv1.FlowRun) {
			run.GraphName = "release-the-build"
		}),
	}}
	model := newTestModel(t, Flows(client))
	model, _ = update(t, model, rowsFor(model,
		flowRow(client.runs[0]), flowRow(client.runs[1]), flowRow(client.runs[2])))
	model, _ = update(t, model, runes("j"))

	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyBackspace})
	if model.mode != modeConfirm {
		t.Fatalf("backspace acted without asking, and there is no way back from the wrong row")
	}
	if !strings.Contains(model.View(), "nightly-backlog") {
		t.Fatalf("the question does not name the flow it is about:\n%s", model.View())
	}
	model, cmd := update(t, model, runes("y"))
	if cmd == nil {
		t.Fatal("yes stopped nothing")
	}
	model, refresh := update(t, model, cmd())
	if model.err != nil {
		t.Fatalf("stopping refused: %v", model.err)
	}
	model, _ = update(t, model, refresh())

	if len(client.stopped) != 1 || client.stopped[0] != "2222222222222222bbbbbbbb" {
		t.Fatalf("the system was asked to stop %v, want the flow under the cursor", client.stopped)
	}
	// A flow that went quiet and one somebody halted must never read the same, so the reason is on
	// the record and on the screen.
	if client.reasons[0] == "" {
		t.Fatal("the flow was stopped with no reason, so nothing afterwards says a person did it")
	}
	halted, found := lineFor(model, "nightly-backlog")
	if !found {
		t.Fatalf("the stopped flow is not on the screen:\n%s", model.View())
	}
	if !strings.Contains(halted, flow.StatusStopped) {
		t.Fatalf("the row reads %q, want it stopped", halted)
	}
	if running, _ := lineFor(model, "release-the-build"); !strings.Contains(running, flow.StatusRunning) {
		t.Fatalf("the row beside it reads %q, want it still running", running)
	}
}

func TestTheFlowsViewIsRegisteredAndAnswersToWhatFingersType(t *testing.T) {
	registry, err := NewDefaultRegistry(&flowClient{})
	if err != nil {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}
	for _, typed := range []string{"flows", "flow"} {
		resource, found := registry.Resolve(typed)
		if !found {
			t.Fatalf("typing %q opens nothing", typed)
		}
		if resource.Name != "flows" {
			t.Fatalf("typing %q opens %q, want flows", typed, resource.Name)
		}
	}
}

// No other listing in this console is missing an age, so this says why: a run carries no moment it
// began, in the store or on the wire, and a column that can only draw a dash is worse than none.
func TestTheFlowsViewCarriesNoAgeBecauseARunHasNone(t *testing.T) {
	for _, column := range Flows(&flowClient{}).Columns {
		if column.Title == "age" {
			t.Fatal("the flows view has an age column, and nothing tells it when a flow began")
		}
	}
	run := aRun("1111111111111111aaaaaaaa", flow.StatusRunning, nil)
	if run.GetCreatedAt() != nil {
		t.Fatal("the double invents a moment the control plane never sends, so a column reading it would pass here and draw a dash in front of an operator")
	}
	// Every column has a cell and every cell has a column. A row one cell short draws each cell one
	// place to the left of the heading that names it.
	if got, want := len(flowRow(run).Cells), len(Flows(&flowClient{}).Columns); got != want {
		t.Fatalf("a flow row has %d cells and the view has %d columns", got, want)
	}
}
