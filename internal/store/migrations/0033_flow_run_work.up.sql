-- A flow run is carried by a piece of work, decided in section 14 of docs/ORCHESTRATION.md.
--
-- There is one tree and it is the work tree. A run hangs inside it rather than beside it, because
-- depth and budget are counted once, from the credential, and a run outside the tree would be
-- counted by neither. `work` is the piece of work that carries the run; `step_work` is the piece of
-- work the run's current step went out as, and it is empty whenever no step is out.
alter table flow_runs add column if not exists work text references work (id);
alter table flow_runs add column if not exists step_work text references work (id);

-- The poller's own query: the runs whose step has ended. It reads the few runs that are working
-- rather than every run the crew has ever held.
create index if not exists flow_runs_step_idx on flow_runs (status, step_work);
