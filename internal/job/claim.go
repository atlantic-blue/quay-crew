package job

import (
	"fmt"
	"strings"
	"time"
)

// A job claims the piece of work it is doing, and a second job claiming the same piece of work is
// refused.
//
// The failure it answers happened twice in one run. Two sessions picked up the same issue and built
// it under different names, and the first anybody knew was two pull requests that conflicted on
// files both of them had created. The two designs disagreed in small places, which is the expensive
// part: reconciling them by hand cost more than either would have cost alone. Nothing was in the
// other's way in the filesystem, because each session already had its own working copy. They were in
// each other's way over the work itself, and there was no record of who was doing what.
//
// It is not a lock on a file. It is a record of intent, which is what was missing: both sessions
// would have read it before starting.
//
// The word is also what a controller does to a row, and the two are not the same thing. A lease is a
// controller's hold on a row, it lasts a minute, and nothing outside the system reads it. A claim is
// a job's hold on a piece of work in the world, it lasts as long as the job does, and an operator
// and a session both read it before they start.

// ClaimLimit is how long a claim may be, which is the title's ceiling. A claim names a piece of
// work in one line, and a line longer than a title is a brief.
const ClaimLimit = TitleLimit

// ClaimLife is how long a claim outlives the last movement of the job holding it.
//
// A crashed session must not hold a piece of work forever, so a claim on a job that nothing is
// moving runs out. What the number has to outlast is the longest gap between two movements of a job
// that is genuinely alive. A running job is not one of them: its controller renews the lease on
// every tick and every renewal moves the row, so a job with a controller on it never goes stale at
// all. The two long gaps are a job waiting for a person to answer its question, and a job queued
// behind everything else in its workspace.
//
// The number is chosen rather than measured. What would replace it is the distribution of that gap,
// which nothing takes yet. It is a constant rather than a setting on the workspace because a system
// given no number would hold work forever, which is the deadlock this exists to avoid.
const ClaimLife = 2 * time.Hour

// TidyClaim is the claim as it is stored: the space around it comes off, any run of space inside it
// becomes one space, and what is left is lowercased.
//
// Lowercased because two people naming the same piece of work from memory write it two ways, and a
// claim that misses over a capital letter is a claim that did nothing. Matching too much costs a
// refusal that names the holder, which somebody reads and acts on in a second. Matching too little
// costs the failure this exists to stop, and nobody sees that one until two pull requests conflict.
func TidyClaim(claim string) string {
	return strings.ToLower(strings.Join(strings.Fields(claim), " "))
}

// usableClaim refuses a claim nobody could hold to a piece of work.
func usableClaim(claim string) error {
	if claim == "" {
		return nil
	}
	if len(claim) > ClaimLimit {
		return fmt.Errorf("the claim is %d bytes and the ceiling is %d: name the piece of work in one line, "+
			"as atlantic-blue/quay-krewe#540, and say what is to be done in the brief", len(claim), ClaimLimit)
	}
	return nil
}

// Holding says whether this job still holds the piece of work it claims, as of now.
//
// Three ways a claim ends, and they are the three states a session leaves work in. The job settled,
// so the work is done or it failed and the record says which. Somebody stopped it. Or nothing has
// moved it for longer than a claim lives, which is the crashed session: the container went, no
// controller is renewing anything, and the row is all that is left. Without the third one the first
// crash holds a piece of work forever, and every test about claiming still passes.
func (j *Job) Holding(now time.Time) bool {
	if j == nil || j.Claim == "" || Terminal(j.Phase) {
		return false
	}
	return now.Sub(j.UpdatedAt) < ClaimLife
}

// Held is what the store answers when the work a declaration claims belongs to a job that is still
// holding it.
//
// It carries the holder rather than a sentence, so the refusal names the job to read, which is the
// whole point: a caller told the claim is taken looks for who has it, and a caller told which job
// has it opens that job.
type Held struct {
	// Claim is the piece of work, as both jobs wrote it.
	Claim string
	// Holder is the job that has it, Title is what that job is, and TakenAt is when it was declared.
	Holder  string
	Title   string
	TakenAt time.Time
}

// Error is the refusal as of now, so this reads correctly wherever an error is printed.
func (h *Held) Error() string { return h.Refusal(time.Now().UTC()) }

// Refusal is what the caller of the second declaration reads: what is claimed, which job holds it,
// how old that claim is, and the two ways on.
func (h *Held) Refusal(now time.Time) string {
	return fmt.Sprintf("%q is claimed by job %s, %q, which took it %s ago and holds it still. "+
		"Read what that job is doing with krewe job show %s, and take another piece of work. A claim ends "+
		"when its job is done, when somebody stops it, and %s after the job stops moving.",
		h.Claim, h.Holder, h.Title, ClaimAge(now.Sub(h.TakenAt)), h.Holder, ClaimLife)
}

// ClaimAge is how old a claim is, in the largest unit that says anything: a claim taken four minutes
// ago and one taken four hours ago are different situations, and 4h13m26.5s is a number nobody reads.
func ClaimAge(since time.Duration) string {
	switch {
	case since < time.Minute:
		return "less than a minute"
	case since < 2*time.Minute:
		return "a minute"
	case since < time.Hour:
		return fmt.Sprintf("%d minutes", int(since.Minutes()))
	default:
		return fmt.Sprintf("%dh%dm", int(since.Hours()), int(since.Minutes())%60)
	}
}
