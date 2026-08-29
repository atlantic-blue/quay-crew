package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-crew/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-crew/internal/display"
	"github.com/atlantic-blue/quay-crew/internal/flow"
)

// runFlow drives the automations a crew can run on its own.
//
// A graph is a file: the operator writes it, imports it, and starts runs of it. The file is read
// here and sent as text, because the control plane may be in a container where a path on the
// operator's machine means nothing.
func runFlow(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: quay flow <import|start|schedule|unschedule|list|show|stop|answer>")
	}
	switch args[0] {
	case "import":
		return runFlowImport(ctx, client, args[1:], out)
	case "start":
		return runFlowStart(ctx, client, args[1:], out)
	case "list":
		return runFlowList(ctx, client, args[1:], out)
	case "show":
		return runFlowShow(ctx, client, args[1:], out)
	case "stop":
		return runFlowStop(ctx, client, args[1:], out)
	case "answer":
		return runFlowAnswer(ctx, client, args[1:], out)
	case "schedule":
		return runFlowSchedule(ctx, client, args[1:], out)
	case "unschedule":
		return runFlowUnschedule(ctx, client, args[1:], out)
	default:
		return fmt.Errorf("usage: quay flow <import|start|schedule|unschedule|list|show|stop|answer>")
	}
}

// runFlowImport reads a graph file and stores it at the version written in it.
func runFlowImport(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: quay flow import <file>")
	}
	definition, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("read the graph: %w", err)
	}
	// Parsed here as well as on the other side, so a graph that could not run is refused before it
	// is sent anywhere. The control plane refuses it too, and that is the check that counts.
	if _, err := flow.Parse(definition); err != nil {
		return err
	}
	resp, err := client.ImportFlow(ctx, &quaycrewv1.ImportFlowRequest{Definition: string(definition)})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "imported flow %s version %d\n", resp.GetName(), resp.GetVersion())
	fmt.Fprintf(out, "start a run with quay flow start %s\n", resp.GetName())
	return nil
}

