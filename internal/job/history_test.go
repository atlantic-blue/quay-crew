package job_test

import (
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/krewe/internal/job"
)

// The arithmetic a reader trusts, and the two ways it can lie: a total taken over the page rather
// than the window, and a cap nobody is told about.

func at(day int) time.Time {
	return time.Date(2026, time.August, day, 12, 0, 0, 0, time.UTC)
}

func TestTheTotalCountsHowEveryJobEnded(t *testing.T) {
	total := job.Summarise([]*job.Digest{
		{Phase: job.PhaseDone}, {Phase: job.PhaseDone},
		{Phase: job.PhaseFailed},
		{Phase: job.PhaseStopped},
		{Phase: job.PhaseRunning}, {Phase: job.PhasePending},
	})
	if total.Jobs != 6 {
		t.Fatalf("the window holds %d jobs, want 6", total.Jobs)
	}
	if total.Done != 2 || total.Failed != 1 || total.Stopped != 1 {
		t.Fatalf("the endings read done %d, failed %d, stopped %d; want 2, 1, 1",
			total.Done, total.Failed, total.Stopped)
	}
	// Every phase that is not terminal counts as one word, so a reader is never left with jobs
	// unaccounted for between the total and the endings under it.
	if total.Unfinished != 2 {
		t.Fatalf("%d jobs are still going, want 2", total.Unfinished)
	}
}

// The check the issue asks for by name: a breakdown whose parts do not sum to the total is a number
// that will be trusted and is wrong.
func TestTheEndingsAddUpToTheTotal(t *testing.T) {
	total := job.Summarise([]*job.Digest{
		{Phase: job.PhaseDone}, {Phase: job.PhaseFailed}, {Phase: job.PhaseStopped},
		{Phase: job.PhaseAsking}, {Phase: job.PhaseWaiting}, {Phase: job.PhaseRunning},
		{Phase: job.PhasePending},
	})
	parts := total.Done + total.Failed + total.Stopped + total.Unfinished
	if parts != total.Jobs {
		t.Fatalf("the parts add to %d and the total says %d", parts, total.Jobs)
	}
}

func TestTheTotalAddsTheTokensAndTheSteers(t *testing.T) {
	total := job.Summarise([]*job.Digest{
		{Phase: job.PhaseDone, SpentToken: 1200, Steers: 1},
		{Phase: job.PhaseDone, SpentToken: 800, Steers: 2},
		{Phase: job.PhaseFailed, SpentToken: 40},
	})
	if total.SpentToken != 2040 {
		t.Fatalf("the window cost %d tokens, want 2040", total.SpentToken)
	}
	if total.Steers != 3 {
		t.Fatalf("the window took %d steers, want 3", total.Steers)
	}
}

// Counted once each, and only where one exists. A count of jobs that named a pull request is how
// much of a window reached a reviewer, and counting the empty string would say all of it did.
func TestOnlyJobsThatOpenedAPullRequestAreCounted(t *testing.T) {
	total := job.Summarise([]*job.Digest{
		{Phase: job.PhaseDone, PullRequest: "https://github.com/atlantic-blue/quay-crew/pull/531"},
		{Phase: job.PhaseDone, PullRequest: "https://github.com/atlantic-blue/quay-crew/pull/530"},
		{Phase: job.PhaseDone},
		{Phase: job.PhaseFailed},
	})
	if total.PullRequests != 2 {
		t.Fatalf("%d jobs opened a pull request, want 2", total.PullRequests)
	}
}

// A job that has not finished has no duration, and a system that guessed one would put a number in
// front of a reader that no clock ever measured.
func TestOnlyFinishedJobsContributeWorkingTime(t *testing.T) {
	started := at(28)
	total := job.Summarise([]*job.Digest{
		{Phase: job.PhaseDone, StartedAt: started, FinishedAt: started.Add(30 * time.Minute)},
		{Phase: job.PhaseDone, StartedAt: started, FinishedAt: started.Add(90 * time.Minute)},
		{Phase: job.PhaseRunning, StartedAt: started},
		{Phase: job.PhasePending},
	})
	if total.Working != 2*time.Hour {
		t.Fatalf("the window worked for %s, want 2h0m0s", total.Working)
	}
}

func TestAWindowLeavesOutWhatFallsOutsideIt(t *testing.T) {
	window := job.Window{Since: at(28), Until: at(30)}
	if window.Holds(at(27)) {
		t.Fatal("a job declared before the window is in it")
	}
	if !window.Holds(at(29)) {
		t.Fatal("a job declared inside the window is not in it")
	}
	// The far end is open, so two windows laid end to end count a job once rather than twice.
	if window.Holds(at(30)) {
		t.Fatal("the far end of the window is closed, so a job on the boundary is counted twice")
	}
	if !window.Holds(at(28)) {
		t.Fatal("the near end of the window is open, so a job on the boundary is counted never")
	}
}

func TestAWindowNobodyBoundedIsTheLastWeek(t *testing.T) {
	now := at(30)
	window, err := job.Window{}.Resolve(now)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !window.Until.Equal(now) {
		t.Fatalf("the window ends at %s, want %s", window.Until, now)
	}
	if want := now.Add(-job.DefaultWindow); !window.Since.Equal(want) {
		t.Fatalf("the window starts at %s, want %s", window.Since, want)
	}
}

