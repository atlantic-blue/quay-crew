package job

import (
	"fmt"
	"time"
)

// What waits for a person, in one reading, so every surface says the same thing.
//
// Four jobs stopped for a person on 1 September 2026 and nothing told him. The oldest waited more
// than one hour, and he found out because he asked what the state was. The transition wrote
// job.asked to the event log and nothing read it, while the briefing, the job listing and the
// console all answered the question and all three waited to be opened.
//
// So the reading moves here, where the control plane can hand it to every surface. This file holds
// the part that is a function of a row: which jobs wait, what each one wants, and how long it has
// been. Where the telling then goes is issue 614's other five pieces.

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

// DefaultWaiting is how long a job waits for a person before the telling names the age beside it.
//
// Fifteen minutes, and it is a guess. Nothing has measured it. The measurement that replaces it is
// the median time from job.asked to job.raised over one week of real jobs: a limit under that median
// names an age on every wait and stops being read, and one far above it says nothing while a person
// waits an hour. The one figure in hand is the incident this answers, where the oldest of four jobs
// waited more than one hour, so the guess is deliberately well under that.
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

// WaitingSince is when a job's wait started.
//
// It is the moment the row last moved, which for every one of the three kinds is the moment the wait
// began: the ask, the failure, or the reading that found the board red. The briefing already measures
// it this way, and a second reading of the same thing could only disagree with it.
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
