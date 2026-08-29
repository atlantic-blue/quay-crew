package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// runJob drives the job the crew keeps: declared intent, read back as data.
//
// A job is a row rather than a command that runs now. Declaring it records what should
// happen; nothing here dispatches anything.
func runJob(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: quay job <create|list|show|stop>")
	}
	switch args[0] {
	case "create":
		return runJobCreate(ctx, client, args[1:], out)
	case "list":
		return runJobList(ctx, client, args[1:], out)
	case "show":
		return runJobShow(ctx, client, args[1:], out)
	case "stop":
		return runJobStop(ctx, client, args[1:], out)
	default:
		return fmt.Errorf("there is no job %s command: quay job <create|list|show|stop>", args[0])
	}
}

// jobFlags are the values a declaration carries. They are flags rather than positions because a
// declaration has ten of them and nobody can remember an order that long.
const (
	flagTitle          = "--title"
	flagBrief          = "--brief"
	flagRole           = "--role"
	flagMode           = "--mode"
	flagExpectFile     = "--expect-file"
	flagExpectContains = "--expect-contains"
	flagAfter          = "--after"
	flagDeadline       = "--deadline"
	flagBudgetTokens   = "--budget-tokens"
	flagLabel          = "--label"
	flagRequires       = "--requires"
	flagParent         = "--parent"
	flagPhase          = "--phase"
	flagRoots          = "--roots"
)

// runJobCreate declares a job.
func runJobCreate(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	values, rest, err := readFlags(args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return fmt.Errorf("usage: quay job create [<workspace>/<project>] %s \"...\" %s \"...\"", flagTitle, flagBrief)
	}
	// The parent is refused here rather than sent, in the words the crew refuses it with, because a
	// caller that could set its own parent could set its own depth.
	if values.first(flagParent) != "" {
		return fmt.Errorf("%s is not yours to set: the parent comes from the credential the caller presented, "+
			"so a job you declare is a root and a job a session declares is its child", flagParent)
	}
	typed := ""
	if len(rest) == 1 {
		typed = rest[0]
	}
	project, at, err := whereTheJobRuns(ctx, client, typed)
	if err != nil {
		return err
	}

	request := &quaycrewv1.CreateJobRequest{
		Project:        project,
		Title:          values.first(flagTitle),
		Brief:          values.first(flagBrief),
		Role:           values.first(flagRole),
		Mode:           values.first(flagMode),
		ExpectFile:     values.first(flagExpectFile),
		ExpectContains: values.first(flagExpectContains),
		After:          values[flagAfter],
		Requires:       values[flagRequires],
	}
	if labels, err := readLabels(values[flagLabel]); err != nil {
		return err
	} else if len(labels) > 0 {
		request.Labels = labels
	}
	if budget := values.first(flagBudgetTokens); budget != "" {
		tokens, err := strconv.ParseInt(budget, 10, 64)
		if err != nil {
			return fmt.Errorf("%s takes a number of tokens, and %q is not one", flagBudgetTokens, budget)
		}
		request.BudgetTokens = tokens
	}
	if deadline := values.first(flagDeadline); deadline != "" {
		at, err := time.Parse(time.RFC3339, deadline)
		if err != nil {
			return fmt.Errorf("%s takes a moment written as 2026-08-27T15:04:05Z, and %q is not one", flagDeadline, deadline)
		}
		request.Deadline = timestamppb.New(at)
	}

	resp, err := client.CreateJob(ctx, request)
	if err != nil {
		return err
	}
	declared := resp.GetJob()
	fmt.Fprintf(out, "declared %s%s\n", display.ShortID(declared.GetId()), inAddress(at))
	fmt.Fprintf(out, "%s. A controller picks it up and runs it; read the answer with quay job show %s\n",
		declared.GetPhase(), display.ShortID(declared.GetId()))
	return nil
}

// whereTheJobRuns is the project to declare in, and empty when the crew is to read it from the
// credential the caller presented.
//
// A session running a job is standing nowhere and types no address. It cannot resolve one either:
// resolving an address means listing workspaces and projects, and a role grants the four job verbs
// and nothing else. What it does hold is a credential minted for the job it is running, and that
// credential already says which project that job is in, so the crew reads it there. The same place
// the parent comes from.
//
// An operator standing somewhere is unchanged, and an operator standing in a workspace with no
// project is still told what is missing rather than having the job land somewhere unexpected.
func whereTheJobRuns(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string) (string, string, error) {
	if typed == "" {
		standing, err := currentPath()
		if err != nil {
			return "", "", err
		}
		if standing.IsZero() {
			return "", "", nil
		}
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return "", "", err
	}
	if located.ProjectID == "" {
		return "", "", fmt.Errorf("a job runs in a project: quay job create <workspace>/<project> %s \"...\" %s \"...\"",
			flagTitle, flagBrief)
	}
	return located.ProjectID, located.Path.String(), nil
}

