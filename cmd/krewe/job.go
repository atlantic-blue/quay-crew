package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/display"
	"github.com/atlantic-blue/quay-krewe/internal/forge"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/workspace"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// runJob drives the job the system keeps: declared intent, read back as data.
//
// A job is a row rather than a command that runs now. Declaring it records what should
// happen; nothing here dispatches anything.
func runJob(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: krewe job <create|list|show|stop|ask|answer|step|question|settle|handoff|resume|refuse>")
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
	case "step":
		return runJobStep(ctx, client, args[1:], out)
	case "question":
		return runJobQuestion(ctx, client, args[1:], out)
	case "settle":
		return runJobSettle(ctx, client, args[1:], out)
	case "handoff":
		return runJobHandoff(ctx, client, args[1:], out)
	case "resume":
		return runJobResume(ctx, client, args[1:], out)
	case "refuse":
		return runJobRefuse(ctx, client, args[1:], out)
	default:
		return fmt.Errorf("there is no job %s command: "+
			"krewe job <create|list|show|stop|ask|answer|step|question|settle|handoff|resume|refuse>", args[0])
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
	flagRequest        = "--request"
	flagClaim          = "--claim"
	flagEscalate       = "--escalate"
	flagNoGate         = "--no-gate"
	flagParent         = "--parent"
	flagPhase          = "--phase"
	flagOutcome        = "--outcome"
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
		Request:        values.first(flagRequest),
		Claim:          values.first(flagClaim),
		Escalation:     values.first(flagEscalate),
		Ungated:        values.has(flagNoGate),
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
	// The claim in the spelling it was stored in, which is not always the spelling that was typed: it
	// is lowercased and the space inside it comes out, and that is what a second declaration meets.
	if declared.GetClaim() != "" {
		fmt.Fprintf(out, "claims %s\n", declared.GetClaim())
	}
	fmt.Fprintf(out, "%s. A controller picks it up and runs it; read the answer with krewe job show %s\n",
		declared.GetPhase(), display.ShortID(declared.GetId()))
	sayNoSentence(out, declared)
	sayTheBriefDrifted(out, resp.GetDrifted())
	sayWhatIsLeftOut(out, resp.GetLeftOut())
	return nil
}

// sayTheBriefDrifted names the words of the request the brief never says.
//
// It says nothing at all where the brief carries them, and that silence is the feature. A person who
// reads this line on every job stops reading it, and a check nobody reads is the check that was not
// built. It refuses nothing either: the system knows the brief dropped words, never that the brief
// is wrong, and the person who said the request is often not the person at this terminal.
func sayTheBriefDrifted(out io.Writer, drifted string) {
	if drifted == "" {
		return
	}
	fmt.Fprintln(out, drifted)
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
		return fmt.Errorf("usage: krewe job list [<workspace>/<project>] [%s <phase>] [%s <outcome>] "+
			"[%s <key>=<value>]", flagPhase, flagOutcome, flagLabel)
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
		Outcome: values.first(flagOutcome),
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
	// The column is only there when something in the listing claims a piece of work. It exists to be
	// read before somebody starts work a job already has, and a column of blanks on every listing is a
	// column nobody reads by the second week.
	claiming := anythingClaimed(resp.GetJobs())
	for _, one := range resp.GetJobs() {
		if holding == "" && heldForRoom(one) {
			holding = one.GetReason()
		}
		if where.where == "" {
			fmt.Fprintf(out, "%-10s %-24s %-2d %-8s %-9s %-9s %s%s\n", display.ShortID(one.GetId()),
				addresses[one.GetProject()], one.GetDepth(), phaseOf(one), stageOf(one).Says(),
				outcomeOf(one), claimColumn(one, claiming), truncateLine(one.GetTitle()))
			continue
		}
		fmt.Fprintf(out, "%-10s %-2d %-8s %-9s %-9s %s%s\n",
			display.ShortID(one.GetId()), one.GetDepth(), phaseOf(one), stageOf(one).Says(),
			outcomeOf(one), claimColumn(one, claiming),
			truncateLine(one.GetTitle()))
	}
	// Said once, under the listing, because an operator reading a column of "held" needs to know it
	// is the machine and not the system. A full machine and a stalled system look identical otherwise.
	if holding != "" {
		fmt.Fprintf(out, "\nheld: %s\n", holding)
	}
	where.counted(out, len(resp.GetJobs()))
	return nil
}

