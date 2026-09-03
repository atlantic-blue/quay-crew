do $$
begin
    if exists (select 1 from information_schema.columns
        where table_name = 'workspace_limits' and column_name = 'max_declared') then
        alter table workspace_limits rename column max_declared to max_depth;
    end if;
end $$;

alter table job_events add column if not exists parent text not null default '';
alter table job_events add column if not exists depth int not null default 0;

alter table jobs add column if not exists parent text references jobs (id);
alter table jobs add column if not exists depth int not null default 0;

-- A cause goes back to being a parent, and so does the run a step belonged to: the job carrying that
-- run is the row that carried it before.
update jobs set parent = cause where cause <> '';
update jobs step set parent = carrier.id
from jobs carrier
where step.run <> '' and carrier.run = ''
  and carrier.labels ->> 'flow.run' = step.run
  and carrier.labels ->> 'flow.node' is null;

-- The depth cannot be recovered for a row whose parent chain is gone, so every row that has a parent
-- is written back one deep, which is what every one of them was.
update jobs set depth = 1 where parent is not null;

create index if not exists jobs_parent_idx on jobs (parent);
drop index if exists jobs_run_idx;
alter table jobs drop column if exists run;
alter table jobs drop column if exists cause;

-- A steer's tree cannot be recovered, so each one goes back naming its own job as the top, which is
-- what it is now.
alter table job_steers add column if not exists root text not null default '';
update job_steers set root = job where root = '';
create index if not exists job_steers_root_idx on job_steers (root, occurred_at);
drop index if exists job_steers_job_idx;
