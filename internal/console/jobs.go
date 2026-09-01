package console

import (
	"context"
	"fmt"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// Jobs lists what the system has been asked to do, which is the work itself rather than the layer
// underneath it. The console was built when a session was the unit of work, and a job is what an
// operator declares now: five were running on this repository the day this view did not exist.
//
// One line carries the whole story of a piece of automation: which job, where it got to, the word it
// ended on, whose role is doing it, what it is about, the conversation it runs in, how many times it
// has been tried and how old it is. The answer and the brief are not here, because a listing of a hundred answers is a
// listing nobody can read; enter goes to what the job actually did instead.
func Jobs(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "jobs",
		Aliases: []string{"j", "job"},
		// The same rules the sessions listing already follows: a name carries its own colour so the
		// eye finds its rows without reading them, identifiers and counts are dim so they stop
		// competing, and the one cell that says how the row is doing is coloured by meaning.
		Columns: []Column{
			// The call for a person, in the first column, one character wide, empty on every job that
			// wants nothing. It never gives way and it is never coloured out of existence: a job
			// waiting to be answered has to be findable down a screen of forty rows by shape rather
			// than by reading each phase, and a colour is nothing at all on a terminal without one.
			{Title: " ", Width: 2, Colour: colourOfAsking},
			// Headed job because it is the value every job command takes, the way the sessions
			// listing heads its first column session.
			{Title: "job", Width: 10, Colour: dim},
			{Title: "phase", Width: 9, Colour: colourOfPhase},
			// What the job ended on, beside where it got to. The two say different things: the phase
			// is the system's account of the attempt, and this is the work's own. Five jobs reading
			// "done" with one of them unable to do its work is the row this column exists for.
			{Title: "outcome", Width: 9, Colour: colourOfOutcome},
			// Gives way third: a system where every job names a role has a column of one word, and by
			// then the title is worth more than it is.
			{Title: "role", Width: 16, Give: 4, Colour: colourOfName},
			// The flexible column, because it is the line an operator reads to know what this is.
			{Title: "title", Width: 0},
			// Gives way second, since a job with no session yet has nothing here anyway.
			{Title: "session", Width: 10, Give: 2, Colour: dim},
			// How many jobs this one declared, which is what says a row is a run rather than a single
			// piece of work. Gives way after the session, because a run with no steps on screen reads
			// as a run that did nothing.
			{Title: "steps", Width: 6, Give: 3, Colour: dim},
			// Gives way first. It is the count that only matters once something has gone wrong twice.
			{Title: "attempts", Width: 8, Give: 1, Colour: dim},
			{Title: "age", Width: 6, Colour: colourOfAge},
		},
		// No order of its own: both stores answer newest first, and sorting here would be a second
		// order to keep in step with the command line. The age column cannot hold this order either
		// way, because these cells are rendered text and sorting them compares "10d" against "1d" as
		// words.
		SortBy: -1,
		// What the job did, rather than the row it runs in. A job's session is one row and a listing
		// of one row says nothing the line above it did not; the tasks are the whole account of what
		// was asked and what came back. The sessions view cannot be scoped to a session either: its
		// lister reads its parent as a project, so descending there would list nothing at all.
		DrillTo: "tasks",
		DrillBy: sessionOfJob,
		Actions: []Action{
			{
				// The steps of a run, which enter cannot be: enter goes to the work running under this
				// job, and a run declares jobs rather than doing the work itself. Uppercase, beside every
				// other key that acts on the row under the cursor.
				Key:     "S",
				Label:   "Steps",
				Descend: "steps",
			},
			{
				// The same key, in the same meaning, as the sessions view: backspace stops the thing
				// under the cursor and asks first. A job that stopped by itself and one somebody
				// halted must never read the same, so this is on the record rather than silent.
				Key:     "backspace",
				Also:    []string{"x"},
				Label:   "Stop",
				Confirm: true,
				Run: func(ctx context.Context, row Row) error {
					if row.ID == "" {
						return fmt.Errorf("no job selected")
					}
					_, err := client.StopJob(ctx, &quaycrewv1.StopJobRequest{Id: row.ID})
					return err
				},
			},
		},
		// How many of these are waiting for a person, above the columns, so a screen of forty rows says
		// it before anybody scrolls. It says nothing when nothing is waiting.
		Summary: askingSummary(client),
		// parent is a project id when this view is drilled into from one, and empty at the top level,
		// which is every job the system holds.
		// Inside a project this lists the jobs that were declared rather than the ones a run declared
		// for itself: a run and its own steps side by side read as unrelated work. The steps are one key
		// away on the run, and the count in the steps column is what says they are there.
		//
		// The whole listing is read and narrowed here rather than asked for narrowed, because the count
		// of a run's steps comes out of the same answer. Asking for the roots would cost a second call
		// to count what the first one already returned.
		//
		// At the top level nothing is narrowed. That listing is the flat one, and a flat listing that
		// hid every step would be answering a question nobody asked it.
		List: func(ctx context.Context, project string) ([]Row, error) {
			resp, err := client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Project: project})
			if err != nil {
				return nil, err
			}
			rows := make([]Row, 0, len(resp.GetJobs()))
			for _, one := range resp.GetJobs() {
				if project != "" && one.GetParent() != "" {
					continue
				}
				rows = append(rows, jobRow(one, childrenOf(one.GetId(), resp.GetJobs())))
			}
			return rows, nil
		},
	}
}