// anythingClaimed says whether any row in a listing has taken a piece of work.
func anythingClaimed(jobs []*quaycrewv1.Job) bool {
	for _, one := range jobs {
		if one.GetClaim() != "" {
			return true
		}
	}
	return false
}

// claimColumn is the piece of work a row claims, in a column, and nothing at all where no row in the
// listing claims anything.
func claimColumn(one *quaycrewv1.Job, claiming bool) string {
	if !claiming {
		return ""
	}
	claim := one.GetClaim()
	if len(claim) > claimWidth {
		claim = claim[:claimWidth-1] + "…"
	}
	return fmt.Sprintf("%-*s ", claimWidth, claim)
}

// claimWidth is how wide the claim column is. It holds an owner, a name and an issue number, which is
// what most claims are, and cuts anything longer rather than pushing the title off the line.
const claimWidth = 28

// phaseOf is the word the listing carries. A pending job the system is holding back reads "held"
// rather than "pending": both are waiting, and only one of them is waiting for a machine.
func phaseOf(one *quaycrewv1.Job) string {
	if heldForRoom(one) {
		return "held"
	}
	return one.GetPhase()
}

// outcomeOf is the word a job ended on, and a dash where nothing has stated one.
//
// A dash rather than a blank, because an empty cell in the middle of a row reads as a column that
// failed to fill rather than as a job that has not ended. A job that ended without stating one reads
// the same as one still running here, and the reason on `krewe job show` is what says which.
func outcomeOf(one *quaycrewv1.Job) string {
	if one.GetOutcome() == "" {
		return "-"
	}
	return one.GetOutcome()
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
	// Which of the four stages it is in, what closed the stage before it and what opens the next one.
	// It is here, beside the phase, because the pair is what a reader needs: the phase says the system
	// is waiting and the stage says what it is waiting for, and a job can be in the ideation stage and
	// the asking phase at the same time.
	sayWhichStage(out, one)
	// What a person does with what this builds, and what they get back. It is above everything the job
	// says about itself, because it is what the rest of the job is read against: the design is
	// evidence for this sentence rather than a replacement for it.
	if one.GetProduct() != "" {
		fmt.Fprintf(out, "for a person: %s\n", one.GetProduct())
	}
	// The request, and the reading of it, are printed for the same reason they are printed at the
	// write: a job whose brief dropped what was asked for reads exactly like one whose brief did not.
	// The reading is worked out again here rather than stored, because it is a function of two columns
	// the row already carries and a third copy could only disagree with them.
	if one.GetRequest() != "" {
		fmt.Fprintf(out, "asked for: %s\n", one.GetRequest())
		if drifted := job.Drifted(one.GetRequest(), one.GetBrief()); drifted != "" {
			fmt.Fprintln(out, drifted)
		}
	}
	// The score, printed even at zero. Every other field here is hidden when it is empty, and this one
	// is not: no steers is the best a job can do, and a number that only appears once somebody had to
	// steer reads as an error rather than as a measurement.
	fmt.Fprintf(out, "%s, read them with krewe steers %s\n",
		job.Steers(int(one.GetSteers())), display.ShortID(one.GetId()))
	// The word the job ended on, above the prose, because that is what it is: the signal, with the
	// explanation under it. A reader that had to decide from the answer is the reading this ends.
	if one.GetOutcome() != "" {
		fmt.Fprintf(out, "outcome: %s, %s\n", one.GetOutcome(), job.OutcomeMeans(one.GetOutcome()))
	}
	// Why it stopped, before anything else, because a job that halted and a job that went quiet read
	// the same without it.
	if one.GetReason() != "" {
		fmt.Fprintf(out, "%s\n", one.GetReason())
	}
	// And what an earlier attempt failed with, where this one is carrying on past it. A job that is
	// running for the second time and one that is running for the first read the same without it.
	if one.GetResuming() != "" {
		fmt.Fprintf(out, "continuing past: %s\n", one.GetResuming())
	}
	// That it went in circles, which step it went in circles on, and what it escalated to. It is here
	// rather than only in the reason because a job that was handed to another role is running again
	// and carries no reason at all: without this line it reads as a job that has always been this
	// role's, and the three attempts that came before it are invisible.
	sayItLooped(out, one)
	// What the job understood before it planned, what a person said about it, and which of the
	// questions that answer left alone. It is above the plan because it comes before the plan and
	// because the plan is read against it: a reader holding both can see which parts of the
	// understanding a person put there and which the session filled in for itself.
	sayWhatItUnderstood(out, one)
	// What it would build, and whether a person accepted the list. It sits between the reading and the
	// plan, where the stage itself sits, so a reader goes down the page in the order the job went
	// through it.
	sayWhatItWouldBuild(out, one)
	// The requirements that became failing tests, in the same place for the same reason: the stage sits
	// between the accepted list and the plan, and the page reads in the order the job went through it.
	sayTheFailingTests(out, one)
	// The plan, and whether a person approved it. It is above what the session finished, because the
	// steps below are read against it: a reader holding both can see for themselves which step of the
	// plan the work accounted for.
	if plan := one.GetPlan(); plan != "" {
		if one.GetPlanApproved() {
			fmt.Fprintln(out, "plan, approved:")
		} else {
			fmt.Fprintln(out, "plan, not approved yet:")
		}
		for _, line := range strings.Split(plan, "\n") {
			fmt.Fprintf(out, "  %s\n", line)
		}
	}
	// What the verticals were built into, below the plan for the reason the failing tests sit above it:
	// the page reads in the order the job went through the stages.
	sayWhatWasBuilt(out, one)
	// The runs of its stages, which is where every session a fan out bought went. They are not jobs,
	// so they stand nowhere in a listing of declared work, and this is where a person reads what a
	// stage is doing now and what each run answered.
	sayItsRuns(ctx, client, out, one)
	// What its session finished. It is the record a second attempt carries on from, so it is here
	// rather than only inside a task nobody can read.
	if steps := one.GetSteps(); len(steps) > 0 {
		fmt.Fprintln(out, "finished:")
		for _, step := range steps {
			fmt.Fprintf(out, "  %d. %s\n", step.GetSeq(), step.GetSummary())
		}
	}
	// What the readings of this plan could not settle, with what a later lens settled beside it. A row
	// that is open is a row the person at the end is asked about, so the status is printed on every
	// line rather than only where something happened.
	if questions := one.GetQuestions(); len(questions) > 0 {
		fmt.Fprintln(out, "questions:")
		for _, asked := range questions {
			fmt.Fprintf(out, "  %d. %s [%s]", asked.GetSeq(), asked.GetText(), asked.GetStatus())
			if asked.GetAskedBy() != "" {
				fmt.Fprintf(out, " asked by %s", asked.GetAskedBy())
			}
			fmt.Fprintln(out)
			if asked.GetAnswer() != "" {
				fmt.Fprintf(out, "     settled by %s: %s\n", asked.GetSettledBy(), asked.GetAnswer())
			}
		}
	}
	// What each session left behind when it stopped taking work at the context ceiling. The newest is
	// what the session doing it now was handed, so a reader can tell what it was told to carry on from.
	for _, handed := range one.GetHandoffs() {
		fmt.Fprintf(out, "handed over %d, left: %s\n", handed.GetSeq(), handed.GetLeft())
		if handed.GetTried() != "" {
			fmt.Fprintf(out, "  tried already: %s\n", handed.GetTried())
		}
	}
	// How to answer a failure, said where somebody is already looking at one. Both ways, because
	// which of the two this is is the reader's to decide and nothing else can.
	if one.GetPhase() == job.PhaseFailed {
		fmt.Fprintf(out, "carry on from there with krewe job resume %s, or end it with "+
			"krewe job refuse %s \"...\"\n", display.ShortID(one.GetId()), display.ShortID(one.GetId()))
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
	sayWhenItWasTold(out, one)
	for _, waits := range one.GetAfter() {
		fmt.Fprintf(out, "waits for %s\n", display.ShortID(waits))
	}
	if required := one.GetRequires(); len(required) > 0 {
		fmt.Fprintf(out, "requires %s\n", strings.Join(required, ", "))
	}
	// The piece of work it took, so a reader who came here from a refusal sees what this job holds.
	if one.GetClaim() != "" {
		fmt.Fprintf(out, "claims %s\n", one.GetClaim())
	}
	if one.GetRepository() != "" {
		fmt.Fprintf(out, "in %s\n", one.GetRepository())
		// What read this work, or that nothing did. A settled job that says only "done" is what the gate
		// exists to end: the answer was the only evidence, and it was written by the session being
		// judged.
		fmt.Fprintf(out, "%s\n", gateOf(one).PassedBy())
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
		fmt.Fprintf(out, "              %s\n", pullRequestState(one))
	}
	if one.GetAnswer() != "" {
		fmt.Fprintf(out, "\nanswer:\n%s\n", one.GetAnswer())
	}
	if one.GetSession() != "" {
		showContextSpend(ctx, client, one.GetSession(), out)
		fmt.Fprintf(out, "\nread what it did with krewe task list %s\n", display.ShortID(one.GetSession()))
	}
	return nil
}

// sayItLooped says that a job went in circles, on which step, and what the system did about it.
//
// The attempts are printed under it, oldest first, each held to a line. What a person deciding what
// to do next needs is what the session actually said, and a similarity on its own is a number nobody
// can act on.
// sayWhatItUnderstood prints what the job said it understood before it planned, what a person
// answered, and the questions that answer left alone.
//
// Told and Assumed reach the reader as the session marked them, unrewritten, because that mark is the
// whole point: a plan carries no sign of which of its footings a human put there, and this is where a
// reader finds out. The questions still unknown are worked out again here rather than stored, the way
// the reading of the request is, because they are a function of two fields the row already carries
// and a third copy could only disagree with them.
func sayWhatItUnderstood(out io.Writer, one *quaycrewv1.Job) {
	understood := one.GetIdeation()
	if understood == "" {
		return
	}
	if one.GetIdeationAnswer() == "" {
		fmt.Fprintln(out, "what it understands, waiting for you to answer in your own words:")
	} else {
		fmt.Fprintln(out, "what it understood before it planned:")
	}
	for _, line := range strings.Split(understood, "\n") {
		fmt.Fprintf(out, "  %s\n", line)
	}
	if answer := one.GetIdeationAnswer(); answer != "" {
		fmt.Fprintln(out, "you answered:")
		for _, line := range strings.Split(answer, "\n") {
			fmt.Fprintf(out, "  %s\n", line)
		}
		for _, left := range job.StillUnknown(understood, answer) {
			fmt.Fprintf(out, "  still unknown: question %d, %s\n", left.Number, left.Text)
		}
	}
}

// sayWhatItWouldBuild prints the verticals the job proposed and whether a person accepted them.
//
// A line the person put on the list themselves is marked as theirs, because the mark is the point:
// once both are on the row, a list a person changed and a list the machine proposed read the same,
// and a reader a week later cannot say which of the two chose what was built.
func sayWhatItWouldBuild(out io.Writer, one *quaycrewv1.Job) {
	design := one.GetDesign()
	if design == "" {
		return
	}
	if one.GetDesignAccepted() {
		fmt.Fprintln(out, "what it builds, accepted:")
	} else {
		fmt.Fprintln(out, "what it would build, waiting for you to accept the list:")
	}
	for _, line := range strings.Split(design, "\n") {
		fmt.Fprintf(out, "  %s\n", line)
	}
	if yours := yoursOn(design); yours > 0 {
		fmt.Fprintf(out, "  %s\n", saidYours(yours))
	}
}

// sayTheFailingTests prints the requirements that became failing tests before anything was built.
//
// The count rather than every line. The record carries a line for each failing test and a reader of a
// job wants to know the stage closed and what it covers; the whole of it is in the record for whoever
// needs it.
func sayTheFailingTests(out io.Writer, one *quaycrewv1.Job) {
	kept := one.GetTests()
	if kept == "" {
		return
	}
	requirements, failing := job.TestsOn(kept)
	fmt.Fprintf(out, "%s, before anything was built:\n", saidTheTests(requirements, failing))
	for _, line := range strings.Split(kept, "\n") {
		fmt.Fprintf(out, "  %s\n", line)
	}
}

// saidTheTests reads for one and for several, because a line that says "1 requirements" is a line
// that says nobody read it.
func saidTheTests(requirements, failing int) string {
	said := fmt.Sprintf("%d requirements became %d failing tests", requirements, failing)
	if requirements == 1 {
		said = fmt.Sprintf("one requirement became %d failing tests", failing)
	}
	if failing == 1 {
		said = strings.Replace(said, "1 failing tests", "one failing test", 1)
	}
	return said
}

// yoursOn is how many verticals of a list the person put there themselves.
func yoursOn(design string) int {
	count := 0
	for _, one := range job.DesignIn(design).Verticals {
		if one.Yours {
			count++
		}
	}
	return count
}

// saidYours reads for one and for several, because a line that says "1 verticals" is a line that
// says nobody read it.
func saidYours(count int) string {
	if count == 1 {
		return "one of these is yours, opening with Yours"
	}
	return fmt.Sprintf("%d of these are yours, opening with Yours", count)
}

func sayItLooped(out io.Writer, one *quaycrewv1.Job) {
	if one.GetLoopedStep() == 0 {
		return
	}
	fmt.Fprintf(out, "went in circles on step %d, %s\n", one.GetLoopedStep(), escalatedTo(one))
	// The attempts, unless the job is waiting to be told something: the question underneath already
	// carries them, and printing them twice is the reading nobody finishes.
	if one.GetPhase() == job.PhaseAsking {
		return
	}
	for _, attempt := range one.GetAttempted() {
		if attempt.GetStep() != one.GetLoopedStep() {
			continue
		}
		fmt.Fprintf(out, "  attempt %d (%s): %s\n", attempt.GetSeq(),
			job.Alike(attempt.GetSimilarity()), oneLine(attempt.GetSaid()))
	}
}

// escalatedTo is what the system did when the job went in circles, in a person's words.
func escalatedTo(one *quaycrewv1.Job) string {
	route, err := job.ReadRoute(one.GetEscalatedTo())
	if one.GetEscalatedTo() == "" || err != nil {
		return "and stopped"
	}
	return job.Escalating(route)
}

// showContextSpend prints where the session this job ran in spent its context.
//
// It is on the job rather than only on the session listing because the job is what a person reads
// after the work is over, and this is the number that says why it took what it took: whether the
// session filled up on the code it had to read, on tool output it read once, or on its own repeated
// attempts.
//
// The check under it holds the breakdown against the model's own count of the same context. A
// breakdown whose parts do not add up to the model's total is a number that will be trusted and is
// wrong, so the comparison is printed rather than kept.
//
// A session the system cannot read says nothing. A job whose work is elsewhere should not fail to
// print because one number is missing.
func showContextSpend(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient,
	id string, out io.Writer,
) {
	resp, err := client.GetSession(ctx, &quaycrewv1.GetSessionRequest{Id: id})
	if err != nil {
		return
	}
	session := resp.GetSession()
	spent := display.Spend(session)
	if spent.Empty() {
		return
	}
	fmt.Fprintf(out, "\ncontext spent in session %s:\n", display.ShortID(session.GetId()))
	for _, line := range spent.Lines() {
		fmt.Fprintf(out, "  %s\n", line)
	}
	fmt.Fprintf(out, "  %s\n", spent.Against(session.GetContextWindow().GetUsed()).Line())
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

// runJobStep records one thing the session running this job finished.
//
// It names no job, the way `krewe job ask` names none: the system reads which job is recording from
// the credential this session holds. A caller that could name any job could write on any job's
// record.
func runJobStep(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: krewe job step \"<what you finished>\"")
	}
	resp, err := client.RecordJobStep(ctx, &quaycrewv1.RecordJobStepRequest{Summary: args[0]})
	if err != nil {
		return err
	}
	recorded := resp.GetJob()
	steps := recorded.GetSteps()
	fmt.Fprintf(out, "step %d of %s: %s\n", len(steps), display.ShortID(recorded.GetId()), args[0])
	fmt.Fprintln(out, "if this job stops before it is done, it carries on from here rather than from nothing.")
	return nil
}

