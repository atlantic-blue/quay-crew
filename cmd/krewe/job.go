package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/display"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/workspace"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// runJob drives the job the system keeps: declared intent, read back as data.
//
// A job is a row rather than a command that runs now. Declaring it records what should
// happen; nothing here dispatches anything.
func runJob(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: krewe job <create|list|show|stop|ask|answer>")
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
	case "ask":
		return runJobAsk(ctx, client, args[1:], out)
	case "answer":
		return runJobAnswer(ctx, client, args[1:], out)
	default:
		return fmt.Errorf("there is no job %s command: krewe job <create|list|show|stop|ask|answer>", args[0])
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
	flagRepository     = "--repository"
	flagProduct        = "--product"
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
		return fmt.Errorf("usage: krewe job create [<workspace>/<project>] %s \"...\" %s \"...\"", flagTitle, flagBrief)
	}
	// The parent is refused here rather than sent, in the words the system refuses it with, because a
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
		Repository:     values.first(flagRepository),
		Product:        values.first(flagProduct),
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
	fmt.Fprintf(out, "%s. A controller picks it up and runs it; read the answer with krewe job show %s\n",
		declared.GetPhase(), display.ShortID(declared.GetId()))
	sayNoSentence(out, declared)
	sayWhatIsLeftOut(out, resp.GetLeftOut())
	return nil
}

// sayNoSentence tells an operator declaring the job at the top of a tree that nothing says what a
// person does with what it builds.
//
// It says rather than refuses, the way the missing skills above do. The system cannot write the
// sentence, and a tree of jobs that runs an errand needs none, so refusing here would stop work over
// a line the caller may have had no use for.
//
// Only for a root, and only from the tool. A job a session declares carries its parent's, so a
// session is never asked for a sentence somebody already wrote.
func sayNoSentence(out io.Writer, declared *quaycrewv1.Job) {
	if declared.GetProduct() != "" || declared.GetParent() != "" {
		return
	}
	fmt.Fprintf(out, "nothing on this job says what a person does with what it builds and what they get back, "+
		"so nothing under it can tell the product from the design. Say it in one sentence with "+
		"%s \"...\", in the words that person would use.\n", flagProduct)
}

// sayWhatIsLeftOut names the skills the session running this job starts without, and how to fix each.
//
// It prints where the declaration is made, because that is the last moment anybody is looking. The
// system has always known this and said it only in krewe skill list, which is a listing nobody is
// required to read, so a workspace with no credential took a whole tree of job and every session in
// it died on its first clone.
//
// The sentence per skill is the skill listing's own, so there is one wording to keep right.
func sayWhatIsLeftOut(out io.Writer, leftOut []*quaycrewv1.Skill) {
	if len(leftOut) == 0 {
		return
	}
	fmt.Fprintln(out, "this workspace has not set every secret its skills need. The session running this job starts without:")
	for _, one := range leftOut {
		fmt.Fprintf(out, "  %s: %s\n", one.GetName(), one.GetLeftOut())
	}
}