// inAddress is the address to say a job was declared in, and nothing when nobody named one: a
// session declares in the project its credential names, and it has no address to print.
func inAddress(at string) string {
	if strings.TrimSpace(at) == "" {
		return ""
	}
	return " in " + at
}

// runJobList says what a project holds, newest first.
func runJobList(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	values, rest, err := readFlags(args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return fmt.Errorf("usage: quay job list [<workspace>/<project>] [%s <phase>] [%s <key>=<value>]", flagPhase, flagLabel)
	}
	typed := ""
	if len(rest) == 1 {
		typed = rest[0]
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}

	request := &quaycrewv1.ListJobsRequest{
		Workspace: located.WorkspaceID, Project: located.ProjectID,
		Parent: values.first(flagParent), RootsOnly: values.has(flagRoots), Phase: values.first(flagPhase),
	}
	labels, err := readLabels(values[flagLabel])
	if err != nil {
		return err
	}
	for key, value := range labels {
		request.LabelKey, request.LabelValue = key, value
		break
	}
	if len(labels) > 1 {
		return fmt.Errorf("a listing narrows by one label: give %s once", flagLabel)
	}

	resp, err := client.ListJobs(ctx, request)
	if err != nil {
		return err
	}
	if len(resp.GetJobs()) == 0 {
		fmt.Fprintf(out, "no jobs here yet; declare one with quay job create %s \"...\" %s \"...\"\n", flagTitle, flagBrief)
		return nil
	}
	for _, one := range resp.GetJobs() {
		fmt.Fprintf(out, "%-10s %-2d %-8s %s\n",
			display.ShortID(one.GetId()), one.GetDepth(), one.GetPhase(), truncateLine(one.GetTitle()))
	}
	return nil
}

// runJobShow reads one job back: what it is, where it got to, and what came of it.
func runJobShow(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: quay job show <job>")
	}
	one, err := findJob(ctx, client, args[0])
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "%s  %s\n", display.ShortID(one.GetId()), one.GetTitle())
	fmt.Fprintf(out, "%s", one.GetPhase())
	if one.GetRole() != "" {
		fmt.Fprintf(out, ", as %s version %d", one.GetRole(), one.GetRoleVersion())
	}
	if one.GetDepth() > 0 {
		fmt.Fprintf(out, ", at depth %d under %s", one.GetDepth(), display.ShortID(one.GetParent()))
	}
	if one.GetSpentTokens() > 0 {
		fmt.Fprintf(out, ", %d tokens", one.GetSpentTokens())
	}
	fmt.Fprintln(out)
	// Why it stopped, before anything else, because a job that halted and a job that went quiet read
	// the same without it.
	if one.GetReason() != "" {
		fmt.Fprintf(out, "%s\n", one.GetReason())
	}
	if one.GetQuestion() != "" {
		fmt.Fprintf(out, "asking: %s\n", one.GetQuestion())
	}
	for _, waits := range one.GetAfter() {
		fmt.Fprintf(out, "waits for %s\n", display.ShortID(waits))
	}
	if required := one.GetRequires(); len(required) > 0 {
		fmt.Fprintf(out, "requires %s\n", strings.Join(required, ", "))
	}
	if one.GetBudgetTokens() > 0 {
		fmt.Fprintf(out, "budget %d tokens\n", one.GetBudgetTokens())
	}
	if one.GetDeadline() != nil {
		fmt.Fprintf(out, "deadline %s\n", one.GetDeadline().AsTime().Local().Format(time.RFC3339))
	}
	for _, key := range sortedKeys(one.GetLabels()) {
		fmt.Fprintf(out, "label %s=%s\n", key, one.GetLabels()[key])
	}
	fmt.Fprintf(out, "\n%s\n", one.GetBrief())
	// The answer last and whole, because it is what a caller came for.
	if one.GetAnswer() != "" {
		fmt.Fprintf(out, "\nanswer:\n%s\n", one.GetAnswer())
	}
	if one.GetSession() != "" {
		fmt.Fprintf(out, "\nread what it did with quay task list %s\n", display.ShortID(one.GetSession()))
	}
	return nil
}