// sessionOfJob is the conversation to descend into, and the refusal when there is none yet.
//
// A pending job has no session, which is the normal state rather than a fault, so what it says names
// the phase it is in and stops there.
func sessionOfJob(row Row) (string, error) {
	if row.ID == "" {
		return "", fmt.Errorf("no job selected")
	}
	if row.Parent == "" {
		return "", fmt.Errorf("%s is %s, so there is nothing it has done yet: a job reaches a session when a controller starts it",
			display.ShortID(row.ID), phaseOfRow(row))
	}
	return row.Parent, nil
}

// outcomeColumn is where the word a job ended on sits in its row, which the tests read so a column
// added in front of it moves one number rather than several.
const outcomeColumn = 3

// phaseColumn is where a job's phase sits in its row, which the refusal above reads so it says where
// the job got to rather than only that it got nowhere.
const phaseColumn = 2

// theAskingMark is what sits in the first cell of a job that is waiting for a person. One character,
// on every screen, in the column nothing else is ever drawn in.
const theAskingMark = "?"

// askingMark is that mark, and nothing at all on a job that is not waiting for anybody.
func askingMark(phase string) string {
	if phase == job.PhaseAsking {
		return theAskingMark
	}
	return ""
}

func phaseOfRow(row Row) string {
	if len(row.Cells) <= phaseColumn {
		return "not started"
	}
	return row.Cells[phaseColumn]
}

// noSessionYet is what the session cell says on a job that has not reached one. An empty cell reads
// as something missing, and this is a job waiting its turn rather than a job with a hole in it.
const noSessionYet = "not yet"

// jobRow is one job as a listing row.
//
// Parent is the session rather than the project, because the parent is what a drilled down view
// scopes by, and what this view descends into is the session's tasks.
func jobRow(one *quaycrewv1.Job, steps int) Row {
	// The identifiers stay whole: they are what stopping and descending use. Only the cells shorten.
	session := noSessionYet
	if one.GetSession() != "" {
		session = display.ShortID(one.GetSession())
	}
	return Row{
		ID:     one.GetId(),
		Parent: one.GetSession(),
		Label:  one.GetTitle(),
		State:  stateOfPhase(one.GetPhase()),
		Cells: []string{
			askingMark(one.GetPhase()),
			display.ShortID(one.GetId()),
			one.GetPhase(),
			outcomeCell(one.GetOutcome()),
			one.GetRole(),
			oneLine(one.GetTitle()),
			session,
			stepsCell(steps),
			fmt.Sprintf("%d", one.GetAttempts()),
			display.Age(one.GetCreatedAt()),
		},
		// The whole title, for a view that can show more than a cell can hold.
		Detail: one.GetTitle(),
		// What a person types for this job, which is the shortened identifier every job command takes.
		// The title is what they read, and no command takes it.
		Address: display.ShortID(one.GetId()),
	}
}

// stateOfPhase maps where a job got to onto a colour. The words are the job package's, so this and
// the phase column read one set and a phase neither of them knows stays uncoloured rather than being
// dressed as finished.
//
// Asking is coloured as work in flight rather than as a failure. It is the row that most wants a
// person, and red would say it ended badly when it has not ended at all; the word is what says
// somebody is being waited for.
func stateOfPhase(phase string) State {
	switch phase {
	case job.PhaseRunning, job.PhaseAsking:
		return StateBusy
	case job.PhaseDone:
		return StateReady
	case job.PhaseFailed:
		return StateFailed
	case job.PhaseStopped:
		return StateStopped
	// Pending and waiting fall through with everything else. Nothing has happened to them, so the row
	// makes no claim rather than one.
	default:
		return StateUnknown
	}
}

