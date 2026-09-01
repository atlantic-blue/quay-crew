package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/forge"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// runPullRequestConformance holds both stores to what the crew keeps about a pull request it opened.
//
// This is the shape a new column gets wrong. A store that keeps the value in a struct and a store
// that has to name the column in a select, an insert and a scan are the same interface and different
// work, and the second one silently reads zero when a name is left out of one of the three. So the
// reading is written, read back through a second handle, and read again through the query that finds
// what is still worth reading.
func runPullRequestConformance(t *testing.T, newDataset func(t *testing.T) Opener) {
	t.Helper()

	// The absence first. A store that never wrote the columns at all passes every test below this one
	// if what it answers with is an empty word rather than the word unknown.
	t.Run("a pull request nothing has read reads as unknown, and is not green", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		id := aJobThatOpened(t, s, thePullRequestAddress)

		found, err := open(t).GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		reading := found.PullRequestState.Or()
		if reading.Status != forge.StatusUnknown || reading.Checks != forge.ChecksUnknown ||
			reading.Review != forge.ReviewUnknown {
			t.Fatalf("a pull request nothing read comes back as %+v", reading)
		}
		if reading.Taken() || reading.Red() || reading.Settled() {
			t.Fatalf("a pull request nothing read claims something: %+v", reading)
		}
		if !reading.ReadAt.IsZero() {
			t.Fatalf("a pull request nothing read was read at %s", reading.ReadAt)
		}
	})

	t.Run("recording a pull request against a job nothing holds is refused", func(t *testing.T) {
		s := newDataset(t)(t)
		err := s.RecordPullRequest(context.Background(), store.NewID(), forge.Unread())
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("recording against a job that does not exist answered %v, want ErrNotFound", err)
		}
	})

	t.Run("every word of a reading survives being written and read back", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		id := aJobThatOpened(t, s, thePullRequestAddress)

		read := time.Date(2026, 8, 31, 9, 30, 0, 0, time.UTC)
		if err := s.RecordPullRequest(ctx, id, forge.Reading{
			Status: forge.StatusOpen, Checks: forge.ChecksRed, FailedCheck: "integration",
			Review: forge.ReviewChangesRequested, ReadAt: read,
		}); err != nil {
			t.Fatalf("RecordPullRequest: %v", err)
		}

		// Reopened, because a column named in the write and left out of the read comes back empty
		// through a fresh handle and correct through the one that wrote it.
		found, err := open(t).GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		reading := found.PullRequestState
		if reading.Status != forge.StatusOpen || reading.Checks != forge.ChecksRed {
			t.Fatalf("the reading comes back as %q and %q", reading.Status, reading.Checks)
		}
		if reading.FailedCheck != "integration" {
			t.Fatalf("the failed check comes back as %q", reading.FailedCheck)
		}
		if reading.Review != forge.ReviewChangesRequested {
			t.Fatalf("the review comes back as %q", reading.Review)
		}
		if !reading.ReadAt.UTC().Equal(read) {
			t.Fatalf("the moment comes back as %s, want %s", reading.ReadAt.UTC(), read)
		}
		if !reading.Red() {
			t.Fatal("a red board does not read as red once it has been through the store")
		}

		// And a listing carries it too, because a listing is what a page draws from.
		listed, err := open(t).ListJobs(ctx, job.Filter{})
		if err != nil {
			t.Fatalf("ListJobs: %v", err)
		}
		if len(listed) != 1 || listed[0].PullRequestState.Checks != forge.ChecksRed {
			t.Fatalf("the listing says %d jobs and the first reads %+v", len(listed), listed[0].PullRequestState)
		}
	})

	t.Run("a reading that failed keeps its reason and stays unknown", func(t *testing.T) {
		open := newDataset(t)
		s := open(t)
		ctx := context.Background()
		id := aJobThatOpened(t, s, thePullRequestAddress)

		at := time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC)
		if err := s.RecordPullRequest(ctx, id, forge.Unreadable(at, "the rate limit is spent")); err != nil {
			t.Fatalf("RecordPullRequest: %v", err)
		}

		found, err := open(t).GetJob(ctx, id)
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		reading := found.PullRequestState
		if reading.Failed != "the rate limit is spent" {
			t.Fatalf("the reason comes back as %q", reading.Failed)
		}
		if reading.Status != forge.StatusUnknown || reading.Taken() {
			t.Fatalf("a failed reading comes back as %+v", reading)
		}
	})

	t.Run("what is still worth reading is what has not merged or closed", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		nothing := aJobThatOpened(t, s, "")
		open := aJobThatOpened(t, s, "https://github.com/atlantic-blue/quay-crew/pull/1")
		merged := aJobThatOpened(t, s, "https://github.com/atlantic-blue/quay-crew/pull/2")
		closed := aJobThatOpened(t, s, "https://github.com/atlantic-blue/quay-crew/pull/3")
		refused := aJobThatOpened(t, s, "https://github.com/atlantic-blue/quay-crew/pull/4")

		record(t, s, open, forge.Reading{Status: forge.StatusOpen, Checks: forge.ChecksPending})
		record(t, s, merged, forge.Reading{Status: forge.StatusMerged, Checks: forge.ChecksGreen})
		record(t, s, closed, forge.Reading{Status: forge.StatusClosed, Checks: forge.ChecksGreen})
		record(t, s, refused, forge.Unreadable(time.Now().UTC(), "the rate limit is spent"))

		unsettled, err := s.UnsettledPullRequests(ctx, 20)
		if err != nil {
			t.Fatalf("UnsettledPullRequests: %v", err)
		}
		held := map[string]bool{}
		for _, one := range unsettled {
			held[one.ID] = true
		}
		// A job that opened nothing has nothing to read, and a settled one is read once more and then
		// left alone. A refused reading is still worth another go: a failure that settled a pull
		// request would freeze it as unknown for ever.
		for _, one := range []struct {
			id     string
			listed bool
			what   string
		}{
			{nothing, false, "a job that opened no pull request"},
			{open, true, "an open pull request"},
			{merged, false, "a merged pull request"},
			{closed, false, "a closed pull request"},
			{refused, true, "a pull request the forge refused"},
		} {
			if held[one.id] != one.listed {
				t.Errorf("%s is on the reading list: %v, want %v", one.what, held[one.id], one.listed)
			}
		}
	})

	t.Run("the longest unread is read first, so a batch delays a reading and never starves one", func(t *testing.T) {
		s := newDataset(t)(t)
		ctx := context.Background()

		first := aJobThatOpened(t, s, "https://github.com/atlantic-blue/quay-crew/pull/11")
		second := aJobThatOpened(t, s, "https://github.com/atlantic-blue/quay-crew/pull/12")
		never := aJobThatOpened(t, s, "https://github.com/atlantic-blue/quay-crew/pull/13")

		read := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
		record(t, s, second, forge.Reading{Status: forge.StatusOpen, Checks: forge.ChecksPending, ReadAt: read})
		record(t, s, first, forge.Reading{
			Status: forge.StatusOpen, Checks: forge.ChecksPending, ReadAt: read.Add(-time.Hour),
		})

		unsettled, err := s.UnsettledPullRequests(ctx, 2)
		if err != nil {
			t.Fatalf("UnsettledPullRequests: %v", err)
		}
		if len(unsettled) != 2 {
			t.Fatalf("the limit returned %d rows, want 2", len(unsettled))
		}
		// Never read comes before read an hour ago, which comes before read a moment ago.
		if unsettled[0].ID != never || unsettled[1].ID != first {
			t.Fatalf("the reading order is %s then %s", unsettled[0].ID, unsettled[1].ID)
		}
	})
}

const thePullRequestAddress = "https://github.com/atlantic-blue/quay-crew/pull/549"

// aJobThatOpened is a finished job carrying one pull request address, and an address of nothing where
// the job opened none.
func aJobThatOpened(t *testing.T, s store.Store, address string) string {
	t.Helper()
	workspace, project := aProject(t, s)
	declared := &job.Job{
		ID: store.NewID(), Workspace: workspace, Project: project,
		Title: "read the electricity bill", Brief: "open the bill and say when it is due",
		Repository: "atlantic-blue/quay-crew", Phase: job.PhaseDone, PullRequest: address,
		Version: 1,
	}
	if err := s.CreateJob(context.Background(), declared, declaredEvent(declared)); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return declared.ID
}

func record(t *testing.T, s store.Store, id string, reading forge.Reading) {
	t.Helper()
	if err := s.RecordPullRequest(context.Background(), id, reading); err != nil {
		t.Fatalf("RecordPullRequest: %v", err)
	}
}
