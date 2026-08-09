-- What a run has spent and why it stopped.
--
-- transitions counts movements against the cap its graph declares, so a cycling graph terminates on
-- its own. spent is what the run's conversation had cost when it was last checked. reason says why
-- a stopped run stopped: a run that went quiet and a run that was halted must never read the same.
alter table flow_runs add column if not exists transitions int    not null default 0;
alter table flow_runs add column if not exists spent       bigint not null default 0;
alter table flow_runs add column if not exists reason      text   not null default '';
