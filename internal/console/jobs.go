package console

import (
	"context"
	"fmt"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// jobColumns is the shape of a job on one line: the same columns in the listing of every job and in
// the view of one of them, so a person who has learnt to read a row here reads it there.
//
// The same rules the sessions listing already follows: a name carries its own colour so the eye
// finds its rows without reading them, identifiers and counts are dim so they stop competing, and
// the one cell that says how the row is doing is coloured by meaning.
func jobColumns() []Column {
	return []Column{
		// Headed job because it is the value every job command takes, the way the sessions
		// listing heads its first column session. Eight wide, which is the shortened identifier
		// itself: every column here is as wide as the widest thing it can hold and no wider,
		// because what they leave over is the title.
		{Title: "job", Width: 8, Colour: dim},
		{Title: "phase", Width: 7, Colour: colourOfPhase},
		// How far through the work the job is, beside what the system is doing with the row. A
		// job waiting for an answer about what it understood and a job waiting for an answer
		// about a failed build both read "asking", and those two are days apart.
		{Title: "stage", Width: 8, Colour: dim},
		// What the job ended on, beside where it got to. The two say different things: the phase
		// is the system's account of the attempt, and this is the work's own. Five jobs reading
		// "done" with one of them unable to do its work is the row this column exists for.
		{Title: "outcome", Width: 8, Colour: colourOfOutcome},
		// Gives way third: a system where every job names a role has a column of one word, and by
		// then the title is worth more than it is.
		{Title: "role", Width: 16, Give: 3, Colour: colourOfName},
		// The flexible column, because it is the line an operator reads to know what this is.
		{Title: "title", Width: 0},
		// Gives way second, since a job with no session yet has nothing here anyway.
		{Title: "session", Width: 10, Give: 2, Colour: dim},
		// Gives way first. It is the count that only matters once something has gone wrong twice,
		// and it is headed by the shorter word because the heading was five characters wider than
		// anything it ever holds, all of them taken off the title.
		{Title: "tries", Width: 5, Give: 1, Colour: dim},
		{Title: "age", Width: 6, Colour: colourOfAge},
	}
}

// Jobs lists what the system has been asked to do, which is the work itself rather than the layer
// underneath it. The console was built when a session was the unit of work, and a job is what an
// operator declares now: five were running on this repository the day this view did not exist.
//
// One line carries the whole story of a piece of automation: which job, where it got to, how far
// through the work it is, the word it ended on, whose role is doing it, what it is about, the
// conversation it runs in, how many times it has been tried and how old it is. The answer and the
// brief are not here, because a listing of a hundred answers is a listing nobody can read; enter
// goes to what the job actually did instead.
func Jobs(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "jobs",
		Aliases: []string{"j", "job"},
		Columns: jobColumns(),
		// No order of its own: both stores answer newest first, and sorting here would be a second
		// order to keep in step with the command line. The age column cannot hold this order either
		// way, because these cells are rendered text and sorting them compares "10d" against "1d" as
		// words.
		SortBy: -1,
		// The job itself, scoped by the job under the cursor. Enter used to open the tasks of the
		// session under the job, which is one level past the thing a person pointed at, and it
		// refused a job that had reached no session at all: the row somebody watches most is the row
		// it would not open. What that session ran is a level below the job now.
		DrillTo: "onejob",
		// A run drawn beneath a job is not a job: it is one session working on one part of it, and
		// enter on it keeps opening what that session did. The two rows are one keystroke apart in
		// the same listing.
		DrillWhere: func(row Row) string {
			if row.Under != "" {
				return "exec"
			}
			return "onejob"
		},
		DrillBy: scopeOfJobRow,
		Actions: []Action{
			{
				// A job that fans out has one part for each requirement, and all of them used to be
				// rows beside the job that declared them: six rows, five of them machinery, and the
				// work a person asked for at the bottom of the six. The parts are under their job
				// now, and this is the key that shows them.
				//
				// Tab, because it is the key that opens what is folded away in an editor, and
				// because every letter a vim user reaches for is a move or already taken.
				Key:   "tab",
				Label: "Parts",
				Folds: true,
			},
			answerAJob(client, false),
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
			rows := make([]Row, 0, len(resp.GetJobs()))
			for _, one := range resp.GetJobs() {
				rows = append(rows, jobRow(one))
			}
			// And the runs of every stage of every one of them, each drawn under the job it belongs
			// to. It is one call for the whole listing rather than one per job: a view drawing a
			// hundred jobs must not make a hundred calls to say what is running under them. It is
			// narrowed the same way the jobs above it are, so the two halves of the screen cannot
			// disagree about which project is being read.
			runs, err := client.ListExecutions(ctx, &quaycrewv1.ListExecutionsRequest{Project: project})
			if err != nil {
				return nil, err
			}
			for _, run := range runs.GetExecutions() {
				rows = append(rows, executionRow(run))
			}
			return rows, nil
		},
	}
}

// scopeOfJobRow is what enter opens each kind of row on, scoped by.
//
// A job is scoped by itself: what opens under it is the job. A run drawn beneath a job is scoped by
// the session it works in, because what opens under it is what that one session did, and a run that
// has reached no session yet says so rather than opening an empty listing.
func scopeOfJobRow(row Row) (string, error) {
	if row.ID == "" {
		return "", fmt.Errorf("no job selected")
	}
	if row.Under == "" {
		return row.ID, nil
	}
	if row.Parent == "" {
		return "", fmt.Errorf("%s is %s, so there is nothing it has done yet: a job reaches a session when a controller starts it",
			display.ShortID(row.ID), phaseOfRow(row))
	}
	return row.Parent, nil
}

