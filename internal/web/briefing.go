package web

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/display"
	"github.com/atlantic-blue/krewe/internal/flow"
	"github.com/atlantic-blue/krewe/internal/headroom"
	"github.com/atlantic-blue/krewe/internal/job"
	"github.com/atlantic-blue/krewe/internal/sandbox"
	"github.com/atlantic-blue/krewe/internal/workspace"
)

// The briefing is the front door, and it answers three questions in the order a decision needs them:
// what needs the operator, what is blocked, and what the system produced. What is running comes last,
// because the console and the command line already answer it twice.
//
// Every block draws jobs as the tree in docs/ORCHESTRATION.md section 12: a root job, its children
// under it, and the session as a cell on the row rather than a level of its own. A job that answers a
// question is drawn under the ancestors it belongs to, so a child that asked something is never a
// loose row with no work behind it.

// landedAtMost is how many produced jobs the newest block carries. A page that shows everything shows
// nothing, and what landed last week is a search rather than a briefing.
const landedAtMost = 10

// checksUnread is what the system knows about a pull request once it has the address: nothing. It reads
// as unread rather than as passing, because a reading nobody took must never look like a green one.
// Reading a forge back is https://github.com/atlantic-blue/quay-crew/issues/549.
const checksUnread = "checks not read"

// noPullRequest is a job that ended done and named no address. It is not the same as a job whose
// address nobody has looked at, so it does not render as a blank cell.
const noPullRequest = "no pull request"

// jobRow is one job on the briefing. Every block draws the same row, and a block leaves out what it
// has nothing to say about: an empty field is not drawn at all, so a cell is never a blank that reads
// as missing.
type jobRow struct {
	ID    string
	Short string
	// Place is the workspace and project the job was declared in, written the way an operator says it.
	Place string
	Title string
	Phase string
	Age   string
	// Depth is how far under its root this row sits, which is what indents the tree. Context marks a
	// row drawn only so the row beneath it has its work above it: it answers no question itself.
	Depth   int
	Context bool
	// Session is the conversation the job runs in, and SessionID links to it. Empty until one exists.
	Session   string
	SessionID string
	// Question and Answer are what an asking job waits for, and the command that ends the wait.
	Question string
	Answer   string
	// Why says which kind of stuck this is, and Reason is what the system wrote when it stopped.
	Why    string
	Reason string
	// PullRequest, Checks and Cost are what the job produced and what it cost.
	PullRequest string
	Checks      string
	Cost        string
	// Ended is how long ago the job finished, and Waited how long it has been asking.
	Ended  string
	Waited string
}

// block is one question and the rows that answer it. Says is what the block reads as when nothing
// answers it, because a page with nothing blocked must say nothing is blocked rather than draw an
// empty table and leave the operator to guess whether it broke.
type block struct {
	ID      string
	Heading string
	Says    string
	Rows    []jobRow
	// More is said under a block that was capped, so a cut listing never reads as the whole of it.
	More string
}

// redrawSeconds is how often the browser draws the page again. A page that sits in a tab and looks
// current is the failure this answers, and the moment it was drawn is only half of that: a reader who
// has to remember to reload is a reader reading yesterday. A meta refresh needs no build step and no
// library, which is what a system that ships as one binary can hold. Following a job as it moves,
// rather than redrawing the lot, is issue 334, and this takes that road when it lands.
const redrawSeconds = 15

type briefingPage struct {
	shell
	// Header is the system in one line: what is running, what it spent, what the machine has left and
	// what the last probe found. It sits above the blocks because it is a glance rather than a queue.
	Header headerLine
	Blocks []block
	// DrawnAt is the moment this page was built.
	DrawnAt string
}

