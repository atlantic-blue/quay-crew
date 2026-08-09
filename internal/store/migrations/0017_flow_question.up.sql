-- What an asking run is waiting to be told.
--
-- On the run rather than in a queue of its own: a run asks one question at a time, because it has
-- one current node, so a second table would hold at most one row per asking run and a join to find
-- it.
alter table flow_runs add column if not exists question text not null default '';