// answerAJob is the key that tells a job that stopped for a person what to do. A job that stops rings
// the bell and draws a line across the listing, and until this key nothing answered it: the answer
// had to be typed at the command line or into the web briefing, which is the one place the person
// watching was not looking.
//
// `a` for answer, on the listing of every job and on the view of one job, because a second letter for
// the same thing is a letter somebody has to learn. It costs nothing that was there before: enter
// opens a job, and the sessions view's own `a` opens a conversation, which a job row has nothing to
// do with.
//
// onScope is what makes it the job's key rather than the cursor's. On the listing the row under the
// cursor is the job. On a job's own view the rows are the work running under that job, and a run has
// no question, so the key reads what the view is open on.
func answerAJob(client quaycrewv1.ControlPlaneServiceClient, onScope bool) Action {
	return Action{
		Key:     "a",
		Label:   "Answer",
		Asks:    "answer",
		OnScope: onScope,
		Refuses: onlyAnAskingJob,
		RunTyped: func(ctx context.Context, row Row, typed string) error {
			if row.ID == "" {
				return fmt.Errorf("no job selected")
			}
			// An empty line is a cancel here rather than an answer, which is the opposite of naming a
			// session: an empty name clears a name, and an empty answer is a job restarted with
			// nothing to go on and a person who thinks they answered it.
			if strings.TrimSpace(typed) == "" {
				return fmt.Errorf("nothing was typed, so %s is still asking: answer it in words, "+
					"or press escape to leave it", display.ShortID(row.ID))
			}
			_, err := client.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{Id: row.ID, Answer: typed})
			return err
		},
	}
}

// Where each cell a reader of a row goes looking for sits in it. They are named so a column added in
// front of one moves a number here rather than a dozen through the tests.
const (
	// phaseColumn is what the system is doing with the row, which the refusals below read so they
	// say where the job got to rather than only that it got nowhere.
	phaseColumn = 1
	// stageColumn is how far through the work the job is.
	stageColumn = 2
	// outcomeColumn is the word the job ended on.
	outcomeColumn = 3
	// titleColumn is the line a person reads the row by, and the column the tree is drawn in: how
	// many parts are under a job, and an indent on each part.
	titleColumn = 5
	// sessionColumn is the conversation the job runs in, or how many are running under it.
	sessionColumn = 6
	// attemptsColumn is how many times the job has been tried.
	attemptsColumn = 7
)

func phaseOfRow(row Row) string {
	if len(row.Cells) <= phaseColumn {
		return "not started"
	}
	return row.Cells[phaseColumn]
}

// onlyAnAskingJob is why the answer key does nothing on most rows. A job that is not asking has no
// question to answer, and starting a line of text for one would take an answer nothing is waiting
// for and throw it away.
func onlyAnAskingJob(row Row) error {
	if row.ID == "" {
		return fmt.Errorf("no job selected")
	}
	if phaseOfRow(row) != job.PhaseAsking {
		return fmt.Errorf("%s is %s rather than asking, so there is no question to answer: a job that "+
			"wants a person reads %s here", display.ShortID(row.ID), phaseOfRow(row), job.PhaseAsking)
	}
	return nil
}

// noSessionYet is what the session cell says on a job that has not reached one. An empty cell reads
// as something missing, and this is a job waiting its turn rather than a job with a hole in it.
const noSessionYet = "not yet"

// jobRow is one job as a listing row.
//
// Parent is the session rather than the project, because the parent is what a drilled down view
// scopes by, and what this view descends into is the session's tasks.
//
// A job that fans out has no session of its own and is not idle: the build stage runs one session for
// each vertical, all at once, and the job waits. How many are running comes off the row, so a fan out
// of three says three are working rather than "not yet", which is the row a person watching a build
// is looking straight at.
func jobRow(one *quaycrewv1.Job) Row {
	working := int(one.GetRunningExecutions())
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
			// Read off the row by the package that decides it, so this cell, `krewe job list` and
			// `krewe job show` cannot say three different things about one job. A job that runs no
			// stages says "-" rather than naming one it is not in.
			job.StageOfWire(one).Says(),
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

// executionRow is one run of one stage as a listing row, drawn under the job it belongs to.
//
// It is the only thing that belongs to a job, so it is the only thing this view folds. A run is not a
// job: it states no title, it runs no stages and nobody declared it, so the cells a job fills with
// what a person wrote say what the run is instead, which is the stage and the number it runs.
func executionRow(run *quaycrewv1.Execution) Row {
	session := noSessionYet
	if run.GetSession() != "" {
		session = display.ShortID(run.GetSession())
	}
	return Row{
		ID:      run.GetId(),
		Parent:  run.GetSession(),
		Under:   run.GetJob(),
		Label:   executionTitle(run),
		Address: display.ShortID(run.GetId()),
		State:   stateOfPhase(run.GetPhase()),
		Cells: []string{
			display.ShortID(run.GetId()),
			run.GetPhase(),
			run.GetStage(),
			outcomeCell(run.GetOutcome()),
			"",
			executionTitle(run),
			session,
			fmt.Sprintf("%d", run.GetAttempts()),
			display.Age(run.GetCreatedAt()),
		},
		Detail: executionTitle(run),
	}
}

// executionTitle is what to call a run in a listing. Nobody wrote it a title, so it is the stage it
// runs and the number it runs for, in the words the stage that made it uses.
func executionTitle(run *quaycrewv1.Execution) string {
	return job.RunCalled(run.GetStage(), int(run.GetNumber()))
}
