package store

import (
	"context"
	"fmt"
	"time"

	"github.com/atlantic-blue/quay-krewe/internal/forge"
	"github.com/atlantic-blue/quay-krewe/internal/job"
)

// unsettled is the pull requests still worth reading: one is on the row, and the last reading did not
// say it had merged or closed. Longest unread first, so a batch cap delays a reading and never
// starves one.
//
// The two words are forge.StatusMerged and forge.StatusClosed, written out rather than passed as
// parameters so this matches the predicate of jobs_unsettled_pull_request_idx: Postgres cannot prove
// a parameter equals a literal in an index predicate, so a parameterised form would read the whole
// table. TestTheUnsettledQueryUsesTheWordsTheForgeWrites holds the two together.
const unsettled = `
	where pull_request <> '' and pull_request_status not in ('merged', 'closed')
	order by pull_request_read_at nulls first, created_at, id`

// UnsettledPullRequests is the jobs whose pull request is worth reading again.
func (p *Postgres) UnsettledPullRequests(ctx context.Context, limit int) ([]*job.Job, error) {
	return p.jobMatching(ctx, unsettled, limit)
}

// RecordPullRequest writes what the forge said onto the job.
//
// It is not a movement of the job, so it writes no event and does not touch updated_at: the job
// ended when it ended, and what happened to the work afterwards happened on the forge. A job whose
// updated_at moved every two minutes would also be a job the session lifecycle reads as busy.
func (p *Postgres) RecordPullRequest(ctx context.Context, id string, reading forge.Reading) error {
	kept := reading.Or()
	tag, err := p.pool.Exec(ctx, `
		update jobs set pull_request_status = $2, pull_request_checks = $3, pull_request_check = $4,
			pull_request_review = $5, pull_request_read_at = $6, pull_request_failed = $7
		where id = $1`,
		id, kept.Status, kept.Checks, kept.FailedCheck, kept.Review,
		stampOrNil(kept.ReadAt), kept.Failed)
	if err != nil {
		return fmt.Errorf("record the pull request of %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// stampOrNil is a moment, and null where nothing took one. Null is how a row says the forge was never
// read, which is a different answer from a reading taken at the zero moment.
func stampOrNil(at time.Time) *time.Time {
	if at.IsZero() {
		return nil
	}
	return &at
}
