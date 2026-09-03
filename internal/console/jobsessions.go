package console

import (
	"context"
	"fmt"
	"os/exec"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// JobSessions is every conversation running under one job, one line each.
//
// A job in its test stage holds six of them: the session the job itself runs in, and one for each
// requirement its stage fanned out into. Five of the six were only readable at the command line,
// because enter on a job row descended into what the job's own session had done and said nothing
// about the runs beside it.
//
// Enter opens the conversation on the line under the cursor, which is the key the sessions listing
// already spends on a conversation, and `t` opens what that conversation did.
func JobSessions(client quaycrewv1.ControlPlaneServiceClient) Resource {
	return Resource{
		Name:    "jobsessions",
		Aliases: []string{"js"},
		Columns: []Column{
			// Headed session because it is the value every session command takes, at the width the
			// sessions listing already heads it.
			{Title: "session", Width: 10, Colour: dim},
			{Title: "phase", Width: 7, Colour: colourOfPhase},
			{Title: "stage", Width: 8, Colour: dim},
			// The flexible column: what this conversation is working on, which is the line a person
			// reads to tell one of six apart from the other five.
			{Title: "doing", Width: 0},
			{Title: "age", Width: 6, Colour: colourOfAge},
		},
		// The job's own conversation first, then its runs in the order the control plane answers
		// them. Sorting the cells here would compare rendered text, and the order these arrive in is
		// already the order the command line prints.
		SortBy:  -1,
		Actions: jobSessionActions(client),
		List: func(ctx context.Context, one string) ([]Row, error) {
			if one == "" {
				return nil, fmt.Errorf(
					"open the sessions of a job from the jobs listing: there are none to read without a job")
			}
			held, err := jobInTheListing(ctx, client, one)
			if err != nil {
				return nil, err
			}
			// Narrowed to this job, the way the system narrows it, so another job's runs cannot be
			// drawn under this one.
			runs, err := client.ListExecutions(ctx, &quaycrewv1.ListExecutionsRequest{Job: one})
			if err != nil {
				return nil, err
			}
			rows := make([]Row, 0, len(runs.GetExecutions())+1)
			if held.GetSession() != "" {
				rows = append(rows, jobOwnSessionRow(held))
			}
			for _, run := range runs.GetExecutions() {
				rows = append(rows, runSessionRow(run))
			}
			return rows, nil
		},
	}
}

// jobInTheListing is the job this view is opened on, found in the jobs the control plane lists.
//
// The one thing read off the job here is the conversation the job itself runs in, which is one of the
// six lines a fanned out job draws. It comes from the listing so this view asks for what the listing
// above it already asks for, and a job that has since gone says so by name rather than drawing its
// runs under a job that is not there.
func jobInTheListing(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, one string) (
	*quaycrewv1.Job, error) {
	listed, err := client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{})
	if err != nil {
		return nil, err
	}
	for _, held := range listed.GetJobs() {
		if held.GetId() == one {
			return held, nil
		}
	}
	return nil, fmt.Errorf("the system holds no job %s, so nothing is running under it", display.ShortID(one))
}

// theJobItself is what the doing cell says on the job's own conversation. It is the one line here
// that is not a run, and a person reading six lines has to be able to say which one the job is.
const theJobItself = "the job itself"

// jobOwnSessionRow is the conversation the job runs in.
func jobOwnSessionRow(one *quaycrewv1.Job) Row {
	return Row{
		ID: one.GetSession(),
		// The job, because that is what every line here belongs to.
		Parent:  one.GetId(),
		Label:   theJobItself,
		Address: display.ShortID(one.GetSession()),
		State:   stateOfPhase(one.GetPhase()),
		Cells: []string{
			display.ShortID(one.GetSession()),
			one.GetPhase(),
			job.StageOfWire(one).Says(),
			theJobItself,
			display.Age(one.GetCreatedAt()),
		},
		// The whole of what the job is, for a view that can show more than a cell holds.
		Detail: one.GetTitle(),
	}
}

// runSessionRow is the conversation one run of one stage works in.
//
// A run that has not reached a session yet keeps its line and says so, because a fan out of five that
// drew three lines would read as a job running three sessions. Its identifier is empty, which is what
// the keys here refuse on: a run is not a conversation and there is nothing to open.
func runSessionRow(run *quaycrewv1.Execution) Row {
	shown := noSessionYet
	if run.GetSession() != "" {
		shown = display.ShortID(run.GetSession())
	}
	return Row{
		ID:      run.GetSession(),
		Parent:  run.GetJob(),
		Label:   executionTitle(run),
		Address: shown,
		State:   stateOfPhase(run.GetPhase()),
		Cells: []string{
			shown,
			run.GetPhase(),
			run.GetStage(),
			executionTitle(run),
			display.Age(run.GetCreatedAt()),
		},
		Detail: executionTitle(run),
	}
}

// jobSessionActions are the two keys on a line here, and both are the sessions listing's own keys in
// the sessions listing's meaning: enter opens the conversation, and `t` opens what it did.
//
// Enter opening what the job's session had done is the key this view replaced, so `t` is the way back
// to it, from any of the six lines rather than from the job's own.
func jobSessionActions(client quaycrewv1.ControlPlaneServiceClient) []Action {
	return []Action{
		{
			Key:   "enter",
			Also:  []string{"a"},
			Label: "Open",
			// In a panel the conversation opens beside the console, so the line the cursor is on is
			// the conversation the operator ends up talking to.
			Conversation: true,
			Refuses:      onlyALineWithASession,
			Shell: func(row Row) (*exec.Cmd, error) {
				return attachCommand(client, row.ID)
			},
		},
		{
			Key:     "t",
			Label:   "History",
			Descend: "exec",
			Refuses: onlyALineWithASession,
		},
	}
}

// onlyALineWithASession is why both keys do nothing on a run that has not started. The line is drawn
// so the count of them is the truth, and neither key has anything to act on until a controller gives
// that run a conversation.
func onlyALineWithASession(row Row) error {
	if row.ID == "" {
		return fmt.Errorf("%s has not reached a session yet, so there is no conversation to open",
			row.Name())
	}
	return nil
}
