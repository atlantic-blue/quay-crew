package job_test

import (
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// A claim is a job's hold on a piece of work in the world. What is proved here is when it ends,
// because a claim that never ends deadlocks the system the first time a container dies, and every
// test about taking one passes just the same.

// The expiry first. A claim system with no expiry passes every test about claiming.
func TestAClaimOnAJobNothingHasMovedRunsOut(t *testing.T) {
	crashed := &job.Job{
		Claim: "atlantic-blue/quay-krewe#540", Phase: job.PhaseRunning,
		UpdatedAt: time.Now().UTC().Add(-job.ClaimLife - time.Minute),
	}
	if crashed.Holding(time.Now().UTC()) {
		t.Fatalf("a job that has not moved for %s still holds its claim, so one dead session holds a piece "+
			"of work forever", job.ClaimLife)
	}
}

// The other side of the same rule. A job whose controller is renewing its lease moves on every tick,
// so a claim that ran out while the work was in flight would be worse than no claim at all.
func TestAClaimOnAMovingJobHolds(t *testing.T) {
	running := &job.Job{
		Claim: "atlantic-blue/quay-krewe#540", Phase: job.PhaseRunning,
		UpdatedAt: time.Now().UTC().Add(-time.Minute),
	}
	if !running.Holding(time.Now().UTC()) {
		t.Fatal("a job that moved a minute ago does not hold its claim, so a second job would take work in flight")
	}
}

// Settling is the ordinary end of a claim, and it is every terminal phase rather than the one a
// happy path takes: work that failed is work somebody else should be able to pick up.
func TestAClaimEndsWhenTheJobSettles(t *testing.T) {
	for _, phase := range []string{job.PhaseDone, job.PhaseFailed, job.PhaseStopped} {
		settled := &job.Job{Claim: "atlantic-blue/quay-krewe#540", Phase: phase, UpdatedAt: time.Now().UTC()}
		if settled.Holding(time.Now().UTC()) {
			t.Errorf("a %s job still holds its claim, so the work it did not finish is held by nobody working on it", phase)
		}
	}
}

func TestALiveJobHoldsItsClaimInEveryPhaseNothingEnded(t *testing.T) {
	for _, phase := range []string{job.PhasePending, job.PhaseWaiting, job.PhaseRunning, job.PhaseAsking} {
		live := &job.Job{Claim: "atlantic-blue/quay-krewe#540", Phase: phase, UpdatedAt: time.Now().UTC()}
		if !live.Holding(time.Now().UTC()) {
			t.Errorf("a %s job does not hold its claim, so a second job takes work this one has not finished", phase)
		}
	}
}

func TestAJobClaimingNothingHoldsNothing(t *testing.T) {
	nothing := &job.Job{Phase: job.PhaseRunning, UpdatedAt: time.Now().UTC()}
	if nothing.Holding(time.Now().UTC()) {
		t.Fatal("a job with no claim holds a piece of work, so every job would block every other")
	}
}

// Two people naming the same piece of work write it two ways. A claim that misses over a capital
// letter or a stray space is a claim that did nothing.
func TestTheSamePieceOfWorkWrittenTwoWaysIsOneClaim(t *testing.T) {
	for _, written := range []string{
		"atlantic-blue/quay-krewe#540",
		"  atlantic-blue/quay-krewe#540 ",
		"Atlantic-Blue/Quay-Krewe#540",
		"atlantic-blue/quay-krewe#540\n",
	} {
		if tidy := job.TidyClaim(written); tidy != "atlantic-blue/quay-krewe#540" {
			t.Errorf("%q is stored as %q, so it would not meet the same claim written plainly", written, tidy)
		}
	}
	if tidy := job.TidyClaim("the   payments  page"); tidy != "the payments page" {
		t.Errorf("a claim with space inside it is stored as %q", tidy)
	}
}

// A claim longer than a title is accepted, where it used to lose the whole declaration.
//
// The guide stays: a claim is one line naming a piece of work, and a surface that draws it in a
// column cuts it there. Nothing refuses it.
func TestAClaimLongerThanATitleIsAccepted(t *testing.T) {
	claim := strings.Repeat("c", job.ClaimLimit+1)
	if err := (job.Declaration{
		Title: "read the electricity bill", Brief: "open it", Claim: claim,
	}).Validate(); err != nil {
		t.Fatalf("a claim of %d bytes was refused: %v", len(claim), err)
	}
}

func TestAnOrdinaryClaimIsDeclared(t *testing.T) {
	if err := (job.Declaration{
		Title: "fix the defect", Brief: "fix it", Claim: "atlantic-blue/quay-krewe#540",
	}).Validate(); err != nil {
		t.Fatalf("an ordinary claim was refused: %v", err)
	}
}

// The refusal is the whole product of this rule: what a second caller reads is the difference
// between going to look at the other job and building the same thing again.
func TestTheRefusalNamesTheHolderAndHowOldTheClaimIs(t *testing.T) {
	now := time.Now().UTC()
	held := &job.Held{
		Claim: "atlantic-blue/quay-krewe#540", Holder: "0123456789abcdef01234567",
		Title: "a job claims the work it holds", TakenAt: now.Add(-14 * time.Minute),
	}
	refusal := held.Refusal(now)
	for _, want := range []string{
		"atlantic-blue/quay-krewe#540", "0123456789abcdef01234567", "a job claims the work it holds",
		"14 minutes", "krewe job show",
	} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal does not say %q: %s", want, refusal)
		}
	}
}

func TestHowOldAClaimIsReadsInTheUnitThatSaysSomething(t *testing.T) {
	for _, one := range []struct {
		since time.Duration
		want  string
	}{
		{20 * time.Second, "less than a minute"},
		{90 * time.Second, "a minute"},
		{14 * time.Minute, "14 minutes"},
		{2*time.Hour + 5*time.Minute, "2h5m"},
	} {
		if got := job.ClaimAge(one.since); got != one.want {
			t.Errorf("a claim taken %s ago reads as %q, want %q", one.since, got, one.want)
		}
	}
}