// runJobHandoff writes down the state a fresh session starts this job from.
//
// It names no job, the way krewe job step names none: the system reads which job is handing over from
// the credential this session holds. A caller that could name any job could write on any job's record.
func runJobHandoff(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: krewe job handoff \"<what is left>\" [\"<what you tried that did not work>\"]")
	}
	tried := ""
	if len(args) == 2 {
		tried = args[1]
	}
	resp, err := client.RecordJobHandoff(ctx, &quaycrewv1.RecordJobHandoffRequest{
		Left: args[0], Tried: tried,
	})
	if err != nil {
		return err
	}
	handed := resp.GetJob()
	fmt.Fprintf(out, "handed over %s\n", display.ShortID(handed.GetId()))
	fmt.Fprintln(out, "the rest of this job goes to a fresh session, which is given those words, what you "+
		"recorded as finished, and nothing else you can see.")
	return nil
}

// runJobResume continues a job that failed, from the first step it did not finish.
//
// It says what the job failed with before it says anything else, and it says how to refuse it. A
// failure that was the work being wrong must not be continued, and the person typing this is the
// only one who can tell the two apart.
func runJobResume(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: krewe job resume <job>")
	}
	one, err := findJob(ctx, client, args[0])
	if err != nil {
		return err
	}
	failure := one.GetReason()
	resp, err := client.ResumeJob(ctx, &quaycrewv1.ResumeJobRequest{Id: one.GetId()})
	if err != nil {
		return err
	}
	resumed := resp.GetJob()
	fmt.Fprintf(out, "continuing %s\n", display.ShortID(resumed.GetId()))
	fmt.Fprintf(out, "it failed with: %s\n", failure)
	sayWhatIsFinished(out, resumed.GetSteps())
	fmt.Fprintf(out, "\na controller picks it up and carries on in the session it is already in. It is asked "+
		"to fetch the branch this work is based on and say what moved while it was stopped.\n")
	fmt.Fprintf(out, "if the work was wrong rather than the run, end it instead with "+
		"krewe job refuse %s \"...\"\n", display.ShortID(resumed.GetId()))
	return nil
}

