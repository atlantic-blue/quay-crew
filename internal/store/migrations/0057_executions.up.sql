-- An execution is one run of one stage of one job, and it is not a job.
--
-- A stage that fans out used to write a full job row for each requirement: a title, a brief, a
-- sentence, a plan, gates and an acceptance, none of which a run of a stage has. Those rows then
-- stood in every listing of the work a person declared, and twelve places in the code asked whether
-- a row had a parent to decide whether it was really a job.
--
-- This table holds what a run needs and nothing a person wrote. What a run needs of its job, the
-- project, the mode, the repository and the words the session is asked, is read off the job at the
-- moment of the dispatch: a second copy of any of it here could only disagree with the job.
create table if not exists executions (
    id             text        primary key,
    -- The job this run belongs to, and the stage of that job it runs. Every execution belongs to
    -- exactly one of each.
    job            text        not null references jobs (id),
    stage          text        not null,
    -- The requirement or the vertical this run is for, counting from one. It is the number the stage
    -- gathers its reports under.
    number         int         not null,
    -- The piece of work this run holds. A second live run claiming it is refused, so a stage ticked
    -- twice runs one session and not two.
    claim          text        not null default '',

    -- What the controller writes, and nobody else.
    phase          text        not null default 'pending',
    session        text        not null default '',
    attempts       int         not null default 0,
    answer         text        not null default '',
    outcome        text        not null default '',
    reason         text        not null default '',
    branch         text        not null default '',
    pull_request   text        not null default '',
    spent_tokens   bigint      not null default 0,

    lease_owner    text        not null default '',
    lease_until    timestamptz,

    trace_id       text        not null default '',
    parent_span_id text        not null default '',

    created_at     timestamptz not null default now(),
    updated_at     timestamptz not null default now(),
    started_at     timestamptz,
    finished_at    timestamptz
);

-- The two questions asked of this table. What has run for this stage of this job, which is how a
-- stage gathers, and what is open across the crew, which is what a controller scans.
create index if not exists executions_job_idx on executions (job, stage, created_at);
create index if not exists executions_phase_idx on executions (phase, created_at);

-- A record hangs off the job it belongs to, and names the run it happened in. Before this a run was
-- a job, so its records hung off itself; now the job carries them and this column says which of its
-- runs wrote each one.
alter table job_events add column if not exists execution text not null default '';

-- The rows already in this table that were executions all along: a job under a parent, holding the
-- claim a fan out writes, which is the parent's identifier then the word and the number. Nothing
-- else has ever held a claim of that shape, so the match is exact rather than a guess.
--
-- The identifier is kept, so a link somebody wrote down still reaches the same run.
insert into executions (id, job, stage, number, claim, phase, session, attempts, answer, outcome,
    reason, pull_request, spent_tokens, lease_owner, lease_until, trace_id, parent_span_id,
    created_at, updated_at, started_at, finished_at)
select j.id, j.parent,
    case when j.claim ~ ' requirement [0-9]+$' then 'test' else 'build' end,
    (regexp_match(j.claim, '([0-9]+)$'))[1]::int,
    j.claim, j.phase, j.session, j.attempts, j.answer, j.outcome, j.reason, j.pull_request,
    j.spent_tokens, j.lease_owner, j.lease_until, j.trace_id, j.parent_span_id,
    j.created_at, j.updated_at, j.started_at, j.finished_at
from jobs j
where j.parent is not null
  and (j.claim ~ ('^' || j.parent || ' requirement [0-9]+$')
    or j.claim ~ ('^' || j.parent || ' build [0-9]+$'))
on conflict (id) do nothing;

-- Their records move onto the job, naming the run they happened in, so nothing that was written down
-- is lost when the row they hung off leaves this table.
update job_events e
set execution = e.job, job = x.job
from executions x
where e.job = x.id;

-- The rows leave the jobs table. What hung off them, their steps, their handoffs, the questions a
-- reading wrote and what each attempt said, goes with them: every one of those tables cascades on
-- delete, and a run has none of them in the new model.
delete from jobs where id in (select id from executions);

-- The flag that said a job builds against tests it may not change. A job never did: it was only ever
-- set on the workers the build stage declared, and those are executions now, which say the stage
-- they run.
alter table jobs drop column if exists building;
