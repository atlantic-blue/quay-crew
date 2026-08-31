-- A job keeps what the forge says about the pull request it opened.
--
-- The address landed on the row and nothing read it again, so a change that merged and a change whose
-- checks went red an hour later read the same: produced. One of the three questions an operator asks
-- could not be answered at all, because "a check is red" was not a state the crew held.
--
-- Six columns rather than one word, because the questions are separate. A merged pull request with a
-- red check on its last run and an open one whose reviewer asked for changes are different work for
-- the person reading them.
--
-- Every one of them defaults to empty, and empty is read as unknown. That is the rule the machine
-- reading already holds: a figure nobody measured is the word rather than a zero, because an operator
-- acts on these and a pull request that reads as passing because nobody could read it is the one they
-- will not look at.

-- Whether it is open, merged or closed, in the forge's own terms. Merged and closed are the two ends:
-- a pull request in either is read once more and then left alone, which is what keeps the cost of
-- this one call for each pull request still moving.
alter table jobs add column if not exists pull_request_status text not null default '';

-- What the checks on the head commit say together: green, red, pending, none, or unknown. Red beats
-- pending and pending beats green, so a board with one failure and nine still running says red.
alter table jobs add column if not exists pull_request_checks text not null default '';

-- The name of the check that failed, and empty for every other answer. A red board is read by opening
-- the first thing that failed, so the name is what makes the state actionable rather than alarming.
alter table jobs add column if not exists pull_request_check text not null default '';

-- What the reviews add up to: approved, changes requested, none, or unknown. Changes requested is the
-- one that stops a merge, which is why it is kept apart from the checks.
alter table jobs add column if not exists pull_request_review text not null default '';

-- When the forge was last read. Null means never, and a row with no moment on it says unknown however
-- old the job is.
alter table jobs add column if not exists pull_request_read_at timestamptz;

-- Why the last reading did not happen, and empty where it did. It sits beside the unknowns so an
-- operator reads the reason rather than working out for themselves why every pull request reads
-- unknown. A missing forge credential is the reason it will most often carry.
alter table jobs add column if not exists pull_request_failed text not null default '';

-- The one query the reader makes: the pull requests still worth reading, longest unread first. It is
-- partial, so it holds only the rows that name a pull request rather than every job the system has
-- ever run.
create index if not exists jobs_unsettled_pull_request_idx
    on jobs (pull_request_read_at nulls first, created_at)
    where pull_request <> '' and pull_request_status not in ('merged', 'closed');
