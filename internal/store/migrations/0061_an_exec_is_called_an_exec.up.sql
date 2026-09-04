-- The system has one word for what a session runs, and this table held the other one.
--
-- The command line, the console, the model runner and the events all said exec while the database
-- said task, so every query in postgres.go named a table nobody else named. One thing under two
-- names is a thing a reader has to translate, and the translation is where a defect hides.
--
-- Every step is guarded, because the control plane applies the migrations on every start and a
-- rename is the one shape that cannot be written twice. `alter table ... rename` has no
-- `if exists`, so the guard is the check.
do $$
begin
    if exists (select 1 from information_schema.tables
               where table_schema = 'public' and table_name = 'tasks')
       and not exists (select 1 from information_schema.tables
                       where table_schema = 'public' and table_name = 'execs') then
        alter table tasks rename to execs;
    end if;
    if exists (select 1 from pg_class where relname = 'tasks_session_idx')
       and not exists (select 1 from pg_class where relname = 'execs_session_idx') then
        alter index tasks_session_idx rename to execs_session_idx;
    end if;

    -- How far into the conversation the system last described the session. It counts execs, so it
    -- carries the word with it.
    if exists (select 1 from information_schema.columns
               where table_name = 'sessions' and column_name = 'described_at_task') then
        alter table sessions rename column described_at_task to described_at_exec;
    end if;
end $$;
