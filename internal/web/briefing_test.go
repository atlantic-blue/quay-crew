package web

import (
	"strings"
	"testing"
	"time"

	quaycrewv1 "github.com/atlantic-blue/krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/krewe/internal/job"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The briefing answers three questions before it says anything about what is running. These are the
// answers, built from jobs and nothing else, so each one is a table rather than a page fetch.

// blockOf is one block of the page by name, and the failure when the page has no such block.
func blockOf(t *testing.T, jobs []*quaycrewv1.Job, id string) block {
	t.Helper()
	for _, one := range blocks(jobs, map[string]string{}) {
		if one.ID == id {
			return one
		}
	}
	t.Fatalf("the briefing has no %q block", id)
	return block{}
}

// TestABlockWithNothingInItSaysSoRatherThanReadingAsBroken is the empty case, first, because it is
// the one that decides whether the page is worth opening. A briefing that draws nothing when nothing
// is blocked and a briefing that failed to read the system look identical, and one of them is a defect.
func TestABlockWithNothingInItSaysSoRatherThanReadingAsBroken(t *testing.T) {
	drawn := blocks(nil, map[string]string{})
	if len(drawn) == 0 {
		t.Fatal("the briefing drew no blocks at all, so this test proves nothing")
	}
	for _, one := range drawn {
		if len(one.Rows) != 0 {
			t.Errorf("the %s block drew %d rows out of no jobs", one.ID, len(one.Rows))
		}
		if strings.TrimSpace(one.Says) == "" {
			t.Errorf("the %s block says nothing when it is empty", one.ID)
		}
	}
}

// TestTheThreeQuestionsComeBeforeWhatIsRunning holds the order the whole issue is about. What is
// running is answered by the console and by the command line already, and it is the question the
// operator cares about least.
func TestTheThreeQuestionsComeBeforeWhatIsRunning(t *testing.T) {
	want := []string{"waiting", "blocked", "produced", "running"}
	drawn := blocks(nil, map[string]string{})
	if len(drawn) != len(want) {
		t.Fatalf("the briefing has %d blocks, want %d", len(drawn), len(want))
	}
	for index, id := range want {
		if drawn[index].ID != id {
			t.Errorf("block %d is %q, want %q", index, drawn[index].ID, id)
		}
	}
}

// TestEachQuestionKeepsOnlyTheJobsThatAnswerIt is the whole sorting rule in one table. A job in the
// wrong block is worse than a job in no block: it tells the operator to do something about work that
// needs nothing.
func TestEachQuestionKeepsOnlyTheJobsThatAnswerIt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		one   *quaycrewv1.Job
		where string
	}{
		{name: "asking waits on a person", one: aJob("choose the store", job.PhaseAsking), where: "waiting"},
		{name: "failed is stuck", one: aJob("push the branch", job.PhaseFailed), where: "blocked"},
		{name: "stopped is stuck", one: aJob("push the branch", job.PhaseStopped), where: "blocked"},
		{name: "done is produced", one: aJob("read the bill", job.PhaseDone), where: "produced"},
		{name: "running is running", one: aJob("read the bill", job.PhaseRunning), where: "running"},
		{name: "waiting is running", one: aJob("read the bill", job.PhaseWaiting), where: "running"},
		{name: "pending is running", one: aJob("read the bill", job.PhasePending), where: "running"},
		{
			name:  "pending with a reason is the machine holding it back",
			one:   withReason(aJob("read the bill", job.PhasePending), "the machine is full"),
			where: "blocked",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jobs := []*quaycrewv1.Job{tc.one}
			for _, one := range blocks(jobs, map[string]string{}) {
				rows := len(one.Rows)
				if one.ID == tc.where && rows != 1 {
					t.Errorf("the %s block drew %d rows, want the job in it", one.ID, rows)
				}
				if one.ID != tc.where && rows != 0 {
					t.Errorf("the %s block drew %d rows, and this job does not answer it", one.ID, rows)
				}
			}
		})
	}
}