func (v *view) briefing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	listed, err := v.reader.ListJobs(ctx, &quaycrewv1.ListJobsRequest{})
	if err != nil {
		http.Error(w, "the system did not answer: "+err.Error(), http.StatusBadGateway)
		return
	}
	names, err := v.names(ctx)
	if err != nil {
		http.Error(w, "the system did not answer: "+err.Error(), http.StatusBadGateway)
		return
	}

	v.render(w, "briefing.html", briefingPage{
		shell: shell{
			Title:   "briefing",
			Where:   "what needs you, what is blocked, what the system produced",
			Refresh: redrawSeconds,
		},
		Header:  v.header(ctx, listed.GetJobs()),
		Blocks:  blocks(listed.GetJobs(), names, v.askingRuns(ctx, listed.GetJobs())),
		DrawnAt: time.Now().Local().Format("15:04:05"),
	})
}

// blocks is the whole page, in the order a decision needs it.
//
// Only the produced block is capped. The other three are queues somebody is meant to empty, so a
// briefing that hid part of one would hide the work it exists to surface; what landed grows for ever
// and is a search rather than a briefing after the first screen.
//
// runs is the flow run carried by each job that is asking, which decides the command its row offers.
func blocks(jobs []*quaycrewv1.Job, names map[string]string, runs map[string]string) []block {
	produced := tree(jobs, names, landed, endedAt, asLanded, landedAtMost)
	return []block{
		{
			ID:      "waiting",
			Heading: "waiting on you",
			Says:    "Nothing is waiting on you.",
			Rows:    tree(jobs, names, asking, since, answering(runs), 0),
		},
		{
			ID:      "blocked",
			Heading: "blocked",
			Says:    "Nothing is blocked.",
			Rows:    tree(jobs, names, blocked, endedAt, asBlocked, 0),
		},
		{
			ID:      "produced",
			Heading: "produced",
			Says:    "The system has produced nothing yet.",
			Rows:    produced,
			More:    leftOut(jobs, landed, produced),
		},
		{
			ID:      "running",
			Heading: "running",
			Says:    "Nothing is running.",
			Rows:    tree(jobs, names, running, declared, asRunning, 0),
		},
	}
}

// asking is a job that put a question to a person and stopped there.
func asking(one *quaycrewv1.Job) bool { return one.GetPhase() == job.PhaseAsking }

// blocked is a job nothing will move without a person: one that failed or was stopped, and one the
// machine is holding back. A job that carries a value in resuming is going again, so it is not stuck.
func blocked(one *quaycrewv1.Job) bool {
	if held(one) {
		return true
	}
	if one.GetResuming() != "" {
		return false
	}
	return one.GetPhase() == job.PhaseFailed || one.GetPhase() == job.PhaseStopped
}

// held is a job pending with a reason on it. Only the system writes a reason on a pending job, and it
// writes one only when it would not start the job, so a full machine and a broken system never read the
// same.
func held(one *quaycrewv1.Job) bool {
	return one.GetPhase() == job.PhasePending && one.GetReason() != ""
}

// landed is a job that reached the end.
func landed(one *quaycrewv1.Job) bool { return one.GetPhase() == job.PhaseDone }

// running is what is under way, which is the question the console and the command line already
// answer. Asking is not among them: a job waiting on a person is in the first block, and drawing it
// here as well would say a person is not needed.
func running(one *quaycrewv1.Job) bool {
	switch one.GetPhase() {
	case job.PhaseRunning, job.PhaseWaiting:
		return true
	case job.PhasePending:
		return !held(one)
	default:
		return false
	}
}

// asAsking, asBlocked, asLanded and asRunning are what each block says about a row beyond the job
// itself. They are separate because a block that drew every field would be the wall of everything
// this page exists not to be.

// answering says what an asking row waits for and what ends the wait.
//
// The command is the half that decides whether the page is worth opening: a briefing that says a job
// is waiting and leaves the operator to work out what to type has moved the problem rather than
// answered it. A job carrying a flow run takes the run's own command, because AnswerFlowRun refuses
// anything that is not a run.
func answering(runs map[string]string) func(*jobRow, *quaycrewv1.Job) {
	return func(row *jobRow, one *quaycrewv1.Job) {
		row.Question = one.GetQuestion()
		row.Waited = display.Age(one.GetUpdatedAt())
		if run, carried := runs[one.GetId()]; carried {
			row.Answer = "krewe flow answer " + display.ShortID(run) + ` "..."`
			return
		}
		row.Answer = "krewe job answer " + display.ShortID(one.GetId()) + ` "..."`
	}
}

