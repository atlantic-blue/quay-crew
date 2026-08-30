-- Back to a job that can only be declared again after it fails.
--
-- Forward only in the control plane, which never applies a down file. This is here for an operator
-- who has to go back deliberately, and it drops the record of what every job finished with it.
drop index if exists job_steps_summary_idx;
drop index if exists job_steps_job_idx;
drop table if exists job_steps;
alter table jobs drop column if exists resuming;
