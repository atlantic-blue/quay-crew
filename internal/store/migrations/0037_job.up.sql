-- Declared intent is called a job, and the tables that hold it say so.
--
-- The word came from Kubernetes, which the crew already borrows from: a Lease, a Phase, a Role and
-- verbs on a resource. A Kubernetes Job is declared intent, run to completion, watched by a
-- controller, with a disposable container underneath, which is this table down to the phase column
-- and the lease. The one word the crew had not borrowed was the name of the resource itself.
--
-- A rename rather than a second table, because two would then disagree and every read would have to
-- say which one wins. Every row written before today travels with its table: a job declared
-- yesterday keeps its identifier, its phase, its answer and its history, and reads back whole.
--
-- The table is plural now, which every other table here already was: sessions, tasks, projects,
-- roles, skills. It was singular because work is a mass noun and jobs are countable.
--
-- Every step is guarded, because the control plane applies the migrations on every start and a
-- rename is the one shape that cannot be written twice. `alter table ... rename` has no `if exists`,
-- so the guard is the check.
do $$
begin
    if exists (select 1 from information_schema.tables
               where table_schema = 'public' and table_name = 'work')
       and not exists (select 1 from information_schema.tables
                       where table_schema = 'public' and table_name = 'jobs') then
        alter table work rename to jobs;
    end if;
    if exists (select 1 from information_schema.tables
               where table_schema = 'public' and table_name = 'work_events')
       and not exists (select 1 from information_schema.tables
                       where table_schema = 'public' and table_name = 'job_events') then
        alter table work_events rename to job_events;
    end if;

    -- What a job waits for, and which job a record belongs to.
    if exists (select 1 from information_schema.columns
               where table_name = 'jobs' and column_name = 'after_work') then
        alter table jobs rename column after_work to after_jobs;
    end if;
    if exists (select 1 from information_schema.columns
               where table_name = 'job_events' and column_name = 'work') then
        alter table job_events rename column work to job;
    end if;

    -- A flow run hangs inside the job tree: one column for the job that carries the run, one for the
    -- job its current step went out as.
    if exists (select 1 from information_schema.columns
               where table_name = 'flow_runs' and column_name = 'work') then
        alter table flow_runs rename column work to job;
    end if;
    if exists (select 1 from information_schema.columns
               where table_name = 'flow_runs' and column_name = 'step_work') then
        alter table flow_runs rename column step_work to step_job;
    end if;

    -- The indexes carry the old word in their names. Renaming them changes nothing a query can see,
    -- and leaving them would put the old vocabulary in front of the next person reading a plan.
    if exists (select 1 from pg_class where relname = 'work_project_idx')
       and not exists (select 1 from pg_class where relname = 'jobs_project_idx') then
        alter index work_project_idx rename to jobs_project_idx;
    end if;
    if exists (select 1 from pg_class where relname = 'work_parent_idx')
       and not exists (select 1 from pg_class where relname = 'jobs_parent_idx') then
        alter index work_parent_idx rename to jobs_parent_idx;
    end if;
    if exists (select 1 from pg_class where relname = 'work_phase_idx')
       and not exists (select 1 from pg_class where relname = 'jobs_phase_idx') then
        alter index work_phase_idx rename to jobs_phase_idx;
    end if;
    if exists (select 1 from pg_class where relname = 'work_lease_idx')
       and not exists (select 1 from pg_class where relname = 'jobs_lease_idx') then
        alter index work_lease_idx rename to jobs_lease_idx;
    end if;
    if exists (select 1 from pg_class where relname = 'work_events_work_idx')
       and not exists (select 1 from pg_class where relname = 'job_events_job_idx') then
        alter index work_events_work_idx rename to job_events_job_idx;
    end if;
    if exists (select 1 from pg_class where relname = 'work_events_recent_idx')
       and not exists (select 1 from pg_class where relname = 'job_events_recent_idx') then
        alter index work_events_recent_idx rename to job_events_recent_idx;
    end if;

    -- The primary keys and the sequence behind job_events.seq were named after the tables when the
    -- tables were made, and a rename does not carry them.
    if exists (select 1 from pg_class where relname = 'work_pkey')
       and not exists (select 1 from pg_class where relname = 'jobs_pkey') then
        alter index work_pkey rename to jobs_pkey;
    end if;
    if exists (select 1 from pg_class where relname = 'work_events_pkey')
       and not exists (select 1 from pg_class where relname = 'job_events_pkey') then
        alter index work_events_pkey rename to job_events_pkey;
    end if;
    if exists (select 1 from pg_class where relname = 'work_events_seq_seq' and relkind = 'S')
       and not exists (select 1 from pg_class where relname = 'job_events_seq_seq') then
        alter sequence work_events_seq_seq rename to job_events_seq_seq;
    end if;
end $$;

-- The kinds a record carries move with it. `work.declared` becomes `job.declared` and so on for
-- started, answered, failed, asked, stopped, claimed and released.
--
-- The alternative was to leave them, on the argument that a record says what happened at the time.
-- It reads well and it costs the reader everything: the crew has one vocabulary now, and a history
-- that answers in two makes every consumer switch on both spellings forever. A kind is the crew's
-- own word for what happened, not the caller's text, so nothing a person wrote is being changed.
update job_events set kind = 'job.' || substring(kind from 6) where kind like 'work.%';