// TestAJobGoingAgainIsNotBlocked keeps a continued job out of the stuck block. It failed, somebody
// answered that, and it is on its way: a briefing that still asks for a decision about it asks twice.
func TestAJobGoingAgainIsNotBlocked(t *testing.T) {
	going := aJob("push the branch", job.PhaseFailed)
	going.Resuming = "the push was refused"

	if rows := blockOf(t, []*quaycrewv1.Job{going}, "blocked").Rows; len(rows) != 0 {
		t.Errorf("a job that is going again is drawn as blocked: %+v", rows)
	}
}

// TestAJobTheMachineHeldBackNeverReadsAsOneThatFailed. A full machine is a moment; a failure is a
// verdict. They ask for different things from the operator, so they may not read the same.
func TestAJobTheMachineHeldBackNeverReadsAsOneThatFailed(t *testing.T) {
	held := withReason(aJob("read the bill", job.PhasePending), "no room on the machine")
	failed := withReason(aJob("push the branch", job.PhaseFailed), "the model refused")

	rows := blockOf(t, []*quaycrewv1.Job{held, failed}, "blocked").Rows
	if len(rows) != 2 {
		t.Fatalf("the blocked block drew %d rows, want 2", len(rows))
	}
	byTitle := map[string]jobRow{}
	for _, row := range rows {
		byTitle[row.Title] = row
	}
	if got := byTitle["read the bill"]; got.Phase != "held" || !strings.Contains(got.Why, "machine") {
		t.Errorf("a job the machine held back reads as %q, %q", got.Phase, got.Why)
	}
	if got := byTitle["push the branch"]; got.Phase != job.PhaseFailed || !strings.Contains(got.Why, "failed") {
		t.Errorf("a failed job reads as %q, %q", got.Phase, got.Why)
	}
	if got := byTitle["push the branch"]; got.Reason != "the model refused" {
		t.Errorf("the failed job says %q, want the reason whole", got.Reason)
	}
}

// TestAJobWaitingOnAPersonCarriesTheQuestionAndTheCommandThatEndsIt. A question with no way to answer
// it is a page that reports a problem and hands back nothing.
func TestAJobWaitingOnAPersonCarriesTheQuestionAndTheCommandThatEndsIt(t *testing.T) {
	askingJob := aJob("choose where the transcripts are stored", job.PhaseAsking)
	askingJob.Id = "0123456789abcdef01234567"
	askingJob.Question = "on demand key value, or a cluster that bills at rest?"

	rows := blockOf(t, []*quaycrewv1.Job{askingJob}, "waiting").Rows
	if len(rows) != 1 {
		t.Fatalf("the waiting block drew %d rows, want 1", len(rows))
	}
	if rows[0].Question != askingJob.GetQuestion() {
		t.Errorf("the row carries the question as %q, want it whole", rows[0].Question)
	}
	if !strings.Contains(rows[0].Answer, "krewe job answer 01234567") {
		t.Errorf("the row says to answer with %q, which is not a command", rows[0].Answer)
	}
	if rows[0].Waited == "" || rows[0].Waited == "-" {
		t.Errorf("the row does not say how long it has waited, it says %q", rows[0].Waited)
	}
}

// TestAProducedJobNeverSaysItsChecksPassed is the refusal in this block. The system keeps the address of
// a pull request and has never read it back, so a row that said anything about the checks would be
// inventing it, and a reading nobody took must never look like a green one.
func TestAProducedJobNeverSaysItsChecksPassed(t *testing.T) {
	landedJob := aJob("make the listing sort by the clock", job.PhaseDone)
	landedJob.PullRequest = "https://github.com/atlantic-blue/quay-crew/pull/454"
	landedJob.SpentTokens = 24_000

	rows := blockOf(t, []*quaycrewv1.Job{landedJob}, "produced").Rows
	if len(rows) != 1 {
		t.Fatalf("the produced block drew %d rows, want 1", len(rows))
	}
	if rows[0].PullRequest != landedJob.GetPullRequest() {
		t.Errorf("the row says the work is at %q, want %q", rows[0].PullRequest, landedJob.GetPullRequest())
	}
	if rows[0].Checks != checksUnread {
		t.Errorf("the row says the checks are %q, want %q", rows[0].Checks, checksUnread)
	}
	for _, green := range []string{"passing", "passed", "green", "ok"} {
		if strings.Contains(strings.ToLower(rows[0].Checks), green) {
			t.Errorf("the row says %q about checks nobody has read", rows[0].Checks)
		}
	}
	if rows[0].Cost != "24k" {
		t.Errorf("the row says the job cost %q, want 24k", rows[0].Cost)
	}
}

