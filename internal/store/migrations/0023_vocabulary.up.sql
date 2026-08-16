-- The crew has one word for a conversation and one for a piece of work: session and task. The
-- database already said sessions, so this is the rest of it.
--
-- The handle is the name a caller dispatches to, and calling it thread_id on a table called sessions
-- said the two were different things. There is no session_id here for the same reason: the row's own
-- key is id, and a second column with that name would read as a pointer to somewhere else.
--
-- Every step is guarded, because the control plane applies the migrations on every start and a
-- rename is the one shape that cannot be written twice. `alter table ... rename` has no `if exists`,
-- so the guard is the check.
do $$
begin
    if exists (select 1 from information_schema.columns
               where table_name = 'sessions' and column_name = 'thread_id') then
        alter table sessions rename column thread_id to handle;
    end if;
    if exists (select 1 from pg_class where relname = 'sessions_project_thread_idx')
       and not exists (select 1 from pg_class where relname = 'sessions_project_handle_idx') then
        alter index sessions_project_thread_idx rename to sessions_project_handle_idx;
    end if;

    -- A turn is a word from conversation analysis and it never said how long the work takes.
    if exists (select 1 from information_schema.tables
               where table_schema = 'public' and table_name = 'turns')
       and not exists (select 1 from information_schema.tables
                       where table_schema = 'public' and table_name = 'tasks') then
        alter table turns rename to tasks;
    end if;
    if exists (select 1 from pg_class where relname = 'turns_session_idx')
       and not exists (select 1 from pg_class where relname = 'tasks_session_idx') then
        alter index turns_session_idx rename to tasks_session_idx;
    end if;
    if exists (select 1 from information_schema.columns
               where table_name = 'tasks' and column_name = 'thread_id') then
        alter table tasks rename column thread_id to handle;
    end if;

    if exists (select 1 from information_schema.columns
               where table_name = 'sessions' and column_name = 'described_at_turn') then
        alter table sessions rename column described_at_turn to described_at_task;
    end if;
end $$;
