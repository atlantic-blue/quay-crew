package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/contextsize"
	"github.com/atlantic-blue/quay-krewe/internal/sandbox"
	"github.com/atlantic-blue/quay-krewe/internal/workspace"
)

// flagFile names the file a design body is read from. A design is a document, so it comes from a
// file rather than from an argument: a shell mangles a page of markdown, and standard input is
// already how a context level is written, which would make the two commands look alike and behave
// differently.
const flagFile = "--file"

const designUsage = "usage: krewe design [<address>]" +
	"\n       krewe design brief [<address>] \"<text>\"" +
	"\n       krewe design set [<address>] --file <path>" +
	"\n       krewe design approve [<address>]"

func runDesign(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "brief" {
		return runDesignBrief(ctx, client, args[1:], out)
	}
	if len(args) > 0 && args[0] == "set" {
		return runDesignSet(ctx, client, args[1:], out)
	}
	if len(args) > 0 && args[0] == "approve" {
		return runDesignApprove(ctx, client, args[1:], out)
	}
	if len(args) > 1 {
		return fmt.Errorf("%s", designUsage)
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	located, err := designProject(ctx, client, typed)
	if err != nil {
		return err
	}
	resp, err := client.GetDesign(ctx, &quaycrewv1.GetDesignRequest{Project: located.ProjectID})
	if err != nil {
		return err
	}
	design := resp.GetDesign()
	if design.GetBrief() == "" && design.GetBody() == "" {
		fmt.Fprintf(out, "%s has no design yet\n\n", located.Path.Project)
		fmt.Fprintf(out, "say what it is for: krewe design brief %s \"...\"\n", typed)
		fmt.Fprintf(out, "write the design:   krewe design set %s %s design.md\n", typed, flagFile)
		return nil
	}
	// The brief, then the approval, then the body, in that order, because the first two are one line
	// each and the body is a document. The body prints last and whole, so this can be piped.
	if design.GetBrief() != "" {
		fmt.Fprintf(out, "brief: %s\n", design.GetBrief())
	}
	fmt.Fprintf(out, "approval: %s\n", approvalOf(design))
	if design.GetBody() == "" {
		fmt.Fprintf(out, "\nno design body yet: krewe design set %s %s design.md\n", typed, flagFile)
		return nil
	}
	fmt.Fprintf(out, "\n%s\n", strings.TrimRight(design.GetBody(), "\n"))
	return nil
}

// approvalOf says where the design stands with the operator.
func approvalOf(design *quaycrewv1.Design) string {
	if !design.GetApproved() {
		return "not approved"
	}
	return "approved " + design.GetApprovedAt().AsTime().Format("2006-01-02 15:04")
}

// runDesignBrief records what a project is for.
//
// With one argument the argument is the text, and with two the first is the address. This is the
// shape krewe exec already has, so an operator standing in a project types the text alone.
func runDesignBrief(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 || len(args) > 2 {
		return fmt.Errorf("usage: krewe design brief [<address>] \"<text>\"")
	}
	typed, text := "", args[0]
	if len(args) == 2 {
		typed, text = args[0], args[1]
	}
	located, err := designProject(ctx, client, typed)
	if err != nil {
		return err
	}
	resp, err := client.SetBrief(ctx, &quaycrewv1.SetBriefRequest{
		Project: located.ProjectID, Brief: text,
	})
	if err != nil {
		return err
	}
	if text == "" {
		fmt.Fprintf(out, "%s says nothing about what it is for\n", located.Path.Project)
		return nil
	}
	fmt.Fprintf(out, "%s is for: %s (%s)\n",
		located.Path.Project, text, contextsize.Characters(len(text)))
	sayWarnings(out, resp.GetWarnings())
	return nil
}

// runDesignSet writes the design document.
//
// written_by comes from the environment rather than from a flag. The variable exists only inside a
// sandbox, so a session records itself and the operator records nobody, and neither has to remember
// to say which they are.
func runDesignSet(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	typed, path, err := fileAndAddress(args)
	if err != nil {
		return err
	}
	located, err := designProject(ctx, client, typed)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading the design from %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return fmt.Errorf("%s is empty, and an empty design is not a design", path)
	}
	resp, err := client.SetDesign(ctx, &quaycrewv1.SetDesignRequest{
		Project: located.ProjectID, Body: string(body), WrittenBy: os.Getenv(sandbox.SessionIDEnv),
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s has a design: %s\n",
		located.Path.Project, contextsize.Characters(len(body)))
	// Said every time, whether or not the design was approved before this write. A person who reads
	// it twice learns the rule, which is that approval is a statement about one text.
	fmt.Fprintln(out, "the approval is cleared: a design that changed is a design nobody has agreed to yet")
	sayWarnings(out, resp.GetWarnings())
	return nil
}

// runDesignApprove records the operator's word on the design as it stands.
//
// It takes no text and no flag. The approval is about the body the project holds when the command
// lands, so the only thing to say is which project.
//
// This is the operator's command and a session cannot make it: the control plane refuses the call to
// a driver's token. What the approval buys is stated back, because a person who reads it learns the
// rule rather than finding it out later at a refusal.
func runDesignApprove(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) > 1 {
		return fmt.Errorf("usage: krewe design approve [<address>]")
	}
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	located, err := designProject(ctx, client, typed)
	if err != nil {
		return err
	}
	resp, err := client.ApproveDesign(ctx, &quaycrewv1.ApproveDesignRequest{Project: located.ProjectID})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s: %s\n", located.Path.Project, approvalOf(resp.GetDesign()))
	fmt.Fprintln(out, "work may be built from it until somebody writes the design again")
	return nil
}

// fileAndAddress reads --file out of the arguments and hands back the address in front of it.
func fileAndAddress(args []string) (typed, path string, err error) {
	rest := make([]string, 0, len(args))
	for at := 0; at < len(args); at++ {
		if args[at] != flagFile {
			rest = append(rest, args[at])
			continue
		}
		if at+1 >= len(args) {
			return "", "", fmt.Errorf("%s needs a path\n\nusage: krewe design set [<address>] %s <path>",
				flagFile, flagFile)
		}
		path = args[at+1]
		at++
	}
	if path == "" {
		return "", "", fmt.Errorf("usage: krewe design set [<address>] %s <path>", flagFile)
	}
	if len(rest) > 1 {
		return "", "", fmt.Errorf("usage: krewe design set [<address>] %s <path>", flagFile)
	}
	if len(rest) == 1 {
		typed = rest[0]
	}
	return typed, path, nil
}

// designProject resolves the address a design command is about, and refuses one that names no
// project: a design belongs to a project, and a workspace is not one.
func designProject(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string) (workspace.Location, error) {
	located, err := locate(ctx, client, typed)
	if err != nil {
		return workspace.Location{}, err
	}
	if !located.HasProject() {
		return workspace.Location{}, needsAProject(ctx, client, located)
	}
	return located, nil
}

// sayWarnings prints what the control plane warned about, and nothing when it warned about nothing.
func sayWarnings(out io.Writer, warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintf(out, "\n%s\n", warning)
	}
}