// runJobRefuse ends a job that failed, so nothing continues it.
func runJobRefuse(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: krewe job refuse <job> [<reason>]")
	}
	one, err := findJob(ctx, client, args[0])
	if err != nil {
		return err
	}
	reason := ""
	if len(args) == 2 {
		reason = args[1]
	}
	resp, err := client.RefuseJob(ctx, &quaycrewv1.RefuseJobRequest{Id: one.GetId(), Reason: reason})
	if err != nil {
		return err
	}
	refused := resp.GetJob()
	fmt.Fprintf(out, "refused %s\n", display.ShortID(refused.GetId()))
	fmt.Fprintf(out, "%s\n", refused.GetReason())
	fmt.Fprintln(out, "\nit is stopped, so nothing continues it. Declare a new job for the work that is left.")
	return nil
}

// sayWhatIsFinished lists the steps a job recorded, which is what the session continuing it is not
// asked to do again.
func sayWhatIsFinished(out io.Writer, steps []*quaycrewv1.JobStep) {
	if len(steps) == 0 {
		fmt.Fprintln(out, "nothing was recorded as finished, so it is asked to look before it repeats itself.")
		return
	}
	fmt.Fprintln(out, "finished already:")
	for _, one := range steps {
		fmt.Fprintf(out, "  %d. %s\n", one.GetSeq(), one.GetSummary())
	}
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

// gateOf is a job as the gate reads it, off the wire. The sentence lives in the package that decides
// it, so the tool and the system cannot say two different things about the same row.
func gateOf(one *quaycrewv1.Job) job.Gate {
	return job.Gate{
		Repository: one.GetRepository(), Ungated: one.GetUngated(),
		Reviewed: one.GetReviewed(), Tested: one.GetTested(), Phase: one.GetPhase(),
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
	flagRoots:  true,
	flagClear:  true,
	flagNoGate: true,
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
		flagProduct, flagRequest, flagClaim, flagEscalate, flagNoGate,
		flagAfter, flagDeadline, flagBudgetTokens, flagLabel, flagRequires, flagPhase, flagOutcome,
		flagRoots,
		// Taken so it can be refused with the sentence that says where a parent comes from,
		// rather than with the tool's general refusal of flags.
		flagParent,
	} {
		taken[name] = true
	}
	return taken
}

// pullRequestState is what the forge last said about a job's pull request, and when.
//
// It reads the row rather than a forge. A command that asked GitHub while it drew would wait as long
// as GitHub takes, which is the rule the headroom and health views already hold.
func pullRequestState(one *quaycrewv1.Job) string {
	reading := forge.Reading{
		Status: one.GetPullRequestStatus(), Checks: one.GetPullRequestChecks(),
		FailedCheck: one.GetPullRequestCheck(), Review: one.GetPullRequestReview(),
		Failed: one.GetPullRequestFailed(),
	}
	read := one.GetPullRequestReadAt()
	if read == nil {
		return reading.String()
	}
	reading.ReadAt = read.AsTime()
	return fmt.Sprintf("%s, read %s ago", reading, display.Age(read))
}

// sayWhenItWasTold prints when this wait started, when the first surface named it to a person, and
// the gap between the two.
//
// The gap is the number the telling is judged on: how long somebody was not knowing that something
// waited on them. Four jobs once waited more than an hour because nothing read job.asked, and a
// system that fixed that and never measured it would have no way to say whether it had.
//
// It belongs to the wait a person is in now, so it is measured from where that wait began rather
// than from the question column, which holds the last question this job asked however many waits ago
// that was. A red board records no start at all, so it says so rather than printing a number
// measured from something else.
//
// A wait nothing has named yet says so rather than printing a gap. That is the state the incident
// was in, and reading it as though nobody had asked would hide exactly the case this exists for.
func sayWhenItWasTold(out io.Writer, one *quaycrewv1.Job) {
	asked, raised := one.GetAskedAt(), one.GetRaisedAt()
	if asked == nil && raised == nil {
		return
	}
	why, _, waiting := job.WaitsOn(one)
	// The question stays on the row after it is answered, so it is this wait's only while this job
	// is the one asking. On any other wait it belongs to a decision somebody already made.
	if asked != nil && why == job.WaitingAsking {
		fmt.Fprintf(out, "asked at: %s\n", asked.AsTime().Local().Format(time.RFC3339))
	}
	if raised == nil {
		fmt.Fprintln(out, "told to nobody yet: no surface has named this as waiting")
		return
	}
	fmt.Fprintf(out, "told at:  %s\n", raised.AsTime().Local().Format(time.RFC3339))
	if !waiting {
		return
	}
	began, known := job.WaitBegan(why, stampOrZero(asked), one.GetUpdatedAt().AsTime())
	if !known {
		fmt.Fprintln(out, "how long that took is not known: nothing records when the checks turned red")
		return
	}
	fmt.Fprintf(out, "the wait was carried after %s\n", job.Waited(raised.AsTime().Sub(began)))
}

// stampOrZero is the moment a row carries, and nil where it carries none, which is the shape the
// arithmetic takes so a column that was never written is never read as the zero moment.
func stampOrZero(at *timestamppb.Timestamp) *time.Time {
	if at == nil {
		return nil
	}
	moment := at.AsTime()
	return &moment
}

// runJobQuestion writes down one thing this reading of the plan could not settle.
//
// It names no job, the way krewe job step names none: the system reads which job is reading from the
// credential this session holds. A caller that could name any job could write on any job's record.
func runJobQuestion(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: krewe job question \"<what this reading could not settle>\"")
	}
	resp, err := client.RecordJobQuestion(ctx, &quaycrewv1.RecordJobQuestionRequest{Text: args[0]})
	if err != nil {
		return err
	}
	recorded := resp.GetJob()
	// The number this row took, which is one past the highest the job held rather than the count: a
	// reading handed rows one to three writes its own as four, and it settles by that number.
	written := 0
	for _, asked := range recorded.GetQuestions() {
		if int(asked.GetSeq()) > written {
			written = int(asked.GetSeq())
		}
	}
	fmt.Fprintf(out, "question %d of %s: %s\n", written, display.ShortID(recorded.GetId()), args[0])
	fmt.Fprintln(out, "the next reader is handed this row and may settle it. What no reader settles is "+
		"what a person is asked.")
	return nil
}

