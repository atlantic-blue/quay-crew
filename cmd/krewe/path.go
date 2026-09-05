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

const pathUsage = "usage: krewe path [<address>] [<feature>]" +
	"\n       krewe path set [<address>] <feature> --file <path>"

// runPath prints one feature's path, or the path of every open feature of the project.
//
// A path belongs to a feature, so the argument is a feature number. It is a bare number and not a
// token, because a feature address has one part.
//
// A closed feature is left out of the whole listing and printed when its number is named, so the
// record of finished work stays readable without filling the listing with it.
func runPath(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "set" {
		return runPathSet(ctx, client, args[1:], out)
	}
	if len(args) > 2 {
		return fmt.Errorf("%s", pathUsage)
	}
	typed, said := "", ""
	switch len(args) {
	case 1:
		// One argument is the feature number when it reads as one, and the address otherwise, which
		// is the shape krewe design brief already has.
		if _, err := strconv.Atoi(args[0]); err == nil {
			said = args[0]
		} else {
			typed = args[0]
		}
	case 2:
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
	if len(features) == 0 {
		fmt.Fprintf(out, "%s has no feature yet\n\n", located.Path.Project)
		fmt.Fprintf(out, "add one: krewe feature add %s \"...\"\n", typed)
		return nil
	}
	if said != "" {
		held, err := featureNumbered(features, said, located.Path.Project)
		if err != nil {
			return err
		}
		return printPath(ctx, client, held, located.Path.Project, typed, out)
	}
	for _, feature := range features {
		if feature.GetState() != "open" {
			continue
		}
		if err := printPath(ctx, client, feature, located.Path.Project, typed, out); err != nil {
			return err
		}
	}
	return nil
}

// printPath draws one feature's path under a heading naming the feature.
//
// The heading is there whichever way the command was called, so a listing of several features and a
// listing of one say the same thing about which path is on the screen.
func printPath(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient,
	feature *quaycrewv1.Feature, project, typed string, out io.Writer) error {
	resp, err := client.ListSteps(ctx, &quaycrewv1.ListStepsRequest{Feature: feature.GetId()})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "feature %d: %s\n", feature.GetNumber(), feature.GetTitle())
	steps := resp.GetSteps()
	if len(steps) == 0 {
		fmt.Fprintf(out, "\nthis feature has no path yet\n\n")
		fmt.Fprintf(out, "write one: krewe path set %s %d %s path.md\n\n",
			typed, feature.GetNumber(), flagFile)
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
	fmt.Fprintln(out)
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

// runPathSet writes one feature's path from a file.
//
// The file goes to the control plane whole. A refused document changes nothing, and the refusal
// names the line, so it is printed as it came rather than rewritten here.
//
// The output names the feature, so nobody reads it as the project's whole path: the other features
// of the project keep the paths they had.
func runPathSet(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	typed, said, path, err := fileFeatureAndAddressFor("krewe path set", args)
	if err != nil {
		return err
	}
	located, err := designProject(ctx, client, typed)
	if err != nil {
		return err
	}
	features, err := featuresOf(ctx, client, located.ProjectID)
	if err != nil {
		return err
	}
	held, err := featureNumbered(features, said, located.Path.Project)
	if err != nil {
		return err
	}
	document, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading the path from %s: %w", path, err)
	}
	resp, err := client.SetPath(ctx, &quaycrewv1.SetPathRequest{
		Feature: held.GetId(), Document: string(document),
	})
	if err != nil {
		return fmt.Errorf("%w\n\nnothing was written", err)
	}
	steps := resp.GetSteps()
	fmt.Fprintf(out, "feature %d of %s has a path of %d steps\n\n",
		held.GetNumber(), located.Path.Project, len(steps))
	for _, step := range steps {
		fmt.Fprintf(out, "  %d. %s\n", step.GetNumber(), step.GetTitle())
	}
	sayWarnings(out, resp.GetWarnings())
	return nil
}

// fileFeatureAndAddressFor reads --file out of the arguments and hands back the feature number and
// the address in front of it. The command names itself, so the usage a refusal prints is the command
// the operator typed.
//
// With one argument left the argument is the feature number, and with two the first is the address.
func fileFeatureAndAddressFor(command string, args []string) (typed, feature, path string, err error) {
	usage := fmt.Sprintf("usage: %s [<address>] <feature> %s <path>", command, flagFile)
	rest, path, err := fileOutOf(args, usage)
	if err != nil {
		return "", "", "", err
	}
	if path == "" || len(rest) == 0 || len(rest) > 2 {
		return "", "", "", fmt.Errorf("%s", usage)
	}
	// One argument left is the feature number when it reads as one, and the address otherwise, which
	// then leaves no feature named at all.
	feature = rest[0]
	if len(rest) == 2 {
		typed, feature = rest[0], rest[1]
	}
	if _, err := strconv.Atoi(feature); err != nil {
		return "", "", "", fmt.Errorf("%s", usage)
	}
	return typed, feature, path, nil
}

// fileAndAddressFor reads --file out of the arguments and hands back the address in front of it. The
// command names itself, so the usage a refusal prints is the command the operator typed.
func fileAndAddressFor(command string, args []string) (typed, path string, err error) {
	usage := fmt.Sprintf("usage: %s [<address>] %s <path>", command, flagFile)
	rest, path, err := fileOutOf(args, usage)
	if err != nil {
		return "", "", err
	}
	if path == "" || len(rest) > 1 {
		return "", "", fmt.Errorf("%s", usage)
	}
	if len(rest) == 1 {
		typed = rest[0]
	}
	return typed, path, nil
}

// fileOutOf takes --file and its path out of the arguments, and hands back everything else in the
// order it was typed.
func fileOutOf(args []string, usage string) (rest []string, path string, err error) {
	rest = make([]string, 0, len(args))
	for at := 0; at < len(args); at++ {
		if args[at] != flagFile {
			rest = append(rest, args[at])
			continue
		}
		if at+1 >= len(args) {
			return nil, "", fmt.Errorf("%s needs a path\n\n%s", flagFile, usage)
		}
		path = args[at+1]
		at++
	}
	return rest, path, nil
}
