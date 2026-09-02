package job_test

import (
	"strings"
	"testing"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/forge"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// What waits for a person, read off a row. Four jobs stopped on 1 September 2026 and nothing told
// anybody, so this is the reading that decides whether a surface says anything at all.

// The three kinds of wait, and what each one wants. A job that failed and a job that asked both need
// a person, and a surface that only knew about questions would leave the failures silent.
func TestEveryKindOfWaitIsRead(t *testing.T) {
	for _, one := range []struct {
		name string
		row  *job.Job
		why  string
		want string
	}{
		{
			name: "a question a person has to answer",
			row:  &job.Job{Phase: job.PhaseAsking, Question: "aurora or a key value store?"},
			why:  job.WaitingAsking,
			want: "aurora or a key value store?",
		},
		{
			name: "a job that failed",
			row:  &job.Job{Phase: job.PhaseFailed, Reason: "the sandbox could not be made"},
			why:  job.WaitingBlocked,
			want: "the sandbox could not be made",
		},
		{
			name: "a job somebody stopped",
			row:  &job.Job{Phase: job.PhaseStopped, Reason: "the operator stopped it"},
			why:  job.WaitingBlocked,
			want: "the operator stopped it",
		},
		{
			name: "a job the machine has no room for",
			row:  &job.Job{Phase: job.PhasePending, Reason: "there is not enough memory"},
			why:  job.WaitingBlocked,
			want: "there is not enough memory",
		},
		{
			name: "a pull request the forge says is red",
			row: &job.Job{Phase: job.PhaseDone, PullRequest: "https://github.com/acme/x/pull/1",
				PullRequestState: forge.Reading{Status: forge.StatusOpen, Checks: forge.ChecksRed, FailedCheck: "unit"}},
			why:  job.WaitingChecks,
			want: "unit",
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			why, want, waiting := job.Waits(one.row)
			if !waiting {
				t.Fatalf("this waits for a person and the reading says it does not: %+v", one.row)
			}
			if why != one.why {
				t.Errorf("the wait reads as %q, want %q", why, one.why)
			}
			if !strings.Contains(want, one.want) {
				t.Errorf("what it wants reads as %q, and does not say %q", want, one.want)
			}
		})
	}
}

// The quiet cases, which matter as much as the loud ones: a telling that fires when nothing waits is
// worse than no telling, because a person learns to ignore it and then ignores the real one.
func TestNothingThatIsWorkingReadsAsWaiting(t *testing.T) {
	for _, one := range []struct {
		name string
		row  *job.Job
	}{
		{"a job that is running", &job.Job{Phase: job.PhaseRunning}},
		{"a job waiting its turn", &job.Job{Phase: job.PhasePending}},
		{"a job held back by something it waits for", &job.Job{Phase: job.PhaseWaiting}},
		{"a job that finished", &job.Job{Phase: job.PhaseDone}},
		{
			name: "a job that finished with a green board",
			row: &job.Job{Phase: job.PhaseDone, PullRequest: "https://github.com/acme/x/pull/1",
				PullRequestState: forge.Reading{Status: forge.StatusOpen, Checks: forge.ChecksGreen}},
		},
		{
			name: "a job whose board nobody has read",
			row: &job.Job{Phase: job.PhaseDone, PullRequest: "https://github.com/acme/x/pull/1",
				PullRequestState: forge.Unread()},
		},
		{
			name: "a failure a person already answered",
			row:  &job.Job{Phase: job.PhaseFailed, Reason: "it stopped", Resuming: "carrying on past it"},
		},
		{"nothing at all", nil},
	} {
		t.Run(one.name, func(t *testing.T) {
			if why, _, waiting := job.Waits(one.row); waiting {
				t.Errorf("this reads as waiting for a person, as %q: %+v", why, one.row)
			}
		})
	}
}

// The limit is a workspace's where it set one, and the system's guess where it did not. Zero is not
// off: a workspace that turned the age off would be a workspace where an hour long wait reads the
// same as a wait of one second.
func TestTheWaitingLimitIsTheWorkspacesOrTheSystemsOwn(t *testing.T) {
	if held := (job.Limits{WaitingSeconds: 60}).Waiting(); held != time.Minute {
		t.Errorf("a workspace that set one minute reads back %s", held)
	}
	if held := (job.Limits{}).Waiting(); held != job.DefaultWaiting {
		t.Errorf("a workspace that set nothing reads back %s, want the system's own %s", held, job.DefaultWaiting)
	}
	if held := (job.Limits{WaitingSeconds: -30}).Waiting(); held != job.DefaultWaiting {
		t.Errorf("a limit below zero reads back %s rather than the system's own", held)
	}
}

// The system's own is fifteen minutes, and it is a guess. The test says the number out loud so
// changing it is a decision somebody makes rather than a line that slid.
func TestTheSystemsOwnWaitIsFifteenMinutes(t *testing.T) {
	if job.DefaultWaiting != 15*time.Minute {
		t.Errorf("the system's own wait is %s", job.DefaultWaiting)
	}
}

// The age is read in a sentence, so it is written the way somebody says it. "1h4m0s" in the middle
// of a line about a job that stopped an hour ago reads as a setting rather than as a length of time.
func TestAnAgeIsWrittenTheWayAPersonSaysIt(t *testing.T) {
	for _, one := range []struct {
		elapsed time.Duration
		says    string
	}{
		{0, "0 seconds"},
		{time.Second, "1 second"},
		{45 * time.Second, "45 seconds"},
		{time.Minute, "1 minute"},
		{16 * time.Minute, "16 minutes"},
		{time.Hour, "1 hour"},
		{64 * time.Minute, "1 hour 4 minutes"},
		{3 * time.Hour, "3 hours"},
		{25*time.Hour + 30*time.Minute, "25 hours 30 minutes"},
	} {
		if said := job.Waited(one.elapsed); said != one.says {
			t.Errorf("%s reads as %q, want %q", one.elapsed, said, one.says)
		}
	}
}

// The wait is measured from the moment the row last moved, which is when it stopped. Measuring it
// from the declaration would say a job that ran for a day had been waiting for a day.
func TestTheWaitIsMeasuredFromWhenTheRowStopped(t *testing.T) {
	stopped := time.Now().Add(-90 * time.Minute)
	one := &job.Job{Phase: job.PhaseAsking, CreatedAt: stopped.Add(-24 * time.Hour), UpdatedAt: stopped}
	if since := job.WaitingSince(one); !since.Equal(stopped) {
		t.Errorf("the wait started at %s, want %s", since, stopped)
	}
}
