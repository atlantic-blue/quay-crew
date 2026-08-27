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

// runWork drives the work the crew keeps: declared intent, read back as data.
//
// A piece of work is a row rather than a command that runs now. Declaring it records what should
// happen; nothing here dispatches anything.
func runWork(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: quay work <create|list|show|stop>")
	}
	switch args[0] {
	case "create":
		return runWorkCreate(ctx, client, args[1:], out)
	case "list":
		return runWorkList(ctx, client, args[1:], out)
	case "show":
		return runWorkShow(ctx, client, args[1:], out)
	case "stop":
		return runWorkStop(ctx, client, args[1:], out)
	default:
		return fmt.Errorf("there is no work %s command: quay work <create|list|show|stop>", args[0])
	}
}

// workFlags are the values a declaration carries. They are flags rather than positions because a
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
	flagParent         = "--parent"
	flagPhase          = "--phase"
	flagRoots          = "--roots"
)

// runWorkCreate declares a piece of work.
func runWorkCreate(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	values, rest, err := readFlags(args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return fmt.Errorf("usage: quay work create [<workspace>/<project>] %s \"...\" %s \"...\"", flagTitle, flagBrief)
	}
	// The parent is refused here rather than sent, in the words the crew refuses it with, because a
	// caller that could set its own parent could set its own depth.
	if values.first(flagParent) != "" {
		return fmt.Errorf("%s is not yours to set: the parent comes from the credential the caller presented, "+
			"so work you declare is a root and work a session declares is its child", flagParent)
	}
	typed := ""
	if len(rest) == 1 {
		typed = rest[0]
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if located.ProjectID == "" {
		return fmt.Errorf("work runs in a project: quay work create <workspace>/<project> %s \"...\" %s \"...\"",
			flagTitle, flagBrief)
	}

	request := &quaycrewv1.CreateWorkRequest{
		Project:        located.ProjectID,
		Title:          values.first(flagTitle),
		Brief:          values.first(flagBrief),
		Role:           values.first(flagRole),
		Mode:           values.first(flagMode),
		ExpectFile:     values.first(flagExpectFile),
		ExpectContains: values.first(flagExpectContains),
		After:          values[flagAfter],
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

	resp, err := client.CreateWork(ctx, request)
	if err != nil {
		return err
	}
	declared := resp.GetWork()
	fmt.Fprintf(out, "declared %s in %s\n", display.ShortID(declared.GetId()), located.Path)
	fmt.Fprintf(out, "%s, and nothing runs it yet: read it back with quay work show %s\n",
		declared.GetPhase(), display.ShortID(declared.GetId()))
	return nil
}

// runWorkList says what a project holds, newest first.
func runWorkList(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	values, rest, err := readFlags(args)
	if err != nil {
		return err
	}
	if len(rest) > 1 {
		return fmt.Errorf("usage: quay work list [<workspace>/<project>] [%s <phase>] [%s <key>=<value>]", flagPhase, flagLabel)
	}
	typed := ""
	if len(rest) == 1 {
		typed = rest[0]
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}

	request := &quaycrewv1.ListWorkRequest{
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

	resp, err := client.ListWork(ctx, request)
	if err != nil {
		return err
	}
	if len(resp.GetWork()) == 0 {
		fmt.Fprintf(out, "no work here yet; declare some with quay work create %s \"...\" %s \"...\"\n", flagTitle, flagBrief)
		return nil
	}
	for _, one := range resp.GetWork() {
		fmt.Fprintf(out, "%-10s %-2d %-8s %s\n",
			display.ShortID(one.GetId()), one.GetDepth(), one.GetPhase(), truncateLine(one.GetTitle()))
	}
	return nil
}

// runWorkShow reads one piece of work back: what it is, where it got to, and what came of it.
func runWorkShow(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: quay work show <work>")
	}
	one, err := findWork(ctx, client, args[0])
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
	// Why it stopped, before anything else, because work that halted and work that went quiet read
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
		fmt.Fprintf(out, "\nread what it did with quay tasks %s\n", display.ShortID(one.GetSession()))
	}
	return nil
}

// runWorkStop halts work that has not ended. The reason is what somebody reading it tomorrow has.
func runWorkStop(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: quay work stop <work> [<reason>]")
	}
	one, err := findWork(ctx, client, args[0])
	if err != nil {
		return err
	}
	reason := ""
	if len(args) == 2 {
		reason = args[1]
	}
	resp, err := client.StopWork(ctx, &quaycrewv1.StopWorkRequest{Id: one.GetId(), Reason: reason})
	if err != nil {
		return err
	}
	stopped := resp.GetWork()
	fmt.Fprintf(out, "stopped %s\n", display.ShortID(stopped.GetId()))
	fmt.Fprintf(out, "%s\n", stopped.GetReason())
	return nil
}

// findWork resolves work by its full identifier or by the short one a listing prints.
func findWork(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string) (*quaycrewv1.Work, error) {
	if resp, err := client.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: typed}); err == nil {
		return resp.GetWork(), nil
	}
	listed, err := client.ListWork(ctx, &quaycrewv1.ListWorkRequest{})
	if err != nil {
		return nil, err
	}
	var matched []*quaycrewv1.Work
	for _, one := range listed.GetWork() {
		if strings.HasPrefix(one.GetId(), typed) {
			matched = append(matched, one)
		}
	}
	switch len(matched) {
	case 1:
		// Read again in full, because a listing leaves the answer out and this is the call that
		// carries it.
		resp, err := client.GetWork(ctx, &quaycrewv1.GetWorkRequest{Id: matched[0].GetId()})
		if err != nil {
			return nil, err
		}
		return resp.GetWork(), nil
	case 0:
		return nil, fmt.Errorf("there is no work %s; quay work list says what there is", typed)
	default:
		return nil, fmt.Errorf("%s could be %d pieces of work: give more of the identifier", typed, len(matched))
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

// workFlagsTaken is every flag the work commands take, which is what keeps the tool's refusal of
// flags from refusing these.
func workFlagsTaken() map[string]bool {
	taken := map[string]bool{}
	for _, name := range []string{
		flagTitle, flagBrief, flagRole, flagMode, flagExpectFile, flagExpectContains,
		flagAfter, flagDeadline, flagBudgetTokens, flagLabel, flagPhase, flagRoots,
		// Taken so it can be refused with the sentence that says where a parent comes from,
		// rather than with the tool's general refusal of flags.
		flagParent,
	} {
		taken[name] = true
	}
	return taken
}