// runFlowStart begins a run in a project, and says where to watch it rather than waiting for it.
func runFlowStart(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	typed, graph, err := addressAndValue(args, "start", "<graph>")
	if err != nil {
		return err
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if located.ProjectID == "" {
		return fmt.Errorf("a flow runs in a project: quay flow start <workspace>/<project> %s", graph)
	}
	resp, err := client.StartFlow(ctx, &quaycrewv1.StartFlowRequest{
		Graph: graph, Project: located.ProjectID,
	})
	if err != nil {
		return err
	}
	run := resp.GetRun()
	fmt.Fprintf(out, "started %s version %d as run %s\n", run.GetGraphName(), run.GetGraphVersion(), display.ShortID(run.GetId()))
	fmt.Fprintf(out, "it dispatches tasks of its own; watch it with quay flow show %s\n", display.ShortID(run.GetId()))
	return nil
}

// addressAndValue reads the two shapes a command takes: a value on its own, acting where the
// operator already is, or an address and a value.
func addressAndValue(args []string, verb, what string) (typed, value string, err error) {
	switch len(args) {
	case 1:
		return "", args[0], nil
	case 2:
		return args[0], args[1], nil
	default:
		return "", "", fmt.Errorf("usage: quay flow %s [<workspace>/<project>] %s", verb, what)
	}
}

// runFlowList says what has run, newest first.
func runFlowList(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	typed := ""
	if len(args) == 1 {
		typed = args[0]
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: quay flow list [<workspace>/<project>]")
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	resp, err := client.ListFlowRuns(ctx, &quaycrewv1.ListFlowRunsRequest{Project: located.ProjectID})
	if err != nil {
		return err
	}
	if len(resp.GetRuns()) == 0 {
		fmt.Fprintf(out, "nothing has run here yet; start one with quay flow start <graph>\n")
		return nil
	}
	for _, run := range resp.GetRuns() {
		fmt.Fprintf(out, "%-10s %-24s %-10s %s\n",
			display.ShortID(run.GetId()), run.GetGraphName(), run.GetStatus(), run.GetNode())
	}
	return nil
}

// runFlowShow reads one run back: where it got to, and what it knows.
func runFlowShow(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: quay flow show <run>")
	}
	run, err := findFlowRun(ctx, client, args[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s  %s version %d\n", display.ShortID(run.GetId()), run.GetGraphName(), run.GetGraphVersion())
	fmt.Fprintf(out, "%s at node %s, %d transitions", run.GetStatus(), run.GetNode(), run.GetTransitions())
	if run.GetSpent() > 0 {
		fmt.Fprintf(out, ", %d tokens", run.GetSpent())
	}
	fmt.Fprintln(out)
	// Why it stopped, on its own line and before the state, because a run that halted and a run
	// that went quiet look identical without it.
	if run.GetReason() != "" {
		fmt.Fprintf(out, "%s\n", run.GetReason())
	}
	// A run waiting on a person is the one state the operator has to act on, so the question and
	// the way to answer it are said outright rather than left to be looked up.
	if run.GetQuestion() != "" {
		fmt.Fprintf(out, "asking: %s\n", run.GetQuestion())
		fmt.Fprintf(out, "answer it with quay flow answer %s <answer>\n", display.ShortID(run.GetId()))
	}
	keys := make([]string, 0, len(run.GetState()))
	for key := range run.GetState() {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(out, "  %-16s %s\n", key, truncateLine(run.GetState()[key]))
	}
	// Where the run sits in the job tree. A run is carried by a job and every step is
	// another under it, so this is the road to the answer of each step as a value rather than as a
	// transcript.
	if run.GetJob() != "" {
		fmt.Fprintf(out, "read its steps with quay job list --label flow.run=%s\n", run.GetId())
	}
	// What the run actually did is in its sessions, and the summary above is the model's own account
	// of it. The two can disagree, so the way to read the tasks is printed rather than left to be
	// worked out from an identifier in the state. Every session, because each step has its own.
	for _, session := range flow.SessionsIn(run.GetState()) {
		fmt.Fprintf(out, "read what it did with quay task list %s\n", display.ShortID(session))
	}
	return nil
}

// runFlowStop halts a run in flight. The reason is optional and worth giving: it is what somebody
// reading the run tomorrow has to go on.
func runFlowStop(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: quay flow stop <run> [<reason>]")
	}
	run, err := findFlowRun(ctx, client, args[0])
	if err != nil {
		return err
	}
	reason := ""
	if len(args) == 2 {
		reason = args[1]
	}
	resp, err := client.StopFlowRun(ctx, &quaycrewv1.StopFlowRunRequest{Id: run.GetId(), Reason: reason})
	if err != nil {
		return err
	}
	stopped := resp.GetRun()
	fmt.Fprintf(out, "stopped run %s at node %s\n", display.ShortID(stopped.GetId()), stopped.GetNode())
	fmt.Fprintf(out, "%s\n", stopped.GetReason())
	// The task already running finishes: the model is mid sentence and abandoning it gains nothing.
	fmt.Fprintf(out, "a task already under way finishes; the run takes no further step\n")
	return nil
}

// runFlowAnswer tells a run what the operator decided, which is the only thing that moves a run
// waiting on a person.
func runFlowAnswer(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: quay flow answer <run> <answer>")
	}
	run, err := findFlowRun(ctx, client, args[0])
	if err != nil {
		return err
	}
	resp, err := client.AnswerFlowRun(ctx, &quaycrewv1.AnswerFlowRunRequest{Id: run.GetId(), Answer: args[1]})
	if err != nil {
		return err
	}
	answered := resp.GetRun()
	fmt.Fprintf(out, "answered %s with %q\n", display.ShortID(answered.GetId()), args[1])
	fmt.Fprintf(out, "%s at node %s\n", answered.GetStatus(), answered.GetNode())
	return nil
}

// runFlowSchedule sets a graph running on its own in a project, as often as the graph itself says.
func runFlowSchedule(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	typed, graph, err := addressAndValue(args, "schedule", "<graph>")
	if err != nil {
		return err
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if located.ProjectID == "" {
		return fmt.Errorf("a flow runs in a project: quay flow schedule <workspace>/<project> %s", graph)
	}
	resp, err := client.ScheduleFlow(ctx, &quaycrewv1.ScheduleFlowRequest{
		Graph: graph, Project: located.ProjectID,
	})
	if err != nil {
		return err
	}
	every := time.Duration(resp.GetEverySeconds()) * time.Second
	fmt.Fprintf(out, "%s now runs in %s every %s\n", graph, located.Path, every)
	fmt.Fprintf(out, "the first run is one interval away; stop it with quay flow unschedule %s\n", graph)
	return nil
}

// runFlowUnschedule stops a graph running on its own.
func runFlowUnschedule(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	typed, graph, err := addressAndValue(args, "unschedule", "<graph>")
	if err != nil {
		return err
	}
	located, err := locate(ctx, client, typed)
	if err != nil {
		return err
	}
	if _, err := client.UnscheduleFlow(ctx, &quaycrewv1.UnscheduleFlowRequest{
		Graph: graph, Project: located.ProjectID,
	}); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s no longer runs on its own in %s; runs already under way are untouched\n", graph, located.Path)
	return nil
}

// findFlowRun resolves a run by its full identifier or by the short one a listing shows.
func findFlowRun(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, typed string) (*quaycrewv1.FlowRun, error) {
	if resp, err := client.GetFlowRun(ctx, &quaycrewv1.GetFlowRunRequest{Id: typed}); err == nil {
		return resp.GetRun(), nil
	}
	listed, err := client.ListFlowRuns(ctx, &quaycrewv1.ListFlowRunsRequest{})
	if err != nil {
		return nil, err
	}
	var matched []*quaycrewv1.FlowRun
	for _, run := range listed.GetRuns() {
		if strings.HasPrefix(run.GetId(), typed) {
			matched = append(matched, run)
		}
	}
	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return nil, fmt.Errorf("there is no run %s; quay flow list says what has run", typed)
	default:
		return nil, fmt.Errorf("%s could be %d runs: give more of the identifier", typed, len(matched))
	}
}

// truncateLine keeps a reply that ran to a page from taking the listing with it.
func truncateLine(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	if len(value) <= 72 {
		return value
	}
	return value[:71] + "…"
}
