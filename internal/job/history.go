package job

import (
	"fmt"
	"sort"
	"time"
)

// History is what the crew did over a window of time, which is the one question a session could not
// ask before.
//
// A session reads the repository it stands in and nothing else. It cannot read the jobs that ran,
// what they cost, or why one failed, so the operator had to type those facts into a brief by hand:
// one job to write about two days of the crew's own work took 1,109 words, almost all of them
// already in the crew's own database. See issue 543.
//
// Two shapes were rejected on the way here. A context level holds text somebody wrote, and it is
// stale the moment the next job runs. A skill holds a method, and the method here is one sentence:
// what was missing is the data. This is computed from the store on every read, so it cannot go stale,
// and it is bounded by a window, so it cannot become the dump that costs a reader its whole context.

// The window a history covers when the caller does not say. Bounded on purpose: a history with no
// window is every job the crew ever ran, which is the dump this read exists to avoid.
const DefaultWindow = 7 * 24 * time.Hour

// How many jobs a history returns. The window is the first bound and this is the second, for a busy
// week that fills one.
const (
	defaultHistoryLimit = 50
	maxHistoryLimit     = 500
)

// HistoryLimit applies the default when nothing was asked for and the ceiling when too much was, the
// way a task listing does.
func HistoryLimit(limit int) int {
	switch {
	case limit <= 0:
		return defaultHistoryLimit
	case limit > maxHistoryLimit:
		return maxHistoryLimit
	default:
		return limit
	}
}

// Window is the span of time a history covers, read against when each job was declared.
//
// Declared rather than finished, because a job that is still running has no finish to be read
// against, and a window read against finishing would move a job out of the day somebody asked for it
// and into the day it happened to end.
type Window struct {
	Since time.Time
	Until time.Time
}

// Resolve fills in what the caller left out and refuses a window that could hold nothing.
//
// An unset Until is now, and an unset Since is the default window back from Until. Both are resolved
// here rather than in the store, so the two stores cannot disagree about what "the last week" means,
// and so the answer can say which window it actually read.
func (w Window) Resolve(now time.Time) (Window, error) {
	if w.Until.IsZero() {
		w.Until = now
	}
	if w.Since.IsZero() {
		w.Since = w.Until.Add(-DefaultWindow)
	}
	w.Since, w.Until = w.Since.UTC(), w.Until.UTC()
	if w.Until.Before(w.Since) {
		return Window{}, fmt.Errorf("the window ends before it starts: %s is after %s",
			w.Since.Format(time.RFC3339), w.Until.Format(time.RFC3339))
	}
	return w, nil
}

// Holds says whether a moment falls in the window. The start is included and the end is not, so two
// windows laid end to end count each job once rather than twice.
func (w Window) Holds(at time.Time) bool {
	return !at.Before(w.Since) && at.Before(w.Until)
}

// Digest is one job reduced to the facts a reader needs to say what happened.
//
// No brief, no answer and no steps. Those are what make a job too large to read a hundred of, and
// leaving them out is the whole reason a session can read a week of work and still have room to do
// any.
type Digest struct {
	ID      string
	Project string
	Title   string
	Role    string
	Phase   string
	// SpentToken is what this job cost: its own session, and every run of every stage under it.
	//
	// The runs are counted here because they are what the job spent. A stage that fans out buys one
	// session for each requirement, and those sessions are most of what a job costs, so a number that
	// left them out would tell an operator a week of work was cheap. They used to be job rows of their
	// own and were counted as separate lines; they are not jobs, so the job they belong to carries
	// what they cost. See Runs below for how many there were.
	SpentToken int64
	// Runs is how many runs of its stages this job had, which is what explains the cost above. Zero
	// for a job that never fanned out.
	Runs int
	// PullRequest is the address this job's own answer named, and empty for a job that opened none.
	PullRequest string
	// Opened is every address the runs of this job's stages opened, which is where the work of a job
	// that fanned out actually is: the job itself often names none. They are counted once each across
	// a window, because one requirement has one pull request and two runs land in it.
	Opened []string
	// Reason is why a failed or stopped job ended. It is on the digest because "what failed and why"
	// is one question: a reader who has to ask again for every failure is back where they started.
	Reason     string
	Steers     int
	CreatedAt  time.Time
	StartedAt  time.Time
	FinishedAt time.Time
}

