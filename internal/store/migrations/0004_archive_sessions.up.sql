-- Archiving puts a thread away without losing it. Nothing is deleted: the conversation store, the
-- project files and this row all stay exactly where they are, and clearing the stamp brings the
-- thread back.
--
-- It is a stamp rather than a flag so the listing can say when it was put away, and so restoring is
-- setting it to null rather than flipping a boolean nobody can date.
alter table sessions add column if not exists archived_at timestamptz;

-- The default listing is the live threads, which is every read the console and the command line make.
create index if not exists sessions_live_idx on sessions (project) where archived_at is null;
