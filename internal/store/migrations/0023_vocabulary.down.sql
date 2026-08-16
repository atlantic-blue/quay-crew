alter table sessions rename column described_at_task to described_at_turn;
alter table tasks rename column handle to thread_id;
alter index if exists tasks_session_idx rename to turns_session_idx;
alter table tasks rename to turns;

alter index if exists sessions_project_handle_idx rename to sessions_project_thread_idx;
alter table sessions rename column handle to thread_id;