func TestAWindowThatEndsBeforeItStartsIsRefused(t *testing.T) {
	_, err := job.Window{Since: at(30), Until: at(28)}.Resolve(at(31))
	if err == nil {
		t.Fatal("a window that ends before it starts was accepted")
	}
	// Named, because a caller that mixed the two ends up needs to be told which way round they go.
	if !strings.Contains(err.Error(), "ends before it starts") {
		t.Fatalf("the refusal reads %q, and does not say the window ends before it starts", err)
	}
}

func TestAWindowGivenOnlyAnEndReachesBackFromIt(t *testing.T) {
	window, err := job.Window{Until: at(30)}.Resolve(at(31))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := at(30).Add(-job.DefaultWindow); !window.Since.Equal(want) {
		t.Fatalf("the window starts at %s, want %s", window.Since, want)
	}
}

// The whole correctness of this read. A limit smaller than the window must not move the total, or a
// reader is given a summary of the page and no way to tell.
func TestTheTotalCoversTheWindowAndNotThePage(t *testing.T) {
	history := []*job.Digest{}
	for i := 0; i < 30; i++ {
		history = append(history, &job.Digest{Phase: job.PhaseDone, SpentToken: 100})
	}
	total := job.Summarise(history)
	page, leftOut := job.Page(history, 5)

	if len(page) != 5 {
		t.Fatalf("the page holds %d jobs, want 5", len(page))
	}
	if leftOut != 25 {
		t.Fatalf("%d jobs were left out, want 25", leftOut)
	}
	if total.Jobs != 30 || total.SpentToken != 3000 {
		t.Fatalf("the total reads %d jobs and %d tokens; want 30 and 3000, over the window rather "+
			"than over the page", total.Jobs, total.SpentToken)
	}
}

func TestAPageThatHoldsEverythingLeavesNothingOut(t *testing.T) {
	history := []*job.Digest{{Phase: job.PhaseDone}, {Phase: job.PhaseDone}}
	page, leftOut := job.Page(history, 50)
	if len(page) != 2 || leftOut != 0 {
		t.Fatalf("the page holds %d and left out %d, want 2 and 0", len(page), leftOut)
	}
}

func TestALimitIsDefaultedAndCapped(t *testing.T) {
	if got := job.HistoryLimit(0); got != 50 {
		t.Fatalf("no limit reads as %d, want the default of 50", got)
	}
	if got := job.HistoryLimit(-3); got != 50 {
		t.Fatalf("a negative limit reads as %d, want the default of 50", got)
	}
	if got := job.HistoryLimit(10_000); got != 500 {
		t.Fatalf("a limit above the ceiling reads as %d, want 500", got)
	}
	if got := job.HistoryLimit(7); got != 7 {
		t.Fatalf("a limit inside the range reads as %d, want 7", got)
	}
}

// A digest is the facts and nothing else. If this ever compiles against a brief or an answer, the
// read has stopped being one a session can afford.
func TestADigestCarriesTheFactsAndNotTheProse(t *testing.T) {
	started, finished := at(28), at(29)
	digest := job.DigestOf(&job.Job{
		ID: "job-1", Project: "project-1", Title: "write the post", Role: "marketing",
		Phase: job.PhaseDone, SpentTokens: 4200, PullRequest: "owner/name#12", Reason: "",
		Steers: 2, Brief: "a thousand words of brief", Answer: "a very long answer",
		Steps:     []job.Step{{Seq: 1, Summary: "read the history"}},
		CreatedAt: at(28), StartedAt: &started, FinishedAt: &finished,
	})
	if digest.Title != "write the post" || digest.Role != "marketing" || digest.SpentToken != 4200 {
		t.Fatalf("the digest reads %+v", digest)
	}
	if digest.PullRequest != "owner/name#12" || digest.Steers != 2 {
		t.Fatalf("the digest lost the pull request or the steers: %+v", digest)
	}
	if !digest.StartedAt.Equal(started) || !digest.FinishedAt.Equal(finished) {
		t.Fatalf("the digest reads the moments as %s and %s", digest.StartedAt, digest.FinishedAt)
	}
}

// A job that never started reads as not known rather than as the first of January year one, which is
// what the zero time would draw as.
func TestADigestOfAJobThatNeverRanHasNoMoments(t *testing.T) {
	digest := job.DigestOf(&job.Job{ID: "job-1", Phase: job.PhasePending, CreatedAt: at(28)})
	if !digest.StartedAt.IsZero() || !digest.FinishedAt.IsZero() {
		t.Fatalf("a job that never ran reads as started %s and finished %s",
			digest.StartedAt, digest.FinishedAt)
	}
}

func TestAHistoryIsNewestFirst(t *testing.T) {
	history := []*job.Digest{
		{ID: "b", CreatedAt: at(28)},
		{ID: "c", CreatedAt: at(30)},
		{ID: "a", CreatedAt: at(29)},
	}
	job.SortDigests(history)
	if history[0].ID != "c" || history[1].ID != "a" || history[2].ID != "b" {
		t.Fatalf("the history reads %s, %s, %s; want c, a, b",
			history[0].ID, history[1].ID, history[2].ID)
	}
}