// runJobSettle answers a row an earlier reading left open.
func runJobSettle(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, args []string, out io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: krewe job settle <number> \"<what settles it>\"")
	}
	seq, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("a row is settled by its number, as krewe job settle 2 \"...\" (you sent %q)", args[0])
	}
	resp, err := client.SettleJobQuestion(ctx, &quaycrewv1.SettleJobQuestionRequest{
		Seq: int32(seq), Answer: args[1],
	})
	if err != nil {
		return err
	}
	settled := resp.GetJob()
	fmt.Fprintf(out, "settled question %d of %s: %s\n", seq, display.ShortID(settled.GetId()), args[1])
	fmt.Fprintln(out, "a settled row does not reach a person, so settle what your lens can answer and "+
		"leave open only what it cannot.")
	return nil
}

// stageOf is a job as the stages read it, off the wire. The reading lives in the package that
// decides it, so the tool and the system cannot say two different things about the same row.
func stageOf(one *quaycrewv1.Job) job.Stage {
	return job.StageOf(&job.Job{
		Product: one.GetProduct(), Parent: one.GetParent(),
		IdeationAnswer: one.GetIdeationAnswer(),
		Design:         one.GetDesign(), DesignAccepted: one.GetDesignAccepted(),
		Tests: one.GetTests(), Build: one.GetBuild(), Accepted: one.GetAccepted(),
		Plan: one.GetPlan(), PlanApproved: one.GetPlanApproved(),
	})
}

