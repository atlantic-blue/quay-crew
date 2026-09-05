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
// The tool composes none of the text a session is given. It sends the project and the number, and
// prints what came back, so the console and the command line ask for the same words.

const stepUsage = "usage: krewe step take [<address>] <number>"

func runStep(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "take" {
		return runStepTake(ctx, client, args[1:], out)
	}
	return fmt.Errorf("%s", stepUsage)
}

// runStepTake gives one step to a session that starts now.
//
// With one argument the argument is the number, and with two the first is the address. This is the
// shape krewe design brief already has, so an operator standing in a project types the number alone.
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
	number, err := strconv.Atoi(said)
	if err != nil {
		return fmt.Errorf("%q is not a step number\n\n%s", said, stepUsage)
	}
	located, err := designProject(ctx, client, typed)
	if err != nil {
		return err
	}
	resp, err := client.TakeStep(ctx, &quaycrewv1.TakeStepRequest{
		Project: located.ProjectID, Number: int32(number),
	})
	if err != nil {
		return fmt.Errorf("%w\n\nnothing was started", err)
	}
	step := resp.GetStep()
	fmt.Fprintf(out, "step %d of %s is taken: %s\n",
		step.GetNumber(), located.Path.Project, step.GetTitle())
	fmt.Fprintf(out, "(session %s, handle %s)\n\n",
		resp.GetSession().GetId(), resp.GetSession().GetHandle())
	fmt.Fprintf(out, "it was asked to:\n\n%s\n", strings.TrimRight(resp.GetText(), "\n"))
	sayWarnings(out, resp.GetWarnings())
	return nil
}