// whereTheJobRuns is the project to declare in, and empty when the system is to read it from the
// credential the caller presented.
//
// A session running a job is standing nowhere and types no address. It cannot resolve one either:
// resolving an address means listing workspaces and projects, and a role grants the four job verbs
// and nothing else. What it does hold is a credential minted for the job it is running, and that
// credential already says which project that job is in, so the system reads it there. The same place
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
		return "", "", fmt.Errorf("a job runs in a project: krewe job create <workspace>/<project> %s \"...\" %s \"...\"",
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
		return fmt.Errorf("usage: krewe job list [<workspace>/<project>] [%s <phase>] [%s <key>=<value>]", flagPhase, flagLabel)
	}
	typed := ""
	if len(rest) == 1 {
		typed = rest[0]
	}
	// The word that reads every project. Without it a listing narrows to where the operator stands
	// and says nothing about having done so, which is how nine jobs one address away go unseen.
	where := systemWide("jobs")
	request := &quaycrewv1.ListJobsRequest{
		Parent: values.first(flagParent), RootsOnly: values.has(flagRoots), Phase: values.first(flagPhase),
	}
	if !readsTheSystem(typed) {
		located, err := locate(ctx, client, typed)
		if err != nil {
			return err
		}
		request.Workspace, request.Project = located.WorkspaceID, located.ProjectID
		where = narrowedTo("jobs", located.Path.String(), "krewe job list system reads every project")
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
		where.nothing(out)
		fmt.Fprintf(out, "declare one with krewe job create %s \"...\" %s \"...\"\n", flagTitle, flagBrief)
		return nil
	}
	// A listing that read every project says which project each row is in, or the rows are a heap
	// of identifiers with no address on any of them.
	addresses := map[string]string{}
	if where.where == "" {
		addresses = jobAddresses(ctx, client)
	}
	holding := ""
	for _, one := range resp.GetJobs() {
		if holding == "" && heldForRoom(one) {
			holding = one.GetReason()
		}
		if where.where == "" {
			fmt.Fprintf(out, "%-10s %-24s %-2d %-8s %s\n", display.ShortID(one.GetId()),
				addresses[one.GetProject()], one.GetDepth(), phaseOf(one), truncateLine(one.GetTitle()))
			continue
		}
		fmt.Fprintf(out, "%-10s %-2d %-8s %s\n",
			display.ShortID(one.GetId()), one.GetDepth(), phaseOf(one), truncateLine(one.GetTitle()))
	}
	// Said once, under the listing, because an operator reading a column of "held" needs to know it
	// is the machine and not the system. A full machine and a stalled system look identical otherwise.
	if holding != "" {
		fmt.Fprintf(out, "\nheld: %s\n", holding)
	}
	where.counted(out, len(resp.GetJobs()))
	return nil
}

// phaseOf is the word the listing carries. A pending job the system is holding back reads "held"
// rather than "pending": both are waiting, and only one of them is waiting for a machine.
func phaseOf(one *quaycrewv1.Job) string {
	if heldForRoom(one) {
		return "held"
	}
	return one.GetPhase()
}

// heldForRoom says whether this job is pending because the system would not start it. Only the system
// writes a reason on a pending job, and it writes one only when it holds the job back.
func heldForRoom(one *quaycrewv1.Job) bool {
	return one.GetPhase() == job.PhasePending && one.GetReason() != ""
}

// jobAddresses maps a project identifier to the address a person reads, so a system wide listing can
// say where each row is. A name it cannot find falls back to the short identifier rather than
// leaving the column blank.
func jobAddresses(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient) map[string]string {
	resp, err := client.ListProjects(ctx, &quaycrewv1.ListProjectsRequest{})
	if err != nil {
		return map[string]string{}
	}
	workspaces := workspaceNames(ctx, client)
	addresses := make(map[string]string, len(resp.GetProjects()))
	for _, one := range resp.GetProjects() {
		addresses[one.GetId()] = display.Name(workspaces[one.GetWorkspace()], one.GetWorkspace()) +
			workspace.Separator + one.GetName()
	}
	return addresses
}

