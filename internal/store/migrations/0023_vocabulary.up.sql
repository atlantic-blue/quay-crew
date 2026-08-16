-- The crew has one word for a conversation and one for a piece of work: session and task. The
-- database already said sessions, so this is the rest of it.
--
-- The handle is the name a caller dispatches to, and calling it thread_id on a table called sessions
-- said the two were different things. There is no session_id here for the same reason: the row's own
-- key is id, and a second column with that name would read as a pointer somewhere else.
alter table sessions rename column thread_id to handle;
alter index if exists sessions_project_thread_idx rename to sessions_project_handle_idx;

-- A turn is a word from conversation analysis and it never said how long the work takes.
alter table turns rename to tasks;
alter index if exists turns_session_idx rename to tasks_session_idx;
alter table tasks rename column thread_id to handle;
alter table sessions rename column described_at_turn to described_at_task;
