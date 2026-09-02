package job

import (
	"fmt"
	"time"

	quaycrewv1 "github.com/atlantic-blue/quay-krewe/gen/quaycrew/v1"
	"github.com/atlantic-blue/quay-krewe/internal/forge"
)

// What waits for a person, read off a row, so every surface says the same thing. Four jobs stopped
// on 1 September 2026 and nothing told anybody, because the briefing, the job listing and the console
// all answered this question and all three waited to be opened.

// The kinds of wait, in one word each. A surface prints the word, so they are what a person reads.
const (
	// WaitingAsking is a question on the row and nothing but an answer moves it.
	WaitingAsking = "asking"
	// WaitingBlocked is a job that failed, was stopped, or is held back by the machine. Nothing moves
	// it without a person either.
	WaitingBlocked = "blocked"
	// WaitingChecks is a job that ended and whose pull request the forge says is red. The job is over
	// and the work is not, which is a wait a listing of phases cannot see.
	WaitingChecks = "checks"
)

// DefaultWaiting is how long a job waits before the telling names the age beside it.
//
// Fifteen minutes, and it is a guess: nothing has measured it. What replaces it is the median gap
// between a wait starting and a surface naming it, over a week of real jobs, taken from the kinds
// that record when their wait began. A limit under that median names an age on every wait and stops
// being read; one far above it says nothing while a person waits an hour.
const DefaultWaiting = 15 * time.Minute

// Waiting is how long a job may wait here before the telling names the age, or the system's own
// where the workspace says nothing. Zero and below take the system's own: a workspace cannot turn
// the age off by setting a negative one.
func (l Limits) Waiting() time.Duration {
	if l.WaitingSeconds > 0 {
		return time.Duration(l.WaitingSeconds) * time.Second
	}
	return DefaultWaiting
}

// Waits says whether a job waits for a person, which kind of wait it is, and what it wants from
// them.
//
// The three kinds are the three ways work stops without anybody being told. A job that is asking
// wants an answer. A job that failed, was stopped, or is held back by a full machine wants a
// decision. A job that ended with a pull request the forge says is red wants somebody to open it.
//
// A flow at an ask node needs nothing of its own here: the engine writes the same phase and question
// onto the job that carries the run, so it arrives as an asking job.
func Waits(one *Job) (why, want string, waiting bool) {
	if one == nil {
		return "", "", false
	}
	switch {
	case one.Phase == PhaseAsking:
		return WaitingAsking, one.Question, true
	case one.Phase == PhaseFailed, one.Phase == PhaseStopped:
		// A job carrying a value in Resuming is going again, so it is not stuck. The briefing draws
		// the same distinction, and drawing it differently here would make two surfaces disagree about
		// one row.
		if one.Resuming != "" {
			return "", "", false
		}
		return WaitingBlocked, one.Reason, true
	case one.Phase == PhasePending && one.Reason != "":
		// Only the system writes a reason on a pending job, and only when it would not start it, so a
		// full machine and a broken system never read the same.
		return WaitingBlocked, one.Reason, true
	case one.PullRequestState.Red():
		return WaitingChecks, redCheck(one.PullRequestState.FailedCheck), true
	default:
		return "", "", false
	}
}

// redCheck is what a red board wants from a person, naming the check where the forge named one.
func redCheck(failed string) string {
	if failed == "" {
		return "the checks on its pull request are red"
	}
	return fmt.Sprintf("the check %s is red on its pull request", failed)
}

// WaitingSince is when a job's wait started, as every surface measures its age.
//
// It is the moment the row last moved, which is the ask for a job that is asking and the failure,
// the stop or the hold for a blocked one. A red board is the exception and it is worth naming: the
// forge reading writes what it read without touching the row, on purpose, so this is when the job
// ended rather than when the checks turned red. The age of that kind of wait therefore counts from
// the end of the work, which is earlier than the moment anybody could have acted on it.
//
// The briefing already measures it this way, and a second reading of the same thing could only
// disagree with it. Where the exact start of the wait matters rather than its age, WaitBegan says
// which kinds record one.
func WaitingSince(one *Job) time.Time {
	if one == nil {
		return time.Time{}
	}
	return one.UpdatedAt
}

// Waited is how long a wait has lasted, written the way a person says it: "1 hour 4 minutes".
//
// Spelled out rather than compact, because this is read in a sentence rather than in a column, and
// "1h4m" in the middle of a line about a job that stopped an hour ago reads as a setting.
func Waited(elapsed time.Duration) string {
	if elapsed < time.Minute {
		return plural(int(elapsed.Seconds()), "second")
	}
	if elapsed < time.Hour {
		return plural(int(elapsed.Minutes()), "minute")
	}
	hours := int(elapsed.Hours())
	minutes := int(elapsed.Minutes()) - hours*60
	if minutes == 0 {
		return plural(hours, "hour")
	}
	return plural(hours, "hour") + " " + plural(minutes, "minute")
}

func plural(count int, unit string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", count, unit)
}

// WaitsOn is Waits read off the record a caller holds rather than off a row.
//
// The command line reads a job back through the control plane, so it holds this shape and not the
// one above. The two answer the same question and a test holds them to the same word for the same
// job: a surface that decided the kind of wait for itself is how two surfaces come to disagree
// about one job.
func WaitsOn(one *quaycrewv1.Job) (why, want string, waiting bool) {
	if one == nil {
		return "", "", false
	}
	reading := forge.Reading{Checks: one.GetPullRequestChecks()}
	switch {
	case one.GetPhase() == PhaseAsking:
		return WaitingAsking, one.GetQuestion(), true
	case one.GetPhase() == PhaseFailed, one.GetPhase() == PhaseStopped:
		if one.GetResuming() != "" {
			return "", "", false
		}
		return WaitingBlocked, one.GetReason(), true
	case one.GetPhase() == PhasePending && one.GetReason() != "":
		return WaitingBlocked, one.GetReason(), true
	case reading.Red():
		return WaitingChecks, redCheck(one.GetPullRequestCheck()), true
	default:
		return "", "", false
	}
}

// WaitBegan is when the wait a job is in now started, and false where the row records no moment for
// it.
//
// The gap this is half of is the number the telling is judged on, so it has to belong to the wait a
// person is in now. asked_at is written at the question and nothing clears it, so a job that asked,
// was answered, ran on and then failed still carries that first moment. Dating the later wait from
// it reports the answer and the whole run as time somebody spent not knowing: on a job that stopped
// ten minutes ago and was named a minute later, it read two hours fifty one minutes.
//
// Two of the three kinds record their start. A question stamps the moment in the same statement that
// moves the phase. A job that failed, was stopped or is held moved the row, so the row's own moment
// is when it stopped. Nothing records when a board turned red, because the forge reading writes what
// it read without touching the row and says so where it is written, so that kind answers false
// rather than reaching for the nearest moment lying on the row.
func WaitBegan(why string, asked *time.Time, moved time.Time) (time.Time, bool) {
	switch why {
	case WaitingAsking:
		// A question with no moment on it predates the column. The row still moved when it was
		// asked, which is the same moment for every asking transition that writes both.
		if asked == nil {
			return moved, !moved.IsZero()
		}
		return *asked, true
	case WaitingBlocked:
		return moved, !moved.IsZero()
	default:
		return time.Time{}, false
	}
}
