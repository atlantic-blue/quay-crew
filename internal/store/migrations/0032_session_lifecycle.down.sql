drop index if exists sessions_settled_idx;
alter table workspace_limits drop column if exists archive_seconds;
alter table workspace_limits drop column if exists reclaim_seconds;
-- A session the crew reclaimed carries a status this schema no longer has a stamp for, so it is put
-- back to idle: its container is gone either way, and the next task builds a fresh one.
update sessions set status = 'idle' where status = 'reclaimed';
alter table sessions drop column if exists reclaimed_at;
