-- The mode a thread's turns run in. It belongs to the thread rather than to a turn, so a thread
-- started to plan something keeps planning across turns instead of being re armed on every dispatch.
--
-- Every existing thread has been running acceptEdits, which was hardcoded, so that is the default and
-- nothing changes behaviour on the way in.
alter table sessions add column if not exists permission_mode text not null default 'acceptEdits';