func asBlocked(row *jobRow, one *quaycrewv1.Job) {
	row.Reason = one.GetReason()
	switch {
	case held(one):
		row.Why = "the machine held it back"
	case one.GetPhase() == job.PhaseStopped:
		row.Why = "somebody stopped it"
	default:
		row.Why = "it failed"
	}
	if one.GetFinishedAt() != nil {
		row.Ended = display.Age(one.GetFinishedAt())
	}
}

func asLanded(row *jobRow, one *quaycrewv1.Job) {
	row.PullRequest = one.GetPullRequest()
	if row.PullRequest == "" {
		row.PullRequest = noPullRequest
	} else {
		// The system holds the address and has never read it back, so the state of the checks is a
		// thing it does not know. Saying so is the whole of what it may say.
		row.Checks = checksUnread
	}
	row.Cost = display.Tokens(one.GetSpentTokens())
	row.Ended = display.Age(one.GetFinishedAt())
}

func asRunning(row *jobRow, one *quaycrewv1.Job) {
	row.Cost = display.Tokens(one.GetSpentTokens())
}

// since, endedAt and declared are the moment a block orders by: when the job started asking, when it
// ended, and when it was declared.

func since(one *quaycrewv1.Job) time.Time { return at(one.GetUpdatedAt()) }

func endedAt(one *quaycrewv1.Job) time.Time {
	if one.GetFinishedAt() != nil {
		return at(one.GetFinishedAt())
	}
	return at(one.GetUpdatedAt())
}

func declared(one *quaycrewv1.Job) time.Time { return at(one.GetCreatedAt()) }

func at(stamp interface{ AsTime() time.Time }) time.Time {
	if stamp == nil {
		return time.Time{}
	}
	return stamp.AsTime()
}

// leftOut says how much a cap dropped, counted off the rows that were actually drawn rather than off
// the cap, so a listing that was cut never reads as the whole of what the system has and a sentence
// about a cut never appears over a block that was not.
func leftOut(jobs []*quaycrewv1.Job, matches func(*quaycrewv1.Job) bool, rows []jobRow) string {
	found := 0
	for _, one := range jobs {
		if matches(one) {
			found++
		}
	}
	drawn := 0
	for _, row := range rows {
		// A row drawn only to carry the tree is not an answer, so it does not count against the cap.
		if !row.Context {
			drawn++
		}
	}
	if drawn >= found {
		return ""
	}
	return "The newest " + strconv.Itoa(drawn) + " of " + strconv.Itoa(found) +
		". The rest is in krewe job list."
}

// branch is one root job and the rows under it that this block draws, in the order the tree runs.
type branch struct {
	rows []jobRow
	// latest is the newest moment among the rows that answered, which is what orders the branches.
	latest time.Time
	// found is how many rows in this branch answered, which is what the cap counts.
	found int
}

