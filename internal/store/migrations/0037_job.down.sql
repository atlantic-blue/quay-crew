-- Back to the word the crew used before declared intent was called a job.
--
-- Forward only in the control plane, which never applies a down file. This is here for an operator
-- who has to go back deliberately, and it is guarded the same way and for the same reason.
do $$
begin
    if exists (select 1 from information_schema.tables
               where table_schema = 'public' and table_name = 'jobs')
       and not exists (select 1 from information_schema.tables
                       where table_schema = 'public' and table_name = 'work') then
        alter table jobs rename to work;
    end if;
    if exists (select 1 from information_schema.tables
               where table_schema = 'public' and table_name = 'job_events')
       and not exists (select 1 from information_schema.tables
                       where table_schema = 'public' and table_name = 'work_events') then
        alter table job_events rename to work_events;
    end if;

    if exists (select 1 from information_schema.columns
               where table_name = 'work' and column_name = 'after_jobs') then
        alter table work rename column after_jobs to after_work;
    end if;
    if exists (select 1 from information_schema.columns
               where table_name = 'work_events' and column_name = 'job') then
        alter table work_events rename column job to work;
    end if;
    if exists (select 1 from information_schema.columns
               where table_name = 'flow_runs' and column_name = 'job') then
        alter table flow_runs rename column job to work;
    end if;
    if exists (select 1 from information_schema.columns
               where table_name = 'flow_runs' and column_name = 'step_job') then
        alter table flow_runs rename column step_job to step_work;
    end if;

    if exists (select 1 from pg_class where relname = 'jobs_project_idx')
       and not exists (select 1 from pg_class where relname = 'work_project_idx') then
        alter index jobs_project_idx rename to work_project_idx;
    end if;
    if exists (select 1 from pg_class where relname = 'jobs_parent_idx')
       and not exists (select 1 from pg_class where relname = 'work_parent_idx') then
        alter index jobs_parent_idx rename to work_parent_idx;
    end if;
    if exists (select 1 from pg_class where relname = 'jobs_phase_idx')
       and not exists (select 1 from pg_class where relname = 'work_phase_idx') then
        alter index jobs_phase_idx rename to work_phase_idx;
    end if;
    if exists (select 1 from pg_class where relname = 'jobs_lease_idx')
       and not exists (select 1 from pg_class where relname = 'work_lease_idx') then
        alter index jobs_lease_idx rename to work_lease_idx;
    end if;
    if exists (select 1 from pg_class where relname = 'job_events_job_idx')
       and not exists (select 1 from pg_class where relname = 'work_events_work_idx') then
        alter index job_events_job_idx rename to work_events_work_idx;
    end if;
    if exists (select 1 from pg_class where relname = 'job_events_recent_idx')
       and not exists (select 1 from pg_class where relname = 'work_events_recent_idx') then
        alter index job_events_recent_idx rename to work_events_recent_idx;
    end if;
    if exists (select 1 from pg_class where relname = 'jobs_pkey')
       and not exists (select 1 from pg_class where relname = 'work_pkey') then
        alter index jobs_pkey rename to work_pkey;
    end if;
    if exists (select 1 from pg_class where relname = 'job_events_pkey')
       and not exists (select 1 from pg_class where relname = 'work_events_pkey') then
        alter index job_events_pkey rename to work_events_pkey;
    end if;
    if exists (select 1 from pg_class where relname = 'job_events_seq_seq' and relkind = 'S')
       and not exists (select 1 from pg_class where relname = 'work_events_seq_seq') then
        alter sequence job_events_seq_seq rename to work_events_seq_seq;
    end if;
end $$;

update work_events set kind = 'work.' || substring(kind from 5) where kind like 'job.%';
