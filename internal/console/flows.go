package console

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/flow"
)

// Flows lists what the automation graphs have started: one line per run, the graph it came from,
// where it got to, and what it is doing there.
//
// It is the view an operator opens when something is happening that nobody typed. Until now the only
// answer was `krewe flow list`, so a person watching the console had to leave it to find out whether
// an automation was running at all.
//
// A run has no key that descends into its steps. Each step is a job under the job the run carries,
// and the jobs view reads its scope as a project, so descending there would list every job in the
// project rather than the four this run made. That is a lister of its own rather than a word change,
// and it is not this change.
func Flows(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "flows",
		Aliases: []string{"flow"},
		Columns: []Column{
			// Headed flow because it is the value every flow command takes, the way the jobs listing
			// heads its first column job.
			{Title: "flow", Width: 10, Colour: dim},
			{Title: "graph", Width: 24, Colour: colourOfName},
			{Title: "status", Width: 9, Colour: colourOfFlowStatus},
			// Where the run sits in its graph, which is the cell that says what it is doing now.
			{Title: "at", Width: 0},
			// How many movements it has taken. It gives way first: the count only matters once a run
			// is going round in circles.
			//
			// There is no age beside it, and no other listing in this console is missing one. A run
			// carries no moment it began: not on the wire, and not in the store either, so a column
			// here could only ever draw a dash.
			{Title: "steps", Width: 7, Give: 1, Colour: dim},
		},
		// No order of its own: the control plane answers newest first, and sorting here would be a
		// second order to keep in step with the command line. The age column cannot hold this order
		// either way, because these cells are rendered text and sorting them compares "10d" against
		// "1d" as words.
		SortBy: -1,
		Actions: []Action{
			{
				// The same key, in the same meaning, as the jobs and sessions views: backspace stops
				// the thing under the cursor and asks first. An automation nobody wanted is the row
				// this key exists for.
				Key:     "backspace",
				Also:    []string{"x"},
				Label:   "Stop",
				Confirm: true,
				Run: func(ctx context.Context, row Row) error {
					if row.ID == "" {
						return fmt.Errorf("no flow selected")
					}
					_, err := client.StopFlowRun(ctx, &quaycrewv1.StopFlowRunRequest{
						Id: row.ID, Reason: stoppedFromTheConsole,
					})
					return err
				},
			},
		},
		// parent is a project id when this view is drilled into from one, and empty at the top level,
		// which is every run the system holds.
		List: func(ctx context.Context, project string) ([]Row, error) {
			resp, err := client.ListFlowRuns(ctx, &quaycrewv1.ListFlowRunsRequest{Project: project})
			if err != nil {
				return nil, err
			}
			rows := make([]Row, 0, len(resp.GetRuns()))
			for _, run := range resp.GetRuns() {
				rows = append(rows, flowRow(run))
			}
			return rows, nil
		},
	}
}

// stoppedFromTheConsole is the reason a halted run carries. A run that went quiet and a run somebody
// stopped must never read the same, and the reason is what tells them apart afterwards.
const stoppedFromTheConsole = "stopped from the console"

// flowRow is one run as a listing row.
//
// A run that is waiting on a person shows the question where the node would be, because that is the
// answer to what it is doing. The same rule the exec view already follows with a failed task, which
// shows why it failed where the reply would have been.
func flowRow(run *quaycrewv1.FlowRun) Row {
	at := run.GetNode()
	switch {
	case run.GetQuestion() != "":
		at = oneLine(run.GetQuestion())
	case run.GetReason() != "":
		at = oneLine(run.GetReason())
	}
	return Row{
		// The identifier stays whole: it is what stopping uses. Only the cell shortens.
		ID:      run.GetId(),
		Parent:  run.GetProject(),
		Label:   run.GetGraphName(),
		Address: display.ShortID(run.GetId()),
		State:   stateOfFlow(run.GetStatus()),
		Cells: []string{
			display.ShortID(run.GetId()),
			graphCell(run),
			run.GetStatus(),
			at,
			fmt.Sprintf("%d", run.GetTransitions()),
		},
	}
}

// graphCell is the graph a run came from, with the version it pinned. The version is on the same cell
// rather than in a column of its own: a run is pinned to the version it started on, so the two are
// one fact, and the graph a run followed is not the graph of that name today.
func graphCell(run *quaycrewv1.FlowRun) string {
	if run.GetGraphVersion() == 0 {
		return run.GetGraphName()
	}
	return fmt.Sprintf("%s v%d", run.GetGraphName(), run.GetGraphVersion())
}

// stateOfFlow maps a run's status onto a colour. The words are the flow package's, so this and the
// status column read one set and a status neither of them knows stays uncoloured rather than being
// dressed as finished.
//
// Asking is coloured as work in flight rather than as a failure, the way an asking job is: it is the
// row that most wants a person, and red would say it ended badly when it has not ended at all.
func stateOfFlow(status string) State {
	switch status {
	case flow.StatusRunning, flow.StatusWorking, flow.StatusAsking:
		return StateBusy
	case flow.StatusDone:
		return StateReady
	case flow.StatusFailed:
		return StateFailed
	case flow.StatusStopped:
		return StateStopped
	// Waiting falls through. A run sitting on a wait node until its time comes has had nothing
	// happen to it, so the row makes no claim rather than one.
	default:
		return StateUnknown
	}
}

func colourOfFlowStatus(cell string) string {
	switch stateOfFlow(cell) {
	case StateFailed:
		return ansiRedCode
	case StateBusy:
		return ansiYellowCode
	case StateReady:
		return ansiGreenCode
	case StateStopped:
		return dimCode
	default:
		return ""
	}
}