// sayWhichStage says which of the four stages this job is in, what closed the stage before it, and
// what opens the next one.
//
// Where the job stands inside its stage gets a line of its own. The last stage holds a job writing
// its plan, a job whose verticals are being built in a session each, and a job waiting for somebody
// to accept what arrived, and a reader told only "stage 4 of 4: build" cannot tell those apart.
func sayWhichStage(out io.Writer, one *quaycrewv1.Job) {
	stage := stageOf(one)
	if stage.Outside != "" {
		fmt.Fprintf(out, "no stage, phase %s: %s\n", one.GetPhase(), stage.Outside)
		return
	}
	fmt.Fprintf(out, "%s, phase %s\n", stage.Where(), one.GetPhase())
	fmt.Fprintf(out, "  %s\n", stage.Closed)
	fmt.Fprintf(out, "  %s\n", stage.Opens)
	if stage.Doing != "" {
		fmt.Fprintf(out, "  %s\n", stage.Doing)
	}
}

// sayWhatWasBuilt prints what a job's verticals were built into, once every one of them is green.
//
// The whole record rather than a count, the way the failing tests are printed: this is what a person
// is being asked to accept, and a line saying three verticals were built tells them nothing about
// whether the value arrived. The files are in it because they are the difference between a build and
// a claim of one.
func sayWhatWasBuilt(out io.Writer, one *quaycrewv1.Job) {
	kept := one.GetBuild()
	if kept == "" {
		return
	}
	verticals, passing := job.BuiltOn(kept)
	fmt.Fprintf(out, "%s:\n", saidTheBuild(verticals, passing))
	for _, line := range strings.Split(kept, "\n") {
		fmt.Fprintf(out, "  %s\n", line)
	}
	sayWhereThePicturesAre(out, one)
}

