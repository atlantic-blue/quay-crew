-- When a waiting run should be looked at again.
--
-- A column rather than a timer in memory, which is the whole reason a wait survives the crew being
-- restarted underneath it: a process holding a timer forgets every wait it was holding, and a run
-- that was going to be resumed in ten minutes simply never is.
alter table flow_runs add column if not exists due_at timestamptz;

-- The poller asks one question, "which runs are due", on every tick. Without this it is a scan of
-- every run the crew has ever made, growing forever, several times a minute.
create index if not exists flow_runs_due_at_idx on flow_runs (due_at) where due_at is not null;
