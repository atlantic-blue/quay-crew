-- A job keeps the steps it finished, and the failure a continued attempt is carrying on past.
--
-- Until now a job that failed could only be declared a second time, and the second attempt paid for
-- the first: it read the same issue, cut the same worktree and made the same discoveries, so one
-- slice came back as two branches under two names. On the acceptance run of 29 August 2026 the
-- container runtime went down and took six jobs with it, and a credential ran out sixty seconds into
-- another, and none of those failures was about the work being wrong.
--
-- A step is what the session said it finished rather than anything the system watched, the way an
-- answer is. That is the only record that survives the attempt: nothing can see inside a container,
-- and a session that dies takes with it everything it did not write down.
--
-- Rows rather than an array column, because each one carries when it was finished and the order it
-- was finished in, and because the unique index below is what makes recording the same step twice
-- leave one row. A session that is continued says again what it said before, and the record has to
-- stay the set of what is finished rather than a log of what was said.
create table if not exists job_steps (
    job         text        not null references jobs (id) on delete cascade,
    -- Where this step sits in the order they were finished, counting from one.
    seq         int         not null,
    summary     text        not null,
    finished_at timestamptz not null default now(),
    primary key (job, seq)
);

-- One job's steps, in the order they were finished, which is the only way they are ever read.
create index if not exists job_steps_job_idx on job_steps (job, seq);

-- The same step twice leaves one row. It is a unique index rather than a check in the code because
-- two calls arriving at once would both read no such step and both write one.
create unique index if not exists job_steps_summary_idx on job_steps (job, summary);

-- What the attempt in flight is continuing past, which is what the job failed with.
--
-- It is a column of its own rather than the reason column left as it was, because a pending job that
-- carries a reason is a job the system is holding back for want of a machine, and a listing says
-- "held" for it. A job going again is not held, so the failure moves here and the reason is cleared.
--
-- Empty string rather than null, the way every other text column on this table already is: a reader
-- that has to tell null from empty is a reader with two cases where there is one.
alter table jobs add column if not exists resuming text not null default '';
