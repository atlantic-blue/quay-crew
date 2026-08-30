package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/display"
	"github.com/atlantic-blue/krewe/internal/job"
)

// The score of a job is how many times the operator had to steer it, and these two commands are the
// whole of keeping it.
//
// `krewe steer "..."` is one word on purpose. The mark is made in the moment, with a hand already on
// the keyboard mid sentence, and anything longer than one word does not get made at all: what this
// replaces is thirteen of them written out from memory two days later.
//
// `krewe steers` reads them back. The listing answers "how does this compare with the one before",
// and naming a job answers "how many, and what were they".

// runSteer marks one moment the operator had to say something the system should have known.
//
// The job is the last argument's neighbour rather than a flag, because this tool takes no flags. With
// one argument the text is all of it and the job is the one in flight; with two, the first names the
// job.
func runSteer(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: krewe steer [<job>] \"...\"\n\n%s", job.WhatASteerIs)
	}
	// A sentence typed without quotes arrives as one argument per word, which is the mistake this
	// command invites: it is typed fast, mid sentence, with a hand already on the next thing. It is
	// refused rather than joined up, and the refusal is the same line with the quotes in it, because a
	// steer that was quietly assembled out of five arguments is a steer nobody can see went wrong.
	if len(args) > 2 {
		return fmt.Errorf("a steer is one sentence in quotes, and this is %d arguments. Type:\n\n"+
			"  krewe steer %q", len(args), strings.Join(args, " "))
	}
	named, text := "", args[0]
	if len(args) > 1 {
		named, text = args[0], args[1]
	}
	// An identifier on its own is a job somebody meant to steer and then said nothing about. Recording
	// it would put the identifier in the report where the sentence belongs.
	if named == "" && display.LooksLikeIdentifier(text) {
		return fmt.Errorf("%q is an identifier rather than something you said: krewe steer %s \"...\"", text, text)
	}

	landed, err := theJobBeingSteered(ctx, client, named)
	if err != nil {
		return err
	}
	recorded, err := client.RecordSteer(ctx, &quaycrewv1.RecordSteerRequest{Job: landed.GetId(), Text: text})
	if err != nil {
		return err
	}

	root := recorded.GetRoot()
	fmt.Fprintf(out, "%s on %s (%s)\n", job.Steers(int(root.GetSteers())),
		display.ShortID(root.GetId()), truncateLine(root.GetTitle()))
	fmt.Fprintf(out, "read them back with krewe steers %s\n", display.ShortID(root.GetId()))
	return nil
}

// theJobBeingSteered is the job a steer lands on: the one named, or the one job in flight where the
// operator is standing.
//
// It refuses rather than choosing when two are in flight. A steer counted against the wrong tree is
// worse than one that was not recorded, because the number then reads as measured.
func theJobBeingSteered(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, named string) (*quaycrewv1.Job, error) {
	if named != "" {
		return findJob(ctx, client, named)
	}
	located, err := locate(ctx, client, "")
	if err != nil {
		return nil, err
	}
	listed, err := client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{
		Workspace: located.WorkspaceID, Project: located.ProjectID, RootsOnly: true,
	})
	if err != nil {
		return nil, err
	}
	inFlight := []*quaycrewv1.Job{}
	for _, one := range listed.GetJobs() {
		if !job.Terminal(one.GetPhase()) {
			inFlight = append(inFlight, one)
		}
	}
	switch len(inFlight) {
	case 1:
		return inFlight[0], nil
	case 0:
		return nil, fmt.Errorf("no job is in flight in %s, so there is nothing to steer: name one with "+
			"krewe steer <job> \"...\", and krewe job list says what there is", located.Path)
	default:
		return nil, fmt.Errorf("%d jobs are in flight in %s, so this would count the steer against the "+
			"wrong one: name it with krewe steer <job> \"...\"", len(inFlight), located.Path)
	}
}

// runSteers reads the marks back: one job's, or every job where the operator is standing against the
// job before it.
func runSteers(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: krewe steers [<job>]")
	}
	if len(args) == 0 {
		return runSteersHere(ctx, client, out)
	}
	named, err := findJob(ctx, client, args[0])
	if err != nil {
		return err
	}
	listed, err := client.ListSteers(ctx, &quaycrewv1.ListSteersRequest{Job: named.GetId()})
	if err != nil {
		return err
	}

	root := listed.GetRoot()
	fmt.Fprintf(out, "%s  %s\n", display.ShortID(root.GetId()), root.GetTitle())
	// The moment and the job each one landed on, because the count on its own says a job was hard and
	// not which part of it kept needing a person.
	for _, one := range listed.GetSteers() {
		fmt.Fprintf(out, "%s  %-10s %s\n", one.GetOccurredAt().AsTime().Local().Format(time.RFC3339),
			display.ShortID(one.GetJob()), one.GetText())
	}
	if len(listed.GetSteers()) == 0 {
		fmt.Fprintln(out, "nobody steered this job")
	}
	fmt.Fprintf(out, "\n%s\n", job.Steers(len(listed.GetSteers())))
	// The definition, under the number, because a count is worth nothing when what it counts drifts.
	fmt.Fprintf(out, "\n%s\n", job.WhatASteerIs)
	return nil
}

// runSteersHere is every job where the operator is standing, oldest first, each against the one
// before it. It is the answer to "is this better than last time", which is the question the count
// exists for.
func runSteersHere(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, out io.Writer) error {
	located, err := locate(ctx, client, "")
	if err != nil {
		return err
	}
	listed, err := client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{
		Workspace: located.WorkspaceID, Project: located.ProjectID, RootsOnly: true,
	})
	if err != nil {
		return err
	}
	if len(listed.GetJobs()) == 0 {
		fmt.Fprintf(out, "no jobs in %s yet\n", located.Path)
		return nil
	}

	// Oldest first, which is the opposite of every other listing here and deliberate: a comparison
	// reads down the page in the order the jobs happened.
	jobs := listed.GetJobs()
	before := -1
	for i := len(jobs) - 1; i >= 0; i-- {
		one := jobs[i]
		count := int(one.GetSteers())
		// The title is held to its column rather than left whole. It is the one field here of no fixed
		// length, and a long one pushes the count and the comparison, which are what this listing is read
		// for, off to the right of every other line.
		fmt.Fprintf(out, "%-10s %-8s %-24.24s %s, %s\n", display.ShortID(one.GetId()), one.GetPhase(),
			truncateLine(one.GetTitle()), job.Steers(count), job.Compared(count, before))
		before = count
	}
	fmt.Fprintf(out, "\n%s\n", job.WhatASteerIs)
	return nil
}
