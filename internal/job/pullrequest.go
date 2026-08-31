package job

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/atlantic-blue/krewe/internal/forge"
)

// A job opened a pull request, ended done, and the crew stopped there. It kept the address and never
// looked at it again, so a change that merged and a change whose checks went red an hour later read
// exactly the same. This is the read back.

// DefaultReadEvery is how often the system reads the pull requests it opened and has not settled.
//
// Two minutes, and what it buys is one call for each unsettled pull request each time. A tick with
// nothing unsettled costs one indexed query and no call at all, which is the property the job
// controller and the flow poller already have: nothing bills while nobody has work open.
//
// Two minutes rather than ten because the answer an operator acts on is a check going red, and a
// check goes red minutes after a push. Two rather than one because a pipeline takes minutes to say
// anything, so reading every minute doubles the calls to learn the same thing twice. What would
// change it is a measurement nothing has taken yet: the time from opening a pull request to its
// checks settling, over the first fifty. A pipeline that takes eight minutes does not need a reading
// every two.
const DefaultReadEvery = 2 * time.Minute

// DefaultReadBatch is how many unsettled pull requests one tick reads. A system holding two hundred
// open pull requests is not a reason for one tick to make two hundred calls: the oldest reading goes
// first, so nothing is starved, and the rest are read on the ticks after it.
const DefaultReadBatch = 20

// readReason is how much of a refusal is kept on the row. The reason is read in a listing beside the
// address, so a forge that answers with a page of hypertext must not put a page of it there.
const readReason = 200

// A PullRequestStore is the rows a watcher reads and writes. It is the smallest interface that does
// this job, so nothing else about a store has to exist for a watcher to be tested.
type PullRequestStore interface {
	// UnsettledPullRequests is the jobs whose pull request is worth reading: one is on the row, and
	// the last reading did not say it had merged or closed. Longest unread first.
	UnsettledPullRequests(ctx context.Context, limit int) ([]*Job, error)
	// RecordPullRequest writes what the forge said onto the job. It is not a movement of the job, so
	// it carries no record of its own: the job ended when it ended, and what happened to the work
	// afterwards happened on the forge.
	RecordPullRequest(ctx context.Context, id string, reading forge.Reading) error
}

// A Watcher reads back the pull requests the crew opened, on its own timer.
//
// Everything that reports the state of a pull request reads the row. Nothing calls a forge while a
// page draws, which is the rule GetHeadroom and GetHealth already hold: a page answers from the last
// reading, and a slow forge slows this timer and never a command.
type Watcher struct {
	store  PullRequestStore
	reader forge.Reader
	every  time.Duration
	batch  int
	// now is the clock, so a test can say when a reading was taken. Nil takes the real one.
	now func() time.Time
}

// NewWatcher builds one over a store and a forge. A nil forge is a system with nothing to read a
// pull request with, and it leaves every reading unknown rather than pretending to have taken one.
func NewWatcher(store PullRequestStore, reader forge.Reader, every time.Duration) *Watcher {
	if every <= 0 {
		every = DefaultReadEvery
	}
	return &Watcher{store: store, reader: reader, every: every, batch: DefaultReadBatch}
}

// At sets the clock a reading is stamped with. A test owns it, so a scenario can say when a reading
// was taken rather than assert that one moment is close to another.
func (w *Watcher) At(now func() time.Time) *Watcher { w.now = now; return w }

// Batch is how many unsettled pull requests one tick reads. Zero keeps DefaultReadBatch.
func (w *Watcher) Batch(most int) *Watcher {
	if most > 0 {
		w.batch = most
	}
	return w
}

// Run reads until the context ends. It reads once immediately, because a system that came up a
// minute ago should not tell the first operator who looks that it knows nothing.
func (w *Watcher) Run(ctx context.Context) {
	if w == nil || w.store == nil || w.reader == nil {
		return
	}
	w.Tick(ctx)
	ticker := time.NewTicker(w.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Tick(ctx)
		}
	}
}

// Tick reads every unsettled pull request once, up to the batch. Exported so a test and a scenario
// drive one reading rather than waiting for a ticker, which would be slow when it passed and flaky
// when it did not.
func (w *Watcher) Tick(ctx context.Context) {
	if w == nil || w.store == nil || w.reader == nil {
		return
	}
	open, err := w.store.UnsettledPullRequests(ctx, w.batch)
	if err != nil {
		slog.WarnContext(ctx, "the pull requests this crew opened could not be listed", "error", err)
		return
	}
	for _, one := range open {
		reading := w.read(ctx, one)
		if err := w.store.RecordPullRequest(ctx, one.ID, reading); err != nil {
			slog.WarnContext(ctx, "a pull request was read and the reading could not be kept",
				"job", one.ID, "error", err)
		}
	}
}

// read is what the forge says about one job's pull request, and unknown with the reason where it
// said nothing.
//
// A failure leaves every word unknown rather than the words of an older reading. A status that
// stopped being true is worse than no status: the operator acts on it either way, and only one of
// the two tells them they are acting on nothing.
func (w *Watcher) read(ctx context.Context, one *Job) forge.Reading {
	now := w.stamp()
	at, err := forge.Parse(one.PullRequest)
	if err != nil {
		return forge.Unreadable(now, shortReason(err.Error()))
	}
	reading, err := w.reader.Read(ctx, at)
	if err != nil {
		return forge.Unreadable(now, shortReason(err.Error()))
	}
	reading.ReadAt, reading.Failed = now, ""
	return reading.Or()
}

func (w *Watcher) stamp() time.Time {
	if w.now != nil {
		return w.now().UTC()
	}
	return time.Now().UTC()
}

// shortReason holds a refusal to one line a person reads.
func shortReason(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	if len(flat) <= readReason {
		return flat
	}
	return flat[:readReason-1] + "…"
}
