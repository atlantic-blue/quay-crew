drop index if exists jobs_outcome_idx;
alter table jobs drop column if exists outcome;
