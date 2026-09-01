package job_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/forge"
	"github.com/atlantic-blue/quay-krewe/internal/job"
	"github.com/atlantic-blue/quay-krewe/internal/store"
)

// The crew keeps a pull request address and reads it back. What is specified here is what the row
// then says, and above all what it says when the reading did not happen.

const theAddress = "https://github.com/atlantic-blue/quay-crew/pull/549"

// theMomentItWasRead is a fixed clock, so a test can say when a reading was taken rather than assert
// that some moment is near another one.
var theMomentItWasRead = time.Date(2026, 8, 31, 9, 30, 0, 0, time.UTC)

// aJobThatOpened is a finished job with a pull request against its repository, in a store.
func aJobThatOpened(t *testing.T, address string) (*store.Memory, string) {
	t.Helper()
	kept := store.NewMemory()
	declared := &job.Job{
		ID: store.NewID(), Workspace: "w", Project: "p", Title: "read the electricity bill",
		Brief: "open the bill and say when it is due", Repository: "atlantic-blue/quay-crew",
		Phase: job.PhaseDone, PullRequest: address, Version: 1,
	}
	if err := kept.CreateJob(context.Background(), declared, &job.Event{
		ID: store.NewID(), Kind: job.EventDeclared, Job: declared.ID,
		Workspace: "w", Project: "p", OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	return kept, declared.ID
}

func readingOf(t *testing.T, kept *store.Memory, id string) forge.Reading {
	t.Helper()
	found, err := kept.GetJob(context.Background(), id)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	return found.PullRequestState
}

// watching is a watcher over one store and one forge, on a clock a test owns.
func watching(kept job.PullRequestStore, reader forge.Reader) *job.Watcher {
	return job.NewWatcher(kept, reader, time.Minute).At(func() time.Time { return theMomentItWasRead })
}

// The sad paths first. A reading nobody took must never read as a passing one, which is the whole
// reason this exists: an operator picks up what the crew says is stuck, and a pull request that reads
// as fine because nothing could read it is the one they will not look at.

func TestAPullRequestTheCrewCouldNotReadSaysUnknown(t *testing.T) {
	kept, id := aJobThatOpened(t, theAddress)
	refusing := forge.NewFake().Refuses(theAddress, "the rate limit is spent")

	watching(kept, refusing).Tick(context.Background())

	reading := readingOf(t, kept, id)
	if reading.Status != forge.StatusUnknown || reading.Checks != forge.ChecksUnknown {
		t.Fatalf("a pull request nobody could read says %q and %q", reading.Status, reading.Checks)
	}
	if reading.Red() || reading.Taken() {
		t.Fatalf("a failed reading claims something: %+v", reading)
	}
	if !strings.Contains(reading.Failed, "rate limit") {
		t.Fatalf("the reading says %q about why it failed", reading.Failed)
	}
	if !reading.ReadAt.Equal(theMomentItWasRead) {
		t.Fatalf("the attempt was stamped %s", reading.ReadAt)
	}
}

// A system with no forge credential is the state every fresh crew is in, and the reason it will most
// often carry. The refusal has to reach the row, not only a log line nobody reads.
func TestASystemWithNoForgeCredentialSaysSoOnTheRow(t *testing.T) {
	kept, id := aJobThatOpened(t, theAddress)

	watching(kept, &forge.GitHub{}).Tick(context.Background())

	reading := readingOf(t, kept, id)
	if reading.Status != forge.StatusUnknown {
		t.Fatalf("with no credential the pull request reads as %q", reading.Status)
	}
	if !strings.Contains(reading.Failed, "krewe secret set system "+forge.TokenName) {
		t.Fatalf("the reason is %q, and it never says how to set a credential", reading.Failed)
	}
}

// A reading that fails must not leave the words of an older one standing. A status that stopped being
// true is worse than no status: the operator acts on it either way, and only one of the two tells
// them they are acting on nothing.
func TestAFailedReadingClearsTheOneBeforeIt(t *testing.T) {
	kept, id := aJobThatOpened(t, theAddress)
	answering := forge.NewFake().Says(theAddress, forge.Reading{
		Status: forge.StatusOpen, Checks: forge.ChecksGreen, Review: forge.ReviewNone,
	})
	watching(kept, answering).Tick(context.Background())
	if got := readingOf(t, kept, id).Checks; got != forge.ChecksGreen {
		t.Fatalf("the first reading says %q", got)
	}

	answering.Refuses(theAddress, "github.com answered 502 Bad Gateway")
	watching(kept, answering).Tick(context.Background())

	reading := readingOf(t, kept, id)
	if reading.Checks != forge.ChecksUnknown || reading.Status != forge.StatusUnknown {
		t.Fatalf("a failed reading left %q and %q standing", reading.Status, reading.Checks)
	}
}

// An address the system cannot read is refused before any call is made, because a number it guessed
// is a number it would then go and ask a forge about.
func TestAnAddressThatIsNotAPullRequestIsNeverAskedAbout(t *testing.T) {
	kept, id := aJobThatOpened(t, "https://github.com/atlantic-blue/quay-crew")
	asked := forge.NewFake()

	watching(kept, asked).Tick(context.Background())

	if asked.Asked("https://github.com/atlantic-blue/quay-crew") != 0 {
		t.Fatal("the forge was asked about something that is not a pull request address")
	}
	reading := readingOf(t, kept, id)
	if reading.Status != forge.StatusUnknown || reading.Failed == "" {
		t.Fatalf("the reading is %+v, and it says nothing about why", reading)
	}
}

// A system built with no forge at all reads nothing rather than reporting something it did not read.
func TestASystemWithNoForgeReadsNothing(t *testing.T) {
	kept, id := aJobThatOpened(t, theAddress)

	job.NewWatcher(kept, nil, time.Minute).Tick(context.Background())

	if reading := readingOf(t, kept, id); reading.Taken() || reading.Failed != "" {
		t.Fatalf("a system with no forge wrote %+v", reading)
	}
}

// And then what a forge that answers puts on the row.
func TestWhatTheForgeSaidLandsOnTheJob(t *testing.T) {
	for _, one := range []struct {
		name    string
		said    forge.Reading
		reads   string
		settled bool
	}{
		{
			name:  "open, with its checks still running",
			said:  forge.Reading{Status: forge.StatusOpen, Checks: forge.ChecksPending, Review: forge.ReviewNone},
			reads: "open, checks pending",
		},
		{
			name:    "merged",
			said:    forge.Reading{Status: forge.StatusMerged, Checks: forge.ChecksGreen, Review: forge.ReviewApproved},
			reads:   "merged, checks green",
			settled: true,
		},
		{
			name:    "closed without merging",
			said:    forge.Reading{Status: forge.StatusClosed, Checks: forge.ChecksGreen, Review: forge.ReviewNone},
			reads:   "closed, checks green",
			settled: true,
		},
		{
			name: "a check went red, and the check is named",
			said: forge.Reading{
				Status: forge.StatusOpen, Checks: forge.ChecksRed, FailedCheck: "integration",
				Review: forge.ReviewNone,
			},
			reads: "open, checks red: integration",
		},
		{
			name: "a review asked for changes",
			said: forge.Reading{
				Status: forge.StatusOpen, Checks: forge.ChecksGreen, Review: forge.ReviewChangesRequested,
			},
			reads: "a review asked for changes",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			kept, id := aJobThatOpened(t, theAddress)
			watching(kept, forge.NewFake().Says(theAddress, one.said)).Tick(context.Background())

			reading := readingOf(t, kept, id)
			if !strings.Contains(reading.String(), one.reads) {
				t.Fatalf("the job reads as %q, and never says %q", reading, one.reads)
			}
			if reading.Settled() != one.settled {
				t.Fatalf("settled = %v, want %v", reading.Settled(), one.settled)
			}
			if !reading.Taken() {
				t.Fatalf("a reading that happened says it did not: %+v", reading)
			}
			if reading.Failed != "" {
				t.Fatalf("a reading that happened says it failed: %q", reading.Failed)
			}
		})
	}
}

// A merged or closed pull request is read once more and then left alone. Without this the crew pays
// one call every two minutes, for ever, for every pull request it ever opened.
func TestASettledPullRequestIsNotReadAgain(t *testing.T) {
	kept, _ := aJobThatOpened(t, theAddress)
	answering := forge.NewFake().Says(theAddress, forge.Reading{
		Status: forge.StatusMerged, Checks: forge.ChecksGreen, Review: forge.ReviewApproved,
	})
	watcher := watching(kept, answering)

	watcher.Tick(context.Background())
	if asked := answering.Asked(theAddress); asked != 1 {
		t.Fatalf("the first tick asked %d times", asked)
	}

	watcher.Tick(context.Background())
	watcher.Tick(context.Background())
	if asked := answering.Asked(theAddress); asked != 1 {
		t.Fatalf("a settled pull request was read %d times", asked)
	}
}

// An open one is read again, which is the other half of the same rule: settling is what stops the
// reading, and nothing else does.
func TestAnOpenPullRequestIsReadOnEveryTick(t *testing.T) {
	kept, _ := aJobThatOpened(t, theAddress)
	answering := forge.NewFake().Says(theAddress, forge.Reading{
		Status: forge.StatusOpen, Checks: forge.ChecksPending, Review: forge.ReviewNone,
	})
	watcher := watching(kept, answering)

	watcher.Tick(context.Background())
	watcher.Tick(context.Background())
	if asked := answering.Asked(theAddress); asked != 2 {
		t.Fatalf("an open pull request was read %d times over two ticks", asked)
	}
}

// A job that opened nothing is never read, so a crew whose work does not go near a forge pays for
// none of this.
func TestAJobWithNoPullRequestIsNeverRead(t *testing.T) {
	kept := store.NewMemory()
	declared := &job.Job{
		ID: store.NewID(), Workspace: "w", Project: "p", Title: "read the electricity bill",
		Brief: "open the bill", Phase: job.PhaseDone, Version: 1,
	}
	if err := kept.CreateJob(context.Background(), declared, &job.Event{
		ID: store.NewID(), Kind: job.EventDeclared, Job: declared.ID, Workspace: "w", Project: "p",
		OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	open, err := kept.UnsettledPullRequests(context.Background(), 20)
	if err != nil {
		t.Fatalf("UnsettledPullRequests: %v", err)
	}
	if len(open) != 0 {
		t.Fatalf("a job that opened no pull request is on the reading list: %d rows", len(open))
	}
}

// One tick reads a bounded number of pull requests, longest unread first, so a crew holding two
// hundred open ones does not make two hundred calls at once. Nothing is starved: the ones this tick
// left are the ones the next tick reads first.
func TestOneTickReadsABoundedNumberLongestUnreadFirst(t *testing.T) {
	kept := store.NewMemory()
	ctx := context.Background()
	addresses := []string{
		"https://github.com/atlantic-blue/quay-crew/pull/1",
		"https://github.com/atlantic-blue/quay-crew/pull/2",
		"https://github.com/atlantic-blue/quay-crew/pull/3",
	}
	answering := forge.NewFake()
	declaredAt := time.Date(2026, 8, 30, 8, 0, 0, 0, time.UTC)
	for i, address := range addresses {
		declared := &job.Job{
			ID: store.NewID(), Workspace: "w", Project: "p", Title: address,
			Brief: "open the bill", Repository: "atlantic-blue/quay-crew", Phase: job.PhaseDone,
			PullRequest: address, Version: 1,
			CreatedAt: declaredAt.Add(time.Duration(i) * time.Hour),
		}
		if err := kept.CreateJob(ctx, declared, &job.Event{
			ID: store.NewID(), Kind: job.EventDeclared, Job: declared.ID, Workspace: "w", Project: "p",
			OccurredAt: declaredAt,
		}); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}
		answering.Says(address, forge.Reading{
			Status: forge.StatusOpen, Checks: forge.ChecksPending, Review: forge.ReviewNone,
		})
	}

	watching(kept, answering).Batch(2).Tick(ctx)

	// The two oldest declared, because none of the three has ever been read.
	if answering.Asked(addresses[0]) != 1 || answering.Asked(addresses[1]) != 1 {
		t.Fatalf("the first tick read %d and %d of the two oldest",
			answering.Asked(addresses[0]), answering.Asked(addresses[1]))
	}
	if got := answering.Asked(addresses[2]); got != 0 {
		t.Fatalf("the tick read %d past its batch", got)
	}

	// And the one it left is the one the next tick reads, because the two it read are no longer the
	// longest unread.
	job.NewWatcher(kept, answering, time.Minute).
		At(func() time.Time { return theMomentItWasRead.Add(time.Hour) }).Batch(2).Tick(ctx)
	if got := answering.Asked(addresses[2]); got != 1 {
		t.Fatalf("the pull request the first tick left was read %d times", got)
	}
}
