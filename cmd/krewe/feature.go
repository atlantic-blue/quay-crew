package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/contextsize"
	"github.com/atlantic-blue/quay-krewe/internal/display"
)

// The narrowed parts of a project: reading them, adding one, and saying what one narrows to.
//
// A project delivers several features at the same time. Authentication ships sign up, then sign in,
// then reset. Payment ships checkout, then refunds. Two paths, and neither waits for the other.
//
// The operator never chooses a number. The server gives it, in the write itself, so two adds at one
// moment cannot take the same one.

const featureUsage = "usage: krewe feature [<address>]" +
	"\n       krewe feature add [<address>] \"<title>\"" +
	"\n       krewe feature intention [<address>] <feature> \"<text>\""

func runFeature(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "add" {
		return runFeatureAdd(ctx, client, args[1:], out)
	}
	if len(args) > 0 && args[0] == "intention" {
		return runFeatureIntention(ctx, client, args[1:], out)
	}
	if len(args) > 1 {
		return fmt.Errorf("%s", featureUsage)
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
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
	// Number order, and it is the control plane's order rather than this tool's, so the console and
	// the command line cannot draw one project two ways.
	rows := make([][]string, 0, len(features))
	for _, feature := range features {
		steps, err := theStepsUnder(ctx, client, feature)
		if err != nil {
			return err
		}
		rows = append(rows, []string{
			strconv.FormatInt(int64(feature.GetNumber()), 10),
			feature.GetTitle(),
			feature.GetState(),
			stepsDone(steps),
			feature.GetIntention(),
		})
	}
	fmt.Fprint(out, display.Rows([]string{"FEATURE", "TITLE", "STATE", "STEPS", "INTENTION"}, rows))
	return nil
}

// theStepsUnder is the path of one feature, which the count of steps done is taken from.
//
// The count is the feature's own steps and never the project's, which would print one project wide
// number against every feature of it and say nothing about any of them.
func theStepsUnder(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient,
	feature *quaycrewv1.Feature) ([]*quaycrewv1.Step, error) {
	resp, err := client.ListSteps(ctx, &quaycrewv1.ListStepsRequest{Feature: feature.GetId()})
	if err != nil {
		return nil, err
	}
	return resp.GetSteps(), nil
}

// stepsDone is how far a feature got: the steps that finished, out of the steps it holds.
func stepsDone(steps []*quaycrewv1.Step) string {
	done := 0
	for _, step := range steps {
		if step.GetState() == "done" {
			done++
		}
	}
	return fmt.Sprintf("%d/%d", done, len(steps))
}

// runFeatureAdd gives the project one more narrowed part of itself.
//
// With one argument the argument is the title, and with two the first is the address. This is the
// shape krewe design brief already has, so an operator standing in a project types the title alone.
func runFeatureAdd(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 || len(args) > 2 {
		return fmt.Errorf("usage: krewe feature add [<address>] \"<title>\"")
	}
	typed, title := "", args[0]
	if len(args) == 2 {
		typed, title = args[0], args[1]
	}
	located, err := designProject(ctx, client, typed)
	if err != nil {
		return err
	}
	resp, err := client.AddFeature(ctx, &quaycrewv1.AddFeatureRequest{
		Project: located.ProjectID, Title: title,
	})
	if err != nil {
		return err
	}
	feature := resp.GetFeature()
	fmt.Fprintf(out, "%s has feature %d: %s\n\n", located.Path.Project, feature.GetNumber(), feature.GetTitle())
	fmt.Fprintf(out, "say what it narrows to: krewe feature intention %s %d \"...\"\n",
		typed, feature.GetNumber())
	return nil
}

// runFeatureIntention says which part of the project one feature narrows to.
//
// The number is resolved here rather than sent, because the call takes the feature's identifier and
// the number is what a person types. A number that names no feature is refused with the numbers that
// do exist, so the operator reads the answer rather than going to look for it.
func runFeatureIntention(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	usage := "usage: krewe feature intention [<address>] <feature> \"<text>\""
	if len(args) < 2 || len(args) > 3 {
		return fmt.Errorf("%s", usage)
	}
	typed, number, text := "", args[0], args[1]
	if len(args) == 3 {
		typed, number, text = args[0], args[1], args[2]
	}
	located, err := designProject(ctx, client, typed)
	if err != nil {
		return err
	}
	features, err := featuresOf(ctx, client, located.ProjectID)
	if err != nil {
		return err
	}
	held, err := featureNumbered(features, number, located.Path.Project)
	if err != nil {
		return err
	}
	resp, err := client.SetFeatureIntention(ctx, &quaycrewv1.SetFeatureIntentionRequest{
		Feature: held.GetId(), Intention: text,
	})
	if err != nil {
		return err
	}
	if text == "" {
		fmt.Fprintf(out, "feature %d of %s says nothing about what it narrows to\n",
			held.GetNumber(), located.Path.Project)
		return nil
	}
	fmt.Fprintf(out, "feature %d of %s narrows to: %s (%s)\n", held.GetNumber(), located.Path.Project,
		text, contextsize.Characters(len(text)))
	sayWarnings(out, resp.GetWarnings())
	return nil
}

// featuresOf is a project's features, in the order the control plane answered with.
func featuresOf(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, project string) ([]*quaycrewv1.Feature, error) {
	resp, err := client.ListFeatures(ctx, &quaycrewv1.ListFeaturesRequest{Project: project})
	if err != nil {
		return nil, err
	}
	return resp.GetFeatures(), nil
}

// featureNumbered is the feature a person typed the number of.
//
// A number nobody wrote and a number one past the end read the same to whoever typed it, so the
// refusal names the numbers that exist rather than saying the number is wrong.
func featureNumbered(features []*quaycrewv1.Feature, typed, project string) (*quaycrewv1.Feature, error) {
	number, err := strconv.Atoi(typed)
	if err != nil {
		return nil, fmt.Errorf("%q is not a feature number, and a feature is numbered from one", typed)
	}
	for _, feature := range features {
		if feature.GetNumber() == int32(number) {
			return feature, nil
		}
	}
	if len(features) == 0 {
		return nil, fmt.Errorf("%s has no feature %d, because it has no feature yet: add one with krewe feature add",
			project, number)
	}
	held := make([]string, 0, len(features))
	for _, feature := range features {
		held = append(held, strconv.FormatInt(int64(feature.GetNumber()), 10))
	}
	return nil, fmt.Errorf("%s has no feature %d: it has %s", project, number, strings.Join(held, ", "))
}
