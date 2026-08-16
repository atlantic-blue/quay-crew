do $$
begin
    if exists (select 1 from information_schema.columns
               where table_name = 'sessions' and column_name = 'described_at_task') then
        alter table sessions rename column described_at_task to described_at_turn;
    end if;

    if exists (select 1 from information_schema.columns
               where table_name = 'tasks' and column_name = 'handle') then
        alter table tasks rename column handle to thread_id;
    end if;
    if exists (select 1 from pg_class where relname = 'tasks_session_idx')
       and not exists (select 1 from pg_class where relname = 'turns_session_idx') then
        alter index tasks_session_idx rename to turns_session_idx;
    end if;
    if exists (select 1 from information_schema.tables
               where table_schema = 'public' and table_name = 'tasks')
       and not exists (select 1 from information_schema.tables
                       where table_schema = 'public' and table_name = 'turns') then
        alter table tasks rename to turns;
    end if;

    if exists (select 1 from pg_class where relname = 'sessions_project_handle_idx')
       and not exists (select 1 from pg_class where relname = 'sessions_project_thread_idx') then
        alter index sessions_project_handle_idx rename to sessions_project_thread_idx;
    end if;
    if exists (select 1 from information_schema.columns
               where table_name = 'sessions' and column_name = 'handle') then
        alter table sessions rename column handle to thread_id;
    end if;
end $$;