// sayWhereThePicturesAre tells a person how to open what they are being asked to look at.
//
// The record above names each picture and says where it came from, and a file name is not something
// anybody can open. The pictures are in the workspace's shared folder, which is a generated
// identifier three levels down, so the line names the command that answers with the path rather than
// the path: this tool talks to a system that may be on another machine.
func sayWhereThePicturesAre(out io.Writer, one *quaycrewv1.Job) {
	shots := job.PicturesIn(one.GetBuild())
	if len(shots) == 0 || one.GetAccepted() {
		return
	}
	fmt.Fprintf(out, "open the %s in this workspace's shared folder, which krewe where %s names, "+
		"then answer this job\n", saidThePictures(len(shots)), one.GetWorkspace())
}

// saidThePictures reads for one and for several, because a line that says "1 pictures" is a line that
// says nobody read it.
func saidThePictures(count int) string {
	if count == 1 {
		return "picture"
	}
	return fmt.Sprintf("%d pictures", count)
}

// saidTheBuild reads for one and for several, because a line that says "1 verticals" is a line that
// says nobody read it.
func saidTheBuild(verticals, passing int) string {
	said := fmt.Sprintf("%d verticals were built, and %d tests pass now", verticals, passing)
	if verticals == 1 {
		said = fmt.Sprintf("one vertical was built, and %d tests pass now", passing)
	}
	if passing == 1 {
		said = strings.Replace(said, "1 tests pass now", "one test passes now", 1)
	}
	return said
}

