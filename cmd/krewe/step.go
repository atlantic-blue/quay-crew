package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
)

// The steps of a path, one at a time. Taking one starts a session on it.
//
// The tool composes none of the text a session is given. It sends the feature and the number, and
// prints what came back, so the console and the command line ask for the same words.

const stepUsage = "usage: krewe step take [<address>] <feature>.<number>"

func runStep(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "take" {
		return runStepTake(ctx, client, args[1:], out)
	}
	return fmt.Errorf("%s", stepUsage)
}

// runStepTake gives one step to a session that starts now.
//
// With one argument the argument is the step, and with two the first is the address. This is the
// shape krewe design brief already has, so an operator standing in a project types the step alone.
//
// It lets go of the exec, so closing the terminal does not take the work with it. The output names
// the session, and attaching is a separate command whenever the operator wants it.
func runStepTake(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 || len(args) > 2 {
		return fmt.Errorf("%s", stepUsage)
	}
	typed, said := "", args[0]
	if len(args) == 2 {
		typed, said = args[0], args[1]
	}
	located, err := designProject(ctx, client, typed)
	if err != nil {
		return err
	}
	features, err := featuresOf(ctx, client, located.ProjectID)
	if err != nil {
		return err
	}
	held, number, err := stepAddressed(said, features, located.Path.Project)
	if err != nil {
		return err
	}
	resp, err := client.TakeStep(ctx, &quaycrewv1.TakeStepRequest{
		Feature: held.GetId(), Number: number,
	})
	if err != nil {
		return fmt.Errorf("%w\n\nnothing was started", err)
	}
	step := resp.GetStep()
	fmt.Fprintf(out, "step %d.%d of %s is taken: %s\n",
		held.GetNumber(), step.GetNumber(), located.Path.Project, step.GetTitle())
	fmt.Fprintf(out, "(session %s, handle %s)\n\n",
		resp.GetSession().GetId(), resp.GetSession().GetHandle())
	fmt.Fprintf(out, "it was asked to:\n\n%s\n", strings.TrimRight(resp.GetText(), "\n"))
	sayWarnings(out, resp.GetWarnings())
	return nil
}

// stepAddressed reads a step token, `<feature>.<number>`, into the feature it names and the number
// inside that feature's path.
//
// A bare number was a whole step address before a path belonged to a feature, so it is in somebody's
// notes and in their shell history. It names nothing now and it says so, rather than being guessed
// at, and it says so even when the project holds exactly one feature: a guess that is right today is
// wrong the moment a second feature is added, and it would be wrong silently.
func stepAddressed(said string, features []*quaycrewv1.Feature, project string) (*quaycrewv1.Feature, int32, error) {
	before, after, found := strings.Cut(said, ".")
	if !found {
		return nil, 0, fmt.Errorf("name a step as <feature>.<number>, for example 2.3\n\n%s",
			whatIsOpen(features, project))
	}
	number, err := strconv.Atoi(after)
	if err != nil {
		return nil, 0, fmt.Errorf("%q is not a step: the number after the full stop reads %q", said, after)
	}
	feature, err := featureNumbered(features, before, project)
	if err != nil {
		return nil, 0, err
	}
	return feature, int32(number), nil
}

// whatIsOpen lists the project's open features with their numbers, which is what somebody holding
// the old form needs in order to type the new one.
func whatIsOpen(features []*quaycrewv1.Feature, project string) string {
	open := make([]string, 0, len(features))
	for _, feature := range features {
		if feature.GetState() == "open" {
			open = append(open, fmt.Sprintf("  %d. %s", feature.GetNumber(), feature.GetTitle()))
		}
	}
	if len(open) == 0 {
		return fmt.Sprintf("%s has no open feature: add one with krewe feature add", project)
	}
	return fmt.Sprintf("%s has these open features:\n%s", project, strings.Join(open, "\n"))
}
