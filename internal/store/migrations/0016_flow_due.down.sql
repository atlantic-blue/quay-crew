drop index if exists flow_runs_due_at_idx;
alter table flow_runs drop column if exists due_at;