// sayItsRuns prints the runs of this job's stages, one line each, in the order they were made.
//
// A run has no title, so the line is built from what it is: the stage, the number it holds, where it
// got to and what it cost. Nothing is printed for a job whose stages never fanned out, and a call
// that fails prints nothing rather than failing the whole reading: what a reader came for is the job.
func sayItsRuns(ctx context.Context, client quaycrewv1.ControlPlaneServiceClient, out io.Writer,
	one *quaycrewv1.Job) {
	listed, err := client.ListExecutions(ctx, &quaycrewv1.ListExecutionsRequest{Job: one.GetId()})
	if err != nil || len(listed.GetExecutions()) == 0 {
		return
	}
	fmt.Fprintln(out, "runs of its stages:")
	for _, run := range listed.GetExecutions() {
		fmt.Fprintf(out, "  %s  %s %d  %s", display.ShortID(run.GetId()), run.GetStage(),
			run.GetNumber(), run.GetPhase())
		if run.GetSession() != "" {
			fmt.Fprintf(out, " in %s", display.ShortID(run.GetSession()))
		}
		if run.GetOutcome() != "" {
			fmt.Fprintf(out, ", %s", run.GetOutcome())
		}
		if run.GetReason() != "" {
			fmt.Fprintf(out, ", %s", run.GetReason())
		}
		fmt.Fprintln(out)
	}
}
