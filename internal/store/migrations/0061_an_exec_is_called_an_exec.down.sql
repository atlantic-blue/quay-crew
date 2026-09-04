-- Back to the word the database used on its own.
do $$
begin
    if exists (select 1 from information_schema.columns
               where table_name = 'sessions' and column_name = 'described_at_exec') then
        alter table sessions rename column described_at_exec to described_at_task;
    end if;
    if exists (select 1 from pg_class where relname = 'execs_session_idx')
       and not exists (select 1 from pg_class where relname = 'tasks_session_idx') then
        alter index execs_session_idx rename to tasks_session_idx;
    end if;
    if exists (select 1 from information_schema.tables
               where table_schema = 'public' and table_name = 'execs')
       and not exists (select 1 from information_schema.tables
                       where table_schema = 'public' and table_name = 'tasks') then
        alter table execs rename to tasks;
    end if;
end $$;