// TestAProducedJobThatNamedNoAddressSaysSo. An empty cell reads as a value the page failed to draw.
// This job opened nothing, which is a different thing and is worth saying.
func TestAProducedJobThatNamedNoAddressSaysSo(t *testing.T) {
	rows := blockOf(t, []*quaycrewv1.Job{aJob("read the electricity bill", job.PhaseDone)}, "produced").Rows
	if len(rows) != 1 {
		t.Fatalf("the produced block drew %d rows, want 1", len(rows))
	}
	if rows[0].PullRequest != noPullRequest {
		t.Errorf("a job that opened nothing says %q, want %q", rows[0].PullRequest, noPullRequest)
	}
	if rows[0].Checks != "" {
		t.Errorf("a job with no pull request says %q about checks", rows[0].Checks)
	}
}

// TestAChildIsDrawnUnderTheJobItBelongsTo is the tree from section 12 of the orchestration design. A
// child that asked a question is not a loose row: the work it belongs to is above it, dimmed, because
// the parent answers no question of its own.
func TestAChildIsDrawnUnderTheJobItBelongsTo(t *testing.T) {
	root := aJob("ship the briefing", job.PhaseRunning)
	root.Id = "1111111111111111111111aa"
	child := aJob("choose what the page leaves out", job.PhaseAsking)
	child.Id = "2222222222222222222222bb"
	child.Parent = root.GetId()

	rows := blockOf(t, []*quaycrewv1.Job{root, child}, "waiting").Rows
	if len(rows) != 2 {
		t.Fatalf("the waiting block drew %d rows, want the child under its root:\n%+v", len(rows), rows)
	}
	if rows[0].Title != root.GetTitle() || !rows[0].Context || rows[0].Depth != 0 {
		t.Errorf("the first row is %+v, want the root drawn as context at depth 0", rows[0])
	}
	if rows[0].Question != "" {
		t.Errorf("the root carries %q, and it asked nothing", rows[0].Question)
	}
	if rows[1].Title != child.GetTitle() || rows[1].Context || rows[1].Depth != 1 {
		t.Errorf("the second row is %+v, want the child at depth 1", rows[1])
	}
}

// TestAJobWhoseParentIsNotInTheListingIsStillDrawn. A row that answers the question must never
// disappear into a gap, so a child with nothing above it is drawn as a root of its own.
func TestAJobWhoseParentIsNotInTheListingIsStillDrawn(t *testing.T) {
	orphan := aJob("choose what the page leaves out", job.PhaseAsking)
	orphan.Parent = "a-job-this-listing-does-not-hold"

	rows := blockOf(t, []*quaycrewv1.Job{orphan}, "waiting").Rows
	if len(rows) != 1 || rows[0].Depth != 0 {
		t.Fatalf("a job whose parent is missing is drawn as %+v, want one row at depth 0", rows)
	}
}

// TestTheProducedBlockPutsTheNewestFirst. What landed an hour ago is what a decision is made on;
// what landed last week is a search.
func TestTheProducedBlockPutsTheNewestFirst(t *testing.T) {
	old := finishedAgo(aJob("the older piece of work", job.PhaseDone), 4*time.Hour)
	recent := finishedAgo(aJob("the newer piece of work", job.PhaseDone), time.Minute)

	rows := blockOf(t, []*quaycrewv1.Job{old, recent}, "produced").Rows
	if len(rows) != 2 {
		t.Fatalf("the produced block drew %d rows, want 2", len(rows))
	}
	if rows[0].Title != recent.GetTitle() {
		t.Errorf("the block reads %q first, want the newest finished", rows[0].Title)
	}
}

