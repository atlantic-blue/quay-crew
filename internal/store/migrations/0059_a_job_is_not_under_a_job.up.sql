-- A job cannot be under another job. The hierarchy is workspaces, then projects, then jobs, then
-- executions, and nothing else belongs to a job.
--
-- Two things used to declare a job under a job. A session declaring work, which wrote the job it was
-- running into the parent column, and a flow run declaring its steps, which wrote the job carrying
-- the run. Neither is containment. The first is a cause, which is worth keeping as a plain
-- reference, and the second belongs to the run rather than to a job.

-- What brought a job about, and empty on a job an operator declared. No foreign key: a cause is a
-- fact about how this row came to exist, so deleting the job that caused it must not be refused or
-- cascade into it.
alter table jobs add column if not exists cause text not null default '';
-- The flow run this job is one step of, and empty on every job a run did not declare.
alter table jobs add column if not exists run text not null default '';

-- The steps of the runs already here. A step carries the run and the node it ran in its labels, and
-- the job carrying a run carries the run without a node, so the node is what tells the two apart.
--
-- Read with ->> rather than with the containment operator, because a question mark in a statement
-- sent with numbered parameters is read as a placeholder by more than one thing on the way.
update jobs set run = labels ->> 'flow.run'
where parent is not null
  and labels ->> 'flow.run' is not null
  and labels ->> 'flow.node' is not null;

-- Everything else a session declared keeps its identity as a job in its project and gains the cause.
-- The job carrying a flow run is one of these: what caused it is the job whose session started the
-- run.
update jobs set cause = parent
where parent is not null and run = '';

-- A run reads its own steps by the column above.
create index if not exists jobs_run_idx on jobs (run) where run <> '';

-- Nothing hangs under a job any more.
drop index if exists jobs_parent_idx;
alter table jobs drop column if exists parent;
alter table jobs drop column if exists depth;

-- A record of what happened to a job says where the job sits, and a job sits in a project. It never
-- sat anywhere else, whatever these two columns said.
alter table job_events drop column if exists parent;
alter table job_events drop column if exists depth;

-- The ceiling counted how deep a tree could go, and there is no tree. It is now how many jobs one
-- session may declare, and zero still means none: default deny, raised deliberately per workspace.
--
-- Guarded, the way this file guards every other statement, so running it over a database that
-- already carries the new name says nothing rather than failing.
do $$
begin
    if exists (select 1 from information_schema.columns
        where table_name = 'workspace_limits' and column_name = 'max_depth') then
        alter table workspace_limits rename column max_depth to max_declared;
    end if;
end $$;

-- A steer belonged to the job it landed on and to the job at the top of that tree, and the count was
-- added to every job in between. There is no tree, so a steer belongs to the job it landed on. The
-- marks already recorded keep the job they landed on, and the column naming the top goes.
drop index if exists job_steers_root_idx;
alter table job_steers drop column if exists root;
create index if not exists job_steers_job_idx on job_steers (job, occurred_at);
