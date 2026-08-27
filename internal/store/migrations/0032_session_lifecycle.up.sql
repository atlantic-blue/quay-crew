-- When the crew took a session's container back, and how long a workspace lets one sit before it does.
--
-- A session that went quiet and a session somebody halted must never read the same, so reclaiming is
-- its own status and its own stamp rather than a reuse of 'stopped'. An operator who reads 'stopped'
-- goes looking for who stopped it. An operator who reads 'reclaimed' looks for nothing, because the
-- next task fixes it: the row, the conversation handle, the workspace's conversation store and the
-- project's files are all still here, so a fresh container over the same mounts is the same
-- conversation.
--
-- reclaimed_at is a stamp of its own rather than a reading of updated_at, because how long a session
-- has been reclaimed is what the archive time is measured against, and updated_at moves on every
-- write to the row.
alter table sessions add column if not exists reclaimed_at timestamptz;

-- Both times ship unset, and unset means the controller does nothing at all.
--
-- No number is written here, in a default, or anywhere else in this repository. Three measurements
-- decide them and none has been taken: the distribution of the gap between one task landing in a
-- session and the next starting, what a resume costs, and what an idle container holds. Section 11
-- of docs/ORCHESTRATION.md names each one and the command that would take it. A crew that chose a
-- number before those runs would be guessing at how long an operator leaves a conversation open, and
-- getting that wrong throws away a container somebody was about to use.
alter table workspace_limits add column if not exists reclaim_seconds int not null default 0;
alter table workspace_limits add column if not exists archive_seconds int not null default 0;

-- The fourth query the controller runs each tick: the sessions nothing is holding open, oldest
-- touched first. A crew with a thousand sessions and two settled reads two rows.
create index if not exists sessions_settled_idx on sessions (status, updated_at) where archived_at is null;