// TestTheProducedBlockIsCappedAndSaysWhatItLeftOut. A page that shows all of it shows none of it, and
// a cut listing that says nothing reads as the whole of what the system has.
func TestTheProducedBlockIsCappedAndSaysWhatItLeftOut(t *testing.T) {
	jobs := make([]*quaycrewv1.Job, 0, landedAtMost+3)
	for index := range landedAtMost + 3 {
		one := finishedAgo(aJob("a piece of work", job.PhaseDone), time.Duration(index)*time.Minute)
		one.Id = strings.Repeat("abcdef", 4)[:20] + string(rune('a'+index))
		jobs = append(jobs, one)
	}

	drawn := blockOf(t, jobs, "produced")
	if len(drawn.Rows) > landedAtMost {
		t.Errorf("the produced block drew %d rows, and the cap is %d", len(drawn.Rows), landedAtMost)
	}
	if drawn.More == "" {
		t.Error("the produced block was cut and says nothing about what it left out")
	}
	if !strings.Contains(drawn.More, "13") {
		t.Errorf("the block says %q, want it to say how many there are", drawn.More)
	}
}

// TestTheRunningBlockLeavesOutAJobWaitingOnAPerson. It is in the first block, and drawing it here as
// well would say the system is getting on with something that is stopped.
func TestTheRunningBlockLeavesOutAJobWaitingOnAPerson(t *testing.T) {
	if rows := blockOf(t, []*quaycrewv1.Job{aJob("choose the store", job.PhaseAsking)}, "running").Rows; len(rows) != 0 {
		t.Errorf("the running block drew a job that is waiting on a person: %+v", rows)
	}
}

// TestARowSaysWhereTheJobWasDeclared. A briefing over twenty jobs in four projects is unreadable
// without it, and an identifier is not a place.
func TestARowSaysWhereTheJobWasDeclared(t *testing.T) {
	one := aJob("read the electricity bill", job.PhaseRunning)
	one.Workspace, one.Project = "workspace-id", "project-id"
	one.Session = "0123456789abcdef01234567"

	rows := blockOf(t, []*quaycrewv1.Job{one}, "running").Rows
	if len(rows) != 1 {
		t.Fatalf("the running block drew %d rows, want 1", len(rows))
	}
	names := map[string]string{"workspace-id": "me", "project-id": "house-bills"}
	drawn := blocks([]*quaycrewv1.Job{one}, names)[3].Rows[0]
	if drawn.Place != "me/house-bills" {
		t.Errorf("the row says the job is in %q, want me/house-bills", drawn.Place)
	}
	if drawn.Session != "01234567" {
		t.Errorf("the row says the session is %q, want the short identifier", drawn.Session)
	}
}

// aJob is one job as the control plane hands it over, with the moments a row reads filled in.
func aJob(title, phase string) *quaycrewv1.Job {
	now := time.Now()
	return &quaycrewv1.Job{
		Id: "0123456789abcdef0123456f", Title: title, Phase: phase,
		CreatedAt: timestamppb.New(now.Add(-time.Hour)),
		UpdatedAt: timestamppb.New(now.Add(-2 * time.Minute)),
	}
}

func withReason(one *quaycrewv1.Job, reason string) *quaycrewv1.Job {
	one.Reason = reason
	return one
}

func finishedAgo(one *quaycrewv1.Job, ago time.Duration) *quaycrewv1.Job {
	one.FinishedAt = timestamppb.New(time.Now().Add(-ago))
	return one
}

// TestOneBigTreeIsDrawnWholeRatherThanCutToNothing is the cap's edge. Half a tree is worse than a
// long one: the rows left would hang off parents whose other children silently went, and a block that
// cut every branch it had would draw nothing at all while saying there were thirteen.
func TestOneBigTreeIsDrawnWholeRatherThanCutToNothing(t *testing.T) {
	root := aJob("ship the briefing", job.PhaseDone)
	root.Id = "1111111111111111111111aa"
	jobs := []*quaycrewv1.Job{finishedAgo(root, time.Minute)}
	for index := range landedAtMost + 2 {
		child := finishedAgo(aJob("a slice of it", job.PhaseDone), time.Duration(index+2)*time.Minute)
		child.Id = "222222222222222222222" + string(rune('a'+index)) + "bb"
		child.Parent = root.GetId()
		jobs = append(jobs, child)
	}

	drawn := blockOf(t, jobs, "produced")
	if len(drawn.Rows) != len(jobs) {
		t.Errorf("the produced block drew %d rows out of one tree of %d", len(drawn.Rows), len(jobs))
	}
	if drawn.More != "" {
		t.Errorf("the block says %q, and it left nothing out", drawn.More)
	}
}