// runJobShow reads one job back: what it is, where it got to, and what came of it.
func runJobShow(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: krewe job show <job>")
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
	// What a person does with what this builds, and what they get back. It is above everything the job
	// says about itself, because it is what the rest of the job is read against: the design is
	// evidence for this sentence rather than a replacement for it.
	if one.GetProduct() != "" {
		fmt.Fprintf(out, "for a person: %s\n", one.GetProduct())
	}
	// Why it stopped, before anything else, because a job that halted and a job that went quiet read
	// the same without it.
	if one.GetReason() != "" {
		fmt.Fprintf(out, "%s\n", one.GetReason())
	}
	// What it asked, and what it was told. Both stay on the row after the answer, because an answer
	// on its own says nothing and a question on its own leaves a reader looking for the decision.
	if question := one.GetQuestion(); question != "" {
		if one.GetPhase() == job.PhaseAsking {
			fmt.Fprintf(out, "asking: %s\n", question)
			fmt.Fprintf(out, "answer it with krewe job answer %s \"...\"\n", display.ShortID(one.GetId()))
		} else {
			fmt.Fprintf(out, "asked: %s\n", question)
		}
	}
	if one.GetTold() != "" {
		fmt.Fprintf(out, "told: %s\n", one.GetTold())
	}
	for _, waits := range one.GetAfter() {
		fmt.Fprintf(out, "waits for %s\n", display.ShortID(waits))
	}
	if required := one.GetRequires(); len(required) > 0 {
		fmt.Fprintf(out, "requires %s\n", strings.Join(required, ", "))
	}
	if one.GetRepository() != "" {
		fmt.Fprintf(out, "in %s\n", one.GetRepository())
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
	// The answer last and whole, because it is what a caller came for. The pull request sits above it
	// rather than inside it: reading a job says where the work is without anybody reading an answer to
	// the end, or opening a sandbox to find out.
	if one.GetPullRequest() != "" {
		fmt.Fprintf(out, "\npull request: %s\n", one.GetPullRequest())
	}
	if one.GetAnswer() != "" {
		fmt.Fprintf(out, "\nanswer:\n%s\n", one.GetAnswer())
	}
	if one.GetSession() != "" {
		fmt.Fprintf(out, "\nread what it did with krewe task list %s\n", display.ShortID(one.GetSession()))
	}
	return nil
}

// runJobStop halts job that has not ended. The reason is what somebody reading it tomorrow has.
func runJobStop(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: krewe job stop <job> [<reason>]")
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

// runJobAsk puts a question to a person about the job this session is running.
//
// It names no job. The system reads which job is asking from the credential this session holds, the
// same way it reads the parent of anything the session declares: a caller that could name any job
// could stop any job.
func runJobAsk(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: krewe job ask \"<question>\"")
	}
	resp, err := client.AskJob(ctx, &quaycrewv1.AskJobRequest{Question: args[0]})
	if err != nil {
		return err
	}
	asking := resp.GetJob()
	fmt.Fprintf(out, "asked %s\n", display.ShortID(asking.GetId()))
	fmt.Fprintf(out, "%s\n", asking.GetQuestion())
	// Said out loud, because the session reading this is about to end its task and a model that
	// thinks it is waiting inside the call would sit there instead.
	fmt.Fprintln(out, "\nnothing moves this job until a person answers, so end your task now and say "+
		"in your answer that you are waiting. The answer arrives as your next task.")
	return nil
}

// runJobAnswer tells an asking job what was decided.
func runJobAnswer(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: krewe job answer <job> \"<answer>\"")
	}
	one, err := findJob(ctx, client, args[0])
	if err != nil {
		return err
	}
	resp, err := client.AnswerJob(ctx, &quaycrewv1.AnswerJobRequest{Id: one.GetId(), Answer: args[1]})
	if err != nil {
		return err
	}
	answered := resp.GetJob()
	fmt.Fprintf(out, "answered %s\n", display.ShortID(answered.GetId()))
	fmt.Fprintf(out, "%s\n", answered.GetTold())
	fmt.Fprintln(out, "\nit starts again with that answer, in the session that asked.")
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
		return nil, fmt.Errorf("there is no job %s; krewe job list says what there is", typed)
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

// valuelessFlags are the flags that are a word on their own. Each says which of two things a
// command does rather than carrying a value, so the word after one belongs to the command.
var valuelessFlags = map[string]bool{
	flagRoots: true,
	flagClear: true,
}

// readFlags separates the values from the words.// readFlags separates the values from the words. This tool takes no flags anywhere else, so the
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
		// The flags that carry no value. Everything else takes the word after it, and a flag at the
		// end of the line with nothing after it is a value the caller thinks they gave.
		if valuelessFlags[name] {
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

// readLabels turns key=value pairs into the map the system keeps.
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
		flagTitle, flagBrief, flagRole, flagMode, flagExpectFile, flagExpectContains, flagRepository,
		flagProduct,
		flagAfter, flagDeadline, flagBudgetTokens, flagLabel, flagRequires, flagPhase, flagRoots,
		// Taken so it can be refused with the sentence that says where a parent comes from,
		// rather than with the tool's general refusal of flags.
		flagParent,
	} {
		taken[name] = true
	}
	return taken
}
