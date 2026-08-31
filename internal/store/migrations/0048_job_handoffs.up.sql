-- A session hands the rest of a job over rather than running until its context window is full.
--
-- A session used to take tasks whatever its context window held, so the last task of a long job, the
-- one that opens the pull request and writes the answer, was done at the point where the model is
-- worst. The system printed the share in a column and acted on none of it.
--
-- Two things are added here. A ceiling per workspace, which is the share past which a session takes
-- no new task on the job it is doing. And the record a fresh session starts from, because a gate that
-- only refused would leave the work stranded in a conversation nobody can add to.
--
-- Rows rather than a column on the job, for the reason the steps are rows: a long job can reach the
-- ceiling more than once, each handoff carries when it was written and which conversation wrote it,
-- and the conversation is what tells a handoff waiting to be taken up from one already taken up.
create table if not exists job_handoffs (
    job        text        not null references jobs (id) on delete cascade,
    -- Where this handoff sits in the order they were written, counting from one.
    seq        int         not null,
    -- What is left to do. It is the whole content of a handoff, so the code refuses an empty one
    -- before it ever reaches here. Named remaining rather than left, which is a reserved word here.
    remaining  text        not null,
    -- What that session tried that did not work. Empty is honest: a session that hit no dead end has
    -- none to report.
    tried      text        not null default '',
    -- The conversation that wrote it.
    session    text        not null default '',
    written_at timestamptz not null default now(),
    primary key (job, seq)
);

-- One job's handoffs, in the order they were written, which is the only way they are ever read.
create index if not exists job_handoffs_job_idx on job_handoffs (job, seq);

-- How full a session's context window may be in this workspace before the system gives it no new
-- task. Zero takes the system's own, which is 70 per cent.
--
-- This is the one number on this row that ships set rather than unset. It comes from the standard
-- quay-crew#539 names, which says quality falls off between 50 and 70 per cent of a window and is
-- poor past 70, and from no measurement of this crew. A workspace that wants the gate off sets 100.
alter table workspace_limits add column if not exists context_ceiling_percent int not null default 0;
