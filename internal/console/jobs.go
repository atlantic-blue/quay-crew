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
			{Title: "role", Width: 16, Give: 3, Colour: colourOfName},
			// The flexible column, because it is the line an operator reads to know what this is.
			{Title: "title", Width: 0},
			// Gives way second, since a job with no session yet has nothing here anyway.
			{Title: "session", Width: 10, Give: 2, Colour: dim},
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
		// parent is a project id when this view is drilled into from one, and empty at the top level,
		// which is every job the system holds.
		List: func(ctx context.Context, project string) ([]Row, error) {
			resp, err := client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{Project: project})
			if err != nil {
				return nil, err
			}
			working := sessionsUnder(resp.GetJobs())
			rows := make([]Row, 0, len(resp.GetJobs()))
			for _, one := range resp.GetJobs() {
				rows = append(rows, jobRow(one, working[one.GetId()]))
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
const outcomeColumn = 2

// phaseColumn is where a job's phase sits in its row, which the refusal above reads so it says where
// the job got to rather than only that it got nowhere.
const phaseColumn = 1

func phaseOfRow(row Row) string {
	if len(row.Cells) <= phaseColumn {
		return "not started"
	}
	return row.Cells[phaseColumn]
}

// noSessionYet is what the session cell says on a job that has not reached one. An empty cell reads
// as something missing, and this is a job waiting its turn rather than a job with a hole in it.
const noSessionYet = "not yet"

// sessionsUnder is how many sessions are running under each job, counted off the same listing.
//
// A job that fans out has no session of its own and is not idle: the build stage runs one worker for
// each vertical, all at once, and the row that declared them waits. Counted here rather than asked
// for, because the workers are rows in this same answer and a second call to count them would be a
// second call for a number already on the screen.
func sessionsUnder(jobs []*quaycrewv1.Job) map[string]int {
	working := map[string]int{}
	for _, one := range jobs {
		if one.GetParent() == "" || job.Terminal(one.GetPhase()) {
			continue
		}
		working[one.GetParent()]++
	}
	return working
}

// jobRow is one job as a listing row.
//
// Parent is the session rather than the project, because the parent is what a drilled down view
// scopes by, and what this view descends into is the session's tasks.
//
// working is how many sessions are running under this job, which is what the session cell says on a
// row that has none of its own. A fan out of three reading "not yet" says nothing is happening while
// three sessions build, and that is the row a person watching a build is looking straight at.
func jobRow(one *quaycrewv1.Job, working int) Row {
	// The identifiers stay whole: they are what stopping and descending use. Only the cells shorten.
	session := noSessionYet
	if one.GetSession() != "" {
		session = display.ShortID(one.GetSession())
	}
	if one.GetSession() == "" && working > 0 {
		session = fmt.Sprintf("%d working", working)
	}
	return Row{
		ID:     one.GetId(),
		Parent: one.GetSession(),
		Label:  one.GetTitle(),
		// A job is the one row a person reads by its title and types by its identifier, so the position
		// line takes the short form the listing already prints.
		Address: display.ShortID(one.GetId()),
		State:   stateOfPhase(one.GetPhase()),
		Cells: []string{
			display.ShortID(one.GetId()),
			one.GetPhase(),
			outcomeCell(one.GetOutcome()),
			one.GetRole(),
			oneLine(one.GetTitle()),
			session,
			fmt.Sprintf("%d", one.GetAttempts()),
			display.Age(one.GetCreatedAt()),
		},
		// The whole title, for a view that can show more than a cell can hold.
		Detail: one.GetTitle(),
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