// nothingStated is what the outcome cell says on a job that has not ended on a word. An empty cell
// reads as a column that failed to fill, and this is a job still working or one that stopped.
const nothingStated = "-"

func outcomeCell(outcome string) string {
	if outcome == "" {
		return nothingStated
	}
	return outcome
}

// colourOfOutcome reads the word the same way the job package writes it, so a word neither of them
// knows stays uncoloured rather than being dressed as work that was proved.
func colourOfOutcome(cell string) string {
	switch cell {
	case job.OutcomeProved:
		return ansiGreenCode
	case job.OutcomeBlocked:
		return ansiRedCode
	case job.OutcomeDecide:
		return ansiYellowCode
	// Unproved is left alone. It is work that was done, so red would say it failed, and green would
	// say something checked it. The word is what carries the difference.
	default:
		return ""
	}
}

// colourOfPhase puts a job's state in the one cell that names it, the way colourOfStatus does for a
// session. A line drawn in a single colour cannot carry any of the others, and this listing has a
// role and a title to colour too.
func colourOfPhase(cell string) string {
	switch stateOfPhase(cell) {
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

// askingSummary is the line above the columns: how many of these jobs are waiting for a person. It is
// empty when none are, which draws nothing, because a line saying nothing is waiting is a line that
// teaches an operator to stop reading the one place this is announced.
//
// A second telling of what the mark in the first column already says, deliberately. The mark is found
// by scanning the rows and this is read without scanning anything, and the case they both exist for is
// a listing longer than the screen.
func askingSummary(client quaycrewv1.ControlPlaneServiceClient) Summariser {
	return func(ctx context.Context, project string) (string, State) {
		resp, err := client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{
			Project: project, Phase: job.PhaseAsking,
		})
		if err != nil || len(resp.GetJobs()) == 0 {
			return "", StateUnknown
		}
		if len(resp.GetJobs()) == 1 {
			return "1 job is waiting for a person", StateBusy
		}
		return fmt.Sprintf("%d jobs are waiting for a person", len(resp.GetJobs())), StateBusy
	}
}

// Steps are the jobs one job declared: the steps of a flow run, or the work a job broke itself into.
// It is the jobs listing scoped the other way, by parent rather than by project, so a run and its
// steps read identically and only what they are narrowed by differs.
//
// A resource of its own rather than a second meaning for the jobs listing, because a lister reads one
// parent and the jobs listing already reads that parent as a project. Two narrowings through one field
// is how a view starts listing the wrong rows.
func Steps(client quaycrewv1.ControlPlaneServiceClient) Resource {
	steps := Jobs(client)
	steps.Name, steps.Aliases = "steps", []string{"step", "children"}
	// The count belongs to the level above: these rows are already the steps of one job, and a step
	// with steps of its own carries its own count in the same column.
	steps.Summary = askingSummary(client)
	steps.List = func(ctx context.Context, parent string) ([]Row, error) {
		if parent == "" {
			return nil, fmt.Errorf("open the steps of a job from the jobs listing: there are no steps without one")
		}
		resp, err := client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Parent: parent})
		if err != nil {
			return nil, err
		}
		// The steps of a step. A listing narrowed to one parent holds the children and not the
		// grandchildren, so counting inside it would tell every nested run it declared nothing. The
		// project is read off the children rather than passed in, because a lister is given one
		// identifier and it is the parent.
		among := resp.GetJobs()
		if len(among) > 0 {
			if whole, err := client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{
				Project: among[0].GetProject(),
			}); err == nil {
				among = whole.GetJobs()
			}
		}
		rows := make([]Row, 0, len(resp.GetJobs()))
		for _, one := range resp.GetJobs() {
			rows = append(rows, jobRow(one, childrenOf(one.GetId(), among)))
		}
		return rows, nil
	}
	return steps
}

// childrenOf is how many of these jobs name one as their parent, which is what the steps cell says.
func childrenOf(id string, among []*quaycrewv1.Job) int {
	count := 0
	for _, one := range among {
		if one.GetParent() == id {
			count++
		}
	}
	return count
}

// noSteps is what the steps cell says on a job that declared none, which is most of them. An empty
// cell reads as a column that failed to fill, and a zero reads as a measurement of nothing.
const noSteps = "-"

func stepsCell(count int) string {
	if count == 0 {
		return noSteps
	}
	return fmt.Sprintf("%d", count)
}
