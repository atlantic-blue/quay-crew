drop index if exists flow_runs_step_idx;
alter table flow_runs drop column if exists step_work;
alter table flow_runs drop column if exists work;