// tree draws the jobs that answer a block, each under the ancestors it belongs to.
//
// A flat list of eleven sessions with no relation between them is the problem section 12 names, and a
// flat list of eleven jobs is the same problem one level up. So a row that answers is drawn with its
// parents above it, dimmed and carrying nothing of their own, and the branches are ordered by the
// newest thing that happened in each.
func tree(
	jobs []*quaycrewv1.Job,
	names map[string]string,
	matches func(*quaycrewv1.Job) bool,
	moment func(*quaycrewv1.Job) time.Time,
	say func(*jobRow, *quaycrewv1.Job),
	limit int,
) []jobRow {
	known := make(map[string]*quaycrewv1.Job, len(jobs))
	for _, one := range jobs {
		known[one.GetId()] = one
	}
	children := map[string][]*quaycrewv1.Job{}
	roots := make([]*quaycrewv1.Job, 0, len(jobs))
	for _, one := range jobs {
		// A job whose parent the listing does not hold is drawn as a root of its own rather than
		// dropped, because a row that answers the question must never disappear into a gap.
		if parent := one.GetParent(); parent != "" && known[parent] != nil {
			children[parent] = append(children[parent], one)
			continue
		}
		roots = append(roots, one)
	}

	branches := make([]branch, 0, len(roots))
	for _, root := range roots {
		grown := grow(root, children, names, matches, moment, say, 0, map[string]bool{})
		if grown.found > 0 {
			branches = append(branches, grown)
		}
	}
	sort.SliceStable(branches, func(a, b int) bool { return branches[a].latest.After(branches[b].latest) })

	// The branch is the unit the cap counts in, because half a tree is worse than a long one: the rows
	// left would hang off parents whose other children silently went. So the first branch is always
	// drawn whole, however big it is, and the cap decides whether the next one joins it.
	rows := make([]jobRow, 0, len(jobs))
	drawn := 0
	for _, one := range branches {
		if limit > 0 && drawn > 0 && drawn+one.found > limit {
			break
		}
		rows = append(rows, one.rows...)
		drawn += one.found
		if limit > 0 && drawn >= limit {
			break
		}
	}
	return rows
}

// grow walks one job and everything under it, keeping the rows that answer and the ancestors they
// hang from.
func grow(
	one *quaycrewv1.Job,
	children map[string][]*quaycrewv1.Job,
	names map[string]string,
	matches func(*quaycrewv1.Job) bool,
	moment func(*quaycrewv1.Job) time.Time,
	say func(*jobRow, *quaycrewv1.Job),
	depth int,
	walked map[string]bool,
) branch {
	// A parent that names a child already walked would otherwise draw for ever. The store does not
	// write one, and a page is a poor place to find out that it did.
	if walked[one.GetId()] {
		return branch{}
	}
	walked[one.GetId()] = true

	grown := branch{}
	for _, child := range children[one.GetId()] {
		under := grow(child, children, names, matches, moment, say, depth+1, walked)
		if under.found == 0 {
			continue
		}
		grown.rows = append(grown.rows, under.rows...)
		grown.found += under.found
		if under.latest.After(grown.latest) {
			grown.latest = under.latest
		}
	}

	if matches(one) {
		row := rowOf(one, names, depth)
		say(&row, one)
		grown.rows = append([]jobRow{row}, grown.rows...)
		grown.found++
		if when := moment(one); when.After(grown.latest) {
			grown.latest = when
		}
	} else if grown.found > 0 {
		// An ancestor of something that answered. It is drawn so the row below it has its work above
		// it, and it carries nothing of its own, because it is not an answer to this question.
		context := rowOf(one, names, depth)
		context.Context = true
		grown.rows = append([]jobRow{context}, grown.rows...)
	}
	return grown
}

// rowOf is what every block says about a job, before the block adds what it came for.
func rowOf(one *quaycrewv1.Job, names map[string]string, depth int) jobRow {
	row := jobRow{
		ID:    one.GetId(),
		Short: display.ShortID(one.GetId()),
		Place: place(one, names),
		Title: one.GetTitle(),
		Phase: phase(one),
		Age:   display.Age(one.GetCreatedAt()),
		Depth: depth,
	}
	if one.GetSession() != "" {
		row.Session = display.ShortID(one.GetSession())
		row.SessionID = one.GetSession()
	}
	return row
}

// phase is the word the command line already prints, so the page, the console and the tool say the
// same thing about the same row.
func phase(one *quaycrewv1.Job) string {
	if held(one) {
		return "held"
	}
	return one.GetPhase()
}

// place is where the job was declared, written workspace/project the way an operator says it.
func place(one *quaycrewv1.Job, names map[string]string) string {
	return display.Name(names[one.GetWorkspace()], one.GetWorkspace()) +
		workspace.Separator +
		display.Name(names[one.GetProject()], one.GetProject())
}

