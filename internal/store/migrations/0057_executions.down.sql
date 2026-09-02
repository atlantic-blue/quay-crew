-- The flag comes back before the rows that carried it do.
alter table jobs add column if not exists building boolean not null default false;

-- The runs go back into the jobs table as the children they used to be, with the title a fan out
-- wrote for them. The brief cannot come back: it was built from the job's own record at the moment
-- of the dispatch and was never stored, so a run that goes back this way carries the title alone.
insert into jobs (id, workspace, project, title, brief, parent, depth, version, phase, session,
    attempts, answer, outcome, reason, pull_request, spent_tokens, lease_owner, lease_until,
    trace_id, parent_span_id, claim, ungated, building, mode, repository, product, request,
    created_at, updated_at, started_at, finished_at)
select x.id, j.workspace, j.project,
    case when x.stage = 'test'
        then 'tests for requirement ' || x.number
        else 'build vertical ' || x.number end,
    'this run was written back from the executions table and its brief was never stored',
    x.job, j.depth + 1, 1, x.phase, x.session, x.attempts, x.answer, x.outcome, x.reason,
    x.pull_request, x.spent_tokens, x.lease_owner, x.lease_until, x.trace_id, x.parent_span_id,
    x.claim, true, x.stage = 'build', j.mode, j.repository, j.product, j.request,
    x.created_at, x.updated_at, x.started_at, x.finished_at
from executions x join jobs j on j.id = x.job
on conflict (id) do nothing;

update job_events set job = execution, execution = '' where execution <> '';

alter table job_events drop column if exists execution;
drop table if exists executions;