// Totals is a window of jobs added up.
//
// The arithmetic lives here rather than in either store, for the reason a session's last moved time
// does: it is a fact about a window of jobs and not a fact about where those jobs are kept. Both
// stores filter and order, and neither counts, so neither can disagree with the other about a number
// nobody could check.
type Totals struct {
	Jobs       int
	Done       int
	Failed     int
	Stopped    int
	Unfinished int
	SpentToken int64
	// PullRequests is how many jobs named one, which is how much of the window reached a reviewer.
	PullRequests int
	Steers       int
	// Working is the time jobs spent running, summed over those that both started and finished. A job
	// still running adds nothing: its duration is not known yet, and guessing it would put a number
	// in front of a reader that no clock ever measured.
	Working time.Duration
}

// Summarise adds a window of digests up.
//
// It is given every job in the window and never the page a limit cut down to, because a summary that
// counted only the rows it printed would be wrong in exactly the way a reader cannot see.
func Summarise(digests []*Digest) Totals {
	total := Totals{}
	// Every pull request this window opened, so one address counted twice is counted once.
	seen := map[string]bool{}
	for _, one := range digests {
		if one == nil {
			continue
		}
		total.Jobs++
		switch one.Phase {
		case PhaseDone:
			total.Done++
		case PhaseFailed:
			total.Failed++
		case PhaseStopped:
			total.Stopped++
		default:
			total.Unfinished++
		}
		total.SpentToken += one.SpentToken
		total.Steers += one.Steers
		// Counted once for each address rather than once for each row that names one. A requirement has
		// one pull request and two runs land in it, so counting rows would say a job opened twice as
		// many as it did.
		for _, address := range append([]string{one.PullRequest}, one.Opened...) {
			if address == "" || seen[address] {
				continue
			}
			seen[address] = true
			total.PullRequests++
		}
		if !one.StartedAt.IsZero() && !one.FinishedAt.IsZero() && one.FinishedAt.After(one.StartedAt) {
			total.Working += one.FinishedAt.Sub(one.StartedAt)
		}
	}
	return total
}

// Page cuts a window down to what a reader was willing to take, and says how many it left behind.
//
// The count comes back rather than being dropped, because a cap nobody is told about reads as
// complete coverage. That is the one way a bounded read can lie to the reader it exists to serve.
func Page(digests []*Digest, limit int) (page []*Digest, leftOut int) {
	limit = HistoryLimit(limit)
	if len(digests) <= limit {
		return digests, 0
	}
	return digests[:limit], len(digests) - limit
}

// HistoryQuery is what a history narrows by: where to read, and over what window.
//
// It is its own type rather than a Filter with two more fields, because a history narrows by none of
// the things a listing narrows by. A parent, a phase or a label on a history would each be a way of
// asking a question the summary above the rows would then answer wrongly.
type HistoryQuery struct {
	// Project wins over Workspace when both are set, being the narrower. Neither set reads every
	// project.
	Workspace string
	Project   string
	Window    Window
}

// DigestOf reduces a job to the facts a history reports, with the runs of its stages folded in.
//
// Both stores build a digest through this, so a field added to one is never missing from the other,
// and the conformance suite is holding two callers of one function rather than two transcriptions of
// one shape. Each store reads the runs its own way and hands them here, so neither can disagree with
// the other about what a job cost.
func DigestOf(one *Job, runs []*Execution) *Digest {
	if one == nil {
		return nil
	}
	digest := &Digest{
		ID: one.ID, Project: one.Project, Title: one.Title, Role: one.Role, Phase: one.Phase,
		SpentToken: one.SpentTokens, PullRequest: one.PullRequest, Reason: one.Reason,
		Steers: one.Steers, CreatedAt: one.CreatedAt,
	}
	// What the stages of this job spent, and where their work went. A run is not a job, so it is no
	// line of its own in a history: what it cost belongs to the job that ran it.
	for _, run := range runs {
		digest.Runs++
		digest.SpentToken += run.SpentTokens
		if run.PullRequest != "" {
			digest.Opened = append(digest.Opened, run.PullRequest)
		}
	}
	if one.StartedAt != nil {
		digest.StartedAt = *one.StartedAt
	}
	if one.FinishedAt != nil {
		digest.FinishedAt = *one.FinishedAt
	}
	return digest
}

// SortDigests puts a history newest first, with the identifier breaking a tie, so two reads of one
// window come back in one order. It is the order a job listing already comes back in.
func SortDigests(digests []*Digest) {
	sort.SliceStable(digests, func(i, j int) bool {
		if digests[i].CreatedAt.Equal(digests[j].CreatedAt) {
			return digests[i].ID > digests[j].ID
		}
		return digests[i].CreatedAt.After(digests[j].CreatedAt)
	})
}