// headerLine is the system in one line above the blocks: what is running now, what it has spent, what
// the machine has left, and what the last probe of the system found.
//
// The words are the ones GetHeadroom and GetHealth already use, so this line, the console and
// krewe header cannot say different things about one system. Every figure is optional: a system that
// cannot say what the machine has left still has four blocks worth reading, so a call that does not
// answer leaves its own figure empty rather than taking the page down.
type headerLine struct {
	Running int
	Tokens  string
	Room    string
	// RoomState is the one word on its own, so the page can colour it: full has to be readable
	// without reading the number beside it.
	RoomState string
	Health    string
	// Degraded is a system with a part that is down, which is the other state that has to be readable
	// at a glance.
	Degraded bool
}

// header reads the four figures. Nothing here fails the page.
func (v *view) header(ctx context.Context, jobs []*quaycrewv1.Job) headerLine {
	line := headerLine{}
	for _, one := range jobs {
		if running(one) {
			line.Running++
		}
	}
	if spent, err := v.reader.GetUsage(ctx, &quaycrewv1.GetUsageRequest{}); err == nil {
		line.Tokens = display.Tokens(sandbox.Usage{
			Input:        spent.GetTotal().GetInput(),
			Output:       spent.GetTotal().GetOutput(),
			CacheRead:    spent.GetTotal().GetCacheRead(),
			CacheWritten: spent.GetTotal().GetCacheWritten(),
		}.Total())
	}
	if room, err := v.reader.GetHeadroom(ctx, &quaycrewv1.GetHeadroomRequest{}); err == nil {
		line.RoomState = room.GetState()
		// A system that measured nothing says unknown once and says nothing else, which is how the
		// console draws it. A figure nobody measured, dressed as a figure, is what let eighteen
		// sandboxes be killed with nothing said about it.
		line.Room = headroom.StateUnknown
		if state := room.GetState(); state != "" && state != headroom.StateUnknown {
			line.Room = room.GetUsed() + " of " + room.GetLimit() + " " + state
		}
	}
	line.Health, line.Degraded = v.health(ctx)
	return line
}

// health is the system's own health in a few words, in the words the call already uses.
//
// A part that is down names itself, because that is the part somebody has to go and look at. A system
// that has never probed says so rather than reading as serving: a part nobody checked must never read
// the same as a part that answered.
func (v *view) health(ctx context.Context) (string, bool) {
	return healthOf(v.reader.GetHealth(ctx, &quaycrewv1.GetHealthRequest{}))
}

// healthOf is what the header says about a probe, and whether the system reads as degraded.
func healthOf(answer *quaycrewv1.GetHealthResponse, err error) (string, bool) {
	if err != nil || len(answer.GetComponents()) == 0 {
		return display.HealthNotChecked, false
	}
	for _, component := range answer.GetComponents() {
		if component.GetState() == display.HealthDown {
			return component.GetName() + " is " + display.HealthDown, true
		}
	}
	return display.HealthServing, false
}

// askingRuns is the run identifier for each job that carries an asking flow run.
//
// There are two answer commands and the page has to pick the right one. A job that asked for itself
// takes krewe job answer. A job carrying a run takes the run's own call, because AnswerFlowRun refuses
// anything that is not a run, so offering the other command would hand the operator a refusal. A read
// that fails costs the flow commands and never the page: the job command is still an answer.
func (v *view) askingRuns(ctx context.Context, jobs []*quaycrewv1.Job) map[string]string {
	carried := map[string]string{}
	waiting := false
	for _, one := range jobs {
		if asking(one) {
			waiting = true
			break
		}
	}
	if !waiting {
		return carried
	}
	listed, err := v.reader.ListFlowRuns(ctx, &quaycrewv1.ListFlowRunsRequest{})
	if err != nil {
		return carried
	}
	for _, run := range listed.GetRuns() {
		if run.GetJob() != "" && run.GetStatus() == flow.StatusAsking {
			carried[run.GetJob()] = run.GetId()
		}
	}
	return carried
}