// runJobStop halts job that has not ended. The reason is what somebody reading it tomorrow has.
func runJobStop(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: quay job stop <job> [<reason>]")
	}
	one, err := findJob(ctx, client, args[0])
	if err != nil {
		return err
	}
	reason := ""
	if len(args) == 2 {
		reason = args[1]
	}
	resp, err := client.StopJob(ctx, &quaycrewv1.StopJobRequest{Id: one.GetId(), Reason: reason})
	if err != nil {
		return err
	}
	stopped := resp.GetJob()
	fmt.Fprintf(out, "stopped %s\n", display.ShortID(stopped.GetId()))
	fmt.Fprintf(out, "%s\n", stopped.GetReason())
	return nil
}

// findJob resolves job by its full identifier or by the short one a listing prints.
func findJob(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string) (*quaycrewv1.Job, error) {
	if resp, err := client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: typed}); err == nil {
		return resp.GetJob(), nil
	}
	listed, err := client.ListJobs(ctx, &quaycrewv1.ListJobsRequest{})
	if err != nil {
		return nil, err
	}
	var matched []*quaycrewv1.Job
	for _, one := range listed.GetJobs() {
		if strings.HasPrefix(one.GetId(), typed) {
			matched = append(matched, one)
		}
	}
	switch len(matched) {
	case 1:
		// Read again in full, because a listing leaves the answer out and this is the call that
		// carries it.
		resp, err := client.GetJob(ctx, &quaycrewv1.GetJobRequest{Id: matched[0].GetId()})
		if err != nil {
			return nil, err
		}
		return resp.GetJob(), nil
	case 0:
		return nil, fmt.Errorf("there is no job %s; quay job list says what there is", typed)
	default:
		return nil, fmt.Errorf("%s could be %d jobs: give more of the identifier", typed, len(matched))
	}
}

// given is what an invocation gave for each flag, in the order it gave them, so a flag that may be
// repeated keeps its order and one that may not is read with first.
type given map[string][]string

func (g given) first(name string) string {
	if len(g[name]) == 0 {
		return ""
	}
	return strings.TrimSpace(g[name][0])
}

func (g given) has(name string) bool { return len(g[name]) > 0 }

// readFlags separates the values from the words. This tool takes no flags anywhere else, so the
// parsing is here rather than in a package: `--name value` and `--name=value`, and a flag with no
// value is refused by name rather than swallowing the word after it.
func readFlags(args []string) (given, []string, error) {
	values, rest := given{}, []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			rest = append(rest, arg)
			continue
		}
		name, value, joined := strings.Cut(arg, "=")
		if joined {
			values[name] = append(values[name], value)
			continue
		}
		// The one flag that carries no value. Everything else takes the word after it, and a flag
		// at the end of the line with nothing after it is a value the caller thinks they gave.
		if name == flagRoots {
			values[name] = append(values[name], "")
			continue
		}
		if i+1 >= len(args) {
			return nil, nil, fmt.Errorf("%s was given nothing: write %s <value>", name, name)
		}
		i++
		values[name] = append(values[name], args[i])
	}
	return values, rest, nil
}

// readLabels turns key=value pairs into the map the crew keeps.
func readLabels(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	labels := make(map[string]string, len(pairs))
	for _, pair := range pairs {
		key, value, joined := strings.Cut(pair, "=")
		if !joined || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("a label is written key=value, and %q is not: for example %s owner=julian", pair, flagLabel)
		}
		labels[strings.TrimSpace(key)] = value
	}
	return labels, nil
}

func sortedKeys(labels map[string]string) []string {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// jobFlagsTaken is every flag the job commands take, which is what keeps the tool's refusal of
// flags from refusing these.
func jobFlagsTaken() map[string]bool {
	taken := map[string]bool{}
	for _, name := range []string{
		flagTitle, flagBrief, flagRole, flagMode, flagExpectFile, flagExpectContains,
		flagAfter, flagDeadline, flagBudgetTokens, flagLabel, flagRequires, flagPhase, flagRoots,
		// Taken so it can be refused with the sentence that says where a parent comes from,
		// rather than with the tool's general refusal of flags.
		flagParent,
	} {
		taken[name] = true
	}
	return taken
}
