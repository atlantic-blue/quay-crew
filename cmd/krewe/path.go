package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
)

// The path a design was broken into: reading it, and writing it from a file.
//
// The tool sends the document and never parses it. One grammar, in one place, so the console and the
// command line cannot drift on what a step heading looks like.

const pathUsage = "usage: krewe path [<address>]" +
	"\n       krewe path set [<address>] --file <path>"

func runPath(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "set" {
		return runPathSet(ctx, client, args[1:], out)
	}
	if len(args) > 1 {
		return fmt.Errorf("%s", pathUsage)
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	located, err := designProject(ctx, client, typed)
	if err != nil {
		return err
	}
	resp, err := client.ListSteps(ctx, &quaycrewv1.ListStepsRequest{Project: located.ProjectID})
	if err != nil {
		return err
	}
	steps := resp.GetSteps()
	if len(steps) == 0 {
		fmt.Fprintf(out, "%s has no path yet\n\n", located.Path.Project)
		fmt.Fprintf(out, "write one: krewe path set %s %s path.md\n", typed, flagFile)
		return nil
	}
	// Number order, and it is the control plane's order rather than this tool's, so the console and
	// the command line cannot draw one path two ways.
	rows := make([][]string, 0, len(steps))
	for _, step := range steps {
		rows = append(rows, []string{
			strconv.FormatInt(int64(step.GetNumber()), 10),
			step.GetTitle(),
			step.GetState(),
			sessionOn(step),
			display.Age(step.GetTakenAt()),
		})
	}
	fmt.Fprint(out, display.Rows([]string{"STEP", "TITLE", "STATE", "SESSION", "AGE"}, rows))
	return nil
}

// sessionOn is the session holding a step, and a dash where nobody holds it. A dash rather than an
// empty cell, because an empty cell in the middle of a row reads as a column that failed to render.
func sessionOn(step *quaycrewv1.Step) string {
	if step.GetSession() == "" {
		return "-"
	}
	return display.ShortID(step.GetSession())
}

// runPathSet writes the path from a file.
//
// The file goes to the control plane whole. A refused document changes nothing, and the refusal
// names the line, so it is printed as it came rather than rewritten here.
func runPathSet(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	typed, path, err := fileAndAddressFor("krewe path set", args)
	if err != nil {
		return err
	}
	located, err := designProject(ctx, client, typed)
	if err != nil {
		return err
	}
	document, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading the path from %s: %w", path, err)
	}
	resp, err := client.SetPath(ctx, &quaycrewv1.SetPathRequest{
		Project: located.ProjectID, Document: string(document),
	})
	if err != nil {
		return fmt.Errorf("%w\n\nnothing was written", err)
	}
	steps := resp.GetSteps()
	fmt.Fprintf(out, "%s has a path of %d steps\n\n", located.Path.Project, len(steps))
	for _, step := range steps {
		fmt.Fprintf(out, "  %d. %s\n", step.GetNumber(), step.GetTitle())
	}
	sayWarnings(out, resp.GetWarnings())
	return nil
}

// fileAndAddressFor reads --file out of the arguments and hands back the address in front of it. The
// command names itself, so the usage a refusal prints is the command the operator typed.
func fileAndAddressFor(command string, args []string) (typed, path string, err error) {
	usage := fmt.Sprintf("usage: %s [<address>] %s <path>", command, flagFile)
	rest := make([]string, 0, len(args))
	for at := 0; at < len(args); at++ {
		if args[at] != flagFile {
			rest = append(rest, args[at])
			continue
		}
		if at+1 >= len(args) {
			return "", "", fmt.Errorf("%s needs a path\n\n%s", flagFile, usage)
		}
		path = args[at+1]
		at++
	}
	if path == "" || len(rest) > 1 {
		return "", "", fmt.Errorf("%s", usage)
	}
	if len(rest) == 1 {
		typed = rest[0]
	}
	return typed, path, nil
}
